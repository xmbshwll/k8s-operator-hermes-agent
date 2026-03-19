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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HermesAgentImageSpec defines the Hermes runtime image.
type HermesAgentImageSpec struct {
	// repository is the container image repository.
	// +kubebuilder:validation:MinLength=1
	Repository string `json:"repository"`

	// tag is the container image tag.
	// +kubebuilder:default:="gateway-core"
	Tag string `json:"tag,omitempty"`

	// pullPolicy is the image pull policy.
	// +kubebuilder:default:="IfNotPresent"
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

// HermesAgentConfigSource defines how a config file is provided to Hermes.
type HermesAgentConfigSource struct {
	// raw is the inline file content to mount.
	// +optional
	Raw string `json:"raw,omitempty"`

	// configMapRef points to a ConfigMap key containing the file content.
	// +optional
	ConfigMapRef *corev1.ConfigMapKeySelector `json:"configMapRef,omitempty"`
}

// HermesAgentPersistenceSpec defines persistent storage for Hermes state.
type HermesAgentPersistenceSpec struct {
	// enabled controls whether a PVC is created.
	// +kubebuilder:default:=true
	Enabled *bool `json:"enabled,omitempty"`

	// size is the requested PVC size.
	// +kubebuilder:default:="10Gi"
	Size string `json:"size,omitempty"`

	// storageClassName is the PVC storage class.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// accessModes defines PVC access modes.
	// +kubebuilder:default={"ReadWriteOnce"}
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// HermesAgentStorageSpec defines Hermes storage settings.
type HermesAgentStorageSpec struct {
	// persistence configures the Hermes state PVC.
	// +optional
	Persistence HermesAgentPersistenceSpec `json:"persistence,omitempty"`
}

// HermesAgentTerminalSpec defines operator-side terminal wiring for Hermes.
type HermesAgentTerminalSpec struct {
	// backend declares the terminal backend the operator should wire for this pod.
	// Keep this in sync with the backend configured in Hermes itself.
	// The operator uses this for Kubernetes-side behavior such as generated egress rules;
	// the runtime image still reads its own terminal backend from config.yaml.
	// +kubebuilder:validation:Enum=local;ssh
	// +kubebuilder:default:="local"
	Backend string `json:"backend,omitempty"`
}

// HermesAgentProbeSpec configures a single probe profile.
type HermesAgentProbeSpec struct {
	// enabled controls whether this probe is configured.
	Enabled *bool `json:"enabled,omitempty"`

	// initialDelaySeconds is the probe initial delay.
	// +optional
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`

	// periodSeconds is the probe period.
	// +optional
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`

	// timeoutSeconds is the probe timeout.
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// failureThreshold is the probe failure threshold.
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// HermesAgentProbesSpec defines startup, readiness, and liveness behavior.
type HermesAgentProbesSpec struct {
	// startup configures the startup probe.
	// +optional
	Startup HermesAgentProbeSpec `json:"startup,omitempty"`

	// readiness configures the readiness probe.
	// +optional
	Readiness HermesAgentProbeSpec `json:"readiness,omitempty"`

	// liveness configures the liveness probe.
	// +optional
	Liveness HermesAgentProbeSpec `json:"liveness,omitempty"`

	// requireConnectedPlatform makes readiness stricter when enabled.
	// +optional
	RequireConnectedPlatform bool `json:"requireConnectedPlatform,omitempty"`
}

// HermesAgentServiceSpec defines an optional Service for exposed modes.
type HermesAgentServiceSpec struct {
	// enabled controls whether a Service is created.
	// +kubebuilder:default:=false
	Enabled bool `json:"enabled,omitempty"`

	// type is the Kubernetes Service type.
	// +kubebuilder:default:="ClusterIP"
	Type corev1.ServiceType `json:"type,omitempty"`

	// port is the service port.
	// +kubebuilder:default:=8080
	// +optional
	Port int32 `json:"port,omitempty"`
}

// HermesAgentNetworkPolicySpec defines optional NetworkPolicy generation.
type HermesAgentNetworkPolicySpec struct {
	// enabled controls whether an egress-focused NetworkPolicy is created.
	Enabled *bool `json:"enabled,omitempty"`

	// additionalTCPPorts adds extra TCP egress ports to the generated policy.
	// Use this when your Hermes workflow needs outbound TCP beyond the default DNS, HTTP, HTTPS, and optional SSH rules.
	// +optional
	AdditionalTCPPorts []int32 `json:"additionalTCPPorts,omitempty"`

	// additionalUDPPorts adds extra UDP egress ports to the generated policy.
	// Use this when your Hermes workflow needs outbound UDP beyond the default DNS rule.
	// +optional
	AdditionalUDPPorts []int32 `json:"additionalUDPPorts,omitempty"`
}

// HermesAgentSpec defines the desired state of HermesAgent.
type HermesAgentSpec struct {
	// image defines the Hermes runtime image.
	Image HermesAgentImageSpec `json:"image"`

	// mode selects the Hermes runtime mode.
	// +kubebuilder:validation:Enum=gateway
	// +kubebuilder:default:="gateway"
	Mode string `json:"mode,omitempty"`

	// config provides the main Hermes config.yaml content.
	// +optional
	Config HermesAgentConfigSource `json:"config,omitempty"`

	// gatewayConfig provides the Hermes gateway.json content.
	// +optional
	GatewayConfig HermesAgentConfigSource `json:"gatewayConfig,omitempty"`

	// env contains direct environment variable overrides.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// envFrom contains ConfigMap/Secret environment sources.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// secretRefs are additional Secrets the controller may project or reference.
	// +optional
	SecretRefs []corev1.LocalObjectReference `json:"secretRefs,omitempty"`

	// storage defines Hermes state persistence.
	// +optional
	Storage HermesAgentStorageSpec `json:"storage,omitempty"`

	// terminal defines the terminal backend Hermes should use.
	// +optional
	Terminal HermesAgentTerminalSpec `json:"terminal,omitempty"`

	// resources defines container resource requests and limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// probes defines startup, readiness, and liveness behavior.
	// +optional
	Probes HermesAgentProbesSpec `json:"probes,omitempty"`

	// service defines an optional Service.
	// +optional
	Service HermesAgentServiceSpec `json:"service,omitempty"`

	// networkPolicy controls optional NetworkPolicy generation.
	// +kubebuilder:default:={enabled:false}
	// +optional
	NetworkPolicy HermesAgentNetworkPolicySpec `json:"networkPolicy,omitempty"`
}

// HermesAgentStatus defines the observed state of HermesAgent.
type HermesAgentStatus struct {
	// phase is a coarse-grained summary of HermesAgent state.
	// +optional
	Phase string `json:"phase,omitempty"`

	// observedGeneration is the last processed metadata generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// readyReplicas is the number of ready Hermes pods.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// persistenceBound indicates whether the Hermes PVC is bound.
	// +optional
	PersistenceBound bool `json:"persistenceBound,omitempty"`

	// lastReconcileTime records the last reconcile timestamp.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// conditions represent the current state of the HermesAgent resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ha
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Persistent",type=boolean,JSONPath=".status.persistenceBound"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// HermesAgent is the Schema for the hermesagents API.
type HermesAgent struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of HermesAgent
	// +required
	Spec HermesAgentSpec `json:"spec"`

	// status defines the observed state of HermesAgent
	// +optional
	Status HermesAgentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// HermesAgentList contains a list of HermesAgent.
type HermesAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []HermesAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HermesAgent{}, &HermesAgentList{})
}
