package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	configHashAnnotation = "hermes.nous.ai/config-hash"
	hermesContainerName  = "hermes"
	hermesGatewayMode    = "gateway"
	hermesDataPath       = "/data"
	hermesHomePath       = "/data/hermes"
	hermesSecretBasePath = "/var/run/hermes/secrets"
	hermesDataVolumeName = "hermes-data"
	hermesTmpPath        = "/tmp"
	hermesTmpVolumeName  = "tmp"
	hermesRuntimeUID     = int64(10001)
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

type podTemplateInputs struct {
	Annotations  map[string]string
	Env          []corev1.EnvVar
	EnvFrom      []corev1.EnvFromSource
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
}

func buildConfigPlan(agent *hermesv1alpha1.HermesAgent) (configPlan, error) {
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
	plan.Hash = computeConfigHash(agent, plan)
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
					}},
					Volumes: volumes,
				},
			},
		},
	}
}

func computeConfigHash(agent *hermesv1alpha1.HermesAgent, plan configPlan) string {
	payload := struct {
		Files      []resolvedConfigFile          `json:"files"`
		Env        []corev1.EnvVar               `json:"env"`
		EnvFrom    []corev1.EnvFromSource        `json:"envFrom"`
		SecretRefs []corev1.LocalObjectReference `json:"secretRefs"`
	}{
		Files:      plan.Files,
		Env:        agent.Spec.Env,
		EnvFrom:    agent.Spec.EnvFrom,
		SecretRefs: agent.Spec.SecretRefs,
	}

	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
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
