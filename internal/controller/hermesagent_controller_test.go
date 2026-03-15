package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
)

func TestReconcileUpdatesStatefulSetConfigHashAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}

	var initialStatefulSet appsv1.StatefulSet
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &initialStatefulSet); err != nil {
		t.Fatalf("get initial StatefulSet returned error: %v", err)
	}
	initialHash := initialStatefulSet.Spec.Template.Annotations[configHashAnnotation]
	if initialHash == "" {
		t.Fatal("expected initial StatefulSet pod template annotation to include config hash")
	}

	updatedAgent := &hermesv1alpha1.HermesAgent{}
	if err := k8sClient.Get(context.Background(), req.NamespacedName, updatedAgent); err != nil {
		t.Fatalf("get HermesAgent returned error: %v", err)
	}
	updatedAgent.Spec.Config.Raw = testUpdatedConfig
	if err := k8sClient.Update(context.Background(), updatedAgent); err != nil {
		t.Fatalf("update HermesAgent returned error: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	var updatedStatefulSet appsv1.StatefulSet
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet returned error: %v", err)
	}
	updatedHash := updatedStatefulSet.Spec.Template.Annotations[configHashAnnotation]
	if updatedHash == "" {
		t.Fatal("expected updated StatefulSet pod template annotation to include config hash")
	}
	if initialHash == updatedHash {
		t.Fatalf("expected StatefulSet pod template config hash to change, got %q", updatedHash)
	}
}

func TestReconcileCreatesOwnedPersistentVolumeClaim(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-1"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Storage: hermesv1alpha1.HermesAgentStorageSpec{
				Persistence: hermesv1alpha1.HermesAgentPersistenceSpec{
					Size:        testPersistentVolumeSize,
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	var persistentVolumeClaim corev1.PersistentVolumeClaim
	pvcKey := client.ObjectKey{Name: persistentVolumeClaimName(agent.Name), Namespace: agent.Namespace}
	if err := k8sClient.Get(context.Background(), pvcKey, &persistentVolumeClaim); err != nil {
		t.Fatalf("get PersistentVolumeClaim returned error: %v", err)
	}
	if persistentVolumeClaim.Spec.Resources.Requests.Storage().String() != testPersistentVolumeSize {
		t.Fatalf("expected PVC storage request %s, got %s", testPersistentVolumeSize, persistentVolumeClaim.Spec.Resources.Requests.Storage().String())
	}
	if !metav1.IsControlledBy(&persistentVolumeClaim, agent) {
		t.Fatal("expected PersistentVolumeClaim to be owned by HermesAgent")
	}

	var persistentVolumeClaimList corev1.PersistentVolumeClaimList
	if err := k8sClient.List(context.Background(), &persistentVolumeClaimList, client.InNamespace(agent.Namespace)); err != nil {
		t.Fatalf("list PersistentVolumeClaims returned error: %v", err)
	}
	if len(persistentVolumeClaimList.Items) != 1 {
		t.Fatalf("expected exactly 1 PersistentVolumeClaim after repeated reconcile, got %d", len(persistentVolumeClaimList.Items))
	}
}

func TestReconcileCreatesOwnedStatefulSetWithHermesWorkloadSpec(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-2"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Mode: "gateway",
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resourceMustParse(t, "500m"),
					corev1.ResourceMemory: resourceMustParse(t, "1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resourceMustParse(t, "2"),
					corev1.ResourceMemory: resourceMustParse(t, "4Gi"),
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var statefulSet appsv1.StatefulSet
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &statefulSet); err != nil {
		t.Fatalf("get StatefulSet returned error: %v", err)
	}
	if !metav1.IsControlledBy(&statefulSet, agent) {
		t.Fatal("expected StatefulSet to be owned by HermesAgent")
	}
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 {
		t.Fatalf("expected StatefulSet replicas to be 1, got %+v", statefulSet.Spec.Replicas)
	}

	container := statefulSet.Spec.Template.Spec.Containers[0]
	if container.Image != "ghcr.io/example/hermes-agent:gateway-core" {
		t.Fatalf("expected Hermes image ghcr.io/example/hermes-agent:gateway-core, got %q", container.Image)
	}
	if len(container.Args) != 2 || container.Args[0] != "hermes" || container.Args[1] != hermesGatewayMode {
		t.Fatalf("expected Hermes args [hermes gateway], got %+v", container.Args)
	}
	if container.Resources.Requests.Cpu().String() != "500m" {
		t.Fatalf("expected CPU request 500m, got %s", container.Resources.Requests.Cpu().String())
	}
	if container.Resources.Limits.Memory().String() != "4Gi" {
		t.Fatalf("expected memory limit 4Gi, got %s", container.Resources.Limits.Memory().String())
	}
	requireHermesPodSecurityContext(t, statefulSet.Spec.Template.Spec)
	requireHermesContainerSecurityContext(t, container)
	requireVolumeMount(t, container.VolumeMounts, hermesDataVolumeName, hermesDataPath)
	requireVolumeMount(t, container.VolumeMounts, hermesTmpVolumeName, hermesTmpPath)
	requireExecProbe(t, container.StartupProbe, hermesGatewayPIDFile, hermesGatewayStateFile)
	requireExecProbe(t, container.ReadinessProbe, "kill -0", hermesGatewayPIDFile)
	requireExecProbe(t, container.LivenessProbe, "kill -0", hermesGatewayPIDFile, hermesGatewayStateFile)
}

func TestReconcileDoesNotCreateServiceByDefault(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-service-default"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var serviceList corev1.ServiceList
	if err := k8sClient.List(context.Background(), &serviceList, client.InNamespace(agent.Namespace)); err != nil {
		t.Fatalf("list Services returned error: %v", err)
	}
	if len(serviceList.Items) != 0 {
		t.Fatalf("expected no Services by default, got %d", len(serviceList.Items))
	}
}

func TestReconcileLeavesNonOwnedServiceUntouchedWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-service-foreign"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
		},
	}
	foreignService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "foreign"},
			Ports:    []corev1.ServicePort{{Port: 9000, TargetPort: intstr.FromInt32(9000)}},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent, foreignService).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var service corev1.Service
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &service); err != nil {
		t.Fatalf("get Service returned error: %v", err)
	}
	if len(service.OwnerReferences) != 0 {
		t.Fatalf("expected non-owned Service to remain untouched, got owner refs %+v", service.OwnerReferences)
	}
	if service.Spec.Ports[0].Port != 9000 {
		t.Fatalf("expected non-owned Service port 9000 to remain unchanged, got %+v", service.Spec.Ports)
	}
}

func TestReconcileReturnsErrorForForeignServiceWhenEnabled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-service-conflict"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config:  hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
			Service: hermesv1alpha1.HermesAgentServiceSpec{Enabled: true, Port: 8080},
		},
	}
	foreignService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "foreign"},
			Ports:    []corev1.ServicePort{{Port: 9000, TargetPort: intstr.FromInt32(9000)}},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent, foreignService).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err == nil {
		t.Fatal("expected reconcile to fail when same-name Service is not owned by HermesAgent")
	}

	var service corev1.Service
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &service); err != nil {
		t.Fatalf("get Service returned error: %v", err)
	}
	if len(service.OwnerReferences) != 0 {
		t.Fatalf("expected foreign Service owner refs to remain unchanged, got %+v", service.OwnerReferences)
	}
	if service.Spec.Ports[0].Port != 9000 {
		t.Fatalf("expected foreign Service port 9000 to remain unchanged, got %+v", service.Spec.Ports)
	}
}

func TestReconcileCreatesAndDeletesOwnedServiceWhenEnabled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-service-enabled"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
			Service: hermesv1alpha1.HermesAgentServiceSpec{
				Enabled: true,
				Type:    corev1.ServiceTypeClusterIP,
				Port:    8080,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	var service corev1.Service
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &service); err != nil {
		t.Fatalf("get Service returned error: %v", err)
	}
	if !metav1.IsControlledBy(&service, agent) {
		t.Fatal("expected Service to be owned by HermesAgent")
	}
	if service.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("expected Service type %q, got %q", corev1.ServiceTypeClusterIP, service.Spec.Type)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != 8080 {
		t.Fatalf("expected Service port 8080, got %+v", service.Spec.Ports)
	}

	updatedAgent := &hermesv1alpha1.HermesAgent{}
	if err := k8sClient.Get(context.Background(), req.NamespacedName, updatedAgent); err != nil {
		t.Fatalf("get HermesAgent returned error: %v", err)
	}
	updatedAgent.Spec.Service.Enabled = false
	if err := k8sClient.Update(context.Background(), updatedAgent); err != nil {
		t.Fatalf("update HermesAgent returned error: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile after disabling service returned error: %v", err)
	}

	if err := k8sClient.Get(context.Background(), req.NamespacedName, &service); !apierrors.IsNotFound(err) {
		t.Fatalf("expected Service to be deleted when disabled, got error: %v", err)
	}
}

func TestReconcileDoesNotCreateNetworkPolicyByDefault(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-networkpolicy-default"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var networkPolicyList networkingv1.NetworkPolicyList
	if err := k8sClient.List(context.Background(), &networkPolicyList, client.InNamespace(agent.Namespace)); err != nil {
		t.Fatalf("list NetworkPolicies returned error: %v", err)
	}
	if len(networkPolicyList.Items) != 0 {
		t.Fatalf("expected no NetworkPolicies by default, got %d", len(networkPolicyList.Items))
	}
}

func TestReconcileLeavesNonOwnedNetworkPolicyUntouchedWhenDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-networkpolicy-foreign"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
		},
	}
	foreignNetworkPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "foreign"}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: portIntOrString(8443)}},
			}},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent, foreignNetworkPolicy).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var networkPolicy networkingv1.NetworkPolicy
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &networkPolicy); err != nil {
		t.Fatalf("get NetworkPolicy returned error: %v", err)
	}
	if len(networkPolicy.OwnerReferences) != 0 {
		t.Fatalf("expected non-owned NetworkPolicy to remain untouched, got owner refs %+v", networkPolicy.OwnerReferences)
	}
	if networkPolicy.Spec.Egress[0].Ports[0].Port == nil || networkPolicy.Spec.Egress[0].Ports[0].Port.IntVal != 8443 {
		t.Fatalf("expected non-owned NetworkPolicy port 8443 to remain unchanged, got %+v", networkPolicy.Spec.Egress)
	}
}

func TestReconcileReturnsErrorForForeignNetworkPolicyWhenEnabled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	enabled := true
	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-networkpolicy-conflict"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config:        hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
			NetworkPolicy: hermesv1alpha1.HermesAgentNetworkPolicySpec{Enabled: &enabled},
		},
	}
	foreignNetworkPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "foreign"}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: portIntOrString(8443)}},
			}},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent, foreignNetworkPolicy).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err == nil {
		t.Fatal("expected reconcile to fail when same-name NetworkPolicy is not owned by HermesAgent")
	}

	var networkPolicy networkingv1.NetworkPolicy
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &networkPolicy); err != nil {
		t.Fatalf("get NetworkPolicy returned error: %v", err)
	}
	if len(networkPolicy.OwnerReferences) != 0 {
		t.Fatalf("expected foreign NetworkPolicy owner refs to remain unchanged, got %+v", networkPolicy.OwnerReferences)
	}
	if networkPolicy.Spec.Egress[0].Ports[0].Port == nil || networkPolicy.Spec.Egress[0].Ports[0].Port.IntVal != 8443 {
		t.Fatalf("expected foreign NetworkPolicy port 8443 to remain unchanged, got %+v", networkPolicy.Spec.Egress)
	}
}

func TestReconcileCreatesAndDeletesOwnedNetworkPolicyWhenEnabled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	enabled := true
	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-networkpolicy-enabled"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config:        hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
			Terminal:      hermesv1alpha1.HermesAgentTerminalSpec{Backend: "ssh"},
			NetworkPolicy: hermesv1alpha1.HermesAgentNetworkPolicySpec{Enabled: &enabled},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	var networkPolicy networkingv1.NetworkPolicy
	if err := k8sClient.Get(context.Background(), req.NamespacedName, &networkPolicy); err != nil {
		t.Fatalf("get NetworkPolicy returned error: %v", err)
	}
	if !metav1.IsControlledBy(&networkPolicy, agent) {
		t.Fatal("expected NetworkPolicy to be owned by HermesAgent")
	}
	if len(networkPolicy.Spec.PolicyTypes) != 1 || networkPolicy.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Fatalf("expected egress-only NetworkPolicy, got %+v", networkPolicy.Spec.PolicyTypes)
	}
	if len(networkPolicy.Spec.Egress) != 3 {
		t.Fatalf("expected ssh-enabled NetworkPolicy to include 3 egress rules, got %d", len(networkPolicy.Spec.Egress))
	}

	updatedAgent := &hermesv1alpha1.HermesAgent{}
	if err := k8sClient.Get(context.Background(), req.NamespacedName, updatedAgent); err != nil {
		t.Fatalf("get HermesAgent returned error: %v", err)
	}
	updatedAgent.Spec.NetworkPolicy.Enabled = nil
	if err := k8sClient.Update(context.Background(), updatedAgent); err != nil {
		t.Fatalf("update HermesAgent returned error: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile after disabling NetworkPolicy returned error: %v", err)
	}

	if err := k8sClient.Get(context.Background(), req.NamespacedName, &networkPolicy); !apierrors.IsNotFound(err) {
		t.Fatalf("expected NetworkPolicy to be deleted when disabled, got error: %v", err)
	}
}

func TestReconcileUpdatesStatusForPendingResources(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-3"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedAgent := &hermesv1alpha1.HermesAgent{}
	if err := k8sClient.Get(context.Background(), req.NamespacedName, updatedAgent); err != nil {
		t.Fatalf("get HermesAgent returned error: %v", err)
	}
	if updatedAgent.Status.ObservedGeneration != updatedAgent.Generation {
		t.Fatalf("expected observedGeneration %d, got %d", updatedAgent.Generation, updatedAgent.Status.ObservedGeneration)
	}
	if updatedAgent.Status.Phase != phaseStoragePending {
		t.Fatalf("expected phase StoragePending, got %q", updatedAgent.Status.Phase)
	}
	if updatedAgent.Status.ReadyReplicas != 0 {
		t.Fatalf("expected readyReplicas 0, got %d", updatedAgent.Status.ReadyReplicas)
	}
	if updatedAgent.Status.PersistenceBound {
		t.Fatal("expected persistenceBound to be false while PVC is pending")
	}
	requireStatusCondition(t, updatedAgent.Status, conditionTypeConfigReady, metav1.ConditionTrue, "ConfigReconciled")
	requireStatusCondition(t, updatedAgent.Status, conditionTypePersistenceReady, metav1.ConditionFalse, "PersistentVolumeClaimPending")
	requireStatusCondition(t, updatedAgent.Status, conditionTypeWorkloadReady, metav1.ConditionFalse, "WaitingForPersistence")
	requireStatusCondition(t, updatedAgent.Status, conditionTypeReady, metav1.ConditionFalse, "PersistentVolumeClaimPending")
}

func TestReconcileUpdatesStatusForReadyResources(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := hermesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(HermesAgent) returned error: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(CoreV1) returned error: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(AppsV1) returned error: %v", err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(NetworkingV1) returned error: %v", err)
	}

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: testNamespace, UID: "uid-4"},
		Spec: hermesv1alpha1.HermesAgentSpec{
			Image: hermesv1alpha1.HermesAgentImageSpec{
				Repository: "ghcr.io/example/hermes-agent",
				Tag:        "gateway-core",
				PullPolicy: corev1.PullIfNotPresent,
			},
			Config: hermesv1alpha1.HermesAgentConfigSource{Raw: testInlineConfig},
		},
	}
	persistentVolumeClaim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: persistentVolumeClaimName(agent.Name), Namespace: agent.Namespace},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	oneReplica := int32(1)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: agent.Name, Namespace: agent.Namespace, Generation: 1},
		Spec:       appsv1.StatefulSetSpec{Replicas: &oneReplica},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 1,
			ReadyReplicas:      1,
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&hermesv1alpha1.HermesAgent{}).
		WithObjects(agent, persistentVolumeClaim, statefulSet).
		Build()

	reconciler := &HermesAgentReconciler{Client: k8sClient, Scheme: scheme}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(agent)}
	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedAgent := &hermesv1alpha1.HermesAgent{}
	if err := k8sClient.Get(context.Background(), req.NamespacedName, updatedAgent); err != nil {
		t.Fatalf("get HermesAgent returned error: %v", err)
	}
	if updatedAgent.Status.Phase != "Ready" {
		t.Fatalf("expected phase Ready, got %q", updatedAgent.Status.Phase)
	}
	if updatedAgent.Status.ReadyReplicas != 1 {
		t.Fatalf("expected readyReplicas 1, got %d", updatedAgent.Status.ReadyReplicas)
	}
	if !updatedAgent.Status.PersistenceBound {
		t.Fatal("expected persistenceBound to be true when PVC is bound")
	}
	requireStatusCondition(t, updatedAgent.Status, conditionTypeConfigReady, metav1.ConditionTrue, "ConfigReconciled")
	requireStatusCondition(t, updatedAgent.Status, conditionTypePersistenceReady, metav1.ConditionTrue, "PersistentVolumeClaimBound")
	requireStatusCondition(t, updatedAgent.Status, conditionTypeWorkloadReady, metav1.ConditionTrue, "StatefulSetReady")
	requireStatusCondition(t, updatedAgent.Status, conditionTypeReady, metav1.ConditionTrue, "Ready")
}
