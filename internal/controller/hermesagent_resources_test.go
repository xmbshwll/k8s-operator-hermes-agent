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

	networkPolicy := buildNetworkPolicy(agent)
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

func TestBuildNetworkPolicyAllowsSSHWhenTerminalBackendSSH(t *testing.T) {
	agent := &hermesv1alpha1.HermesAgent{}
	agent.Name = testAgentName
	agent.Namespace = testNamespace
	agent.Spec.Terminal.Backend = "ssh"

	networkPolicy := buildNetworkPolicy(agent)
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
	agent.Spec.Terminal.Backend = "ssh"
	agent.Spec.NetworkPolicy.AdditionalTCPPorts = []int32{8443, networkPolicyHTTPSPort, 8081, networkPolicySSHPort}
	agent.Spec.NetworkPolicy.AdditionalUDPPorts = []int32{3478, networkPolicyDNSPort}

	networkPolicy := buildNetworkPolicy(agent)
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
}
