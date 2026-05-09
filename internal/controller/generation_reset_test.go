package controller

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResetDerivedStateSkipsTransientDatabaseRequests(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pr-34",
			Generation: 12,
		},
		Spec: platformv1alpha1.PreviewSpec{
			Database: &platformv1alpha1.DatabaseSpec{
				Enabled:           true,
				CheckpointRestore: "post-seed",
			},
		},
		Status: platformv1alpha1.PreviewStatus{
			ObservedGeneration: 11,
		},
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeJobName, Namespace: "preview-pr-34"}}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testSuiteConfigMap, Namespace: "preview-pr-34"}}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(c).
		WithObjects(c, job, cm).
		Build()

	reconciler := &PreviewReconciler{
		Client: cl,
		Scheme: scheme,
	}

	if err := reconciler.resetDerivedStateForNewGeneration(context.Background(), c, "preview-pr-34"); err != nil {
		t.Fatalf("resetDerivedStateForNewGeneration returned error: %v", err)
	}

	updated := &platformv1alpha1.Preview{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "pr-34"}, updated); err != nil {
		t.Fatalf("failed to fetch updated preview: %v", err)
	}
	if updated.Status.ObservedGeneration != updated.Generation {
		t.Fatalf("observedGeneration = %d, want %d", updated.Status.ObservedGeneration, updated.Generation)
	}

	if err := cl.Get(context.Background(), types.NamespacedName{Name: smokeJobName, Namespace: "preview-pr-34"}, &batchv1.Job{}); err != nil {
		t.Fatalf("expected smoke job to remain present: %v", err)
	}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: testSuiteConfigMap, Namespace: "preview-pr-34"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("expected test suite configmap to remain present: %v", err)
	}
}

func TestResetDerivedStateSkipsTransientAIRerunRequests(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pr-34",
			Generation: 12,
		},
		Spec: platformv1alpha1.PreviewSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled:        true,
				RerunRequested: true,
			},
		},
		Status: platformv1alpha1.PreviewStatus{
			ObservedGeneration: 11,
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				Phase: phaseFailed,
			},
		},
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeJobName, Namespace: "preview-pr-34"}}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testSuiteConfigMap, Namespace: "preview-pr-34"}}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(c).
		WithObjects(c, job, cm).
		Build()

	reconciler := &PreviewReconciler{
		Client: cl,
		Scheme: scheme,
	}

	if err := reconciler.resetDerivedStateForNewGeneration(context.Background(), c, "preview-pr-34"); err != nil {
		t.Fatalf("resetDerivedStateForNewGeneration returned error: %v", err)
	}

	updated := &platformv1alpha1.Preview{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "pr-34"}, updated); err != nil {
		t.Fatalf("failed to fetch updated preview: %v", err)
	}
	if updated.Status.ObservedGeneration != updated.Generation {
		t.Fatalf("observedGeneration = %d, want %d", updated.Status.ObservedGeneration, updated.Generation)
	}

	if err := cl.Get(context.Background(), types.NamespacedName{Name: smokeJobName, Namespace: "preview-pr-34"}, &batchv1.Job{}); err != nil {
		t.Fatalf("expected smoke job to remain present: %v", err)
	}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: testSuiteConfigMap, Namespace: "preview-pr-34"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("expected test suite configmap to remain present: %v", err)
	}
}
