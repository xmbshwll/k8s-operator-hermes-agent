/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

var _ = Describe("HermesAgent Webhook", func() {
	var (
		validator HermesAgentCustomValidator
		defaulter HermesAgentCustomDefaulter
	)

	BeforeEach(func() {
		validator = HermesAgentCustomValidator{}
		defaulter = HermesAgentCustomDefaulter{}
	})

	newNamespace := func() string {
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: fmt.Sprintf("hermes-webhook-%d-", time.Now().UnixNano())},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		return namespace.Name
	}

	newMinimalHermesAgent := func(namespace, name string) *hermesv1alpha1.HermesAgent {
		return &hermesv1alpha1.HermesAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: hermesv1alpha1.HermesAgentSpec{
				Image: hermesv1alpha1.HermesAgentImageSpec{Repository: "ghcr.io/example/hermes-agent"},
			},
		}
	}

	Context("When defaulting HermesAgent", func() {
		It("applies runtime defaults for probes, service, storage, and network policy", func() {
			obj := newMinimalHermesAgent("default", "defaults")

			Expect(defaulter.Default(ctx, obj)).To(Succeed())
			Expect(obj.Spec.Mode).To(Equal("gateway"))
			Expect(obj.Spec.Image.Tag).To(Equal("gateway-core"))
			Expect(obj.Spec.Image.PullPolicy).To(Equal(corev1.PullIfNotPresent))
			Expect(obj.Spec.Terminal.Backend).To(BeEmpty())
			Expect(obj.Spec.Storage.Persistence.Enabled).NotTo(BeNil())
			Expect(*obj.Spec.Storage.Persistence.Enabled).To(BeTrue())
			Expect(obj.Spec.Storage.Persistence.Size).To(Equal("10Gi"))
			Expect(obj.Spec.Storage.Persistence.AccessModes).To(Equal([]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}))
			Expect(obj.Spec.Service.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(obj.Spec.Service.Port).To(Equal(int32(8080)))
			Expect(obj.Spec.NetworkPolicy.Enabled).NotTo(BeNil())
			Expect(*obj.Spec.NetworkPolicy.Enabled).To(BeFalse())
			Expect(obj.Spec.Probes.Startup.Enabled).NotTo(BeNil())
			Expect(*obj.Spec.Probes.Startup.Enabled).To(BeTrue())
			Expect(obj.Spec.Probes.Startup.PeriodSeconds).To(Equal(int32(10)))
			Expect(obj.Spec.Probes.Startup.TimeoutSeconds).To(Equal(int32(5)))
			Expect(obj.Spec.Probes.Startup.FailureThreshold).To(Equal(int32(18)))
			Expect(obj.Spec.Probes.Readiness.InitialDelaySeconds).To(Equal(int32(5)))
			Expect(obj.Spec.Probes.Liveness.InitialDelaySeconds).To(Equal(int32(15)))
		})

		It("defaults objects created through the admission webhook", func() {
			namespace := newNamespace()
			obj := newMinimalHermesAgent(namespace, fmt.Sprintf("defaulted-%d", time.Now().UnixNano()))

			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			stored := &hermesv1alpha1.HermesAgent{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
			Expect(stored.Spec.Mode).To(Equal("gateway"))
			Expect(stored.Spec.Image.Tag).To(Equal("gateway-core"))
			Expect(stored.Spec.Image.PullPolicy).To(Equal(corev1.PullIfNotPresent))
			Expect(stored.Spec.Terminal.Backend).To(BeEmpty())
			Expect(stored.Spec.Service.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(stored.Spec.Service.Port).To(Equal(int32(8080)))
			Expect(stored.Spec.Probes.Startup.FailureThreshold).To(Equal(int32(18)))
			Expect(stored.Spec.Probes.Readiness.InitialDelaySeconds).To(Equal(int32(5)))
			Expect(stored.Spec.Probes.Liveness.InitialDelaySeconds).To(Equal(int32(15)))
		})
	})

	Context("When validating HermesAgent", func() {
		It("rejects mixed raw and referenced config sources", func() {
			namespace := newNamespace()
			obj := newMinimalHermesAgent(namespace, fmt.Sprintf("invalid-mixed-%d", time.Now().UnixNano()))
			obj.Spec.Config.Raw = "model: anthropic/claude-opus-4.1\n"
			obj.Spec.Config.ConfigMapRef = &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config"},
				Key:                  "config.yaml",
			}
			obj.Spec.Config.SecretRef = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "shared-secret"},
				Key:                  "config.yaml",
			}

			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			var statusErr *apierrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue())
			Expect(statusErr.ErrStatus.Message).To(ContainSubstring("raw, configMapRef, and secretRef are mutually exclusive"))
		})

		It("rejects incomplete configMap and secret references", func() {
			namespace := newNamespace()
			obj := newMinimalHermesAgent(namespace, fmt.Sprintf("invalid-ref-%d", time.Now().UnixNano()))
			obj.Spec.GatewayConfig.ConfigMapRef = &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config"},
			}
			obj.Spec.Config.SecretRef = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "shared-secret"},
			}

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.gatewayConfig.configMapRef.key"))
			Expect(err.Error()).To(ContainSubstring("spec.config.secretRef.key"))
		})

		It("rejects invalid storage size, service port, network policy ports, empty image pull secrets, and invalid file mounts", func() {
			namespace := newNamespace()
			invalidMode := int32(0o1000)
			obj := newMinimalHermesAgent(namespace, fmt.Sprintf("invalid-settings-%d", time.Now().UnixNano()))
			obj.Spec.Storage.Persistence.Size = "0Gi"
			obj.Spec.Service.Enabled = true
			obj.Spec.Service.Port = -1
			obj.Spec.NetworkPolicy.AdditionalTCPPorts = []int32{0}
			obj.Spec.NetworkPolicy.AdditionalUDPPorts = []int32{70000}
			obj.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{}}
			obj.Spec.FileMounts = []hermesv1alpha1.HermesAgentFileMountSpec{{
				MountPath:    "relative/path",
				ConfigMapRef: &corev1.LocalObjectReference{Name: "plugins"},
				SecretRef:    &corev1.LocalObjectReference{Name: "ssh-auth"},
			}, {
				MountPath: "/var/run/hermes/plugins",
			}, {
				MountPath:   "/var/run/hermes/plugins",
				SecretRef:   &corev1.LocalObjectReference{},
				DefaultMode: &invalidMode,
				Items: []hermesv1alpha1.HermesAgentFileProjectionItem{{
					Path: "../known_hosts",
				}, {
					Key:  "known_hosts",
					Path: "../known_hosts",
				}, {
					Key:  "known_hosts",
					Path: "known_hosts",
					Mode: &invalidMode,
				}},
			}}

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.storage.persistence.size"))
			Expect(err.Error()).To(ContainSubstring("spec.service.port"))
			Expect(err.Error()).To(ContainSubstring("spec.networkPolicy.additionalTCPPorts[0]"))
			Expect(err.Error()).To(ContainSubstring("spec.networkPolicy.additionalUDPPorts[0]"))
			Expect(err.Error()).To(ContainSubstring("spec.imagePullSecrets[0].name"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[0]"))
			Expect(err.Error()).To(ContainSubstring("mountPath must be absolute"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[1]"))
			Expect(err.Error()).To(ContainSubstring("either configMapRef or secretRef must be set"))
			Expect(err.Error()).To(ContainSubstring("duplicates fileMounts[1].mountPath"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[2].secretRef.name"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[2].defaultMode"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[2].items[0].key"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[2].items[0].path"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[2].items[1].path"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[2].items[2].key"))
			Expect(err.Error()).To(ContainSubstring("spec.fileMounts[2].items[2].mode"))
		})

		It("admits terminal backend fallback hints that differ from inline config", func() {
			namespace := newNamespace()
			obj := newMinimalHermesAgent(namespace, fmt.Sprintf("terminal-hint-%d", time.Now().UnixNano()))
			obj.Spec.Terminal.Backend = "ssh"
			obj.Spec.Config.Raw = "model: anthropic/claude-opus-4.1\nterminal:\n  backend: local\n"

			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})

		It("admits a valid HermesAgent", func() {
			namespace := newNamespace()
			defaultMode := int32(0o444)
			privateKeyMode := int32(0o600)
			obj := newMinimalHermesAgent(namespace, fmt.Sprintf("valid-%d", time.Now().UnixNano()))
			obj.Spec.Config.Raw = "model: anthropic/claude-opus-4.1\nterminal:\n  backend: local\n"
			obj.Spec.GatewayConfig.Raw = "{}\n"
			obj.Spec.EnvFrom = []corev1.EnvFromSource{{
				SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "provider-env"}},
			}}
			obj.Spec.SecretRefs = []corev1.LocalObjectReference{{Name: "ssh-auth"}}
			obj.Spec.FileMounts = []hermesv1alpha1.HermesAgentFileMountSpec{{
				MountPath:    "/var/run/hermes/plugins",
				ConfigMapRef: &corev1.LocalObjectReference{Name: "hermes-plugins"},
				Items: []hermesv1alpha1.HermesAgentFileProjectionItem{{
					Key:  "plugin.py",
					Path: "plugin.py",
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
			obj.Spec.NetworkPolicy.AdditionalTCPPorts = []int32{8443}
			obj.Spec.NetworkPolicy.AdditionalUDPPorts = []int32{3478}

			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})
	})
})
