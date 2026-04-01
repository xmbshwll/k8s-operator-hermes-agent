package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"

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
	terminalBackendLocal    = "local"
	terminalBackendSSH      = "ssh"
)

type resolvedConfigFile struct {
	ID            string
	FileName      string
	ConfigMapName string
	SecretName    string
	SourceKey     string
	MountPath     string
	Generated     bool
	Content       string
}

type configPlan struct {
	Files []resolvedConfigFile
	Hash  string
}

type referencedInputState struct {
	FileRefs      []referencedObjectSnapshot `json:"fileRefs,omitempty"`
	FileMountRefs []referencedObjectSnapshot `json:"fileMountRefs,omitempty"`
	EnvValueRefs  []referencedObjectSnapshot `json:"envValueRefs,omitempty"`
	EnvFrom       []referencedObjectSnapshot `json:"envFrom,omitempty"`
	SecretRefs    []referencedObjectSnapshot `json:"secretRefs,omitempty"`
}

type referencedObjectSnapshot struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Key         string            `json:"key,omitempty"`
	Optional    bool              `json:"optional,omitempty"`
	Present     bool              `json:"present"`
	KeyFound    bool              `json:"keyFound,omitempty"`
	MissingKeys []string          `json:"missingKeys,omitempty"`
	Data        map[string]string `json:"data,omitempty"`
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
	if err := validateFileMountSpecs(agent.Spec.FileMounts); err != nil {
		return configPlan{}, err
	}

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
	hasConfigMapRef := source.ConfigMapRef != nil
	hasSecretRef := source.SecretRef != nil
	sourceCount := 0
	for _, present := range []bool{hasRaw, hasConfigMapRef, hasSecretRef} {
		if present {
			sourceCount++
		}
	}

	if sourceCount > 1 {
		return nil, fmt.Errorf("spec.%s must set exactly one of raw, configMapRef, or secretRef", sourceFieldName(id))
	}
	if sourceCount == 0 {
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
		file.SourceKey = fileName
		return file, nil
	}

	if hasConfigMapRef {
		if source.ConfigMapRef.Name == "" || source.ConfigMapRef.Key == "" {
			return nil, fmt.Errorf("spec.%s.configMapRef requires both name and key", sourceFieldName(id))
		}
		file.ConfigMapName = source.ConfigMapRef.Name
		file.SourceKey = source.ConfigMapRef.Key
		return file, nil
	}

	if source.SecretRef.Name == "" || source.SecretRef.Key == "" {
		return nil, fmt.Errorf("spec.%s.secretRef requires both name and key", sourceFieldName(id))
	}
	file.SecretName = source.SecretRef.Name
	file.SourceKey = source.SecretRef.Key
	return file, nil
}

func validateFileMountSpecs(mounts []hermesv1alpha1.HermesAgentFileMountSpec) error {
	seenMountPaths := map[string]int{}
	for i, mount := range mounts {
		hasConfigMap := mount.ConfigMapRef != nil
		hasSecret := mount.SecretRef != nil
		switch {
		case hasConfigMap && hasSecret:
			return fmt.Errorf("spec.fileMounts[%d] cannot set both configMapRef and secretRef", i)
		case !hasConfigMap && !hasSecret:
			return fmt.Errorf("spec.fileMounts[%d] must set exactly one of configMapRef or secretRef", i)
		}
		if mount.MountPath == "" {
			return fmt.Errorf("spec.fileMounts[%d].mountPath is required", i)
		}
		if !path.IsAbs(mount.MountPath) {
			return fmt.Errorf("spec.fileMounts[%d].mountPath must be absolute", i)
		}
		if previous, exists := seenMountPaths[mount.MountPath]; exists {
			return fmt.Errorf("spec.fileMounts[%d].mountPath duplicates spec.fileMounts[%d].mountPath", i, previous)
		}
		seenMountPaths[mount.MountPath] = i
		if mount.ConfigMapRef != nil && mount.ConfigMapRef.Name == "" {
			return fmt.Errorf("spec.fileMounts[%d].configMapRef.name is required", i)
		}
		if mount.SecretRef != nil && mount.SecretRef.Name == "" {
			return fmt.Errorf("spec.fileMounts[%d].secretRef.name is required", i)
		}
		if err := validateFileModeValue(mount.DefaultMode, fmt.Sprintf("spec.fileMounts[%d].defaultMode", i)); err != nil {
			return err
		}
		seenItemPaths := map[string]int{}
		seenItemKeys := map[string]int{}
		for j, item := range mount.Items {
			if item.Key == "" {
				return fmt.Errorf("spec.fileMounts[%d].items[%d].key is required", i, j)
			}
			if previous, exists := seenItemKeys[item.Key]; exists {
				return fmt.Errorf("spec.fileMounts[%d].items[%d].key duplicates spec.fileMounts[%d].items[%d].key", i, j, i, previous)
			}
			seenItemKeys[item.Key] = j
			if item.Path == "" {
				return fmt.Errorf("spec.fileMounts[%d].items[%d].path is required", i, j)
			}
			if err := validateFileMountItemPath(item.Path); err != nil {
				return fmt.Errorf("spec.fileMounts[%d].items[%d].path %s", i, j, err.Error())
			}
			if previous, exists := seenItemPaths[item.Path]; exists {
				return fmt.Errorf("spec.fileMounts[%d].items[%d].path duplicates spec.fileMounts[%d].items[%d].path", i, j, i, previous)
			}
			seenItemPaths[item.Path] = j
			if err := validateFileModeValue(item.Mode, fmt.Sprintf("spec.fileMounts[%d].items[%d].mode", i, j)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFileMountItemPath(value string) error {
	if path.IsAbs(value) {
		return fmt.Errorf("must be relative")
	}
	clean := path.Clean(value)
	if clean == "." || clean != value || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("must not contain '.' or '..' path segments")
	}
	return nil
}

func validateFileModeValue(mode *int32, fieldName string) error {
	if mode == nil {
		return nil
	}
	if *mode < 0 || *mode > 0o777 {
		return fmt.Errorf("%s must be between 0 and 0777", fieldName)
	}
	return nil
}

func buildPodTemplateInputs(agent *hermesv1alpha1.HermesAgent, plan configPlan) podTemplateInputs {
	inputs := podTemplateInputs{
		Annotations: map[string]string{configHashAnnotation: plan.Hash},
		Env:         append([]corev1.EnvVar{}, agent.Spec.Env...),
		EnvFrom:     append([]corev1.EnvFromSource{}, agent.Spec.EnvFrom...),
	}

	inputs.Env = upsertEnvVar(inputs.Env, corev1.EnvVar{Name: "HERMES_HOME", Value: hermesHomePath})
	appendConfigFileInputs(&inputs, plan.Files)
	appendSecretReferenceInputs(&inputs, agent.Spec.SecretRefs)
	appendFileMountInputs(&inputs, agent.Spec.FileMounts)
	return inputs
}

func appendConfigFileInputs(inputs *podTemplateInputs, files []resolvedConfigFile) {
	for _, file := range files {
		volumeName := configVolumeName(file.ID)
		inputs.Volumes = append(inputs.Volumes, corev1.Volume{
			Name:         volumeName,
			VolumeSource: configFileVolumeSource(file),
		})
		inputs.VolumeMounts = append(inputs.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: file.MountPath,
			SubPath:   file.FileName,
			ReadOnly:  true,
		})
	}
}

func configFileVolumeSource(file resolvedConfigFile) corev1.VolumeSource {
	items := []corev1.KeyToPath{{Key: file.SourceKey, Path: file.FileName}}
	if file.SecretName != "" {
		return corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: file.SecretName,
				Items:      items,
			},
		}
	}
	return corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: file.ConfigMapName},
			Items:                items,
		},
	}
}

func appendSecretReferenceInputs(inputs *podTemplateInputs, secretRefs []corev1.LocalObjectReference) {
	for i, secretRef := range secretRefs {
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
}

func appendFileMountInputs(inputs *podTemplateInputs, fileMounts []hermesv1alpha1.HermesAgentFileMountSpec) {
	for i, fileMount := range fileMounts {
		volumeSource := buildFileMountVolumeSource(fileMount)
		if volumeSource == nil {
			continue
		}
		volumeName := fmt.Sprintf("file-mount-%d", i)
		inputs.Volumes = append(inputs.Volumes, corev1.Volume{
			Name:         volumeName,
			VolumeSource: *volumeSource,
		})
		inputs.VolumeMounts = append(inputs.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: fileMount.MountPath,
			ReadOnly:  true,
		})
	}
}

func buildFileMountVolumeSource(fileMount hermesv1alpha1.HermesAgentFileMountSpec) *corev1.VolumeSource {
	items := fileMountProjectionItems(fileMount.Items)
	if fileMount.ConfigMapRef != nil && fileMount.ConfigMapRef.Name != "" {
		return &corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: *fileMount.ConfigMapRef,
				Items:                items,
				DefaultMode:          fileMount.DefaultMode,
			},
		}
	}
	if fileMount.SecretRef != nil && fileMount.SecretRef.Name != "" {
		return &corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  fileMount.SecretRef.Name,
				Items:       items,
				DefaultMode: fileMount.DefaultMode,
			},
		}
	}
	return nil
}

func fileMountProjectionItems(items []hermesv1alpha1.HermesAgentFileProjectionItem) []corev1.KeyToPath {
	if len(items) == 0 {
		return nil
	}

	projected := make([]corev1.KeyToPath, 0, len(items))
	for _, item := range items {
		projected = append(projected, corev1.KeyToPath{
			Key:  item.Key,
			Path: item.Path,
			Mode: item.Mode,
		})
	}
	return projected
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
			Name:        agent.Name,
			Namespace:   agent.Namespace,
			Labels:      labels,
			Annotations: maps.Clone(agent.Spec.Service.Annotations),
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType(agent),
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Port:       port,
				TargetPort: intstr.FromInt32(serviceTargetPort(agent)),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

func buildNetworkPolicy(agent *hermesv1alpha1.HermesAgent, terminalBackend string) *networkingv1.NetworkPolicy {
	labels := resourceLabels(agent)
	destinations := networkPolicyPeers(agent.Spec.NetworkPolicy.Destinations)
	egress := []networkingv1.NetworkPolicyEgressRule{
		networkPolicyRule(nil,
			append(networkPolicyPorts(corev1.ProtocolUDP, []int32{networkPolicyDNSPort}), networkPolicyPorts(corev1.ProtocolTCP, []int32{networkPolicyDNSPort})...)...,
		),
		networkPolicyRule(destinations, networkPolicyPorts(corev1.ProtocolTCP, []int32{networkPolicyHTTPPort, networkPolicyHTTPSPort})...),
	}

	defaultTCPPorts := networkPolicyPortSet(networkPolicyDNSPort, networkPolicyHTTPPort, networkPolicyHTTPSPort)
	defaultUDPPorts := networkPolicyPortSet(networkPolicyDNSPort)
	if terminalBackend == terminalBackendSSH {
		defaultTCPPorts[networkPolicySSHPort] = struct{}{}
		egress = append(egress, networkPolicyRule(destinations, networkPolicyPorts(corev1.ProtocolTCP, []int32{networkPolicySSHPort})...))
	}

	if ports := additionalNetworkPolicyPorts(agent.Spec.NetworkPolicy.AdditionalTCPPorts, defaultTCPPorts); len(ports) > 0 {
		egress = append(egress, networkPolicyRule(destinations, networkPolicyPorts(corev1.ProtocolTCP, ports)...))
	}
	if ports := additionalNetworkPolicyPorts(agent.Spec.NetworkPolicy.AdditionalUDPPorts, defaultUDPPorts); len(ports) > 0 {
		egress = append(egress, networkPolicyRule(destinations, networkPolicyPorts(corev1.ProtocolUDP, ports)...))
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
	selectorLabels := resourceLabels(agent)
	podLabels := managedPodLabels(agent)
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

	containerPorts := []corev1.ContainerPort{}
	if serviceEnabled(agent) {
		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          "service",
			ContainerPort: serviceTargetPort(agent),
			Protocol:      corev1.ProtocolTCP,
		})
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    selectorLabels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: agent.Name,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: managedPodAnnotations(agent, inputs.Annotations),
				},
				Spec: corev1.PodSpec{
					SecurityContext:              hermesPodSecurityContext(),
					ImagePullSecrets:             append([]corev1.LocalObjectReference{}, agent.Spec.ImagePullSecrets...),
					ServiceAccountName:           agent.Spec.ServiceAccountName,
					AutomountServiceAccountToken: automountServiceAccountToken(agent),
					NodeSelector:                 maps.Clone(agent.Spec.NodeSelector),
					Tolerations:                  append([]corev1.Toleration{}, agent.Spec.Tolerations...),
					Affinity:                     agent.Spec.Affinity.DeepCopy(),
					TopologySpreadConstraints:    append([]corev1.TopologySpreadConstraint{}, agent.Spec.TopologySpreadConstraints...),
					Containers: []corev1.Container{{
						Name:            hermesContainerName,
						Image:           hermesImage(agent.Spec.Image),
						ImagePullPolicy: agent.Spec.Image.PullPolicy,
						Args:            hermesArgs(agent),
						Env:             inputs.Env,
						EnvFrom:         inputs.EnvFrom,
						Ports:           containerPorts,
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
		Files            []resolvedConfigFile                      `json:"files"`
		Env              []corev1.EnvVar                           `json:"env"`
		EnvFrom          []corev1.EnvFromSource                    `json:"envFrom"`
		SecretRefs       []corev1.LocalObjectReference             `json:"secretRefs"`
		FileMounts       []hermesv1alpha1.HermesAgentFileMountSpec `json:"fileMounts"`
		ReferencedInputs referencedInputState                      `json:"referencedInputs,omitempty"`
	}{
		Files:            plan.Files,
		Env:              agent.Spec.Env,
		EnvFrom:          agent.Spec.EnvFrom,
		SecretRefs:       agent.Spec.SecretRefs,
		FileMounts:       agent.Spec.FileMounts,
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
		snapshot.Data = copyConfigMapData(configMap)
	}
	return snapshot
}

func newConfigMapProjectionSnapshot(name string, items []hermesv1alpha1.HermesAgentFileProjectionItem, configMap *corev1.ConfigMap) referencedObjectSnapshot {
	snapshot := referencedObjectSnapshot{
		Kind:    "ConfigMap",
		Name:    name,
		Present: configMap != nil,
	}
	if configMap == nil {
		return snapshot
	}
	if len(items) == 0 {
		snapshot.Data = copyConfigMapData(configMap)
		return snapshot
	}

	snapshot.KeyFound = true
	snapshot.Data = map[string]string{}
	for _, item := range items {
		value, ok := configMapFileValue(configMap, item.Key)
		if !ok {
			snapshot.KeyFound = false
			snapshot.MissingKeys = append(snapshot.MissingKeys, item.Key)
			continue
		}
		snapshot.Data[item.Key] = value
	}
	if len(snapshot.Data) == 0 {
		snapshot.Data = nil
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

func newSecretFileSnapshot(name, key string, secret *corev1.Secret) referencedObjectSnapshot {
	return newSecretKeySnapshot(name, key, false, secret)
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

func newSecretProjectionSnapshot(name string, items []hermesv1alpha1.HermesAgentFileProjectionItem, secret *corev1.Secret) referencedObjectSnapshot {
	snapshot := referencedObjectSnapshot{
		Kind:    "Secret",
		Name:    name,
		Present: secret != nil,
	}
	if secret == nil {
		return snapshot
	}
	if len(items) == 0 {
		snapshot.Data = encodeSecretData(secret.Data)
		return snapshot
	}

	snapshot.KeyFound = true
	snapshot.Data = map[string]string{}
	for _, item := range items {
		value, ok := secret.Data[item.Key]
		if !ok {
			snapshot.KeyFound = false
			snapshot.MissingKeys = append(snapshot.MissingKeys, item.Key)
			continue
		}
		snapshot.Data[item.Key] = base64.StdEncoding.EncodeToString(value)
	}
	if len(snapshot.Data) == 0 {
		snapshot.Data = nil
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

func copyBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func automountServiceAccountToken(agent *hermesv1alpha1.HermesAgent) *bool {
	if agent.Spec.AutomountServiceAccountToken == nil {
		value := false
		return &value
	}
	return copyBoolPtr(agent.Spec.AutomountServiceAccountToken)
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	copied := make(map[string]string, len(values))
	maps.Copy(copied, values)
	return copied
}

func copyConfigMapData(configMap *corev1.ConfigMap) map[string]string {
	values := copyStringMap(configMap.Data)
	if len(configMap.BinaryData) == 0 {
		return values
	}
	if values == nil {
		values = map[string]string{}
	}
	for key, value := range configMap.BinaryData {
		values[key] = base64.StdEncoding.EncodeToString(value)
	}
	return values
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

func managedPodLabels(agent *hermesv1alpha1.HermesAgent) map[string]string {
	return mergeStringMaps(agent.Spec.PodLabels, resourceLabels(agent))
}

func managedPodAnnotations(agent *hermesv1alpha1.HermesAgent, inputs map[string]string) map[string]string {
	return mergeStringMaps(agent.Spec.PodAnnotations, inputs)
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
	readOnlyRootFilesystem := true

	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
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

	checks := []string{
		probeProcessCheck(),
		probeGatewayStateCheck("running"),
	}
	if agent.Spec.Probes.RequireConnectedPlatform {
		checks = append(checks, probeAnyConnectedPlatformConnectedCheck())
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
	return fmt.Sprintf("test -s %[1]s && pid=$(tr -d '\\n' < %[1]s | grep -Eo '\"pid\"[[:space:]]*:[[:space:]]*[0-9]+' | grep -Eo '[0-9]+' | head -n1) && [ -n \"$pid\" ] && kill -0 \"$pid\"", pidPath)
}

func probeGatewayStateCheck(expectedState string) string {
	statePath := shellQuote(hermesGatewayStatePath())
	return fmt.Sprintf("test -s %[1]s && tr -d '\\n' < %[1]s | grep -Eq '\"gateway_state\"[[:space:]]*:[[:space:]]*\"%s\"'", statePath, expectedState)
}

func probeAnyConnectedPlatformConnectedCheck() string {
	statePath := shellQuote(hermesGatewayStatePath())
	return fmt.Sprintf("test -s %[1]s && tr -d '\\n' < %[1]s | grep -Eq '\"platforms\"[[:space:]]*:[[:space:]]*\\{.*\"state\"[[:space:]]*:[[:space:]]*\"connected\"'", statePath)
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

func effectiveTerminalBackend(agent *hermesv1alpha1.HermesAgent, referencedInputs referencedInputState) string {
	if backend, ok := configSourceTerminalBackend(agent.Spec.Config, referencedInputs); ok {
		return backend
	}
	return agent.Spec.Terminal.Backend
}

func configSourceTerminalBackend(source hermesv1alpha1.HermesAgentConfigSource, referencedInputs referencedInputState) (string, bool) {
	if source.Raw != "" {
		return terminalBackendFromConfigContent(source.Raw)
	}

	kind := ""
	name := ""
	key := ""
	switch {
	case source.ConfigMapRef != nil && source.ConfigMapRef.Name != "" && source.ConfigMapRef.Key != "":
		kind = "ConfigMap"
		name = source.ConfigMapRef.Name
		key = source.ConfigMapRef.Key
	case source.SecretRef != nil && source.SecretRef.Name != "" && source.SecretRef.Key != "":
		kind = "Secret"
		name = source.SecretRef.Name
		key = source.SecretRef.Key
	default:
		return "", false
	}

	for _, snapshot := range referencedInputs.FileRefs {
		if snapshot.Kind != kind || snapshot.Name != name || snapshot.Key != key {
			continue
		}
		if !snapshot.Present || !snapshot.KeyFound {
			return "", false
		}
		content := snapshot.Data[key]
		if snapshot.Kind == "Secret" {
			decoded, err := base64.StdEncoding.DecodeString(content)
			if err != nil {
				return "", false
			}
			content = string(decoded)
		}
		return terminalBackendFromConfigContent(content)
	}
	return "", false
}

func terminalBackendFromConfigContent(content string) (string, bool) {
	var config struct {
		Terminal struct {
			Backend string `yaml:"backend"`
		} `yaml:"terminal"`
	}

	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return "", false
	}
	if config.Terminal.Backend == "" {
		return "", false
	}
	return config.Terminal.Backend, true
}

func additionalNetworkPolicyPorts(ports []int32, existing map[int32]struct{}) []int32 {
	unique := map[int32]struct{}{}
	for _, port := range ports {
		if _, ok := existing[port]; ok {
			continue
		}
		unique[port] = struct{}{}
	}

	ordered := make([]int32, 0, len(unique))
	for port := range unique {
		ordered = append(ordered, port)
	}
	slices.Sort(ordered)
	return ordered
}

func networkPolicyPortSet(ports ...int32) map[int32]struct{} {
	set := make(map[int32]struct{}, len(ports))
	for _, port := range ports {
		set[port] = struct{}{}
	}
	return set
}

func networkPolicyRule(to []networkingv1.NetworkPolicyPeer, ports ...networkingv1.NetworkPolicyPort) networkingv1.NetworkPolicyEgressRule {
	return networkingv1.NetworkPolicyEgressRule{To: to, Ports: ports}
}

func networkPolicyPeers(peers []hermesv1alpha1.HermesAgentNetworkPolicyPeer) []networkingv1.NetworkPolicyPeer {
	if len(peers) == 0 {
		return nil
	}

	converted := make([]networkingv1.NetworkPolicyPeer, 0, len(peers))
	for _, peer := range peers {
		converted = append(converted, networkingv1.NetworkPolicyPeer{
			IPBlock:           networkPolicyIPBlock(peer),
			NamespaceSelector: peer.NamespaceSelector.DeepCopy(),
			PodSelector:       peer.PodSelector.DeepCopy(),
		})
	}
	return converted
}

func networkPolicyIPBlock(peer hermesv1alpha1.HermesAgentNetworkPolicyPeer) *networkingv1.IPBlock {
	if peer.CIDR == "" {
		return nil
	}
	return &networkingv1.IPBlock{
		CIDR:   peer.CIDR,
		Except: append([]string{}, peer.Except...),
	}
}

func networkPolicyPorts(protocol corev1.Protocol, ports []int32) []networkingv1.NetworkPolicyPort {
	policyPorts := make([]networkingv1.NetworkPolicyPort, 0, len(ports))
	for _, port := range ports {
		policyPorts = append(policyPorts, networkingv1.NetworkPolicyPort{
			Protocol: protocolPtr(protocol),
			Port:     portIntOrString(port),
		})
	}
	return policyPorts
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

func serviceTargetPort(agent *hermesv1alpha1.HermesAgent) int32 {
	if agent.Spec.Service.TargetPort <= 0 {
		return servicePort(agent)
	}
	return agent.Spec.Service.TargetPort
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
