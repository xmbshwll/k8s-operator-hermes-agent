package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	testAgentName            = "example"
	testNamespace            = "default"
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

func requireHermesPodSecurityContext(t *testing.T, podSpec corev1.PodSpec) {
	t.Helper()

	if podSpec.SecurityContext == nil {
		t.Fatal("expected pod security context to be configured")
	}
	if podSpec.SecurityContext.RunAsNonRoot == nil || !*podSpec.SecurityContext.RunAsNonRoot {
		t.Fatal("expected pod to run as non-root")
	}
	if podSpec.SecurityContext.RunAsUser == nil || *podSpec.SecurityContext.RunAsUser != hermesRuntimeUID {
		t.Fatalf("expected pod runAsUser %d, got %+v", hermesRuntimeUID, podSpec.SecurityContext.RunAsUser)
	}
	if podSpec.SecurityContext.FSGroup == nil || *podSpec.SecurityContext.FSGroup != hermesRuntimeUID {
		t.Fatalf("expected pod fsGroup %d, got %+v", hermesRuntimeUID, podSpec.SecurityContext.FSGroup)
	}
	if podSpec.SecurityContext.SeccompProfile == nil || podSpec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("expected pod seccomp profile RuntimeDefault, got %+v", podSpec.SecurityContext.SeccompProfile)
	}
}

func requireHermesContainerSecurityContext(t *testing.T, container corev1.Container) {
	t.Helper()

	if container.SecurityContext == nil {
		t.Fatal("expected container security context to be configured")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil || *container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("expected allowPrivilegeEscalation to be false")
	}
	if container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("expected readOnlyRootFilesystem to be true")
	}
	if container.SecurityContext.Capabilities == nil {
		t.Fatal("expected container capabilities to be configured")
	}
	if len(container.SecurityContext.Capabilities.Drop) != 1 || container.SecurityContext.Capabilities.Drop[0] != corev1.Capability("ALL") {
		t.Fatalf("expected container to drop all capabilities, got %+v", container.SecurityContext.Capabilities)
	}
}

func requireVolumeMount(t *testing.T, mounts []corev1.VolumeMount, name, mountPath string) {
	t.Helper()

	for _, mount := range mounts {
		if mount.Name == name && mount.MountPath == mountPath {
			return
		}
	}

	t.Fatalf("expected volume mount %s at %s", name, mountPath)
}

func requireEmptyDirVolume(t *testing.T, volumes []corev1.Volume, name string) {
	t.Helper()

	for _, volume := range volumes {
		if volume.Name == name && volume.EmptyDir != nil {
			return
		}
	}

	t.Fatalf("expected emptyDir volume %s", name)
}

func requireExecProbe(t *testing.T, probe *corev1.Probe, commandParts ...string) {
	t.Helper()

	if probe == nil {
		t.Fatal("expected probe to be configured")
	}
	if probe.Exec == nil {
		t.Fatalf("expected exec probe, got %+v", probe)
	}
	if len(probe.Exec.Command) != 3 || probe.Exec.Command[0] != "bash" || probe.Exec.Command[1] != "-ec" {
		t.Fatalf("expected probe command [bash -ec <script>], got %+v", probe.Exec.Command)
	}
	for _, part := range commandParts {
		if !strings.Contains(probe.Exec.Command[2], part) {
			t.Fatalf("expected probe command %q to contain %q", probe.Exec.Command[2], part)
		}
	}
}

func requireExecProbeExcludes(t *testing.T, probe *corev1.Probe, commandParts ...string) {
	t.Helper()

	if probe == nil || probe.Exec == nil {
		t.Fatalf("expected exec probe, got %+v", probe)
	}
	for _, part := range commandParts {
		if strings.Contains(probe.Exec.Command[2], part) {
			t.Fatalf("expected probe command %q not to contain %q", probe.Exec.Command[2], part)
		}
	}
}

func requireStatusCondition(t *testing.T, status hermesv1alpha1.HermesAgentStatus, conditionType string, conditionStatus metav1.ConditionStatus, reason string) {
	t.Helper()

	for _, condition := range status.Conditions {
		if condition.Type != conditionType {
			continue
		}
		if condition.Status != conditionStatus {
			t.Fatalf("expected %s status %s, got %s", conditionType, conditionStatus, condition.Status)
		}
		if reason != "" && condition.Reason != reason {
			t.Fatalf("expected %s reason %s, got %s", conditionType, reason, condition.Reason)
		}
		if condition.Message == "" {
			t.Fatalf("expected %s message to be populated", conditionType)
		}
		return
	}

	t.Fatalf("expected condition %s", conditionType)
}

func requireConditionObservedGeneration(t *testing.T, status hermesv1alpha1.HermesAgentStatus, conditionType string, generation int64) {
	t.Helper()

	for _, condition := range status.Conditions {
		if condition.Type != conditionType {
			continue
		}
		if condition.ObservedGeneration != generation {
			t.Fatalf("expected %s observedGeneration %d, got %d", conditionType, generation, condition.ObservedGeneration)
		}
		return
	}

	t.Fatalf("expected condition %s", conditionType)
}

func requireNetworkPolicyPort(t *testing.T, ports []networkingv1.NetworkPolicyPort, protocol corev1.Protocol, port int32) {
	t.Helper()

	for _, policyPort := range ports {
		if policyPort.Protocol == nil || *policyPort.Protocol != protocol {
			continue
		}
		if policyPort.Port == nil || policyPort.Port.IntVal != port {
			continue
		}
		return
	}

	t.Fatalf("expected NetworkPolicy port %s/%d in %+v", protocol, port, ports)
}

func requireNetworkPolicyCIDRPeer(t *testing.T, peers []networkingv1.NetworkPolicyPeer, cidr string, except ...string) {
	t.Helper()

	for _, peer := range peers {
		if peer.IPBlock == nil || peer.IPBlock.CIDR != cidr {
			continue
		}
		if len(peer.IPBlock.Except) != len(except) {
			t.Fatalf("expected peer %s except %+v, got %+v", cidr, except, peer.IPBlock.Except)
		}
		for i := range except {
			if peer.IPBlock.Except[i] != except[i] {
				t.Fatalf("expected peer %s except %+v, got %+v", cidr, except, peer.IPBlock.Except)
			}
		}
		return
	}

	t.Fatalf("expected NetworkPolicy peer with cidr %s in %+v", cidr, peers)
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

func TestBuildConfigPlanWithSecretBackedConfig(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.SecretRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config-secret"},
		Key:                  "config.yaml",
	}

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}
	if len(plan.Files) != 1 {
		t.Fatalf("expected 1 resolved config file, got %d", len(plan.Files))
	}
	if plan.Files[0].SecretName != "shared-config-secret" {
		t.Fatalf("expected secret-backed config source, got %+v", plan.Files[0])
	}
	if plan.Files[0].SourceKey != "config.yaml" {
		t.Fatalf("expected source key config.yaml, got %q", plan.Files[0].SourceKey)
	}
}

func TestBuildPodTemplateInputsUsesSecretVolumeForSecretBackedConfig(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.SecretRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config-secret"},
		Key:                  "config.yaml",
	}

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}
	inputs := buildPodTemplateInputs(agent, plan)
	if len(inputs.Volumes) != 1 {
		t.Fatalf("expected 1 config volume, got %d", len(inputs.Volumes))
	}
	if inputs.Volumes[0].Secret == nil || inputs.Volumes[0].Secret.SecretName != "shared-config-secret" {
		t.Fatalf("expected secret-backed config volume, got %+v", inputs.Volumes[0])
	}
	if len(inputs.Volumes[0].Secret.Items) != 1 || inputs.Volumes[0].Secret.Items[0].Key != "config.yaml" {
		t.Fatalf("expected secret-backed config key mapping, got %+v", inputs.Volumes[0].Secret.Items)
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

func TestBuildConfigPlanRejectsInvalidFileMounts(t *testing.T) {
	invalidMode := int32(0o1000)
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.FileMounts = []hermesv1alpha1.HermesAgentFileMountSpec{{
		MountPath:    "/var/run/hermes/plugins",
		ConfigMapRef: &corev1.LocalObjectReference{Name: "plugins"},
		SecretRef:    &corev1.LocalObjectReference{Name: "ssh-auth"},
	}, {
		MountPath:   "/var/run/hermes/ssh",
		SecretRef:   &corev1.LocalObjectReference{Name: "ssh-auth"},
		DefaultMode: &invalidMode,
		Items: []hermesv1alpha1.HermesAgentFileProjectionItem{{
			Key:  "id_ed25519",
			Path: "../id_ed25519",
		}},
	}}

	if _, err := buildConfigPlan(agent); err == nil {
		t.Fatal("expected buildConfigPlan to reject invalid fileMounts")
	}
}

func TestBuildPodTemplateInputsIncludesConfigHashAndMountedInputs(t *testing.T) {
	defaultMode := int32(0o444)
	privateKeyMode := int32(0o600)
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
	agent.Spec.FileMounts = []hermesv1alpha1.HermesAgentFileMountSpec{{
		MountPath:    "/var/run/hermes/plugins",
		ConfigMapRef: &corev1.LocalObjectReference{Name: "hermes-plugins"},
		Items: []hermesv1alpha1.HermesAgentFileProjectionItem{{
			Key:  "plugin.py",
			Path: "bundle/plugin.py",
		}},
	}, {
		MountPath:   "/var/run/hermes/ssh",
		SecretRef:   &corev1.LocalObjectReference{Name: "ssh-auth"},
		DefaultMode: &defaultMode,
		Items: []hermesv1alpha1.HermesAgentFileProjectionItem{{
			Key:  "id_ed25519",
			Path: "id_ed25519",
			Mode: &privateKeyMode,
		}, {
			Key:  "known_hosts",
			Path: "known_hosts",
		}},
	}}

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
	if len(inputs.Volumes) != 4 {
		t.Fatalf("expected 4 volumes (config + secret + 2 file mounts), got %d", len(inputs.Volumes))
	}
	if len(inputs.VolumeMounts) != 4 {
		t.Fatalf("expected 4 volume mounts (config + secret + 2 file mounts), got %d", len(inputs.VolumeMounts))
	}
	if inputs.Env[0].Name != "HERMES_HOME" || inputs.Env[0].Value != hermesHomePath {
		t.Fatalf("expected HERMES_HOME env var to be injected, got %+v", inputs.Env[0])
	}
	requireVolumeMount(t, inputs.VolumeMounts, "file-mount-0", "/var/run/hermes/plugins")
	requireVolumeMount(t, inputs.VolumeMounts, "file-mount-1", "/var/run/hermes/ssh")
	if inputs.Volumes[2].ConfigMap == nil || len(inputs.Volumes[2].ConfigMap.Items) != 1 {
		t.Fatalf("expected projected ConfigMap items, got %+v", inputs.Volumes[2])
	}
	if inputs.Volumes[2].ConfigMap.Items[0].Path != "bundle/plugin.py" {
		t.Fatalf("expected plugin file to be projected to bundle/plugin.py, got %+v", inputs.Volumes[2].ConfigMap.Items)
	}
	if inputs.Volumes[3].Secret == nil || len(inputs.Volumes[3].Secret.Items) != 2 {
		t.Fatalf("expected projected Secret items, got %+v", inputs.Volumes[3])
	}
	if inputs.Volumes[3].Secret.DefaultMode == nil || *inputs.Volumes[3].Secret.DefaultMode != defaultMode {
		t.Fatalf("expected secret file mount defaultMode %o, got %+v", defaultMode, inputs.Volumes[3].Secret.DefaultMode)
	}
	if inputs.Volumes[3].Secret.Items[0].Mode == nil || *inputs.Volumes[3].Secret.Items[0].Mode != privateKeyMode {
		t.Fatalf("expected private key mode %o, got %+v", privateKeyMode, inputs.Volumes[3].Secret.Items[0].Mode)
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

func TestConfigHashChangesWhenReferencedConfigMapContentChanges(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.ConfigMapRef = &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config"},
		Key:                  "config.yaml",
	}

	basePlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileRefs: []referencedObjectSnapshot{newConfigMapFileSnapshot("shared-config", "config.yaml", &corev1.ConfigMap{Data: map[string]string{"config.yaml": testInlineConfig}})},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(base) returned error: %v", err)
	}
	updatedPlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileRefs: []referencedObjectSnapshot{newConfigMapFileSnapshot("shared-config", "config.yaml", &corev1.ConfigMap{Data: map[string]string{"config.yaml": testUpdatedConfig}})},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(updated) returned error: %v", err)
	}
	if basePlan.Hash == updatedPlan.Hash {
		t.Fatal("expected config hash to change when referenced ConfigMap content changes")
	}
}

func TestConfigHashChangesWhenReferencedSecretBackedConfigChanges(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Config.SecretRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config-secret"},
		Key:                  "config.yaml",
	}

	basePlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileRefs: []referencedObjectSnapshot{newSecretFileSnapshot("shared-config-secret", "config.yaml", &corev1.Secret{Data: map[string][]byte{"config.yaml": []byte(testInlineConfig)}})},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(base) returned error: %v", err)
	}
	updatedPlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileRefs: []referencedObjectSnapshot{newSecretFileSnapshot("shared-config-secret", "config.yaml", &corev1.Secret{Data: map[string][]byte{"config.yaml": []byte(testUpdatedConfig)}})},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(updated) returned error: %v", err)
	}
	if basePlan.Hash == updatedPlan.Hash {
		t.Fatal("expected config hash to change when referenced Secret-backed config changes")
	}
}

func TestConfigHashChangesWhenReferencedSecretContentChanges(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Env = []corev1.EnvVar{{
		Name: "API_TOKEN",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "provider-secret"},
				Key:                  "token",
			},
		},
	}}
	agent.Spec.EnvFrom = []corev1.EnvFromSource{{
		SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "provider-env"}},
	}}
	agent.Spec.SecretRefs = []corev1.LocalObjectReference{{Name: "ssh-auth"}}

	basePlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		EnvValueRefs: []referencedObjectSnapshot{newSecretKeySnapshot("provider-secret", "token", false, &corev1.Secret{Data: map[string][]byte{"token": []byte("alpha")}})},
		EnvFrom:      []referencedObjectSnapshot{newSecretSnapshot("provider-env", false, &corev1.Secret{Data: map[string][]byte{"TOKEN": []byte("alpha")}})},
		SecretRefs:   []referencedObjectSnapshot{newSecretSnapshot("ssh-auth", false, &corev1.Secret{Data: map[string][]byte{"id_ed25519": []byte("first")}})},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(base) returned error: %v", err)
	}
	updatedPlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		EnvValueRefs: []referencedObjectSnapshot{newSecretKeySnapshot("provider-secret", "token", false, &corev1.Secret{Data: map[string][]byte{"token": []byte("beta")}})},
		EnvFrom:      []referencedObjectSnapshot{newSecretSnapshot("provider-env", false, &corev1.Secret{Data: map[string][]byte{"TOKEN": []byte("beta")}})},
		SecretRefs:   []referencedObjectSnapshot{newSecretSnapshot("ssh-auth", false, &corev1.Secret{Data: map[string][]byte{"id_ed25519": []byte("second")}})},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(updated) returned error: %v", err)
	}
	if basePlan.Hash == updatedPlan.Hash {
		t.Fatal("expected config hash to change when referenced Secret content changes")
	}
}

func TestConfigHashChangesWhenFileMountContentChanges(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.FileMounts = []hermesv1alpha1.HermesAgentFileMountSpec{{
		MountPath:    "/var/run/hermes/plugins",
		ConfigMapRef: &corev1.LocalObjectReference{Name: "hermes-plugins"},
	}, {
		MountPath: "/var/run/hermes/ssh",
		SecretRef: &corev1.LocalObjectReference{Name: "ssh-auth"},
	}}

	basePlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileMountRefs: []referencedObjectSnapshot{
			newConfigMapProjectionSnapshot("hermes-plugins", nil, &corev1.ConfigMap{Data: map[string]string{"plugin.py": "first"}}),
			newSecretProjectionSnapshot("ssh-auth", nil, &corev1.Secret{Data: map[string][]byte{"id_ed25519": []byte("first")}}),
		},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(base) returned error: %v", err)
	}
	updatedPlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileMountRefs: []referencedObjectSnapshot{
			newConfigMapProjectionSnapshot("hermes-plugins", nil, &corev1.ConfigMap{Data: map[string]string{"plugin.py": "second"}}),
			newSecretProjectionSnapshot("ssh-auth", nil, &corev1.Secret{Data: map[string][]byte{"id_ed25519": []byte("second")}}),
		},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(updated) returned error: %v", err)
	}
	if basePlan.Hash == updatedPlan.Hash {
		t.Fatal("expected config hash to change when file mount content changes")
	}
}

func TestConfigHashIgnoresUnselectedFileMountKeys(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.FileMounts = []hermesv1alpha1.HermesAgentFileMountSpec{{
		MountPath:    "/var/run/hermes/plugins",
		ConfigMapRef: &corev1.LocalObjectReference{Name: "hermes-plugins"},
		Items: []hermesv1alpha1.HermesAgentFileProjectionItem{{
			Key:  "plugin.py",
			Path: "plugin.py",
		}},
	}}

	basePlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileMountRefs: []referencedObjectSnapshot{
			newConfigMapProjectionSnapshot("hermes-plugins", agent.Spec.FileMounts[0].Items, &corev1.ConfigMap{Data: map[string]string{"plugin.py": "first", "README.md": "alpha"}}),
		},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(base) returned error: %v", err)
	}
	updatedPlan, err := buildConfigPlanWithReferences(agent, referencedInputState{
		FileMountRefs: []referencedObjectSnapshot{
			newConfigMapProjectionSnapshot("hermes-plugins", agent.Spec.FileMounts[0].Items, &corev1.ConfigMap{Data: map[string]string{"plugin.py": "first", "README.md": "beta"}}),
		},
	})
	if err != nil {
		t.Fatalf("buildConfigPlanWithReferences(updated) returned error: %v", err)
	}
	if basePlan.Hash != updatedPlan.Hash {
		t.Fatal("expected config hash to ignore unselected file mount keys")
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
	agent.Namespace = testNamespace
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

func TestBuildServiceUsesDefaults(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = testNamespace

	service := buildService(agent)
	if service.Name != testAgentName {
		t.Fatalf("expected Service name %q, got %q", testAgentName, service.Name)
	}
	if service.Namespace != testNamespace {
		t.Fatalf("expected Service namespace %s, got %q", testNamespace, service.Namespace)
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("expected Service type %q, got %q", corev1.ServiceTypeClusterIP, service.Spec.Type)
	}
	if len(service.Spec.Ports) != 1 {
		t.Fatalf("expected exactly 1 Service port, got %d", len(service.Spec.Ports))
	}
	if service.Spec.Ports[0].Port != 8080 {
		t.Fatalf("expected Service port 8080, got %d", service.Spec.Ports[0].Port)
	}
	if service.Spec.Ports[0].TargetPort.IntVal != 8080 {
		t.Fatalf("expected Service targetPort 8080, got %+v", service.Spec.Ports[0].TargetPort)
	}
	if service.Spec.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("expected Service protocol TCP, got %q", service.Spec.Ports[0].Protocol)
	}
	if len(service.Spec.Selector) == 0 {
		t.Fatal("expected Service selector to be populated")
	}
	if service.Spec.Selector["app.kubernetes.io/instance"] != testAgentName {
		t.Fatalf("expected Service selector instance label %q, got %+v", testAgentName, service.Spec.Selector)
	}
}

func TestBuildServiceUsesExplicitSpec(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = testNamespace
	agent.Spec.Service.Type = corev1.ServiceTypeNodePort
	agent.Spec.Service.Port = 9443
	agent.Spec.Service.Annotations = map[string]string{"prometheus.io/scrape": "true"}

	service := buildService(agent)
	if service.Spec.Type != corev1.ServiceTypeNodePort {
		t.Fatalf("expected Service type %q, got %q", corev1.ServiceTypeNodePort, service.Spec.Type)
	}
	if service.Spec.Ports[0].Port != 9443 {
		t.Fatalf("expected Service port 9443, got %d", service.Spec.Ports[0].Port)
	}
	if service.Spec.Ports[0].TargetPort.IntVal != 9443 {
		t.Fatalf("expected Service targetPort 9443, got %+v", service.Spec.Ports[0].TargetPort)
	}
	if service.Annotations["prometheus.io/scrape"] != "true" {
		t.Fatalf("expected Service annotation prometheus.io/scrape=true, got %+v", service.Annotations)
	}
}

func TestNetworkPolicyEnabledDefaultsToFalse(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	if networkPolicyEnabled(agent) {
		t.Fatal("expected NetworkPolicy to default to disabled")
	}

	enabled := true
	agent.Spec.NetworkPolicy.Enabled = &enabled
	if !networkPolicyEnabled(agent) {
		t.Fatal("expected NetworkPolicy helper to report explicit enablement")
	}
}

func TestBuildNetworkPolicyUsesEgressOnlyDefaults(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = testNamespace

	networkPolicy := buildNetworkPolicy(agent, effectiveTerminalBackend(agent, referencedInputState{}))
	if networkPolicy.Name != testAgentName {
		t.Fatalf("expected NetworkPolicy name %q, got %q", testAgentName, networkPolicy.Name)
	}
	if networkPolicy.Namespace != testNamespace {
		t.Fatalf("expected NetworkPolicy namespace %s, got %q", testNamespace, networkPolicy.Namespace)
	}
	if len(networkPolicy.Spec.PolicyTypes) != 1 || networkPolicy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Fatalf("expected egress-only policy type, got %+v", networkPolicy.Spec.PolicyTypes)
	}
	if len(networkPolicy.Spec.PodSelector.MatchLabels) == 0 {
		t.Fatal("expected NetworkPolicy pod selector labels to be populated")
	}
	if networkPolicy.Spec.PodSelector.MatchLabels["app.kubernetes.io/instance"] != testAgentName {
		t.Fatalf("expected NetworkPolicy selector instance label %q, got %+v", testAgentName, networkPolicy.Spec.PodSelector.MatchLabels)
	}
	if len(networkPolicy.Spec.Egress) != 2 {
		t.Fatalf("expected 2 default egress rules, got %d", len(networkPolicy.Spec.Egress))
	}

	dnsRule := networkPolicy.Spec.Egress[0]
	if len(dnsRule.To) != 0 {
		t.Fatalf("expected DNS rule to allow port-based egress without destination selectors, got %+v", dnsRule.To)
	}
	requireNetworkPolicyPort(t, dnsRule.Ports, corev1.ProtocolUDP, networkPolicyDNSPort)
	requireNetworkPolicyPort(t, dnsRule.Ports, corev1.ProtocolTCP, networkPolicyDNSPort)

	webRule := networkPolicy.Spec.Egress[1]
	if len(webRule.To) != 0 {
		t.Fatalf("expected web egress rule to allow port-based egress without destination selectors, got %+v", webRule.To)
	}
	requireNetworkPolicyPort(t, webRule.Ports, corev1.ProtocolTCP, networkPolicyHTTPPort)
	requireNetworkPolicyPort(t, webRule.Ports, corev1.ProtocolTCP, networkPolicyHTTPSPort)
}

func TestBuildNetworkPolicyUsesDestinationAwarePeersWhenConfigured(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = testNamespace
	agent.Spec.Terminal.Backend = terminalBackendSSH
	agent.Spec.NetworkPolicy.Destinations = []hermesv1alpha1.HermesAgentNetworkPolicyPeer{{
		CIDR:   "203.0.113.0/24",
		Except: []string{"203.0.113.128/25"},
	}, {
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "shared-services"}},
		PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "proxy"}},
	}}
	agent.Spec.NetworkPolicy.AdditionalTCPPorts = []int32{8443}
	agent.Spec.NetworkPolicy.AdditionalUDPPorts = []int32{3478}

	networkPolicy := buildNetworkPolicy(agent, effectiveTerminalBackend(agent, referencedInputState{}))
	if len(networkPolicy.Spec.Egress) != 5 {
		t.Fatalf("expected 5 egress rules with destination-aware peers, got %d", len(networkPolicy.Spec.Egress))
	}

	dnsRule := networkPolicy.Spec.Egress[0]
	if len(dnsRule.To) != 0 {
		t.Fatalf("expected DNS rule to stay destination-agnostic, got %+v", dnsRule.To)
	}

	for _, ruleIndex := range []int{1, 2, 3, 4} {
		rule := networkPolicy.Spec.Egress[ruleIndex]
		if len(rule.To) != 2 {
			t.Fatalf("expected rule %d to include 2 destination peers, got %+v", ruleIndex, rule.To)
		}
		requireNetworkPolicyCIDRPeer(t, rule.To, "203.0.113.0/24", "203.0.113.128/25")
		if rule.To[1].NamespaceSelector == nil && rule.To[0].NamespaceSelector == nil {
			t.Fatalf("expected rule %d to include a selector-based peer, got %+v", ruleIndex, rule.To)
		}
	}
}

func TestBuildNetworkPolicyAllowsSSHWhenTerminalBackendSSH(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = testNamespace
	agent.Spec.Terminal.Backend = terminalBackendSSH

	networkPolicy := buildNetworkPolicy(agent, effectiveTerminalBackend(agent, referencedInputState{}))
	if len(networkPolicy.Spec.Egress) != 3 {
		t.Fatalf("expected 3 egress rules for ssh backend, got %d", len(networkPolicy.Spec.Egress))
	}

	sshRule := networkPolicy.Spec.Egress[2]
	if len(sshRule.To) != 0 {
		t.Fatalf("expected SSH rule to allow port-based egress without destination selectors, got %+v", sshRule.To)
	}
	requireNetworkPolicyPort(t, sshRule.Ports, corev1.ProtocolTCP, networkPolicySSHPort)
}

func TestBuildNetworkPolicyAddsConfiguredAdditionalPorts(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = testNamespace
	agent.Spec.Terminal.Backend = terminalBackendSSH
	agent.Spec.NetworkPolicy.AdditionalTCPPorts = []int32{8443, networkPolicyHTTPSPort, 8081, networkPolicySSHPort}
	agent.Spec.NetworkPolicy.AdditionalUDPPorts = []int32{3478, networkPolicyDNSPort}

	networkPolicy := buildNetworkPolicy(agent, effectiveTerminalBackend(agent, referencedInputState{}))
	if len(networkPolicy.Spec.Egress) != 5 {
		t.Fatalf("expected 5 egress rules with additional ports, got %d", len(networkPolicy.Spec.Egress))
	}

	extraTCPRule := networkPolicy.Spec.Egress[3]
	if len(extraTCPRule.Ports) != 2 {
		t.Fatalf("expected 2 deduplicated extra TCP ports, got %+v", extraTCPRule.Ports)
	}
	requireNetworkPolicyPort(t, extraTCPRule.Ports, corev1.ProtocolTCP, 8081)
	requireNetworkPolicyPort(t, extraTCPRule.Ports, corev1.ProtocolTCP, 8443)

	extraUDPRule := networkPolicy.Spec.Egress[4]
	if len(extraUDPRule.Ports) != 1 {
		t.Fatalf("expected 1 deduplicated extra UDP port, got %+v", extraUDPRule.Ports)
	}
	requireNetworkPolicyPort(t, extraUDPRule.Ports, corev1.ProtocolUDP, 3478)
}

func TestEffectiveTerminalBackendUsesInlineConfigOverSpec(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Spec.Terminal.Backend = terminalBackendLocal
	agent.Spec.Config.Raw = "model: anthropic/claude-opus-4.1\nterminal:\n  backend: ssh\n"

	backend := effectiveTerminalBackend(agent, referencedInputState{})
	if backend != terminalBackendSSH {
		t.Fatalf("expected inline config terminal backend ssh, got %q", backend)
	}
}

func TestEffectiveTerminalBackendUsesReferencedConfigOverSpec(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Spec.Terminal.Backend = terminalBackendLocal
	agent.Spec.Config.ConfigMapRef = &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config"},
		Key:                  "config.yaml",
	}
	referencedInputs := referencedInputState{
		FileRefs: []referencedObjectSnapshot{
			newConfigMapFileSnapshot("shared-config", "config.yaml", &corev1.ConfigMap{Data: map[string]string{"config.yaml": "model: anthropic/claude-opus-4.1\nterminal:\n  backend: ssh\n"}}),
		},
	}

	backend := effectiveTerminalBackend(agent, referencedInputs)
	if backend != terminalBackendSSH {
		t.Fatalf("expected referenced config terminal backend ssh, got %q", backend)
	}
}

func TestEffectiveTerminalBackendUsesReferencedSecretConfigOverSpec(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Spec.Terminal.Backend = terminalBackendLocal
	agent.Spec.Config.SecretRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config-secret"},
		Key:                  "config.yaml",
	}
	referencedInputs := referencedInputState{
		FileRefs: []referencedObjectSnapshot{
			newSecretFileSnapshot("shared-config-secret", "config.yaml", &corev1.Secret{Data: map[string][]byte{"config.yaml": []byte("model: anthropic/claude-opus-4.1\nterminal:\n  backend: ssh\n")}}),
		},
	}

	backend := effectiveTerminalBackend(agent, referencedInputs)
	if backend != terminalBackendSSH {
		t.Fatalf("expected referenced secret config terminal backend ssh, got %q", backend)
	}
}

func TestEffectiveTerminalBackendFallsBackToSpecWhenConfigOmitsBackend(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Spec.Terminal.Backend = terminalBackendSSH
	agent.Spec.Config.Raw = "model: anthropic/claude-opus-4.1\n"

	backend := effectiveTerminalBackend(agent, referencedInputState{})
	if backend != terminalBackendSSH {
		t.Fatalf("expected fallback terminal backend ssh, got %q", backend)
	}
}

func TestEffectiveTerminalBackendIsEmptyWhenNoConfigOrFallbackDeclaresBackend(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}

	backend := effectiveTerminalBackend(agent, referencedInputState{})
	if backend != "" {
		t.Fatalf("expected empty effective terminal backend, got %q", backend)
	}
}

func TestAdditionalNetworkPolicyPortsDeduplicatesAndSortsPorts(t *testing.T) {
	existing := networkPolicyPortSet(networkPolicyDNSPort, networkPolicyHTTPSPort)
	ports := additionalNetworkPolicyPorts([]int32{8443, networkPolicyHTTPSPort, 8081, 8443, networkPolicyDNSPort}, existing)
	if len(ports) != 2 {
		t.Fatalf("expected 2 additional ports after deduplication, got %+v", ports)
	}
	if ports[0] != 8081 || ports[1] != 8443 {
		t.Fatalf("expected sorted additional ports [8081 8443], got %+v", ports)
	}
}

func TestNetworkPolicyPortsUsesRequestedProtocol(t *testing.T) {
	ports := networkPolicyPorts(corev1.ProtocolUDP, []int32{3478, 5353})
	if len(ports) != 2 {
		t.Fatalf("expected 2 network policy ports, got %+v", ports)
	}
	requireNetworkPolicyPort(t, ports, corev1.ProtocolUDP, 3478)
	requireNetworkPolicyPort(t, ports, corev1.ProtocolUDP, 5353)
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
		t.Fatalf("expected StatefulSet container to mount the Hermes data volume at %s", hermesDataPath)
	}
}

func TestBuildStatefulSetConfiguresHermesProbes(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Spec.Probes.RequireConnectedPlatform = true

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	statefulSet := buildStatefulSet(agent, buildPodTemplateInputs(agent, plan))
	container := statefulSet.Spec.Template.Spec.Containers[0]

	requireExecProbe(t, container.StartupProbe, hermesGatewayPIDFile, hermesGatewayStateFile)
	if container.StartupProbe.InitialDelaySeconds != 0 {
		t.Fatalf("expected startup probe initial delay 0, got %d", container.StartupProbe.InitialDelaySeconds)
	}
	if container.StartupProbe.FailureThreshold != startupFailureThreshold {
		t.Fatalf("expected startup probe failure threshold %d, got %d", startupFailureThreshold, container.StartupProbe.FailureThreshold)
	}

	requireExecProbe(t, container.ReadinessProbe,
		"grep -Eo '\"pid\"[[:space:]]*:[[:space:]]*[0-9]+'",
		"kill -0",
		"\"gateway_state\"[[:space:]]*:[[:space:]]*\"running\"",
		"\"platforms\"[[:space:]]*:[[:space:]]*\\{.*\"state\"[[:space:]]*:[[:space:]]*\"connected\"",
	)
	requireExecProbeExcludes(t, container.ReadinessProbe, "\"connected\"[[:space:]]*:[[:space:]]*true")
	if container.ReadinessProbe.InitialDelaySeconds != readinessInitialDelay {
		t.Fatalf("expected readiness probe initial delay %d, got %d", readinessInitialDelay, container.ReadinessProbe.InitialDelaySeconds)
	}

	requireExecProbe(t, container.LivenessProbe,
		"grep -Eo '\"pid\"[[:space:]]*:[[:space:]]*[0-9]+'",
		"kill -0",
		hermesGatewayStateFile,
	)
	if container.LivenessProbe.InitialDelaySeconds != livenessInitialDelay {
		t.Fatalf("expected liveness probe initial delay %d, got %d", livenessInitialDelay, container.LivenessProbe.InitialDelaySeconds)
	}
}

func TestBuildStatefulSetReadinessWithoutConnectedPlatformStillRequiresRunningGateway(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	statefulSet := buildStatefulSet(agent, buildPodTemplateInputs(agent, plan))
	readinessProbe := statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe
	requireExecProbe(t, readinessProbe, "\"gateway_state\"[[:space:]]*:[[:space:]]*\"running\"")
	requireExecProbeExcludes(t, readinessProbe,
		"\"platforms\"[[:space:]]*:[[:space:]]*\\{.*\"state\"[[:space:]]*:[[:space:]]*\"connected\"",
		"\"connected\"[[:space:]]*:[[:space:]]*true",
	)
}

func TestBuildStatefulSetOmitsDisabledProbes(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	disabled := false
	agent.Spec.Probes.Startup.Enabled = &disabled
	agent.Spec.Probes.Readiness.Enabled = &disabled
	agent.Spec.Probes.Liveness.Enabled = &disabled

	plan, err := buildConfigPlan(agent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	statefulSet := buildStatefulSet(agent, buildPodTemplateInputs(agent, plan))
	container := statefulSet.Spec.Template.Spec.Containers[0]
	if container.StartupProbe != nil {
		t.Fatalf("expected startup probe to be disabled, got %+v", container.StartupProbe)
	}
	if container.ReadinessProbe != nil {
		t.Fatalf("expected readiness probe to be disabled, got %+v", container.ReadinessProbe)
	}
	if container.LivenessProbe != nil {
		t.Fatalf("expected liveness probe to be disabled, got %+v", container.LivenessProbe)
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
	requireHermesPodSecurityContext(t, statefulSet.Spec.Template.Spec)
	requireHermesContainerSecurityContext(t, container)
	requireEmptyDirVolume(t, statefulSet.Spec.Template.Spec.Volumes, hermesTmpVolumeName)
	requireVolumeMount(t, container.VolumeMounts, hermesTmpVolumeName, hermesTmpPath)
	if statefulSet.Spec.Template.Spec.ServiceAccountName != "" {
		t.Fatalf("expected default serviceAccountName to be empty, got %q", statefulSet.Spec.Template.Spec.ServiceAccountName)
	}
	if statefulSet.Spec.Template.Spec.AutomountServiceAccountToken == nil || *statefulSet.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatalf("expected automountServiceAccountToken to default to false, got %+v", statefulSet.Spec.Template.Spec.AutomountServiceAccountToken)
	}
}

func TestBuildStatefulSetIncludesPodPlacementAndRegistryAuthControls(t *testing.T) {
	testAgent := &hermesv1alpha1.HermesAgent{}
	testAgent.Name = testAgentName
	testAgent.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "registry-auth"}}
	testAgent.Spec.PodLabels = map[string]string{"sidecar.istio.io/inject": "false", "app.kubernetes.io/name": "should-not-override"}
	testAgent.Spec.PodAnnotations = map[string]string{"prometheus.io/scrape": "true"}
	testAgent.Spec.ServiceAccountName = "hermes-runtime"
	automountServiceAccountToken := true
	testAgent.Spec.AutomountServiceAccountToken = &automountServiceAccountToken
	testAgent.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "linux"}
	testAgent.Spec.Tolerations = []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "hermes", Effect: corev1.TaintEffectNoSchedule}}
	testAgent.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "node-pool", Operator: corev1.NodeSelectorOpIn, Values: []string{"gpu"}}},
				}},
			},
		},
	}
	testAgent.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{{
		MaxSkew:           1,
		TopologyKey:       "topology.kubernetes.io/zone",
		WhenUnsatisfiable: corev1.ScheduleAnyway,
		LabelSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{"app": "hermes"}},
	}}

	plan, err := buildConfigPlan(testAgent)
	if err != nil {
		t.Fatalf("buildConfigPlan returned error: %v", err)
	}

	statefulSet := buildStatefulSet(testAgent, buildPodTemplateInputs(testAgent, plan))
	podSpec := statefulSet.Spec.Template.Spec
	if statefulSet.Spec.Template.Labels["sidecar.istio.io/inject"] != "false" {
		t.Fatalf("expected pod label sidecar.istio.io/inject=false, got %+v", statefulSet.Spec.Template.Labels)
	}
	if _, ok := statefulSet.Spec.Selector.MatchLabels["sidecar.istio.io/inject"]; ok {
		t.Fatalf("expected pod-only labels not to change the StatefulSet selector, got %+v", statefulSet.Spec.Selector.MatchLabels)
	}
	if statefulSet.Spec.Template.Labels["app.kubernetes.io/name"] != "k8s-operator-hermes-agent" {
		t.Fatalf("expected operator identity label to win, got %+v", statefulSet.Spec.Template.Labels)
	}
	if statefulSet.Spec.Template.Annotations["prometheus.io/scrape"] != "true" {
		t.Fatalf("expected pod annotation prometheus.io/scrape=true, got %+v", statefulSet.Spec.Template.Annotations)
	}
	if len(podSpec.ImagePullSecrets) != 1 || podSpec.ImagePullSecrets[0].Name != "registry-auth" {
		t.Fatalf("expected image pull secrets to be preserved, got %+v", podSpec.ImagePullSecrets)
	}
	if podSpec.ServiceAccountName != "hermes-runtime" {
		t.Fatalf("expected serviceAccountName to be preserved, got %q", podSpec.ServiceAccountName)
	}
	if podSpec.AutomountServiceAccountToken == nil || !*podSpec.AutomountServiceAccountToken {
		t.Fatalf("expected automountServiceAccountToken to be preserved, got %+v", podSpec.AutomountServiceAccountToken)
	}
	if podSpec.NodeSelector["kubernetes.io/os"] != "linux" {
		t.Fatalf("expected nodeSelector to be preserved, got %+v", podSpec.NodeSelector)
	}
	if len(podSpec.Tolerations) != 1 || podSpec.Tolerations[0].Key != "dedicated" {
		t.Fatalf("expected tolerations to be preserved, got %+v", podSpec.Tolerations)
	}
	if podSpec.Affinity == nil || podSpec.Affinity.NodeAffinity == nil {
		t.Fatalf("expected affinity to be preserved, got %+v", podSpec.Affinity)
	}
	if len(podSpec.TopologySpreadConstraints) != 1 || podSpec.TopologySpreadConstraints[0].TopologyKey != "topology.kubernetes.io/zone" {
		t.Fatalf("expected topology spread constraints to be preserved, got %+v", podSpec.TopologySpreadConstraints)
	}
}
