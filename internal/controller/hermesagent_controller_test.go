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
