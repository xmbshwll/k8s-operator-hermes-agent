package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

const (
	testAgentName     = "example"
	testInlineConfig  = "model: anthropic/claude-opus-4.1\n"
	testUpdatedConfig = "model: openai/gpt-4.1-mini\n"
)

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
