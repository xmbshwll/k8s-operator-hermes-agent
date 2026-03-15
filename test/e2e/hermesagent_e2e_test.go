//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
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

const (
	hermesAgentName      = "hermesagent-sample"
	hermesAgentNamespace = "hermes-e2e"
)

var _ = Describe("HermesAgent end-to-end", Ordered, func() {
	var manifestPath string

	BeforeAll(func() {
		By("ensuring the manager namespace exists")
		if _, err := kubectl("get", "ns", namespace); err != nil {
			_, err = kubectl("create", "ns", namespace)
			Expect(err).NotTo(HaveOccurred())
		}

		By("labeling the manager namespace to enforce the restricted security policy")
		_, err := kubectl("label", "--overwrite", "ns", namespace, "pod-security.kubernetes.io/enforce=restricted")
		Expect(err).NotTo(HaveOccurred())

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
		manifestPath, err = renderHermesSampleManifest()
		Expect(err).NotTo(HaveOccurred())

		By("applying the sample HermesAgent")
		_, err = kubectl("apply", "-n", hermesAgentNamespace, "-f", manifestPath)
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

		if manifestPath != "" {
			_ = os.Remove(manifestPath)
		}
	})

	It("reconciles the sample HermesAgent to ready", func() {
		By("waiting for the HermesAgent PVC to bind")
		Eventually(func(g Gomega) {
			phase, err := kubectl("get", "pvc", pvcName(), "-n", hermesAgentNamespace, "-o", "jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(phase)).To(Equal("Bound"))
		}, 5*time.Minute, time.Second).Should(Succeed())

		By("waiting for the HermesAgent StatefulSet to become ready")
			_, err := kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", hermesAgentName), "-n", hermesAgentNamespace, "--timeout=5m")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the HermesAgent status to report Ready")
		Eventually(func(g Gomega) {
			phase, err := kubectl("get", "hermesagent", hermesAgentName, "-n", hermesAgentNamespace, "-o", "jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(phase)).To(Equal("Ready"))
		}, 5*time.Minute, time.Second).Should(Succeed())

		By("verifying the managed pod is ready and writing state to the PVC")
		waitForPodReady(statefulSetPodName())
		bootCount := readBootCount(statefulSetPodName())
		Expect(bootCount).To(BeNumerically(">=", 1))
	})

	It("preserves Hermes state across a StatefulSet pod restart", func() {
		podName := statefulSetPodName()
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

	It("rolls out the workload after a config update", func() {
		podName := statefulSetPodName()
		beforeUID := podUID(podName)
		updatedConfig := "model: openai/gpt-4.1-mini\nterminal:\n  backend: local\n"
		patch := fmt.Sprintf(`{"spec":{"config":{"raw":%q}}}`, updatedConfig)

		By("patching the HermesAgent config")
		_, err := kubectl("patch", "hermesagent", hermesAgentName, "-n", hermesAgentNamespace, "--type=merge", "-p", patch)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the StatefulSet to complete the rollout")
		_, err = kubectl("rollout", "status", fmt.Sprintf("statefulset/%s", hermesAgentName), "-n", hermesAgentNamespace, "--timeout=5m")
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
		Eventually(func(g Gomega) {
			phase, err := kubectl("get", "hermesagent", hermesAgentName, "-n", hermesAgentNamespace, "-o", "jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(phase)).To(Equal("Ready"))
		}, 5*time.Minute, time.Second).Should(Succeed())
	})
})

func renderHermesSampleManifest() (string, error) {
	projectDir, err := utils.GetProjectDir()
	if err != nil {
		return "", err
	}

	samplePath := filepath.Join(projectDir, "config", "samples", "hermes_v1alpha1_hermesagent.yaml")
	content, err := os.ReadFile(samplePath)
	if err != nil {
		return "", err
	}

	rendered := strings.Replace(string(content), "repository: ghcr.io/example/hermes-agent", "repository: example.com/hermes-agent-e2e", 1)
	rendered = strings.Replace(rendered, "tag: gateway-core", "tag: v0.0.1", 1)

	file, err := os.CreateTemp("", "hermesagent-e2e-*.yaml")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(rendered); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func kubectl(args ...string) (string, error) {
	return utils.Run(exec.Command("kubectl", args...))
}

func statefulSetPodName() string {
	return fmt.Sprintf("%s-0", hermesAgentName)
}

func pvcName() string {
	return fmt.Sprintf("%s-data", hermesAgentName)
}

func waitForPodReady(podName string) {
	Eventually(func(g Gomega) {
		status, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(status)).To(Equal("True"))
	}, 5*time.Minute, time.Second).Should(Succeed())
}

func podUID(podName string) string {
	uid, err := kubectl("get", "pod", podName, "-n", hermesAgentNamespace, "-o", "jsonpath={.metadata.uid}")
	Expect(err).NotTo(HaveOccurred())
	return strings.TrimSpace(uid)
}

func readBootCount(podName string) int {
	output, err := kubectl("exec", "-n", hermesAgentNamespace, podName, "--", "cat", "/data/hermes/boot-count")
	Expect(err).NotTo(HaveOccurred())
	count, err := strconv.Atoi(strings.TrimSpace(output))
	Expect(err).NotTo(HaveOccurred())
	return count
}
