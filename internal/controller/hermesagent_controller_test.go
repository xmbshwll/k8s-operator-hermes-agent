package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: "default"},
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

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: "default", UID: "uid-1"},
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

	agent := &hermesv1alpha1.HermesAgent{
		ObjectMeta: metav1.ObjectMeta{Name: testAgentName, Namespace: "default", UID: "uid-2"},
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

	foundDataMount := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == hermesDataVolumeName && mount.MountPath == hermesDataPath {
			foundDataMount = true
			break
		}
	}
	if !foundDataMount {
		t.Fatal("expected StatefulSet container to mount the Hermes data volume at /data")
	}
}
