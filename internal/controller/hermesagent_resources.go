package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"

	corev1 "k8s.io/api/core/v1"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	configHashAnnotation = "hermes.nous.ai/config-hash"
	hermesDataPath       = "/data"
	hermesHomePath       = "/data/hermes"
	hermesSecretBasePath = "/var/run/hermes/secrets"
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

func sourceFieldName(id string) string {
	if id == "config" {
		return "config"
	}
	return "gatewayConfig"
}
