package controller

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHandleAIRerunRequestedClearsDerivedState(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-34"},
		Spec: platformv1alpha1.CellenzaSpec{
			PRNumber: 34,
			Database: &platformv1alpha1.DatabaseSpec{Enabled: true},
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled:        true,
				RerunRequested: true,
			},
		},
		Status: platformv1alpha1.CellenzaStatus{
			Database: &platformv1alpha1.DatabaseStatus{
				Ready:     true,
				Migration: phaseSucceeded,
				Seed:      phaseSucceeded,
			},
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				Phase:       phaseFailed,
				SeedStatus:  phaseSucceeded,
				TestsStatus: phaseFailed,
				Error:       "previous failure",
			},
			Conditions: []metav1.Condition{{
				Type:   platformv1alpha1.ConditionAIEnrichmentReady,
				Status: metav1.ConditionFalse,
			}},
		},
	}

	objects := []client.Object{
		c,
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: migrationJobName, Namespace: "preview-pr-34"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: seedJobName, Namespace: "preview-pr-34"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeJobName, Namespace: "preview-pr-34"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: regressionJobName, Namespace: "preview-pr-34"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: e2eJobName, Namespace: "preview-pr-34"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: aiSeedJobName, Namespace: "preview-pr-34"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: aiTestJobName, Namespace: "preview-pr-34"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: aiSchemaJobName, Namespace: "preview-pr-34"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiEnrichmentConfigMap, Namespace: "preview-pr-34"}},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(c).
		WithObjects(objects...).
		Build()

	reconciler := &CellenzaReconciler{
		Client: cl,
		Scheme: scheme,
	}

	handled, result, err := reconciler.handleAIRerunRequested(context.Background(), c, "preview-pr-34")
	if err != nil {
		t.Fatalf("handleAIRerunRequested returned error: %v", err)
	}
	if !handled {
		t.Fatalf("expected AI rerun request to be handled")
	}
	if !result.Requeue {
		t.Fatalf("expected AI rerun request to trigger requeue, got %#v", result)
	}

	updated := &platformv1alpha1.Cellenza{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "pr-34"}, updated); err != nil {
		t.Fatalf("failed to fetch updated cellenza: %v", err)
	}
	if updated.Status.Database == nil {
		t.Fatalf("expected database status to remain present")
	}
	if updated.Status.Database.Migration != "" || updated.Status.Database.Seed != "" || updated.Status.Database.Ready {
		t.Fatalf("expected database progress to be cleared, got %#v", updated.Status.Database)
	}
	if updated.Status.AIEnrichment == nil || !updated.Status.AIEnrichment.RerunOnly {
		t.Fatalf("expected AI rerun marker to be set, got %#v", updated.Status.AIEnrichment)
	}
	if updated.Status.AIEnrichment.Phase != "" || updated.Status.AIEnrichment.Error != "" || len(updated.Status.AIEnrichment.TestResults) != 0 {
		t.Fatalf("expected AI status to be reset, got %#v", updated.Status.AIEnrichment)
	}
	if len(updated.Status.Conditions) != 0 {
		t.Fatalf("expected AI condition to be cleared, got %#v", updated.Status.Conditions)
	}

	for _, key := range []types.NamespacedName{
		{Name: migrationJobName, Namespace: "preview-pr-34"},
		{Name: seedJobName, Namespace: "preview-pr-34"},
		{Name: smokeJobName, Namespace: "preview-pr-34"},
		{Name: regressionJobName, Namespace: "preview-pr-34"},
		{Name: e2eJobName, Namespace: "preview-pr-34"},
		{Name: aiSeedJobName, Namespace: "preview-pr-34"},
		{Name: aiTestJobName, Namespace: "preview-pr-34"},
		{Name: aiSchemaJobName, Namespace: "preview-pr-34"},
	} {
		if err := cl.Get(context.Background(), key, &batchv1.Job{}); err == nil {
			t.Fatalf("expected job %s/%s to be deleted", key.Namespace, key.Name)
		}
	}

	if err := cl.Get(context.Background(), types.NamespacedName{Name: aiEnrichmentConfigMap, Namespace: "preview-pr-34"}, &corev1.ConfigMap{}); err == nil {
		t.Fatalf("expected AI configmap to be deleted")
	}
}

func TestReconcileAIEnrichmentCompletesAIRerun(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-34"},
		Spec: platformv1alpha1.CellenzaSpec{
			AIEnrichment: &platformv1alpha1.AIEnrichmentSpec{
				Enabled:        true,
				RerunRequested: true,
			},
		},
		Status: platformv1alpha1.CellenzaStatus{
			AIEnrichment: &platformv1alpha1.AIEnrichmentStatus{
				Phase:     phaseSucceeded,
				RerunOnly: true,
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(c).
		WithObjects(c).
		Build()

	reconciler := &CellenzaReconciler{
		Client: cl,
		Scheme: scheme,
	}

	if _, err := reconciler.reconcileAIEnrichment(context.Background(), c, "preview-pr-34"); err != nil {
		t.Fatalf("reconcileAIEnrichment returned error: %v", err)
	}

	updated := &platformv1alpha1.Cellenza{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "pr-34"}, updated); err != nil {
		t.Fatalf("failed to fetch updated cellenza: %v", err)
	}
	if updated.Spec.AIEnrichment == nil || updated.Spec.AIEnrichment.RerunRequested {
		t.Fatalf("expected rerunRequested to be cleared, got %#v", updated.Spec.AIEnrichment)
	}
	if updated.Status.AIEnrichment == nil || updated.Status.AIEnrichment.RerunOnly {
		t.Fatalf("expected rerunOnly to be cleared, got %#v", updated.Status.AIEnrichment)
	}
}
