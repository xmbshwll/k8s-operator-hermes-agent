package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

var _ = Describe("HermesAgentReconciler", func() {
	newNamespace := func() *corev1.Namespace {
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: fmt.Sprintf("hermes-controller-%d-", time.Now().UnixNano()),
			},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		return namespace
	}

	newReconciler := func() *HermesAgentReconciler {
		return &HermesAgentReconciler{Client: k8sClient, Scheme: scheme.Scheme}
	}

	newAgent := func(namespace, name string) *hermesv1alpha1.HermesAgent {
		enabled := true
		return &hermesv1alpha1.HermesAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: hermesv1alpha1.HermesAgentSpec{
				Image: hermesv1alpha1.HermesAgentImageSpec{
					Repository: "ghcr.io/example/hermes-agent",
					Tag:        "gateway-core",
					PullPolicy: corev1.PullIfNotPresent,
				},
				Config:        hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
				GatewayConfig: hermesv1alpha1.HermesAgentConfigSource{Raw: "{}\n"},
				Service: hermesv1alpha1.HermesAgentServiceSpec{
					Enabled: true,
					Port:    8080,
				},
				NetworkPolicy: hermesv1alpha1.HermesAgentNetworkPolicySpec{Enabled: &enabled},
			},
		}
	}

	It("reconciles generated resources for inline config and optional resources", func() {
		namespace := newNamespace()
		agent := newAgent(namespace.Name, fmt.Sprintf("inline-%d", time.Now().UnixNano()))
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())

		reconciler := newReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)})
		Expect(err).NotTo(HaveOccurred())

		configMap := &corev1.ConfigMap{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: generatedConfigMapName(agent.Name, "config"), Namespace: namespace.Name}, configMap)).To(Succeed())
		}).Should(Succeed())
		Expect(configMap.Data).To(HaveKeyWithValue("config.yaml", testInlineConfig))

		gatewayConfigMap := &corev1.ConfigMap{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: generatedConfigMapName(agent.Name, "gateway-config"), Namespace: namespace.Name}, gatewayConfigMap)).To(Succeed())
		}).Should(Succeed())
		Expect(gatewayConfigMap.Data).To(HaveKeyWithValue("gateway.json", "{}\n"))

		persistentVolumeClaim := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: persistentVolumeClaimName(agent.Name), Namespace: namespace.Name}, persistentVolumeClaim)).To(Succeed())
		Expect(metav1.IsControlledBy(persistentVolumeClaim, agent)).To(BeTrue())

		statefulSet := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), statefulSet)).To(Succeed())
		Expect(metav1.IsControlledBy(statefulSet, agent)).To(BeTrue())
		Expect(statefulSet.Spec.Template.Annotations).To(HaveKey(configHashAnnotation))
		Expect(statefulSet.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(statefulSet.Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{Name: "HERMES_HOME", Value: hermesHomePath}))

		service := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), service)).To(Succeed())
		Expect(metav1.IsControlledBy(service, agent)).To(BeTrue())
		Expect(service.Spec.Ports).To(HaveLen(1))
		Expect(service.Spec.Ports[0].Port).To(Equal(int32(8080)))

		networkPolicy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), networkPolicy)).To(Succeed())
		Expect(metav1.IsControlledBy(networkPolicy, agent)).To(BeTrue())
		Expect(networkPolicy.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeEgress}))

		current := &hermesv1alpha1.HermesAgent{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), current)).To(Succeed())
		Expect(current.Status.Phase).To(Equal(phaseStoragePending))
	})

	It("marks the resource ready after persistence and workload report ready", func() {
		namespace := newNamespace()
		agent := newAgent(namespace.Name, fmt.Sprintf("ready-%d", time.Now().UnixNano()))
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())

		reconciler := newReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)})
		Expect(err).NotTo(HaveOccurred())

		persistentVolumeClaim := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: persistentVolumeClaimName(agent.Name), Namespace: namespace.Name}, persistentVolumeClaim)).To(Succeed())
		persistentVolumeClaim.Status.Phase = corev1.ClaimBound
		Expect(k8sClient.Status().Update(ctx, persistentVolumeClaim)).To(Succeed())

		statefulSet := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), statefulSet)).To(Succeed())
		statefulSet.Status.ObservedGeneration = statefulSet.Generation
		statefulSet.Status.Replicas = 1
		statefulSet.Status.ReadyReplicas = 1
		statefulSet.Status.CurrentReplicas = 1
		statefulSet.Status.UpdatedReplicas = 1
		Expect(k8sClient.Status().Update(ctx, statefulSet)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current := &hermesv1alpha1.HermesAgent{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), current)).To(Succeed())
			g.Expect(current.Status.Phase).To(Equal(phaseReady))
			g.Expect(current.Status.ReadyReplicas).To(Equal(int32(1)))
			g.Expect(current.Status.PersistenceBound).To(BeTrue())
			g.Expect(metaConditionStatus(current.Status.Conditions, conditionTypeReady)).To(Equal(metav1.ConditionTrue))
		}).Should(Succeed())
	})

	It("records config errors for invalid mixed config sources", func() {
		namespace := newNamespace()
		agent := newAgent(namespace.Name, fmt.Sprintf("invalid-%d", time.Now().UnixNano()))
		agent.Spec.Config.ConfigMapRef = &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config"},
			Key:                  "config.yaml",
		}
		sharedConfig := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "shared-config", Namespace: namespace.Name},
			Data:       map[string]string{"config.yaml": testInlineConfig},
		}
		Expect(k8sClient.Create(ctx, sharedConfig)).To(Succeed())
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())

		reconciler := newReconciler()
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current := &hermesv1alpha1.HermesAgent{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), current)).To(Succeed())
			g.Expect(current.Status.Phase).To(Equal(phaseConfigError))
			g.Expect(metaConditionStatus(current.Status.Conditions, conditionTypeConfigReady)).To(Equal(metav1.ConditionFalse))
			g.Expect(metaConditionReason(current.Status.Conditions, conditionTypeConfigReady)).To(Equal("InvalidConfig"))
			g.Expect(metaConditionReason(current.Status.Conditions, conditionTypeReady)).To(Equal("InvalidConfig"))
		}).Should(Succeed())

		configMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, client.ObjectKey{Name: generatedConfigMapName(agent.Name, "config"), Namespace: namespace.Name}, configMap)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		statefulSet := &appsv1.StatefulSet{}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(agent), statefulSet)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})

func metaConditionStatus(conditions []metav1.Condition, conditionType string) metav1.ConditionStatus {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return metav1.ConditionUnknown
}

func metaConditionReason(conditions []metav1.Condition, conditionType string) string {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Reason
		}
	}
	return ""
}
