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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	api "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	conditionTypeConfigReady      = "ConfigReady"
	conditionTypePersistenceReady = "PersistenceReady"
	conditionTypeWorkloadReady    = "WorkloadReady"
	conditionTypeReady            = "Ready"
	phaseConfigError              = "ConfigError"
	phaseStoragePending           = "StoragePending"
	phaseStorageError             = "StorageError"
	phaseWorkloadPending          = "WorkloadPending"
	phaseWorkloadError            = "WorkloadError"
	phaseReady                    = "Ready"
	reasonWaitingForPersistence   = "WaitingForPersistence"
)

// HermesAgentReconciler reconciles a HermesAgent object.
type HermesAgentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=hermes.nous.ai,resources=hermesagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hermes.nous.ai,resources=hermesagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hermes.nous.ai,resources=hermesagents/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile moves the current HermesAgent config state toward the desired state.
func (r *HermesAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	agent := &hermesv1alpha1.HermesAgent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if _, err := buildConfigPlan(agent); err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markConfigFailure(status, "InvalidConfig", err.Error(), "HermesAgent configuration is invalid")
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("build config plan: %w (status update failed: %v)", err, statusErr)
		}
		return ctrl.Result{}, nil
	}

	referencedInputs, err := r.resolveReferencedInputs(ctx, agent)
	if err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markConfigFailure(status, "ReferencedInputsReadFailed", fmt.Sprintf("Could not read referenced ConfigMaps or Secrets: %v", err), "Referenced configuration inputs could not be read")
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("read referenced inputs: %w (status update failed: %v)", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	if missingMessages := missingReferenceMessages(referencedInputs); len(missingMessages) > 0 {
		message := joinMessages(missingMessages)
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markConfigFailure(status, "MissingReferencedInput", message, "Referenced configuration inputs are missing")
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("missing referenced inputs: %s (status update failed: %v)", message, statusErr)
		}
		return ctrl.Result{}, nil
	}

	plan, err := buildConfigPlanWithReferences(agent, referencedInputs)
	if err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markConfigFailure(status, "InvalidConfig", err.Error(), "HermesAgent configuration is invalid")
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
				markConfigFailure(status, "ConfigMapReconcileFailed", err.Error(), "Inline configuration resources could not be reconciled")
			}); statusErr != nil {
				return ctrl.Result{}, fmt.Errorf("reconcile configmap: %w (status update failed: %v)", err, statusErr)
			}
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcilePersistentVolumeClaim(ctx, agent); err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markPersistenceFailure(status, err)
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile pvc: %w (status update failed: %v)", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	inputs := buildPodTemplateInputs(agent, plan)
	if err := r.reconcileStatefulSet(ctx, agent, inputs); err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markWorkloadFailure(status, "StatefulSetReconcileFailed", err.Error(), "Hermes workload could not be reconciled")
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile statefulset: %w (status update failed: %v)", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileService(ctx, agent); err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markWorkloadFailure(status, "ServiceReconcileFailed", err.Error(), "Hermes Service could not be reconciled")
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile service: %w (status update failed: %v)", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileNetworkPolicy(ctx, agent); err != nil {
		if statusErr := r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
			markWorkloadFailure(status, "NetworkPolicyReconcileFailed", err.Error(), "Hermes NetworkPolicy could not be reconciled")
		}); statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile networkpolicy: %w (status update failed: %v)", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	log.Info("Reconciled HermesAgent config",
		"name", agent.Name,
		"generatedConfigMaps", countGeneratedFiles(plan.Files),
		"configHash", plan.Hash,
		"volumes", len(inputs.Volumes),
		"envFrom", len(inputs.EnvFrom),
	)

	if err := r.reconcileStatus(ctx, agent); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HermesAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hermesv1alpha1.HermesAgent{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&appsv1.StatefulSet{}).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.findAgentsForConfigMap)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.findAgentsForSecret)).
		Named("hermesagent").
		Complete(r)
}

func (r *HermesAgentReconciler) resolveReferencedInputs(ctx context.Context, agent *hermesv1alpha1.HermesAgent) (referencedInputState, error) {
	referencedInputs := referencedInputState{}

	appendConfigFileSnapshot := func(source hermesv1alpha1.HermesAgentConfigSource) error {
		if source.ConfigMapRef == nil || source.ConfigMapRef.Name == "" {
			return nil
		}

		configMap := &corev1.ConfigMap{}
		err := r.Get(ctx, client.ObjectKey{Name: source.ConfigMapRef.Name, Namespace: agent.Namespace}, configMap)
		if err != nil {
			if apierrors.IsNotFound(err) {
				referencedInputs.FileRefs = append(referencedInputs.FileRefs, newConfigMapFileSnapshot(source.ConfigMapRef.Name, source.ConfigMapRef.Key, nil))
				return nil
			}
			return err
		}

		referencedInputs.FileRefs = append(referencedInputs.FileRefs, newConfigMapFileSnapshot(source.ConfigMapRef.Name, source.ConfigMapRef.Key, configMap))
		return nil
	}

	if err := appendConfigFileSnapshot(agent.Spec.Config); err != nil {
		return referencedInputs, err
	}
	if err := appendConfigFileSnapshot(agent.Spec.GatewayConfig); err != nil {
		return referencedInputs, err
	}

	for _, envVar := range agent.Spec.Env {
		if envVar.ValueFrom == nil {
			continue
		}
		if envVar.ValueFrom.ConfigMapKeyRef != nil && envVar.ValueFrom.ConfigMapKeyRef.Name != "" {
			configMap := &corev1.ConfigMap{}
			err := r.Get(ctx, client.ObjectKey{Name: envVar.ValueFrom.ConfigMapKeyRef.Name, Namespace: agent.Namespace}, configMap)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					return referencedInputs, err
				}
				configMap = nil
			}
			referencedInputs.EnvValueRefs = append(referencedInputs.EnvValueRefs, newConfigMapKeySnapshot(envVar.ValueFrom.ConfigMapKeyRef.Name, envVar.ValueFrom.ConfigMapKeyRef.Key, optionalValue(envVar.ValueFrom.ConfigMapKeyRef.Optional), configMap))
		}
		if envVar.ValueFrom.SecretKeyRef != nil && envVar.ValueFrom.SecretKeyRef.Name != "" {
			secret := &corev1.Secret{}
			err := r.Get(ctx, client.ObjectKey{Name: envVar.ValueFrom.SecretKeyRef.Name, Namespace: agent.Namespace}, secret)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					return referencedInputs, err
				}
				secret = nil
			}
			referencedInputs.EnvValueRefs = append(referencedInputs.EnvValueRefs, newSecretKeySnapshot(envVar.ValueFrom.SecretKeyRef.Name, envVar.ValueFrom.SecretKeyRef.Key, optionalValue(envVar.ValueFrom.SecretKeyRef.Optional), secret))
		}
	}

	for _, source := range agent.Spec.EnvFrom {
		if source.ConfigMapRef != nil && source.ConfigMapRef.Name != "" {
			configMap := &corev1.ConfigMap{}
			err := r.Get(ctx, client.ObjectKey{Name: source.ConfigMapRef.Name, Namespace: agent.Namespace}, configMap)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					return referencedInputs, err
				}
				configMap = nil
			}
			referencedInputs.EnvFrom = append(referencedInputs.EnvFrom, newConfigMapEnvFromSnapshot(source.ConfigMapRef.Name, optionalValue(source.ConfigMapRef.Optional), configMap))
		}
		if source.SecretRef != nil && source.SecretRef.Name != "" {
			secret := &corev1.Secret{}
			err := r.Get(ctx, client.ObjectKey{Name: source.SecretRef.Name, Namespace: agent.Namespace}, secret)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					return referencedInputs, err
				}
				secret = nil
			}
			referencedInputs.EnvFrom = append(referencedInputs.EnvFrom, newSecretSnapshot(source.SecretRef.Name, optionalValue(source.SecretRef.Optional), secret))
		}
	}

	for _, secretRef := range agent.Spec.SecretRefs {
		if secretRef.Name == "" {
			continue
		}

		secret := &corev1.Secret{}
		err := r.Get(ctx, client.ObjectKey{Name: secretRef.Name, Namespace: agent.Namespace}, secret)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return referencedInputs, err
			}
			secret = nil
		}
		referencedInputs.SecretRefs = append(referencedInputs.SecretRefs, newSecretSnapshot(secretRef.Name, false, secret))
	}

	return referencedInputs, nil
}

func (r *HermesAgentReconciler) findAgentsForConfigMap(ctx context.Context, obj client.Object) []ctrl.Request {
	return r.findReferencingAgents(ctx, obj.GetNamespace(), func(agent *hermesv1alpha1.HermesAgent) bool {
		return referencesConfigMap(agent, obj.GetName())
	})
}

func (r *HermesAgentReconciler) findAgentsForSecret(ctx context.Context, obj client.Object) []ctrl.Request {
	return r.findReferencingAgents(ctx, obj.GetNamespace(), func(agent *hermesv1alpha1.HermesAgent) bool {
		return referencesSecret(agent, obj.GetName())
	})
}

func (r *HermesAgentReconciler) findReferencingAgents(ctx context.Context, namespace string, matches func(*hermesv1alpha1.HermesAgent) bool) []ctrl.Request {
	agentList := &hermesv1alpha1.HermesAgentList{}
	if err := r.List(ctx, agentList, client.InNamespace(namespace)); err != nil {
		return nil
	}

	requests := []ctrl.Request{}
	for i := range agentList.Items {
		agent := &agentList.Items[i]
		if !matches(agent) {
			continue
		}
		requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)})
	}
	return requests
}

func referencesConfigMap(agent *hermesv1alpha1.HermesAgent, name string) bool {
	if name == "" {
		return false
	}
	if configSourceReferencesConfigMap(agent.Spec.Config, name) || configSourceReferencesConfigMap(agent.Spec.GatewayConfig, name) {
		return true
	}
	for _, envVar := range agent.Spec.Env {
		if envVar.ValueFrom != nil && envVar.ValueFrom.ConfigMapKeyRef != nil && envVar.ValueFrom.ConfigMapKeyRef.Name == name {
			return true
		}
	}
	for _, source := range agent.Spec.EnvFrom {
		if source.ConfigMapRef != nil && source.ConfigMapRef.Name == name {
			return true
		}
	}
	return false
}

func referencesSecret(agent *hermesv1alpha1.HermesAgent, name string) bool {
	if name == "" {
		return false
	}
	for _, envVar := range agent.Spec.Env {
		if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil && envVar.ValueFrom.SecretKeyRef.Name == name {
			return true
		}
	}
	for _, source := range agent.Spec.EnvFrom {
		if source.SecretRef != nil && source.SecretRef.Name == name {
			return true
		}
	}
	for _, secretRef := range agent.Spec.SecretRefs {
		if secretRef.Name == name {
			return true
		}
	}
	return false
}

func configSourceReferencesConfigMap(source hermesv1alpha1.HermesAgentConfigSource, name string) bool {
	return source.ConfigMapRef != nil && source.ConfigMapRef.Name == name
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
		if persistentVolumeClaim.CreationTimestamp.IsZero() {
			persistentVolumeClaim.Spec = desired.Spec
		} else {
			persistentVolumeClaim.Spec.Resources.Requests = desired.Spec.Resources.Requests
		}
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

func (r *HermesAgentReconciler) reconcileService(ctx context.Context, agent *hermesv1alpha1.HermesAgent) error {
	service := &corev1.Service{}
	serviceKey := client.ObjectKey{Name: agent.Name, Namespace: agent.Namespace}
	serviceExists := true
	if err := r.Get(ctx, serviceKey, service); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		serviceExists = false
	}

	if !serviceEnabled(agent) {
		if !serviceExists || !metav1.IsControlledBy(service, agent) {
			return nil
		}
		return r.Delete(ctx, service)
	}

	if serviceExists && !metav1.IsControlledBy(service, agent) {
		return fmt.Errorf("service %s already exists and is not owned by HermesAgent %s", service.Name, agent.Name)
	}

	desired := buildService(agent)
	service.Namespace = desired.Namespace
	service.Name = desired.Name

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		existingPorts := append([]corev1.ServicePort{}, service.Spec.Ports...)
		service.Labels = mergeStringMaps(service.Labels, desired.Labels)
		service.Spec.Type = desired.Spec.Type
		service.Spec.Selector = desired.Spec.Selector
		service.Spec.Ports = desired.Spec.Ports
		if (desired.Spec.Type == corev1.ServiceTypeNodePort || desired.Spec.Type == corev1.ServiceTypeLoadBalancer) && len(existingPorts) == 1 && len(service.Spec.Ports) == 1 && existingPorts[0].NodePort != 0 {
			service.Spec.Ports[0].NodePort = existingPorts[0].NodePort
		}
		return controllerutil.SetControllerReference(agent, service, r.Scheme)
	})
	return err
}

func (r *HermesAgentReconciler) reconcileNetworkPolicy(ctx context.Context, agent *hermesv1alpha1.HermesAgent) error {
	networkPolicy := &networkingv1.NetworkPolicy{}
	networkPolicyKey := client.ObjectKey{Name: agent.Name, Namespace: agent.Namespace}
	networkPolicyExists := true
	if err := r.Get(ctx, networkPolicyKey, networkPolicy); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		networkPolicyExists = false
	}

	if !networkPolicyEnabled(agent) {
		if !networkPolicyExists || !metav1.IsControlledBy(networkPolicy, agent) {
			return nil
		}
		return r.Delete(ctx, networkPolicy)
	}

	if networkPolicyExists && !metav1.IsControlledBy(networkPolicy, agent) {
		return fmt.Errorf("networkpolicy %s already exists and is not owned by HermesAgent %s", networkPolicy.Name, agent.Name)
	}

	desired := buildNetworkPolicy(agent)
	networkPolicy.Namespace = desired.Namespace
	networkPolicy.Name = desired.Name

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, networkPolicy, func() error {
		networkPolicy.Labels = mergeStringMaps(networkPolicy.Labels, desired.Labels)
		networkPolicy.Spec = desired.Spec
		return controllerutil.SetControllerReference(agent, networkPolicy, r.Scheme)
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
	latest.Status.ObservedGeneration = latest.Generation
	mutate(&latest.Status)
	if apiequality.Semantic.DeepEqual(base.Status, latest.Status) {
		return nil
	}

	now := metav1.Now()
	latest.Status.LastReconcileTime = &now

	if err := r.Status().Patch(ctx, latest, client.MergeFrom(base)); err != nil {
		return err
	}

	r.emitStatusEvent(latest, &base.Status, &latest.Status)
	return nil
}

func (r *HermesAgentReconciler) emitStatusEvent(agent *hermesv1alpha1.HermesAgent, before, after *hermesv1alpha1.HermesAgentStatus) {
	if r.Recorder == nil {
		return
	}

	for _, event := range statusEventsForTransition(before, after) {
		r.Recorder.Event(agent, event.eventType, event.reason, event.message)
	}
}

func statusEventsForTransition(before, after *hermesv1alpha1.HermesAgentStatus) []statusEvent {
	events := []statusEvent{}
	persistenceChanged := false
	if event := eventForConditionTransition(before, after, conditionTypeConfigReady); event.ok && event.eventType == corev1.EventTypeWarning {
		events = append(events, event)
	}
	if event := eventForConditionTransition(before, after, conditionTypePersistenceReady); event.ok && event.reason != "Unknown" {
		events = append(events, event)
		persistenceChanged = true
	}
	if event := eventForConditionTransition(before, after, conditionTypeWorkloadReady); event.ok && event.reason != "Unknown" {
		if !persistenceChanged || event.reason != reasonWaitingForPersistence {
			events = append(events, event)
		}
	}
	return events
}

type statusEvent struct {
	eventType string
	reason    string
	message   string
	ok        bool
}

func eventForConditionTransition(before, after *hermesv1alpha1.HermesAgentStatus, conditionType string) statusEvent {
	beforeCondition := api.FindStatusCondition(before.Conditions, conditionType)
	afterCondition := api.FindStatusCondition(after.Conditions, conditionType)
	if afterCondition == nil {
		return statusEvent{}
	}
	if beforeCondition != nil && beforeCondition.Status == afterCondition.Status && beforeCondition.Reason == afterCondition.Reason && beforeCondition.Message == afterCondition.Message {
		return statusEvent{}
	}

	return statusEvent{
		eventType: eventTypeForCondition(afterCondition),
		reason:    afterCondition.Reason,
		message:   afterCondition.Message,
		ok:        afterCondition.Reason != "",
	}
}

func eventTypeForCondition(condition *metav1.Condition) string {
	switch condition.Reason {
	case "InvalidConfig", "MissingReferencedInput", "ReferencedInputsReadFailed", "ConfigMapReconcileFailed", "PersistentVolumeClaimReconcileFailed", "PersistentVolumeClaimLost", "StatefulSetReconcileFailed", "ServiceReconcileFailed", "NetworkPolicyReconcileFailed":
		return corev1.EventTypeWarning
	default:
		return corev1.EventTypeNormal
	}
}

func missingReferenceMessages(referencedInputs referencedInputState) []string {
	messages := []string{}
	appendMessages := func(snapshot referencedObjectSnapshot) {
		if snapshot.Optional && !snapshot.Present {
			return
		}
		if snapshot.Present && snapshot.KeyFound {
			return
		}
		if !snapshot.Present {
			messages = append(messages, fmt.Sprintf("%s %s was not found", snapshot.Kind, snapshot.Name))
			return
		}
		if snapshot.Optional && snapshot.Key != "" && !snapshot.KeyFound {
			return
		}
		if snapshot.Key != "" && !snapshot.KeyFound {
			messages = append(messages, fmt.Sprintf("%s %s is missing key %s", snapshot.Kind, snapshot.Name, snapshot.Key))
		}
	}

	for _, snapshot := range referencedInputs.FileRefs {
		appendMessages(snapshot)
	}
	for _, snapshot := range referencedInputs.EnvValueRefs {
		appendMessages(snapshot)
	}
	for _, snapshot := range referencedInputs.EnvFrom {
		appendMessages(snapshot)
	}
	for _, snapshot := range referencedInputs.SecretRefs {
		appendMessages(snapshot)
	}
	return messages
}

func joinMessages(messages []string) string {
	return strings.Join(messages, "; ")
}

func (r *HermesAgentReconciler) reconcileStatus(ctx context.Context, agent *hermesv1alpha1.HermesAgent) error {
	statusView, err := r.readStatusView(ctx, agent)
	if err != nil {
		return err
	}

	return r.patchStatus(ctx, agent, func(status *hermesv1alpha1.HermesAgentStatus) {
		status.ReadyReplicas = statusView.readyReplicas
		status.PersistenceBound = statusView.persistenceBound
		setCondition(status, conditionTypeConfigReady, metav1.ConditionTrue, "ConfigReconciled", "Configuration inputs resolved successfully")
		setCondition(status, conditionTypePersistenceReady, statusView.persistenceConditionStatus, statusView.persistenceReason, statusView.persistenceMessage)
		setCondition(status, conditionTypeWorkloadReady, statusView.workloadConditionStatus, statusView.workloadReason, statusView.workloadMessage)
		setCondition(status, conditionTypeReady, statusView.readyConditionStatus, statusView.readyReason, statusView.readyMessage)
		status.Phase = statusView.phase
	})
}

type statusView struct {
	readyReplicas              int32
	persistenceBound           bool
	persistenceConditionStatus metav1.ConditionStatus
	persistenceReason          string
	persistenceMessage         string
	workloadConditionStatus    metav1.ConditionStatus
	workloadReason             string
	workloadMessage            string
	readyConditionStatus       metav1.ConditionStatus
	readyReason                string
	readyMessage               string
	phase                      string
}

func (r *HermesAgentReconciler) readStatusView(ctx context.Context, agent *hermesv1alpha1.HermesAgent) (statusView, error) {
	view := statusView{}

	if persistenceEnabled(agent) {
		persistentVolumeClaim := &corev1.PersistentVolumeClaim{}
		pvcKey := client.ObjectKey{Name: persistentVolumeClaimName(agent.Name), Namespace: agent.Namespace}
		if err := r.Get(ctx, pvcKey, persistentVolumeClaim); err != nil {
			if apierrors.IsNotFound(err) {
				view.persistenceConditionStatus = metav1.ConditionFalse
				view.persistenceReason = "PersistentVolumeClaimMissing"
				view.persistenceMessage = fmt.Sprintf("PersistentVolumeClaim %s has not been created yet; inspect reconcile errors and storage settings", pvcKey.Name)
				view.workloadConditionStatus = metav1.ConditionFalse
				view.workloadReason = reasonWaitingForPersistence
				view.workloadMessage = "Workload is waiting for the Hermes PVC to be created and bound"
				view.readyConditionStatus = metav1.ConditionFalse
				view.readyReason = view.persistenceReason
				view.readyMessage = view.persistenceMessage
				view.phase = phaseStoragePending
				return view, nil
			}
			return statusView{}, err
		}

		if persistentVolumeClaim.Status.Phase == corev1.ClaimBound {
			view.persistenceBound = true
			view.persistenceConditionStatus = metav1.ConditionTrue
			view.persistenceReason = "PersistentVolumeClaimBound"
			view.persistenceMessage = fmt.Sprintf("PersistentVolumeClaim %s is bound", persistentVolumeClaim.Name)
		} else {
			view.persistenceConditionStatus = metav1.ConditionFalse
			view.persistenceReason = persistenceReason(persistentVolumeClaim.Status.Phase)
			view.persistenceMessage = persistenceMessage(persistentVolumeClaim)
			view.workloadConditionStatus = metav1.ConditionFalse
			view.workloadReason = reasonWaitingForPersistence
			view.workloadMessage = "Workload is waiting for the Hermes PVC to bind"
			view.readyConditionStatus = metav1.ConditionFalse
			view.readyReason = view.persistenceReason
			view.readyMessage = view.persistenceMessage
			view.phase = persistencePhase(persistentVolumeClaim.Status.Phase)
			return view, nil
		}
	} else {
		view.persistenceConditionStatus = metav1.ConditionTrue
		view.persistenceReason = "PersistenceDisabled"
		view.persistenceMessage = "Persistence is disabled; Hermes is using ephemeral storage"
	}

	statefulSet := &appsv1.StatefulSet{}
	statefulSetKey := client.ObjectKey{Name: agent.Name, Namespace: agent.Namespace}
	if err := r.Get(ctx, statefulSetKey, statefulSet); err != nil {
		if apierrors.IsNotFound(err) {
			view.workloadConditionStatus = metav1.ConditionFalse
			view.workloadReason = "StatefulSetMissing"
			view.workloadMessage = fmt.Sprintf("StatefulSet %s has not been created yet; inspect earlier reconcile errors", statefulSetKey.Name)
			view.readyConditionStatus = metav1.ConditionFalse
			view.readyReason = view.workloadReason
			view.readyMessage = view.workloadMessage
			view.phase = phaseWorkloadPending
			return view, nil
		}
		return statusView{}, err
	}

	view.readyReplicas = statefulSet.Status.ReadyReplicas
	desiredReplicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		desiredReplicas = *statefulSet.Spec.Replicas
	}

	if statefulSet.Status.ReadyReplicas >= desiredReplicas && statefulSet.Status.ObservedGeneration >= statefulSet.Generation {
		view.workloadConditionStatus = metav1.ConditionTrue
		view.workloadReason = "StatefulSetReady"
		view.workloadMessage = fmt.Sprintf("StatefulSet %s has %d/%d ready replicas", statefulSet.Name, statefulSet.Status.ReadyReplicas, desiredReplicas)
		view.readyConditionStatus = metav1.ConditionTrue
		view.readyReason = conditionTypeReady
		view.readyMessage = "Hermes persistence and workload are ready"
		view.phase = phaseReady
		return view, nil
	}

	view.workloadConditionStatus = metav1.ConditionFalse
	view.workloadReason = "StatefulSetProgressing"
	view.workloadMessage = fmt.Sprintf("StatefulSet %s has %d/%d ready replicas; inspect pod events, image pulls, and probe failures if rollout stalls", statefulSet.Name, statefulSet.Status.ReadyReplicas, desiredReplicas)
	view.readyConditionStatus = metav1.ConditionFalse
	view.readyReason = view.workloadReason
	view.readyMessage = view.workloadMessage
	view.phase = phaseWorkloadPending
	return view, nil
}

func setCondition(status *hermesv1alpha1.HermesAgentStatus, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	api.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		ObservedGeneration: status.ObservedGeneration,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

func markConfigFailure(status *hermesv1alpha1.HermesAgentStatus, reason, configMessage, readyMessage string) {
	status.ReadyReplicas = 0
	status.PersistenceBound = false
	setCondition(status, conditionTypeConfigReady, metav1.ConditionFalse, reason, configMessage)
	setCondition(status, conditionTypePersistenceReady, metav1.ConditionUnknown, "Unknown", "Persistence state is unknown until configuration inputs resolve successfully")
	setCondition(status, conditionTypeWorkloadReady, metav1.ConditionUnknown, "Unknown", "Workload state is unknown until configuration inputs resolve successfully")
	setCondition(status, conditionTypeReady, metav1.ConditionFalse, reason, readyMessage)
	status.Phase = phaseConfigError
}

func markPersistenceFailure(status *hermesv1alpha1.HermesAgentStatus, err error) {
	status.ReadyReplicas = 0
	status.PersistenceBound = false
	setCondition(status, conditionTypeConfigReady, metav1.ConditionTrue, "ConfigReconciled", "Configuration inputs resolved successfully")
	setCondition(status, conditionTypePersistenceReady, metav1.ConditionFalse, "PersistentVolumeClaimReconcileFailed", err.Error())
	setCondition(status, conditionTypeWorkloadReady, metav1.ConditionFalse, reasonWaitingForPersistence, "Workload is waiting for the Hermes PVC to reconcile")
	setCondition(status, conditionTypeReady, metav1.ConditionFalse, "PersistentVolumeClaimReconcileFailed", "Hermes persistence could not be reconciled")
	status.Phase = phaseStorageError
}

func markWorkloadFailure(status *hermesv1alpha1.HermesAgentStatus, reason, workloadMessage, readyMessage string) {
	status.ReadyReplicas = 0
	setCondition(status, conditionTypeConfigReady, metav1.ConditionTrue, "ConfigReconciled", "Configuration inputs resolved successfully")
	setCondition(status, conditionTypeWorkloadReady, metav1.ConditionFalse, reason, workloadMessage)
	setCondition(status, conditionTypeReady, metav1.ConditionFalse, reason, readyMessage)
	status.Phase = phaseWorkloadError
}

func persistenceReason(phase corev1.PersistentVolumeClaimPhase) string {
	switch phase {
	case corev1.ClaimBound:
		return "PersistentVolumeClaimBound"
	case corev1.ClaimLost:
		return "PersistentVolumeClaimLost"
	default:
		return "PersistentVolumeClaimPending"
	}
}

func persistenceMessage(persistentVolumeClaim *corev1.PersistentVolumeClaim) string {
	switch persistentVolumeClaim.Status.Phase {
	case corev1.ClaimBound:
		return fmt.Sprintf("PersistentVolumeClaim %s is bound", persistentVolumeClaim.Name)
	case corev1.ClaimLost:
		return fmt.Sprintf("PersistentVolumeClaim %s is lost; inspect the storage backend and recreate the claim if needed", persistentVolumeClaim.Name)
	default:
		return fmt.Sprintf("PersistentVolumeClaim %s is waiting to bind; check storage class, capacity, and access modes", persistentVolumeClaim.Name)
	}
}

func persistencePhase(phase corev1.PersistentVolumeClaimPhase) string {
	if phase == corev1.ClaimLost {
		return phaseStorageError
	}
	return phaseStoragePending
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
