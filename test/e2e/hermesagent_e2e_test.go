//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/xmbshwll/k8s-operator-hermes-agent/test/utils"
)

const hermesAgentNamespace = "hermes-e2e"

var _ = Describe("HermesAgent end-to-end", Ordered, func() {
	var sampleManifestPath string

	BeforeAll(func() {
		By("ensuring the manager namespace exists")
		if _, err := kubectl("get", "ns", namespace); err != nil {
			_, err = kubectl("create", "ns", namespace)
			Expect(err).NotTo(HaveOccurred())
		}

		By("labeling the manager namespace to enforce the restricted security policy")
		_, err := kubectl("label", "--overwrite", "ns", namespace, "pod-security.kubernetes.io/enforce=restricted")
		Expect(err).NotTo(HaveOccurred())

		By("installing cert-manager for webhook certificates")
		Expect(utils.InstallCertManager()).To(Succeed())

		By("installing the HermesAgent CRD")
		_, err = utils.Run(exec.Command("make", "install"))
		Expect(err).NotTo(HaveOccurred())

		By("deploying the controller manager")
		_, err = utils.Run(exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage)))
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the controller manager to be ready")
		Eventually(func(g Gomega) {
			output, err := kubectl("get", "pods", "-n", namespace, "-l", "control-plane=controller-manager", "-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(output)).To(Equal("True"))
		}, 3*time.Minute, time.Second).Should(Succeed())

		By("creating a namespace for HermesAgent validation")
		_, err = kubectl("create", "ns", hermesAgentNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("rendering the sample HermesAgent with the end-to-end runtime image")
		sampleManifestPath, err = renderSampleManifest("config/samples/hermes_v1alpha1_hermesagent.yaml")
		Expect(err).NotTo(HaveOccurred())

		By("applying the sample HermesAgent")
		_, err = kubectl("apply", "-n", hermesAgentNamespace, "-f", sampleManifestPath)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		By("removing the HermesAgent validation namespace")
		_, _ = kubectl("delete", "ns", hermesAgentNamespace, "--ignore-not-found=true", "--timeout=2m")

		By("undeploying the controller manager")
		_, _ = utils.Run(exec.Command("make", "undeploy"))

		By("uninstalling the HermesAgent CRD")
		_, _ = utils.Run(exec.Command("make", "uninstall"))

		By("removing the manager namespace")
		_, _ = kubectl("delete", "ns", namespace, "--ignore-not-found=true", "--timeout=2m")

		By("removing cert-manager")
		utils.UninstallCertManager()

		if sampleManifestPath != "" {
			_ = os.Remove(sampleManifestPath)
		}
	})

	It("reconciles the sample HermesAgent to ready", func() {
		name := "hermesagent-sample"

		By("waiting for the HermesAgent PVC to bind")
		Eventually(func(g Gomega) {
			phase, err := kubectl("get", "pvc", pvcName(name), "-n", hermesAgentNamespace, "-o", "jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(phase)).To(Equal("Bound"))
		}, 5*time.Minute, time.Second).Should(Succeed())

		By("waiting for the HermesAgent StatefulSet to become ready")
		_, err := kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the HermesAgent status to report Ready")
		waitForAgentPhase(name, "Ready")

		By("verifying the managed pod is ready and writing state to the PVC")
		waitForPodReady(statefulSetPodName(name))
		bootCount := readBootCount(statefulSetPodName(name))
		Expect(bootCount).To(BeNumerically(">=", 1))
	})

	It("keeps strict readiness false until a platform reports connected", func() {
		name := "hermesagent-connected-platform"
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: %s
  namespace: %s
spec:
  image:
    repository: %s
  config:
    raw: |
      model: anthropic/claude-opus-4.1
      terminal:
        backend: local
  gatewayConfig:
    raw: |
      {
        "platforms": {
          "telegram": {}
        }
      }
  env:
    - name: HERMES_E2E_PLATFORM
      value: telegram
    - name: HERMES_E2E_CONNECTED_DELAY_SECONDS
      value: "10"
  probes:
    startup:
      enabled: true
      periodSeconds: 2
      failureThreshold: 15
    readiness:
      enabled: true
      initialDelaySeconds: 0
      periodSeconds: 2
      timeoutSeconds: 1
      failureThreshold: 1
    liveness:
      enabled: true
      initialDelaySeconds: 0
      periodSeconds: 2
      timeoutSeconds: 1
      failureThreshold: 3
    requireConnectedPlatform: true
`, name, hermesAgentNamespace, hermesRuntimeImage))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)
		defer kubectl("delete", "-f", manifest, "--ignore-not-found=true")

		_, err = kubectl("apply", "-f", manifest)
		Expect(err).NotTo(HaveOccurred())

		podName := statefulSetPodName(name)
		waitForPodCreated(podName)

		By("verifying Hermes reports a running gateway before any platform connects")
		Eventually(func(g Gomega) {
			state, err := kubectl("exec", "-n", hermesAgentNamespace, podName, "--", "cat", "/data/hermes/gateway_state.json")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state).To(ContainSubstring(`"gateway_state": "running"`))
			g.Expect(state).To(ContainSubstring(`"telegram": {"state": "disconnected"`))
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("verifying the pod stays unready until a platform connects")
		Consistently(func(g Gomega) {
			status, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(status)).NotTo(Equal("True"))
		}, 9*time.Second, time.Second).Should(Succeed())

		By("waiting for the fake runtime to flip the platform to connected")
		Eventually(func(g Gomega) {
			state, err := kubectl("exec", "-n", hermesAgentNamespace, podName, "--", "cat", "/data/hermes/gateway_state.json")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state).To(ContainSubstring(`"telegram": {"state": "connected"`))
		}, 90*time.Second, time.Second).Should(Succeed())

		By("verifying the pod and HermesAgent become ready after the connection appears")
		waitForPodReady(podName)
		waitForAgentPhase(name, "Ready")
	})

	It("serves runtime state through the optional Service when HTTP is enabled", func() {
		name := "hermesagent-http-service"
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: %s
  namespace: %s
spec:
  image:
    repository: %s
  config:
    raw: |
      model: anthropic/claude-opus-4.1
      terminal:
        backend: local
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
  env:
    - name: HERMES_E2E_HTTP_PORT
      value: "8080"
  service:
    enabled: true
    port: 8080
`, name, hermesAgentNamespace, hermesRuntimeImage))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)
		defer kubectl("delete", "-f", manifest, "--ignore-not-found=true")

		_, err = kubectl("apply", "-f", manifest)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		waitForAgentPhase(name, "Ready")

		stopPortForward := startServicePortForward(name, 18080, 8080)
		defer stopPortForward()

		Eventually(func(g Gomega) {
			response, err := http.Get("http://127.0.0.1:18080/")
			g.Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(response.StatusCode).To(Equal(http.StatusOK))
			g.Expect(string(body)).To(ContainSubstring("gateway_state=running"))
			g.Expect(string(body)).To(ContainSubstring("path=/data/hermes/gateway_state.json"))
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("writes an observed-path report for mounted runtime inputs", func() {
		name := "hermesagent-observed-paths"
		pluginSecretName := "hermesagent-plugin-bundle"
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
stringData:
  plugin.py: |
    def register():
      return "plugin-ready"
---
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: %s
  namespace: %s
spec:
  image:
    repository: %s
  config:
    raw: |
      model: anthropic/claude-opus-4.1
      terminal:
        backend: local
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
  env:
    - name: HERMES_E2E_OBSERVED_PATHS
      value: /var/run/hermes/secrets/%s
  secretRefs:
    - name: %s
`, pluginSecretName, hermesAgentNamespace, name, hermesAgentNamespace, hermesRuntimeImage, pluginSecretName, pluginSecretName))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)
		defer kubectl("delete", "-f", manifest, "--ignore-not-found=true")

		_, err = kubectl("apply", "-f", manifest)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		waitForAgentPhase(name, "Ready")

		podName := statefulSetPodName(name)
		Eventually(func(g Gomega) {
			report, err := readObservedReport(podName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(report).To(ContainSubstring("path=/data/hermes/config.yaml"))
			g.Expect(report).To(ContainSubstring("path=/data/hermes/gateway.json"))
			g.Expect(report).To(ContainSubstring("path=/var/run/hermes/secrets/hermesagent-plugin-bundle"))
			g.Expect(report).To(ContainSubstring("child=/var/run/hermes/secrets/hermesagent-plugin-bundle/plugin.py"))
			g.Expect(report).To(ContainSubstring("plugin-ready"))
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("preserves Hermes state across a StatefulSet pod restart", func() {
		name := "hermesagent-sample"
		podName := statefulSetPodName(name)
		beforeRestart := readBootCount(podName)
		beforeUID := podUID(podName)

		By("deleting the HermesAgent pod")
		_, err := kubectl("delete", "pod", podName, "-n", hermesAgentNamespace, "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the StatefulSet pod to be recreated and ready")
		Eventually(func(g Gomega) {
			newUID, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.metadata.uid}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(newUID)).NotTo(Equal(beforeUID))
		}, 5*time.Minute, time.Second).Should(Succeed())
		waitForPodReady(podName)

		By("verifying that boot state persisted on the PVC")
		afterRestart := readBootCount(podName)
		Expect(afterRestart).To(BeNumerically(">", beforeRestart))
	})

	It("rolls out the workload after an inline config update", func() {
		name := "hermesagent-sample"
		podName := statefulSetPodName(name)
		beforeUID := podUID(podName)
		updatedConfig := "model: openai/gpt-4.1-mini\nterminal:\n  backend: local\n"
		patch := fmt.Sprintf(`{"spec":{"config":{"raw":%q}}}`, updatedConfig)

		By("patching the HermesAgent config")
		_, err := kubectl("patch", "hermesagent", name, "-n", hermesAgentNamespace, "--type=merge", "-p", patch)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the StatefulSet to complete the rollout")
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the pod to be recreated with the new config")
		Eventually(func(g Gomega) {
			newUID, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.metadata.uid}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(newUID)).NotTo(Equal(beforeUID))
		}, 5*time.Minute, time.Second).Should(Succeed())
		waitForPodReady(podName)

		By("verifying the updated config was mounted into Hermes home")
		Eventually(func(g Gomega) {
			config, err := kubectl("exec", "-n", hermesAgentNamespace, podName, "--", "cat", "/data/hermes/config.yaml")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(config).To(ContainSubstring("model: openai/gpt-4.1-mini"))
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("verifying the HermesAgent reports ready after the rollout")
		waitForAgentPhase(name, "Ready")
	})

	It("rejects invalid HermesAgent specs through the webhook", func() {
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: invalid-mixed
  namespace: %s
spec:
  image:
    repository: %s
  config:
    raw: |
      model: anthropic/claude-opus-4.1
    configMapRef:
      name: shared-config
      key: config.yaml
`, hermesAgentNamespace, hermesRuntimeImage))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)

		output, err := kubectl("apply", "-f", manifest)
		Expect(err).To(HaveOccurred())
		Expect(output).To(ContainSubstring("raw and configMapRef are mutually exclusive"))
	})

	It("surfaces missing referenced config through status and events", func() {
		name := "hermesagent-missing-ref"
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: %s
  namespace: %s
spec:
  image:
    repository: %s
  config:
    configMapRef:
      name: missing-config
      key: config.yaml
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
`, name, hermesAgentNamespace, hermesRuntimeImage))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)
		defer kubectl("delete", "-f", manifest, "--ignore-not-found=true")

		_, err = kubectl("apply", "-f", manifest)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the HermesAgent to report a config error")
		Eventually(func(g Gomega) {
			reason, err := kubectl("get", "hermesagent", name, "-n", hermesAgentNamespace, "-o", "jsonpath={.status.conditions[?(@.type=='ConfigReady')].reason}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(reason)).To(Equal("MissingReferencedInput"))
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("verifying the warning event is emitted")
		Eventually(func(g Gomega) {
			reason, err := kubectl("get", "events", "-n", hermesAgentNamespace, "--field-selector", fmt.Sprintf("involvedObject.kind=HermesAgent,involvedObject.name=%s,reason=MissingReferencedInput", name), "-o", "jsonpath={.items[0].reason}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(reason)).To(Equal("MissingReferencedInput"))
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("rolls out after referenced ConfigMap updates", func() {
		name := "hermesagent-configmap-ref"
		configMapName := "hermesagent-configmap-ref-config"
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  config.yaml: |
    model: anthropic/claude-opus-4.1
    terminal:
      backend: local
---
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: %s
  namespace: %s
spec:
  image:
    repository: %s
  config:
    configMapRef:
      name: %s
      key: config.yaml
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
`, configMapName, hermesAgentNamespace, name, hermesAgentNamespace, hermesRuntimeImage, configMapName))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)
		defer kubectl("delete", "-f", manifest, "--ignore-not-found=true")

		_, err = kubectl("apply", "-f", manifest)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the referenced-config HermesAgent to become ready")
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		waitForAgentPhase(name, "Ready")

		podName := statefulSetPodName(name)
		beforeUID := podUID(podName)

		By("updating the referenced ConfigMap")
		patch := `{"data":{"config.yaml":"model: openai/gpt-4.1-mini\nterminal:\n  backend: local\n"}}`
		_, err = kubectl("patch", "configmap", configMapName, "-n", hermesAgentNamespace, "--type=merge", "-p", patch)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the rollout triggered by the referenced ConfigMap")
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			newUID, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.metadata.uid}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(newUID)).NotTo(Equal(beforeUID))
		}, 5*time.Minute, time.Second).Should(Succeed())
		waitForPodReady(podName)

		By("verifying the mounted file uses the updated ConfigMap content")
		Eventually(func(g Gomega) {
			config, err := kubectl("exec", "-n", hermesAgentNamespace, podName, "--", "cat", "/data/hermes/config.yaml")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(config).To(ContainSubstring("model: openai/gpt-4.1-mini"))
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("rolls out after Secret-backed env and mounted secret updates", func() {
		name := "hermesagent-secret-rollout"
		envSecretName := "hermesagent-secret-rollout-env"
		mountSecretName := "hermesagent-secret-rollout-files"
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
stringData:
  OPENROUTER_API_KEY: sk-initial
---
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
stringData:
  id_ed25519: |
    initial-key
  known_hosts: |
    github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAinitial
---
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: %s
  namespace: %s
spec:
  image:
    repository: %s
  config:
    raw: |
      model: anthropic/claude-opus-4.1
      terminal:
        backend: local
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
  envFrom:
    - secretRef:
        name: %s
  secretRefs:
    - name: %s
`, envSecretName, hermesAgentNamespace, mountSecretName, hermesAgentNamespace, name, hermesAgentNamespace, hermesRuntimeImage, envSecretName, mountSecretName))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)
		defer kubectl("delete", "-f", manifest, "--ignore-not-found=true")

		_, err = kubectl("apply", "-f", manifest)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		waitForAgentPhase(name, "Ready")

		podName := statefulSetPodName(name)
		beforeUID := podUID(podName)

		By("updating both the envFrom secret and the mounted secret")
		_, err = kubectl("patch", "secret", envSecretName, "-n", hermesAgentNamespace, "--type=merge", "-p", `{"stringData":{"OPENROUTER_API_KEY":"sk-updated"}}`)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("patch", "secret", mountSecretName, "-n", hermesAgentNamespace, "--type=merge", "-p", `{"stringData":{"id_ed25519":"updated-key","known_hosts":"github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAupdated"}}`)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the rollout triggered by the secret updates")
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			newUID, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.metadata.uid}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(newUID)).NotTo(Equal(beforeUID))
		}, 5*time.Minute, time.Second).Should(Succeed())
		waitForPodReady(podName)
	})

	It("creates and removes optional Service and NetworkPolicy resources", func() {
		name := "hermesagent-optional-resources"
		manifest, err := renderManifest(fmt.Sprintf(`
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: %s
  namespace: %s
spec:
  image:
    repository: %s
  config:
    raw: |
      model: anthropic/claude-opus-4.1
      terminal:
        backend: local
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
  service:
    enabled: true
    port: 8080
  networkPolicy:
    enabled: true
`, name, hermesAgentNamespace, hermesRuntimeImage))
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(manifest)
		defer kubectl("delete", "-f", manifest, "--ignore-not-found=true")

		_, err = kubectl("apply", "-f", manifest)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", name), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())
		waitForAgentPhase(name, "Ready")

		By("verifying optional resources are created")
		Eventually(func(g Gomega) {
			_, err := kubectl("get", "service", name, "-n", hermesAgentNamespace)
			g.Expect(err).NotTo(HaveOccurred())
			_, err = kubectl("get", "networkpolicy", name, "-n", hermesAgentNamespace)
			g.Expect(err).NotTo(HaveOccurred())
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("disabling the optional resources")
		_, err = kubectl("patch", "hermesagent", name, "-n", hermesAgentNamespace, "--type=merge", "-p", `{"spec":{"service":{"enabled":false},"networkPolicy":{"enabled":false}}}`)
		Expect(err).NotTo(HaveOccurred())

		By("verifying the owned resources are deleted")
		Eventually(func(g Gomega) {
			_, err := kubectl("get", "service", name, "-n", hermesAgentNamespace)
			g.Expect(err).To(HaveOccurred())
			_, err = kubectl("get", "networkpolicy", name, "-n", hermesAgentNamespace)
			g.Expect(err).To(HaveOccurred())
		}, 2*time.Minute, time.Second).Should(Succeed())
	})
})

func renderSampleManifest(relativePath string) (string, error) {
	projectDir, err := utils.GetProjectDir()
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(filepath.Join(projectDir, relativePath))
	if err != nil {
		return "", err
	}

	rendered := strings.Replace(string(content), "repository: ghcr.io/example/hermes-agent", fmt.Sprintf("repository: %s", hermesRuntimeImage), 1)
	rendered = strings.Replace(rendered, "tag: gateway-core", "tag: v0.0.1", 1)
	return renderManifest(rendered)
}

func renderManifest(content string) (string, error) {
	file, err := os.CreateTemp("", "hermesagent-e2e-*.yaml")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(strings.TrimSpace(content) + "\n"); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func kubectl(args ...string) (string, error) {
	return utils.Run(exec.Command("kubectl", args...))
}

func statefulSetPodName(name string) string {
	return fmt.Sprintf("%s-0", name)
}

func pvcName(name string) string {
	return fmt.Sprintf("%s-data", name)
}

func waitForPodCreated(podName string) {
	Eventually(func(g Gomega) {
		name, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.metadata.name}")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(name)).To(Equal(podName))
	}, 5*time.Minute, time.Second).Should(Succeed())
}

func waitForPodReady(podName string) {
	Eventually(func(g Gomega) {
		status, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(status)).To(Equal("True"))
	}, 5*time.Minute, time.Second).Should(Succeed())
}

func waitForAgentPhase(name, phase string) {
	Eventually(func(g Gomega) {
		currentPhase, err := kubectl("get", "hermesagent", name, "-n", hermesAgentNamespace, "-o", "jsonpath={.status.phase}")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(currentPhase)).To(Equal(phase))
	}, 5*time.Minute, time.Second).Should(Succeed())
}

func podUID(podName string) string {
	uid, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.metadata.uid}")
	Expect(err).NotTo(HaveOccurred())
	return strings.TrimSpace(uid)
}

func readObservedReport(podName string) (string, error) {
	return kubectl("exec", "-n", hermesAgentNamespace, podName, "--", "cat", "/data/hermes/e2e-observed-paths.txt")
}

func startServicePortForward(serviceName string, localPort, remotePort int) func() {
	cmd := exec.Command("kubectl", "port-forward", "-n", hermesAgentNamespace, fmt.Sprintf("service/%s", serviceName), fmt.Sprintf("%d:%d", localPort, remotePort))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	Expect(cmd.Start()).To(Succeed())

	Eventually(func() error {
		response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", localPort))
		if err != nil {
			return err
		}
		defer response.Body.Close()
		return nil
	}, 30*time.Second, 500*time.Millisecond).Should(Succeed())

	return func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

func readBootCount(podName string) int {
	output, err := kubectl("exec", "-n", hermesAgentNamespace, podName, "--", "cat", "/data/hermes/boot-count")
	Expect(err).NotTo(HaveOccurred())
	count, err := strconv.Atoi(strings.TrimSpace(output))
	Expect(err).NotTo(HaveOccurred())
	return count
}
