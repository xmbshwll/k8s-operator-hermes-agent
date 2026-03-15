/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	api "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	conditionTypeConfigReady = "ConfigReady"
)

// HermesAgentReconciler reconciles a HermesAgent object.
type HermesAgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=hermes.nous.ai,resources=hermesagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hermes.nous.ai,resources=hermesagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hermes.nous.ai,resources=hermesagents/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile moves the current HermesAgent config state toward the desired state.
func (r *HermesAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	agent := &hermesv1alpha1.HermesAgent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	plan, err := buildConfigPlan(agent)
	if err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			setStatusCondition(status, metav1.ConditionFalse, "InvalidConfig", err.Error())
			status.Phase = "ConfigError"
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("build config plan: %w (status update failed: %v)", err, statusErr)
		}
		return ctrl.Result{}, nil
	}

	for _, file := range plan.Files {
		if !file.Generated {
			continue
		}
		if err := r.reconcileInlineConfigMap(ctx, agent, file); err != nil {
			if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
				setStatusCondition(status, metav1.ConditionFalse, "ConfigMapReconcileFailed", err.Error())
				status.Phase = "ConfigError"
			}); statusErr != nil {
				return ctrl.Result{}, fmt.Errorf("reconcile configmap: %w (status update failed: %v)", err, statusErr)
			}
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcilePersistentVolumeClaim(ctx, agent); err != nil {
		return ctrl.Result{}, err
	}

	inputs := buildPodTemplateInputs(agent, plan)
	if err := r.reconcileStatefulSet(ctx, agent, inputs); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled HermesAgent config",
		"name", agent.Name,
		"generatedConfigMaps", countGeneratedFiles(plan.Files),
		"configHash", plan.Hash,
		"volumes", len(inputs.Volumes),
		"envFrom", len(inputs.EnvFrom),
	)

	if err := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
		setStatusCondition(status, metav1.ConditionTrue, "ConfigReconciled", "Config inputs resolved successfully")
		status.Phase = "ConfigReady"
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HermesAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hermesv1alpha1.HermesAgent{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&appsv1.StatefulSet{}).
		Named("hermesagent").
		Complete(r)
}

func (r *HermesAgentReconciler) reconcileInlineConfigMap(ctx context.Context, agent *hermesv1alpha1.HermesAgent, file resolvedConfigFile) error {
	configMap := &corev1.ConfigMap{}
	configMap.Namespace = agent.Namespace
	configMap.Name = file.ConfigMapName

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Labels = mergeStringMaps(configMap.Labels, resourceLabels(agent))
		configMap.Data = map[string]string{file.ConfigMapKey: file.Content}
		return controllerutil.SetControllerReference(agent, configMap, r.Scheme)
	})
	return err
}

func (r *HermesAgentReconciler) reconcilePersistentVolumeClaim(ctx context.Context, agent *hermesv1alpha1.HermesAgent) error {
	if !persistenceEnabled(agent) {
		return nil
	}

	desired, err := buildPersistentVolumeClaim(agent)
	if err != nil {
		return err
	}

	persistentVolumeClaim := &corev1.PersistentVolumeClaim{}
	persistentVolumeClaim.Namespace = agent.Namespace
	persistentVolumeClaim.Name = desired.Name

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, persistentVolumeClaim, func() error {
		persistentVolumeClaim.Labels = mergeStringMaps(persistentVolumeClaim.Labels, desired.Labels)
		persistentVolumeClaim.Spec = desired.Spec
		return controllerutil.SetControllerReference(agent, persistentVolumeClaim, r.Scheme)
	})
	return err
}

func (r *HermesAgentReconciler) reconcileStatefulSet(ctx context.Context, agent *hermesv1alpha1.HermesAgent, inputs podTemplateInputs) error {
	statefulSet := &appsv1.StatefulSet{}
	statefulSet.Namespace = agent.Namespace
	statefulSet.Name = agent.Name

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, statefulSet, func() error {
		desired := buildStatefulSet(agent, inputs)
		statefulSet.Labels = mergeStringMaps(statefulSet.Labels, desired.Labels)
		statefulSet.Spec = desired.Spec
		return controllerutil.SetControllerReference(agent, statefulSet, r.Scheme)
	})
	return err
}

func (r *HermesAgentReconciler) patchStatus(ctx context.Context, agent *hermesv1alpha1.HermesAgent, mutate func(*hermesv1alpha1.HermesAgentStatus)) error {
	latest := &hermesv1alpha1.HermesAgent{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(agent), latest); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	base := latest.DeepCopy()
	mutate(&latest.Status)
	latest.Status.ObservedGeneration = latest.Generation
	now := metav1.Now()
	latest.Status.LastReconcileTime = &now

	return r.Status().Patch(ctx, latest, client.MergeFrom(base))
}

func setStatusCondition(status *hermesv1alpha1.HermesAgentStatus, conditionStatus metav1.ConditionStatus, reason, message string) {
	api.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               conditionTypeConfigReady,
		Status:             conditionStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func countGeneratedFiles(files []resolvedConfigFile) int {
	count := 0
	for _, file := range files {
		if file.Generated {
			count++
		}
	}
	return count
}
