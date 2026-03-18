package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	configHashAnnotation    = "hermes.nous.ai/config-hash"
	hermesContainerName     = "hermes"
	hermesGatewayMode       = "gateway"
	hermesDataPath          = "/data/hermes"
	hermesHomePath          = "/data/hermes"
	hermesSecretBasePath    = "/var/run/hermes/secrets"
	hermesDataVolumeName    = "hermes-data"
	hermesTmpPath           = "/tmp"
	hermesTmpVolumeName     = "tmp"
	hermesRuntimeUID        = int64(10001)
	hermesGatewayPIDFile    = "gateway.pid"
	hermesGatewayStateFile  = "gateway_state.json"
	networkPolicyDNSPort    = int32(53)
	networkPolicyHTTPPort   = int32(80)
	networkPolicyHTTPSPort  = int32(443)
	networkPolicySSHPort    = int32(22)
	startupFailureThreshold = int32(18)
	readinessInitialDelay   = int32(5)
	probePeriodSeconds      = int32(10)
	probeTimeoutSeconds     = int32(5)
	probeFailureThreshold   = int32(3)
	livenessInitialDelay    = int32(15)
)

type resolvedConfigFile struct {
	ID            string
	FileName      string
	ConfigMapName string
	ConfigMapKey  string
	MountPath     string
	Generated     bool
	Content       string
}

type configPlan struct {
	Files []resolvedConfigFile
	Hash  string
}

type referencedInputState struct {
	FileRefs     []referencedObjectSnapshot `json:"fileRefs,omitempty"`
	EnvValueRefs []referencedObjectSnapshot `json:"envValueRefs,omitempty"`
	EnvFrom      []referencedObjectSnapshot `json:"envFrom,omitempty"`
	SecretRefs   []referencedObjectSnapshot `json:"secretRefs,omitempty"`
}

type referencedObjectSnapshot struct {
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	Key      string            `json:"key,omitempty"`
	Optional bool              `json:"optional,omitempty"`
	Present  bool              `json:"present"`
	KeyFound bool              `json:"keyFound,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
}

type podTemplateInputs struct {
	Annotations  map[string]string
	Env          []corev1.EnvVar
	EnvFrom      []corev1.EnvFromSource
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
}

func buildConfigPlan(agent *hermesv1alpha1.HermesAgent) (configPlan, error) {
	return buildConfigPlanWithReferences(agent, referencedInputState{})
}

func buildConfigPlanWithReferences(agent *hermesv1alpha1.HermesAgent, referencedInputs referencedInputState) (configPlan, error) {
	files := []resolvedConfigFile{}

	configFile, err := resolveConfigFile(agent, "config", "config.yaml", agent.Spec.Config)
	if err != nil {
		return configPlan{}, err
	}
	if configFile != nil {
		files = append(files, *configFile)
	}

	gatewayFile, err := resolveConfigFile(agent, "gateway-config", "gateway.json", agent.Spec.GatewayConfig)
	if err != nil {
		return configPlan{}, err
	}
	if gatewayFile != nil {
		files = append(files, *gatewayFile)
	}

	plan := configPlan{Files: files}
	plan.Hash = computeConfigHash(agent, plan, referencedInputs)
	return plan, nil
}

func resolveConfigFile(agent *hermesv1alpha1.HermesAgent, id, fileName string, source hermesv1alpha1.HermesAgentConfigSource) (*resolvedConfigFile, error) {
	hasRaw := source.Raw != ""
	hasRef := source.ConfigMapRef != nil

	if hasRaw && hasRef {
		return nil, fmt.Errorf("spec.%s cannot set both raw and configMapRef", sourceFieldName(id))
	}
	if !hasRaw && !hasRef {
		return nil, nil
	}

	file := &resolvedConfigFile{
		ID:        id,
		FileName:  fileName,
		MountPath: path.Join(hermesHomePath, fileName),
	}

	if hasRaw {
		file.Generated = true
		file.Content = source.Raw
		file.ConfigMapName = generatedConfigMapName(agent.Name, id)
		file.ConfigMapKey = fileName
		return file, nil
	}

	if source.ConfigMapRef.Name == "" || source.ConfigMapRef.Key == "" {
		return nil, fmt.Errorf("spec.%s.configMapRef requires both name and key", sourceFieldName(id))
	}

	file.ConfigMapName = source.ConfigMapRef.Name
	file.ConfigMapKey = source.ConfigMapRef.Key
	return file, nil
}

func buildPodTemplateInputs(agent *hermesv1alpha1.HermesAgent, plan configPlan) podTemplateInputs {
	inputs := podTemplateInputs{
		Annotations: map[string]string{configHashAnnotation: plan.Hash},
		Env:         append([]corev1.EnvVar{}, agent.Spec.Env...),
		EnvFrom:     append([]corev1.EnvFromSource{}, agent.Spec.EnvFrom...),
	}

	inputs.Env = upsertEnvVar(inputs.Env, corev1.EnvVar{Name: "HERMES_HOME", Value: hermesHomePath})

	for _, file := range plan.Files {
		volumeName := configVolumeName(file.ID)
		inputs.Volumes = append(inputs.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: file.ConfigMapName},
					Items:                []corev1.KeyToPath{{Key: file.ConfigMapKey, Path: file.FileName}},
				},
			},
		})
		inputs.VolumeMounts = append(inputs.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: file.MountPath,
			SubPath:   file.FileName,
			ReadOnly:  true,
		})
	}

	for i, secretRef := range agent.Spec.SecretRefs {
		if secretRef.Name == "" {
			continue
		}
		volumeName := fmt.Sprintf("secret-ref-%d", i)
		inputs.Volumes = append(inputs.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: secretRef.Name},
			},
		})
		inputs.VolumeMounts = append(inputs.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: path.Join(hermesSecretBasePath, secretRef.Name),
			ReadOnly:  true,
		})
	}

	return inputs
}

func buildPersistentVolumeClaim(agent *hermesv1alpha1.HermesAgent) (*corev1.PersistentVolumeClaim, error) {
	quantity, err := resource.ParseQuantity(persistenceSize(agent))
	if err != nil {
		return nil, fmt.Errorf("parse storage size: %w", err)
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      persistentVolumeClaimName(agent.Name),
			Namespace: agent.Namespace,
			Labels:    resourceLabels(agent),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: persistenceAccessModes(agent),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: quantity,
				},
			},
			StorageClassName: agent.Spec.Storage.Persistence.StorageClassName,
		},
	}, nil
}

func buildService(agent *hermesv1alpha1.HermesAgent) *corev1.Service {
	labels := resourceLabels(agent)
	port := servicePort(agent)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType(agent),
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Port:       port,
				TargetPort: intstr.FromInt32(port),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

func buildNetworkPolicy(agent *hermesv1alpha1.HermesAgent) *networkingv1.NetworkPolicy {
	labels := resourceLabels(agent)
	egress := []networkingv1.NetworkPolicyEgressRule{
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protocolPtr(corev1.ProtocolUDP), Port: portIntOrString(networkPolicyDNSPort)},
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: portIntOrString(networkPolicyDNSPort)},
			},
		},
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: portIntOrString(networkPolicyHTTPPort)},
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: portIntOrString(networkPolicyHTTPSPort)},
			},
		},
	}
	if terminalBackend(agent) == "ssh" {
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{{
				Protocol: protocolPtr(corev1.ProtocolTCP),
				Port:     portIntOrString(networkPolicySSHPort),
			}},
		})
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: labels},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      egress,
		},
	}
}

func buildStatefulSet(agent *hermesv1alpha1.HermesAgent, inputs podTemplateInputs) *appsv1.StatefulSet {
	replicas := int32(1)
	labels := resourceLabels(agent)
	volumes := append([]corev1.Volume{}, inputs.Volumes...)
	volumeMounts := append([]corev1.VolumeMount{}, inputs.VolumeMounts...)

	volumes = append(volumes, hermesDataVolume(agent), hermesTmpVolume())
	volumeMounts = append(volumeMounts,
		corev1.VolumeMount{
			Name:      hermesDataVolumeName,
			MountPath: hermesDataPath,
		},
		corev1.VolumeMount{
			Name:      hermesTmpVolumeName,
			MountPath: hermesTmpPath,
		},
	)

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: agent.Name,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: mergeStringMaps(nil, inputs.Annotations),
				},
				Spec: corev1.PodSpec{
					SecurityContext: hermesPodSecurityContext(),
					Containers: []corev1.Container{{
						Name:            hermesContainerName,
						Image:           hermesImage(agent.Spec.Image),
						ImagePullPolicy: agent.Spec.Image.PullPolicy,
						Args:            hermesArgs(agent),
						Env:             inputs.Env,
						EnvFrom:         inputs.EnvFrom,
						VolumeMounts:    volumeMounts,
						Resources:       agent.Spec.Resources,
						SecurityContext: hermesContainerSecurityContext(),
						StartupProbe:    hermesStartupProbe(agent),
						ReadinessProbe:  hermesReadinessProbe(agent),
						LivenessProbe:   hermesLivenessProbe(agent),
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

func computeConfigHash(agent *hermesv1alpha1.HermesAgent, plan configPlan, referencedInputs referencedInputState) string {
	payload := struct {
		Files            []resolvedConfigFile          `json:"files"`
		Env              []corev1.EnvVar               `json:"env"`
		EnvFrom          []corev1.EnvFromSource        `json:"envFrom"`
		SecretRefs       []corev1.LocalObjectReference `json:"secretRefs"`
		ReferencedInputs referencedInputState          `json:"referencedInputs,omitempty"`
	}{
		Files:            plan.Files,
		Env:              agent.Spec.Env,
		EnvFrom:          agent.Spec.EnvFrom,
		SecretRefs:       agent.Spec.SecretRefs,
		ReferencedInputs: referencedInputs,
	}

	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func newConfigMapFileSnapshot(name, key string, configMap *corev1.ConfigMap) referencedObjectSnapshot {
	snapshot := referencedObjectSnapshot{
		Kind:    "ConfigMap",
		Name:    name,
		Key:     key,
		Present: configMap != nil,
	}
	if configMap == nil {
		return snapshot
	}

	content, ok := configMapFileValue(configMap, key)
	snapshot.KeyFound = ok
	if ok {
		snapshot.Data = map[string]string{key: content}
	}
	return snapshot
}

func newConfigMapKeySnapshot(name, key string, optional bool, configMap *corev1.ConfigMap) referencedObjectSnapshot {
	snapshot := referencedObjectSnapshot{
		Kind:     "ConfigMap",
		Name:     name,
		Key:      key,
		Optional: optional,
		Present:  configMap != nil,
	}
	if configMap == nil {
		return snapshot
	}

	value, ok := configMapFileValue(configMap, key)
	snapshot.KeyFound = ok
	if ok {
		snapshot.Data = map[string]string{key: value}
	}
	return snapshot
}

func newConfigMapEnvFromSnapshot(name string, optional bool, configMap *corev1.ConfigMap) referencedObjectSnapshot {
	snapshot := referencedObjectSnapshot{
		Kind:     "ConfigMap",
		Name:     name,
		Optional: optional,
		Present:  configMap != nil,
	}
	if configMap != nil {
		snapshot.Data = copyStringMap(configMap.Data)
	}
	return snapshot
}

func newSecretKeySnapshot(name, key string, optional bool, secret *corev1.Secret) referencedObjectSnapshot {
	snapshot := referencedObjectSnapshot{
		Kind:     "Secret",
		Name:     name,
		Key:      key,
		Optional: optional,
		Present:  secret != nil,
	}
	if secret == nil {
		return snapshot
	}

	value, ok := secret.Data[key]
	snapshot.KeyFound = ok
	if ok {
		snapshot.Data = map[string]string{key: base64.StdEncoding.EncodeToString(value)}
	}
	return snapshot
}

func newSecretSnapshot(name string, optional bool, secret *corev1.Secret) referencedObjectSnapshot {
	snapshot := referencedObjectSnapshot{
		Kind:     "Secret",
		Name:     name,
		Optional: optional,
		Present:  secret != nil,
	}
	if secret != nil {
		snapshot.Data = encodeSecretData(secret.Data)
	}
	return snapshot
}

func configMapFileValue(configMap *corev1.ConfigMap, key string) (string, bool) {
	if value, ok := configMap.Data[key]; ok {
		return value, true
	}
	if value, ok := configMap.BinaryData[key]; ok {
		return base64.StdEncoding.EncodeToString(value), true
	}
	return "", false
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	copied := make(map[string]string, len(values))
	maps.Copy(copied, values)
	return copied
}

func encodeSecretData(values map[string][]byte) map[string]string {
	if len(values) == 0 {
		return nil
	}

	encoded := make(map[string]string, len(values))
	for key, value := range values {
		encoded[key] = base64.StdEncoding.EncodeToString(value)
	}
	return encoded
}

func optionalValue(value *bool) bool {
	return value != nil && *value
}

func resourceLabels(agent *hermesv1alpha1.HermesAgent) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "k8s-operator-hermes-agent",
		"app.kubernetes.io/managed-by": "kustomize",
		"app.kubernetes.io/instance":   agent.Name,
	}
}

func mergeStringMaps(base, extra map[string]string) map[string]string {
	merged := map[string]string{}
	maps.Copy(merged, base)
	maps.Copy(merged, extra)
	return merged
}

func upsertEnvVar(env []corev1.EnvVar, value corev1.EnvVar) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == value.Name {
			env[i] = value
			return env
		}
	}
	return append(env, value)
}

func configVolumeName(id string) string {
	return fmt.Sprintf("hermes-%s", id)
}

func generatedConfigMapName(resourceName, id string) string {
	return fmt.Sprintf("%s-%s", resourceName, id)
}

func hermesImage(image hermesv1alpha1.HermesAgentImageSpec) string {
	if image.Tag == "" {
		return image.Repository
	}
	return fmt.Sprintf("%s:%s", image.Repository, image.Tag)
}

func hermesArgs(agent *hermesv1alpha1.HermesAgent) []string {
	mode := agent.Spec.Mode
	if mode == "" {
		mode = hermesGatewayMode
	}
	return []string{"hermes", mode}
}

func hermesPodSecurityContext() *corev1.PodSecurityContext {
	runAsNonRoot := true
	uid := hermesRuntimeUID

	return &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRoot,
		RunAsUser:    &uid,
		RunAsGroup:   &uid,
		FSGroup:      &uid,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func hermesContainerSecurityContext() *corev1.SecurityContext {
	allowPrivilegeEscalation := false

	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

func hermesStartupProbe(agent *hermesv1alpha1.HermesAgent) *corev1.Probe {
	config := resolveProbeConfig(agent.Spec.Probes.Startup, startupProbeDefaults())
	if !config.Enabled {
		return nil
	}

	return buildExecProbe(config, probeCommand(
		fmt.Sprintf("test -s %s", shellQuote(hermesGatewayPIDPath())),
		fmt.Sprintf("test -s %s", shellQuote(hermesGatewayStatePath())),
	))
}

func hermesReadinessProbe(agent *hermesv1alpha1.HermesAgent) *corev1.Probe {
	config := resolveProbeConfig(agent.Spec.Probes.Readiness, readinessProbeDefaults())
	if !config.Enabled {
		return nil
	}

	checks := []string{probeProcessCheck()}
	if agent.Spec.Probes.RequireConnectedPlatform {
		checks = append(checks, probeConnectedPlatformCheck())
	} else {
		checks = append(checks, fmt.Sprintf("test -s %s", shellQuote(hermesGatewayStatePath())))
	}

	return buildExecProbe(config, probeCommand(checks...))
}

func hermesLivenessProbe(agent *hermesv1alpha1.HermesAgent) *corev1.Probe {
	config := resolveProbeConfig(agent.Spec.Probes.Liveness, livenessProbeDefaults())
	if !config.Enabled {
		return nil
	}

	return buildExecProbe(config, probeCommand(
		probeProcessCheck(),
		fmt.Sprintf("test -s %s", shellQuote(hermesGatewayStatePath())),
	))
}

func buildExecProbe(config probeConfig, command string) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{"bash", "-ec", command}},
		},
		InitialDelaySeconds: config.InitialDelaySeconds,
		PeriodSeconds:       config.PeriodSeconds,
		TimeoutSeconds:      config.TimeoutSeconds,
		FailureThreshold:    config.FailureThreshold,
	}
}

type probeConfig struct {
	Enabled             bool
	InitialDelaySeconds int32
	PeriodSeconds       int32
	TimeoutSeconds      int32
	FailureThreshold    int32
}

func resolveProbeConfig(spec hermesv1alpha1.HermesAgentProbeSpec, defaults probeConfig) probeConfig {
	config := defaults
	if spec.Enabled != nil {
		config.Enabled = *spec.Enabled
	}
	if spec.InitialDelaySeconds > 0 {
		config.InitialDelaySeconds = spec.InitialDelaySeconds
	}
	if spec.PeriodSeconds > 0 {
		config.PeriodSeconds = spec.PeriodSeconds
	}
	if spec.TimeoutSeconds > 0 {
		config.TimeoutSeconds = spec.TimeoutSeconds
	}
	if spec.FailureThreshold > 0 {
		config.FailureThreshold = spec.FailureThreshold
	}
	return config
}

func startupProbeDefaults() probeConfig {
	return probeConfig{
		Enabled:             true,
		InitialDelaySeconds: 0,
		PeriodSeconds:       probePeriodSeconds,
		TimeoutSeconds:      probeTimeoutSeconds,
		FailureThreshold:    startupFailureThreshold,
	}
}

func readinessProbeDefaults() probeConfig {
	return probeConfig{
		Enabled:             true,
		InitialDelaySeconds: readinessInitialDelay,
		PeriodSeconds:       probePeriodSeconds,
		TimeoutSeconds:      probeTimeoutSeconds,
		FailureThreshold:    probeFailureThreshold,
	}
}

func livenessProbeDefaults() probeConfig {
	return probeConfig{
		Enabled:             true,
		InitialDelaySeconds: livenessInitialDelay,
		PeriodSeconds:       probePeriodSeconds,
		TimeoutSeconds:      probeTimeoutSeconds,
		FailureThreshold:    probeFailureThreshold,
	}
}

func probeCommand(checks ...string) string {
	return fmt.Sprintf("set -euo pipefail; %s", joinWithAnd(checks))
}

func joinWithAnd(checks []string) string {
	if len(checks) == 0 {
		return "true"
	}

	var result strings.Builder
	result.WriteString(checks[0])
	for _, check := range checks[1:] {
		result.WriteString(" && " + check)
	}
	return result.String()
}

func probeProcessCheck() string {
	pidPath := shellQuote(hermesGatewayPIDPath())
	return fmt.Sprintf("pid=$(cat %s) && [ -n \"$pid\" ] && kill -0 \"$pid\"", pidPath)
}

func probeConnectedPlatformCheck() string {
	statePath := shellQuote(hermesGatewayStatePath())
	return fmt.Sprintf("test -s %s && grep -Eq '\"connected\"[[:space:]]*:[[:space:]]*true' %s", statePath, statePath)
}

func hermesGatewayPIDPath() string {
	return path.Join(hermesHomePath, hermesGatewayPIDFile)
}

func hermesGatewayStatePath() string {
	return path.Join(hermesHomePath, hermesGatewayStateFile)
}

func shellQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func terminalBackend(agent *hermesv1alpha1.HermesAgent) string {
	if agent.Spec.Terminal.Backend == "" {
		return "local"
	}
	return agent.Spec.Terminal.Backend
}

func protocolPtr(protocol corev1.Protocol) *corev1.Protocol {
	return &protocol
}

func portIntOrString(port int32) *intstr.IntOrString {
	value := intstr.FromInt32(port)
	return &value
}

func hermesDataVolume(agent *hermesv1alpha1.HermesAgent) corev1.Volume {
	if persistenceEnabled(agent) {
		return corev1.Volume{
			Name: hermesDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: persistentVolumeClaimName(agent.Name),
				},
			},
		}
	}

	return corev1.Volume{
		Name:         hermesDataVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func hermesTmpVolume() corev1.Volume {
	return corev1.Volume{
		Name:         hermesTmpVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func persistenceEnabled(agent *hermesv1alpha1.HermesAgent) bool {
	if agent.Spec.Storage.Persistence.Enabled == nil {
		return true
	}
	return *agent.Spec.Storage.Persistence.Enabled
}

func networkPolicyEnabled(agent *hermesv1alpha1.HermesAgent) bool {
	if agent.Spec.NetworkPolicy.Enabled == nil {
		return false
	}
	return *agent.Spec.NetworkPolicy.Enabled
}

func serviceEnabled(agent *hermesv1alpha1.HermesAgent) bool {
	return agent.Spec.Service.Enabled
}

func serviceType(agent *hermesv1alpha1.HermesAgent) corev1.ServiceType {
	if agent.Spec.Service.Type == "" {
		return corev1.ServiceTypeClusterIP
	}
	return agent.Spec.Service.Type
}

func servicePort(agent *hermesv1alpha1.HermesAgent) int32 {
	if agent.Spec.Service.Port <= 0 {
		return 8080
	}
	return agent.Spec.Service.Port
}

func persistenceSize(agent *hermesv1alpha1.HermesAgent) string {
	if agent.Spec.Storage.Persistence.Size == "" {
		return "10Gi"
	}
	return agent.Spec.Storage.Persistence.Size
}

func persistenceAccessModes(agent *hermesv1alpha1.HermesAgent) []corev1.PersistentVolumeAccessMode {
	if len(agent.Spec.Storage.Persistence.AccessModes) == 0 {
		return []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	return append([]corev1.PersistentVolumeAccessMode{}, agent.Spec.Storage.Persistence.AccessModes...)
}

func persistentVolumeClaimName(resourceName string) string {
	return fmt.Sprintf("%s-data", resourceName)
}

func sourceFieldName(id string) string {
	if id == "config" {
		return "config"
	}
	return "gatewayConfig"
}
