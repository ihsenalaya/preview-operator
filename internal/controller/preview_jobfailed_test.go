package controller

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// TestReconcileDatabaseTaskSetsFailedWhenJobFails verifies that the controller
// detects a failed migration Job and propagates the failure to the Preview status.
func TestReconcileDatabaseTaskSetsFailedWhenJobFails(t *testing.T) {
	scheme := testAIScheme(t)

	const nsName = "preview-pr-99"

	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-99"},
		Spec: platformv1alpha1.PreviewSpec{
			PRNumber: 99,
			Image:    "nginx:alpine",
			Database: &platformv1alpha1.DatabaseSpec{
				Enabled: true,
				Migration: &platformv1alpha1.DatabaseTaskSpec{
					Enabled: true,
					Command: []string{"alembic", "upgrade", "head"},
				},
			},
		},
	}

	// postgres-migrate Job whose backoff limit has been reached.
	failedJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: migrationJobName, Namespace: nsName},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				{
					Type:    batchv1.JobFailed,
					Status:  corev1.ConditionTrue,
					Message: "Job has reached the specified backoff limit",
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(c).
		WithObjects(c, failedJob).
		Build()

	r := &PreviewReconciler{Client: cl, Scheme: scheme}

	done, err := r.reconcileDatabaseTask(
		context.Background(),
		c,
		nsName,
		componentMigration,
		migrationJobName,
		c.Spec.Database.Migration,
	)

	if err == nil {
		t.Fatal("expected error from reconcileDatabaseTask when job is failed, got nil")
	}
	if done {
		t.Fatalf("expected done=false for a failed job, got true")
	}
	if c.Status.Database == nil {
		t.Fatal("expected database status to be populated after job failure")
	}
	if c.Status.Database.Migration != phaseFailed {
		t.Fatalf("expected migration status=%q, got %q", phaseFailed, c.Status.Database.Migration)
	}
}

// TestReconcileDatabaseTaskResumesWhenJobAlreadySucceeded verifies that a previously
// succeeded Job is treated as a no-op — the controller does not recreate it.
func TestReconcileDatabaseTaskResumesWhenJobAlreadySucceeded(t *testing.T) {
	scheme := testAIScheme(t)

	const nsName = "preview-pr-100"

	c := &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-100"},
		Spec: platformv1alpha1.PreviewSpec{
			PRNumber: 100,
			Image:    "nginx:alpine",
			Database: &platformv1alpha1.DatabaseSpec{
				Enabled: true,
				Migration: &platformv1alpha1.DatabaseTaskSpec{
					Enabled: true,
					Command: []string{"alembic", "upgrade", "head"},
				},
			},
		},
		Status: platformv1alpha1.PreviewStatus{
			Database: &platformv1alpha1.DatabaseStatus{
				Migration: phaseSucceeded,
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(c).
		WithObjects(c).
		Build()

	r := &PreviewReconciler{Client: cl, Scheme: scheme}

	done, err := r.reconcileDatabaseTask(
		context.Background(),
		c,
		nsName,
		componentMigration,
		migrationJobName,
		c.Spec.Database.Migration,
	)

	if err != nil {
		t.Fatalf("expected no error when job already succeeded, got: %v", err)
	}
	if !done {
		t.Fatal("expected done=true when migration already succeeded")
	}

	// No new Job should have been created.
	jobList := &batchv1.JobList{}
	if err := cl.List(context.Background(), jobList); err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	if len(jobList.Items) != 0 {
		t.Fatalf("expected 0 jobs (no new job created), got %d", len(jobList.Items))
	}
}
