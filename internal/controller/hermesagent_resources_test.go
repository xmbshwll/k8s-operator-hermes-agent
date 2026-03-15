package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	testAgentName            = "example"
	testInlineConfig         = "model: anthropic/claude-opus-4.1\n"
	testUpdatedConfig        = "model: openai/gpt-4.1-mini\n"
	testPersistentVolumeSize = "25Gi"
)

func resourceMustParse(t *testing.T, value string) resource.Quantity {
	t.Helper()

	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		t.Fatalf("ParseQuantity(%q) returned error: %v", value, err)
	}
	return quantity
}

func TestBuildConfigPlanWithInlineConfig(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.Raw = testInlineConfig
	agent.Spec.GatewayConfig.Raw = "{}\n"

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("expected 2 resolved config files, got %d", len(plan.Files))
	}
	if plan.Hash == "" {
		t.Fatal("expected non-empty config hash")
	}
	if plan.Files[0].ConfigMapName != "example-config" {
		t.Fatalf("expected generated config map name example-config, got %s", plan.Files[0].ConfigMapName)
	}
	if plan.Files[1].ConfigMapName != "example-gateway-config" {
		t.Fatalf("expected generated gateway config map name example-gateway-config, got %s", plan.Files[1].ConfigMapName)
	}
}

func TestBuildConfigPlanRejectsMixedSource(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.Raw = testInlineConfig
	agent.Spec.Config.ConfigMapRef = &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "external-config"},
		Key:                  "config.yaml",
	}

	if _, err := buildConfigPlan(agent); err == nil {
		t.Fatal("expected buildConfigPlan to reject raw+configMapRef for spec.config")
	}
}

func TestBuildPodTemplateInputsIncludesConfigHashAndSecretMounts(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.ConfigMapRef = &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config"},
		Key:                  "config.yaml",
	}
	agent.Spec.EnvFrom = []corev1.EnvFromSource{{
		SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "provider-env"}},
	}}
	agent.Spec.SecretRefs = []corev1.LocalObjectReference{{Name: "hermes-secrets"}}

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	inputs := buildPodTemplateInputs(agent, plan)
	if inputs.Annotations[configHashAnnotation] == "" {
		t.Fatal("expected config hash annotation to be present")
	}
	if len(inputs.EnvFrom) != 1 {
		t.Fatalf("expected envFrom to be preserved, got %d entries", len(inputs.EnvFrom))
	}
	if len(inputs.Volumes) != 2 {
		t.Fatalf("expected 2 volumes (config + secret), got %d", len(inputs.Volumes))
	}
	if len(inputs.VolumeMounts) != 2 {
		t.Fatalf("expected 2 volume mounts (config + secret), got %d", len(inputs.VolumeMounts))
	}
	if inputs.Env[0].Name != "HERMES_HOME" || inputs.Env[0].Value != hermesHomePath {
		t.Fatalf("expected HERMES_HOME env var to be injected, got %+v", inputs.Env[0])
	}
}

func TestConfigHashChangesWhenConfigChanges(t *testing.T) {
	base := &hermesv1alpha1.HermesAgent{}
	base.Name = testAgentName
	base.Spec.Config.Raw = testInlineConfig

	updated := base.DeepCopy()
	updated.Spec.Config.Raw = testUpdatedConfig

	basePlan, err := buildConfigPlan(base)
	if err != nil {
		t.Fatalf("buildConfigPlan(base) returned error: %v", err)
	}
	updatedPlan, err := buildConfigPlan(updated)
	if err != nil {
		t.Fatalf("buildConfigPlan(updated) returned error: %v", err)
	}
	if basePlan.Hash == updatedPlan.Hash {
		t.Fatal("expected config hash to change when inline config changes")
	}
}

func TestBuildStatefulSetIncludesConfigHashAnnotation(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.Raw = testInlineConfig

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	statefulSet := buildStatefulSet(agent, buildPodTemplateInputs(agent, plan))
	if statefulSet.Spec.Template.Annotations[configHashAnnotation] != plan.Hash {
		t.Fatalf("expected StatefulSet pod template to include config hash %q, got %q", plan.Hash, statefulSet.Spec.Template.Annotations[configHashAnnotation])
	}
}

func TestBuildStatefulSetUpdatesPodTemplateAnnotationWhenConfigChanges(t *testing.T) {
	base := &hermesv1alpha1.HermesAgent{}
	base.Name = testAgentName
	base.Spec.Config.Raw = testInlineConfig

	updated := base.DeepCopy()
	updated.Spec.Config.Raw = testUpdatedConfig

	basePlan, err := buildConfigPlan(base)
	if err != nil {
		t.Fatalf("buildConfigPlan(base) returned error: %v", err)
	}
	updatedPlan, err := buildConfigPlan(updated)
	if err != nil {
		t.Fatalf("buildConfigPlan(updated) returned error: %v", err)
	}

	baseStatefulSet := buildStatefulSet(base, buildPodTemplateInputs(base, basePlan))
	updatedStatefulSet := buildStatefulSet(updated, buildPodTemplateInputs(updated, updatedPlan))
	if baseStatefulSet.Spec.Template.Annotations[configHashAnnotation] == updatedStatefulSet.Spec.Template.Annotations[configHashAnnotation] {
		t.Fatal("expected StatefulSet pod template annotation to change when config changes")
	}
}

func TestBuildPersistentVolumeClaimUsesStorageSpec(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = "default"
	agent.Spec.Storage.Persistence.Size = testPersistentVolumeSize
	agent.Spec.Storage.Persistence.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	storageClassName := "fast-ssd"
	agent.Spec.Storage.Persistence.StorageClassName = &storageClassName

	persistentVolumeClaim, err := buildPersistentVolumeClaim(agent)
	if err != nil {
		t.Fatalf("buildPersistentVolumeClaim returned error: %v", err)
	}
	if persistentVolumeClaim.Name != "example-data" {
		t.Fatalf("expected PVC name example-data, got %s", persistentVolumeClaim.Name)
	}
	if persistentVolumeClaim.Spec.Resources.Requests.Storage().String() != testPersistentVolumeSize {
		t.Fatalf("expected PVC storage request %s, got %s", testPersistentVolumeSize, persistentVolumeClaim.Spec.Resources.Requests.Storage().String())
	}
	if len(persistentVolumeClaim.Spec.AccessModes) != 1 || persistentVolumeClaim.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Fatalf("expected PVC access mode ReadWriteOnce, got %+v", persistentVolumeClaim.Spec.AccessModes)
	}
	if persistentVolumeClaim.Spec.StorageClassName == nil || *persistentVolumeClaim.Spec.StorageClassName != storageClassName {
		t.Fatalf("expected PVC storageClassName %q, got %+v", storageClassName, persistentVolumeClaim.Spec.StorageClassName)
	}
}

func TestBuildStatefulSetMountsPersistentDataVolume(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.Raw = testInlineConfig

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	statefulSet := buildStatefulSet(agent, buildPodTemplateInputs(agent, plan))
	foundVolume := false
	for _, volume := range statefulSet.Spec.Template.Spec.Volumes {
		if volume.Name == hermesDataVolumeName && volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == persistentVolumeClaimName(agent.Name) {
			foundVolume = true
			break
		}
	}
	if !foundVolume {
		t.Fatal("expected StatefulSet pod spec to include the Hermes data PVC volume")
	}

	foundMount := false
	for _, mount := range statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == hermesDataVolumeName && mount.MountPath == hermesDataPath {
			foundMount = true
			break
		}
	}
	if !foundMount {
		t.Fatal("expected StatefulSet container to mount the Hermes data volume at /data")
	}
}

func TestBuildStatefulSetUsesHermesImageArgsAndResources(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Mode = "gateway"
	agent.Spec.Image.Repository = "ghcr.io/example/hermes-agent"
	agent.Spec.Image.Tag = "gateway-core"
	agent.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	agent.Spec.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resourceMustParse(t, "500m"),
			corev1.ResourceMemory: resourceMustParse(t, "1Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resourceMustParse(t, "2"),
			corev1.ResourceMemory: resourceMustParse(t, "4Gi"),
		},
	}

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	statefulSet := buildStatefulSet(agent, buildPodTemplateInputs(agent, plan))
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 {
		t.Fatalf("expected StatefulSet replicas to be 1, got %+v", statefulSet.Spec.Replicas)
	}

	container := statefulSet.Spec.Template.Spec.Containers[0]
	if container.Name != hermesContainerName {
		t.Fatalf("expected container name %q, got %q", hermesContainerName, container.Name)
	}
	if container.Image != "ghcr.io/example/hermes-agent:gateway-core" {
		t.Fatalf("expected Hermes image ghcr.io/example/hermes-agent:gateway-core, got %q", container.Image)
	}
	if len(container.Args) != 2 || container.Args[0] != "hermes" || container.Args[1] != hermesGatewayMode {
		t.Fatalf("expected Hermes args [hermes gateway], got %+v", container.Args)
	}
	if container.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Fatalf("expected image pull policy %q, got %q", corev1.PullIfNotPresent, container.ImagePullPolicy)
	}
	if container.Resources.Requests.Cpu().String() != "500m" {
		t.Fatalf("expected CPU request 500m, got %s", container.Resources.Requests.Cpu().String())
	}
	if container.Resources.Limits.Memory().String() != "4Gi" {
		t.Fatalf("expected memory limit 4Gi, got %s", container.Resources.Limits.Memory().String())
	}
}
