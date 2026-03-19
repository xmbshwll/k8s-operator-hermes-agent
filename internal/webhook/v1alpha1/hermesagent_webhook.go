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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	defaultMode                  = "gateway"
	defaultImageTag              = "gateway-core"
	defaultTerminalBackend       = "local"
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
	if obj.Spec.Image.Tag == "" {
		obj.Spec.Image.Tag = defaultImageTag
	}
	if obj.Spec.Image.PullPolicy == "" {
		obj.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if obj.Spec.Terminal.Backend == "" {
		obj.Spec.Terminal.Backend = defaultTerminalBackend
	}

	if obj.Spec.Storage.Persistence.Enabled == nil {
		obj.Spec.Storage.Persistence.Enabled = &defaultPersistenceEnabled
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
	allErrs = append(allErrs, validateTerminal(specPath, obj)...)
	allErrs = append(allErrs, validateEnv(specPath.Child("env"), obj.Spec.Env)...)
	allErrs = append(allErrs, validateEnvFrom(specPath.Child("envFrom"), obj.Spec.EnvFrom)...)
	allErrs = append(allErrs, validateLocalObjectReferences(specPath.Child("secretRefs"), obj.Spec.SecretRefs)...)
	allErrs = append(allErrs, validateLocalObjectReferences(specPath.Child("imagePullSecrets"), obj.Spec.ImagePullSecrets)...)
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
	if source.Raw != "" && source.ConfigMapRef != nil {
		allErrs = append(allErrs, field.Invalid(path, source, "raw and configMapRef are mutually exclusive"))
	}
	if source.ConfigMapRef == nil {
		return allErrs
	}
	if source.ConfigMapRef.Name == "" {
		allErrs = append(allErrs, field.Required(path.Child("configMapRef", "name"), "name is required when configMapRef is set"))
	}
	if source.ConfigMapRef.Key == "" {
		allErrs = append(allErrs, field.Required(path.Child("configMapRef", "key"), "key is required when configMapRef is set"))
	}
	return allErrs
}

func validateTerminal(specPath *field.Path, obj *hermesv1alpha1.HermesAgent) field.ErrorList {
	allErrs := field.ErrorList{}

	if obj.Spec.Config.Raw == "" {
		return allErrs
	}

	backend, found, err := terminalBackendFromConfigRaw(obj.Spec.Config.Raw)
	if err != nil {
		return allErrs
	}
	if !found {
		if obj.Spec.Terminal.Backend == "ssh" {
			allErrs = append(allErrs, field.Invalid(specPath.Child("config", "raw"), obj.Spec.Config.Raw, "spec.terminal.backend is ssh, so spec.config.raw must set terminal.backend to ssh as well"))
		}
		return allErrs
	}
	if backend != obj.Spec.Terminal.Backend {
		allErrs = append(allErrs, field.Invalid(specPath.Child("config", "raw"), obj.Spec.Config.Raw, fmt.Sprintf("terminal.backend in spec.config.raw must match spec.terminal.backend (%s)", obj.Spec.Terminal.Backend)))
	}

	return allErrs
}

func terminalBackendFromConfigRaw(raw string) (string, bool, error) {
	var config struct {
		Terminal struct {
			Backend string `yaml:"backend"`
		} `yaml:"terminal"`
	}

	if err := yaml.Unmarshal([]byte(raw), &config); err != nil {
		return "", false, err
	}
	if config.Terminal.Backend == "" {
		return "", false, nil
	}
	return config.Terminal.Backend, true, nil
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

func validateService(path *field.Path, service hermesv1alpha1.HermesAgentServiceSpec) field.ErrorList {
	allErrs := field.ErrorList{}
	if service.Enabled && service.Port <= 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("port"), service.Port, "port must be greater than zero when service is enabled"))
	}
	return allErrs
}

func validateNetworkPolicy(path *field.Path, networkPolicy hermesv1alpha1.HermesAgentNetworkPolicySpec) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateNetworkPolicyPorts(path.Child("additionalTCPPorts"), networkPolicy.AdditionalTCPPorts)...)
	allErrs = append(allErrs, validateNetworkPolicyPorts(path.Child("additionalUDPPorts"), networkPolicy.AdditionalUDPPorts)...)
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
