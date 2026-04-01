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

package v1alpha1

import (
	"context"
	"fmt"
	"net/netip"
	pathpkg "path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	defaultMode                  = "gateway"
	defaultPersistenceSize       = "10Gi"
	defaultServicePort     int32 = 8080
	defaultProbePeriod     int32 = 10
	defaultProbeTimeout    int32 = 5
	defaultProbeFailures   int32 = 3
	startupInitialDelay    int32 = 0
	startupFailures        int32 = 18
	readinessInitialDelay  int32 = 5
	livenessInitialDelay   int32 = 15
)

var defaultPersistenceEnabled = true
var defaultNetworkPolicyEnabled = false
var defaultProbeEnabled = true
var defaultAutomountServiceAccountToken = false

// nolint:unused
// log is for logging in this package.
var hermesagentlog = logf.Log.WithName("hermesagent-resource")

// SetupHermesAgentWebhookWithManager registers the webhook for HermesAgent in the manager.
func SetupHermesAgentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &hermesv1alpha1.HermesAgent{}).
		WithValidator(&HermesAgentCustomValidator{}).
		WithDefaulter(&HermesAgentCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-hermes-nous-ai-v1alpha1-hermesagent,mutating=true,failurePolicy=fail,sideEffects=None,groups=hermes.nous.ai,resources=hermesagents,verbs=create;update,versions=v1alpha1,name=mhermesagent-v1alpha1.kb.io,admissionReviewVersions=v1

type HermesAgentCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind HermesAgent.
func (d *HermesAgentCustomDefaulter) Default(_ context.Context, obj *hermesv1alpha1.HermesAgent) error {
	hermesagentlog.Info("Defaulting for HermesAgent", "name", obj.GetName())

	if obj.Spec.Mode == "" {
		obj.Spec.Mode = defaultMode
	}
	if obj.Spec.Image.Repository == "" {
		obj.Spec.Image.Repository = hermesv1alpha1.DefaultHermesAgentImageRepository
	}
	if obj.Spec.Image.Tag == "" {
		obj.Spec.Image.Tag = hermesv1alpha1.DefaultHermesAgentImageTag
	}
	if obj.Spec.Image.PullPolicy == "" {
		obj.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	}

	if obj.Spec.Storage.Persistence.Enabled == nil {
		obj.Spec.Storage.Persistence.Enabled = &defaultPersistenceEnabled
	}
	if obj.Spec.Replicas == 0 {
		obj.Spec.Replicas = 1
	}
	if obj.Spec.AutomountServiceAccountToken == nil {
		obj.Spec.AutomountServiceAccountToken = &defaultAutomountServiceAccountToken
	}
	if obj.Spec.Storage.Persistence.Size == "" {
		obj.Spec.Storage.Persistence.Size = defaultPersistenceSize
	}
	if len(obj.Spec.Storage.Persistence.AccessModes) == 0 {
		obj.Spec.Storage.Persistence.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}

	if obj.Spec.Service.Type == "" {
		obj.Spec.Service.Type = corev1.ServiceTypeClusterIP
	}
	if obj.Spec.Service.Port == 0 {
		obj.Spec.Service.Port = defaultServicePort
	}

	if obj.Spec.NetworkPolicy.Enabled == nil {
		obj.Spec.NetworkPolicy.Enabled = &defaultNetworkPolicyEnabled
	}
	if obj.Spec.UpdateStrategy.Type == "" {
		obj.Spec.UpdateStrategy.Type = appsv1.RollingUpdateStatefulSetStrategyType
	}

	defaultProbe(&obj.Spec.Probes.Startup, probeDefaults{
		enabled:             true,
		initialDelaySeconds: startupInitialDelay,
		periodSeconds:       defaultProbePeriod,
		timeoutSeconds:      defaultProbeTimeout,
		failureThreshold:    startupFailures,
	})
	defaultProbe(&obj.Spec.Probes.Readiness, probeDefaults{
		enabled:             true,
		initialDelaySeconds: readinessInitialDelay,
		periodSeconds:       defaultProbePeriod,
		timeoutSeconds:      defaultProbeTimeout,
		failureThreshold:    defaultProbeFailures,
	})
	defaultProbe(&obj.Spec.Probes.Liveness, probeDefaults{
		enabled:             true,
		initialDelaySeconds: livenessInitialDelay,
		periodSeconds:       defaultProbePeriod,
		timeoutSeconds:      defaultProbeTimeout,
		failureThreshold:    defaultProbeFailures,
	})

	return nil
}

// +kubebuilder:webhook:path=/validate-hermes-nous-ai-v1alpha1-hermesagent,mutating=false,failurePolicy=fail,sideEffects=None,groups=hermes.nous.ai,resources=hermesagents,verbs=create;update,versions=v1alpha1,name=vhermesagent-v1alpha1.kb.io,admissionReviewVersions=v1

type HermesAgentCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type HermesAgent.
func (v *HermesAgentCustomValidator) ValidateCreate(_ context.Context, obj *hermesv1alpha1.HermesAgent) (admission.Warnings, error) {
	hermesagentlog.Info("Validation for HermesAgent upon creation", "name", obj.GetName())
	return nil, validateHermesAgent(obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type HermesAgent.
func (v *HermesAgentCustomValidator) ValidateUpdate(_ context.Context, _ *hermesv1alpha1.HermesAgent, newObj *hermesv1alpha1.HermesAgent) (admission.Warnings, error) {
	hermesagentlog.Info("Validation for HermesAgent upon update", "name", newObj.GetName())
	return nil, validateHermesAgent(newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type HermesAgent.
func (v *HermesAgentCustomValidator) ValidateDelete(_ context.Context, obj *hermesv1alpha1.HermesAgent) (admission.Warnings, error) {
	hermesagentlog.Info("Validation for HermesAgent upon deletion", "name", obj.GetName())
	return nil, nil
}

type probeDefaults struct {
	enabled             bool
	initialDelaySeconds int32
	periodSeconds       int32
	timeoutSeconds      int32
	failureThreshold    int32
}

func defaultProbe(probe *hermesv1alpha1.HermesAgentProbeSpec, defaults probeDefaults) {
	if probe.Enabled == nil {
		probe.Enabled = &defaultProbeEnabled
	}
	if probe.InitialDelaySeconds == 0 && defaults.initialDelaySeconds > 0 {
		probe.InitialDelaySeconds = defaults.initialDelaySeconds
	}
	if probe.PeriodSeconds == 0 {
		probe.PeriodSeconds = defaults.periodSeconds
	}
	if probe.TimeoutSeconds == 0 {
		probe.TimeoutSeconds = defaults.timeoutSeconds
	}
	if probe.FailureThreshold == 0 {
		probe.FailureThreshold = defaults.failureThreshold
	}
}

func validateHermesAgent(obj *hermesv1alpha1.HermesAgent) error {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")

	allErrs = append(allErrs, validateConfigSource(specPath.Child("config"), obj.Spec.Config)...)
	allErrs = append(allErrs, validateConfigSource(specPath.Child("gatewayConfig"), obj.Spec.GatewayConfig)...)
	allErrs = append(allErrs, validateEnv(specPath.Child("env"), obj.Spec.Env)...)
	allErrs = append(allErrs, validateEnvFrom(specPath.Child("envFrom"), obj.Spec.EnvFrom)...)
	allErrs = append(allErrs, validateLocalObjectReferences(specPath.Child("secretRefs"), obj.Spec.SecretRefs)...)
	allErrs = append(allErrs, validateFileMounts(specPath.Child("fileMounts"), obj.Spec.FileMounts)...)
	allErrs = append(allErrs, validateLocalObjectReferences(specPath.Child("imagePullSecrets"), obj.Spec.ImagePullSecrets)...)
	allErrs = append(allErrs, validateReplicas(specPath, obj.Spec)...)
	allErrs = append(allErrs, validateUpdateStrategy(specPath.Child("updateStrategy"), obj.Spec.UpdateStrategy)...)
	allErrs = append(allErrs, validateService(specPath.Child("service"), obj.Spec.Service)...)
	allErrs = append(allErrs, validateNetworkPolicy(specPath.Child("networkPolicy"), obj.Spec.NetworkPolicy)...)
	allErrs = append(allErrs, validateStorage(specPath.Child("storage", "persistence"), obj.Spec.Storage.Persistence)...)
	allErrs = append(allErrs, validateProbeSpec(specPath.Child("probes", "startup"), obj.Spec.Probes.Startup)...)
	allErrs = append(allErrs, validateProbeSpec(specPath.Child("probes", "readiness"), obj.Spec.Probes.Readiness)...)
	allErrs = append(allErrs, validateProbeSpec(specPath.Child("probes", "liveness"), obj.Spec.Probes.Liveness)...)

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(schema.GroupKind{Group: "hermes.nous.ai", Kind: "HermesAgent"}, obj.Name, allErrs)
}

func validateConfigSource(path *field.Path, source hermesv1alpha1.HermesAgentConfigSource) field.ErrorList {
	allErrs := field.ErrorList{}
	hasRaw := source.Raw != ""
	hasConfigMapRef := source.ConfigMapRef != nil
	hasSecretRef := source.SecretRef != nil
	sourceCount := 0
	for _, present := range []bool{hasRaw, hasConfigMapRef, hasSecretRef} {
		if present {
			sourceCount++
		}
	}
	if sourceCount > 1 {
		allErrs = append(allErrs, field.Invalid(path, source, "raw, configMapRef, and secretRef are mutually exclusive"))
	}
	if source.ConfigMapRef != nil {
		if source.ConfigMapRef.Name == "" {
			allErrs = append(allErrs, field.Required(path.Child("configMapRef", "name"), "name is required when configMapRef is set"))
		}
		if source.ConfigMapRef.Key == "" {
			allErrs = append(allErrs, field.Required(path.Child("configMapRef", "key"), "key is required when configMapRef is set"))
		}
	}
	if source.SecretRef != nil {
		if source.SecretRef.Name == "" {
			allErrs = append(allErrs, field.Required(path.Child("secretRef", "name"), "name is required when secretRef is set"))
		}
		if source.SecretRef.Key == "" {
			allErrs = append(allErrs, field.Required(path.Child("secretRef", "key"), "key is required when secretRef is set"))
		}
	}
	return allErrs
}

func validateEnv(path *field.Path, envVars []corev1.EnvVar) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, envVar := range envVars {
		envPath := path.Index(i)
		if envVar.Name == "" {
			allErrs = append(allErrs, field.Required(envPath.Child("name"), "name is required"))
		}
		if envVar.ValueFrom == nil {
			continue
		}
		if envVar.ValueFrom.ConfigMapKeyRef != nil {
			if envVar.ValueFrom.ConfigMapKeyRef.Name == "" {
				allErrs = append(allErrs, field.Required(envPath.Child("valueFrom", "configMapKeyRef", "name"), "name is required when configMapKeyRef is set"))
			}
			if envVar.ValueFrom.ConfigMapKeyRef.Key == "" {
				allErrs = append(allErrs, field.Required(envPath.Child("valueFrom", "configMapKeyRef", "key"), "key is required when configMapKeyRef is set"))
			}
		}
		if envVar.ValueFrom.SecretKeyRef != nil {
			if envVar.ValueFrom.SecretKeyRef.Name == "" {
				allErrs = append(allErrs, field.Required(envPath.Child("valueFrom", "secretKeyRef", "name"), "name is required when secretKeyRef is set"))
			}
			if envVar.ValueFrom.SecretKeyRef.Key == "" {
				allErrs = append(allErrs, field.Required(envPath.Child("valueFrom", "secretKeyRef", "key"), "key is required when secretKeyRef is set"))
			}
		}
	}
	return allErrs
}

func validateEnvFrom(path *field.Path, sources []corev1.EnvFromSource) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, source := range sources {
		sourcePath := path.Index(i)
		hasConfigMap := source.ConfigMapRef != nil
		hasSecret := source.SecretRef != nil
		switch {
		case hasConfigMap && hasSecret:
			allErrs = append(allErrs, field.Invalid(sourcePath, source, "configMapRef and secretRef are mutually exclusive"))
		case !hasConfigMap && !hasSecret:
			allErrs = append(allErrs, field.Invalid(sourcePath, source, "either configMapRef or secretRef must be set"))
		}
		if source.ConfigMapRef != nil && source.ConfigMapRef.Name == "" {
			allErrs = append(allErrs, field.Required(sourcePath.Child("configMapRef", "name"), "name is required when configMapRef is set"))
		}
		if source.SecretRef != nil && source.SecretRef.Name == "" {
			allErrs = append(allErrs, field.Required(sourcePath.Child("secretRef", "name"), "name is required when secretRef is set"))
		}
	}
	return allErrs
}

func validateLocalObjectReferences(path *field.Path, refs []corev1.LocalObjectReference) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, ref := range refs {
		if ref.Name == "" {
			allErrs = append(allErrs, field.Required(path.Index(i).Child("name"), "name is required"))
		}
	}
	return allErrs
}

func validateFileMounts(path *field.Path, mounts []hermesv1alpha1.HermesAgentFileMountSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	seenMountPaths := map[string]int{}
	for i, mount := range mounts {
		mountPath := path.Index(i)
		hasConfigMap := mount.ConfigMapRef != nil
		hasSecret := mount.SecretRef != nil
		switch {
		case hasConfigMap && hasSecret:
			allErrs = append(allErrs, field.Invalid(mountPath, mount, "configMapRef and secretRef are mutually exclusive"))
		case !hasConfigMap && !hasSecret:
			allErrs = append(allErrs, field.Invalid(mountPath, mount, "either configMapRef or secretRef must be set"))
		}
		if mount.MountPath == "" {
			allErrs = append(allErrs, field.Required(mountPath.Child("mountPath"), "mountPath is required"))
		} else {
			if mount.MountPath[0] != '/' {
				allErrs = append(allErrs, field.Invalid(mountPath.Child("mountPath"), mount.MountPath, "mountPath must be absolute"))
			}
			if previous, exists := seenMountPaths[mount.MountPath]; exists {
				allErrs = append(allErrs, field.Invalid(mountPath.Child("mountPath"), mount.MountPath, fmt.Sprintf("mountPath duplicates fileMounts[%d].mountPath", previous)))
			} else {
				seenMountPaths[mount.MountPath] = i
			}
		}
		if mount.ConfigMapRef != nil && mount.ConfigMapRef.Name == "" {
			allErrs = append(allErrs, field.Required(mountPath.Child("configMapRef", "name"), "name is required when configMapRef is set"))
		}
		if mount.SecretRef != nil && mount.SecretRef.Name == "" {
			allErrs = append(allErrs, field.Required(mountPath.Child("secretRef", "name"), "name is required when secretRef is set"))
		}
		allErrs = append(allErrs, validateMode(mountPath.Child("defaultMode"), mount.DefaultMode)...)
		seenItemPaths := map[string]int{}
		seenItemKeys := map[string]int{}
		for j, item := range mount.Items {
			itemPath := mountPath.Child("items").Index(j)
			if item.Key == "" {
				allErrs = append(allErrs, field.Required(itemPath.Child("key"), "key is required"))
			} else if previous, exists := seenItemKeys[item.Key]; exists {
				allErrs = append(allErrs, field.Invalid(itemPath.Child("key"), item.Key, fmt.Sprintf("key duplicates fileMounts[%d].items[%d].key", i, previous)))
			} else {
				seenItemKeys[item.Key] = j
			}
			if item.Path == "" {
				allErrs = append(allErrs, field.Required(itemPath.Child("path"), "path is required"))
			} else {
				allErrs = append(allErrs, validateFileMountItemPath(itemPath.Child("path"), item.Path)...)
				if previous, exists := seenItemPaths[item.Path]; exists {
					allErrs = append(allErrs, field.Invalid(itemPath.Child("path"), item.Path, fmt.Sprintf("path duplicates fileMounts[%d].items[%d].path", i, previous)))
				} else {
					seenItemPaths[item.Path] = j
				}
			}
			allErrs = append(allErrs, validateMode(itemPath.Child("mode"), item.Mode)...)
		}
	}
	return allErrs
}

func validateFileMountItemPath(path *field.Path, value string) field.ErrorList {
	allErrs := field.ErrorList{}
	if value == "" {
		return allErrs
	}
	if value[0] == '/' {
		allErrs = append(allErrs, field.Invalid(path, value, "path must be relative"))
		return allErrs
	}
	clean := pathpkg.Clean(value)
	if clean == "." || clean != value || clean == ".." || strings.HasPrefix(clean, "../") {
		allErrs = append(allErrs, field.Invalid(path, value, "path must not contain '.' or '..' path segments"))
	}
	return allErrs
}

func validateMode(path *field.Path, mode *int32) field.ErrorList {
	allErrs := field.ErrorList{}
	if mode == nil {
		return allErrs
	}
	if *mode < 0 || *mode > 0o777 {
		allErrs = append(allErrs, field.Invalid(path, *mode, "must be between 0 and 0777"))
	}
	return allErrs
}

func validateReplicas(path *field.Path, spec hermesv1alpha1.HermesAgentSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.Replicas <= 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("replicas"), spec.Replicas, "replicas must be greater than zero"))
	}
	persistenceEnabled := spec.Storage.Persistence.Enabled == nil || *spec.Storage.Persistence.Enabled
	if spec.Replicas > 1 && persistenceEnabled {
		allErrs = append(allErrs, field.Invalid(path.Child("replicas"), spec.Replicas, "replicas greater than 1 require spec.storage.persistence.enabled=false because the operator does not manage shared Hermes state"))
	}
	return allErrs
}

func validateUpdateStrategy(path *field.Path, strategy hermesv1alpha1.HermesAgentUpdateStrategySpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if strategy.Type == appsv1.OnDeleteStatefulSetStrategyType && strategy.RollingUpdate != nil {
		allErrs = append(allErrs, field.Invalid(path.Child("rollingUpdate"), strategy.RollingUpdate, "rollingUpdate is only valid when type is RollingUpdate"))
	}
	if strategy.RollingUpdate != nil && strategy.RollingUpdate.Partition != nil && *strategy.RollingUpdate.Partition < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("rollingUpdate", "partition"), *strategy.RollingUpdate.Partition, "partition must be zero or greater"))
	}
	return allErrs
}

func validateService(path *field.Path, service hermesv1alpha1.HermesAgentServiceSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if service.Enabled && service.Port <= 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("port"), service.Port, "port must be greater than zero when service is enabled"))
	}
	if service.TargetPort < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("targetPort"), service.TargetPort, "targetPort must be zero or greater"))
	}
	if service.Enabled && service.TargetPort != 0 && service.TargetPort <= 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("targetPort"), service.TargetPort, "targetPort must be greater than zero when service is enabled"))
	}
	return allErrs
}

func validateNetworkPolicy(path *field.Path, networkPolicy hermesv1alpha1.HermesAgentNetworkPolicySpec) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateNetworkPolicyDestinations(path.Child("destinations"), networkPolicy.Destinations)...)
	allErrs = append(allErrs, validateNetworkPolicyPorts(path.Child("additionalTCPPorts"), networkPolicy.AdditionalTCPPorts)...)
	allErrs = append(allErrs, validateNetworkPolicyPorts(path.Child("additionalUDPPorts"), networkPolicy.AdditionalUDPPorts)...)
	return allErrs
}

func validateNetworkPolicyDestinations(path *field.Path, destinations []hermesv1alpha1.HermesAgentNetworkPolicyPeer) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, destination := range destinations {
		destinationPath := path.Index(i)
		hasCIDR := destination.CIDR != ""
		hasNamespaceSelector := destination.NamespaceSelector != nil
		hasPodSelector := destination.PodSelector != nil
		if !hasCIDR && !hasNamespaceSelector && !hasPodSelector {
			allErrs = append(allErrs, field.Invalid(destinationPath, destination, "at least one of cidr, namespaceSelector, or podSelector must be set"))
		}
		if hasCIDR {
			if _, err := netip.ParsePrefix(destination.CIDR); err != nil {
				allErrs = append(allErrs, field.Invalid(destinationPath.Child("cidr"), destination.CIDR, fmt.Sprintf("must be a valid CIDR: %v", err)))
			}
		}
		if len(destination.Except) > 0 && !hasCIDR {
			allErrs = append(allErrs, field.Invalid(destinationPath.Child("except"), destination.Except, "except requires cidr to be set"))
		}
		for exceptIndex, cidr := range destination.Except {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				allErrs = append(allErrs, field.Invalid(destinationPath.Child("except").Index(exceptIndex), cidr, fmt.Sprintf("must be a valid CIDR: %v", err)))
			}
		}
		if destination.NamespaceSelector != nil {
			allErrs = append(allErrs, metav1validation.ValidateLabelSelector(destination.NamespaceSelector, metav1validation.LabelSelectorValidationOptions{}, destinationPath.Child("namespaceSelector"))...)
		}
		if destination.PodSelector != nil {
			allErrs = append(allErrs, metav1validation.ValidateLabelSelector(destination.PodSelector, metav1validation.LabelSelectorValidationOptions{}, destinationPath.Child("podSelector"))...)
		}
	}
	return allErrs
}

func validateNetworkPolicyPorts(path *field.Path, ports []int32) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, port := range ports {
		if port <= 0 || port > 65535 {
			allErrs = append(allErrs, field.Invalid(path.Index(i), port, "must be between 1 and 65535"))
		}
	}
	return allErrs
}

func validateStorage(path *field.Path, persistence hermesv1alpha1.HermesAgentPersistenceSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if persistence.Size == "" {
		return allErrs
	}

	quantity, err := resource.ParseQuantity(persistence.Size)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(path.Child("size"), persistence.Size, fmt.Sprintf("must be a valid Kubernetes quantity: %v", err)))
		return allErrs
	}
	if quantity.Sign() <= 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("size"), persistence.Size, "must be greater than zero"))
	}
	return allErrs
}

func validateProbeSpec(path *field.Path, probe hermesv1alpha1.HermesAgentProbeSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if probe.PeriodSeconds < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("periodSeconds"), probe.PeriodSeconds, "must be zero or greater"))
	}
	if probe.TimeoutSeconds < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("timeoutSeconds"), probe.TimeoutSeconds, "must be zero or greater"))
	}
	if probe.FailureThreshold < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("failureThreshold"), probe.FailureThreshold, "must be zero or greater"))
	}
	if probe.InitialDelaySeconds < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("initialDelaySeconds"), probe.InitialDelaySeconds, "must be zero or greater"))
	}
	return allErrs
}
