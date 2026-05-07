package controller

import (
	"context"
	"testing"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHandleResetRequestedClearsPersistedDatabaseProgress(t *testing.T) {
	scheme := testAIScheme(t)
	c := &platformv1alpha1.Cellenza{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-34"},
		Spec: platformv1alpha1.CellenzaSpec{
			PRNumber: 34,
			Database: &platformv1alpha1.DatabaseSpec{
				Enabled:        true,
				ResetRequested: true,
			},
		},
		Status: platformv1alpha1.CellenzaStatus{
			Database: &platformv1alpha1.DatabaseStatus{
				Ready:     true,
				Migration: phaseSucceeded,
				Seed:      phaseSucceeded,
			},
		},
	}
	migrationJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: migrationJobName, Namespace: "preview-pr-34"}}
	seedJob := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: seedJobName, Namespace: "preview-pr-34"}}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(c).
		WithObjects(c, migrationJob, seedJob).
		Build()

	reconciler := &CellenzaReconciler{
		Client: cl,
		Scheme: scheme,
	}

	handled, result, err := reconciler.handleResetRequested(context.Background(), c, "preview-pr-34")
	if err != nil {
		t.Fatalf("handleResetRequested returned error: %v", err)
	}
	if !handled {
		t.Fatalf("handleResetRequested should have handled reset request")
	}
	if !result.Requeue {
		t.Fatalf("expected reset request to trigger requeue, got %#v", result)
	}

	updated := &platformv1alpha1.Cellenza{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "pr-34"}, updated); err != nil {
		t.Fatalf("failed to fetch updated cellenza: %v", err)
	}
	if updated.Spec.Database == nil || updated.Spec.Database.ResetRequested {
		t.Fatalf("resetRequested should be cleared, got %#v", updated.Spec.Database)
	}
	if updated.Status.Database == nil {
		t.Fatalf("expected database status to remain present")
	}
	if updated.Status.Database.Migration != "" {
		t.Fatalf("migration status = %q, want empty", updated.Status.Database.Migration)
	}
	if updated.Status.Database.Seed != "" {
		t.Fatalf("seed status = %q, want empty", updated.Status.Database.Seed)
	}
	if updated.Status.Database.Ready {
		t.Fatalf("database ready should be false after reset")
	}

	for _, key := range []types.NamespacedName{
		{Name: migrationJobName, Namespace: "preview-pr-34"},
		{Name: seedJobName, Namespace: "preview-pr-34"},
	} {
		if err := cl.Get(context.Background(), key, &batchv1.Job{}); err == nil {
			t.Fatalf("expected job %s/%s to be deleted", key.Namespace, key.Name)
		}
	}
}
