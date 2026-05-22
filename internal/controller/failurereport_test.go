package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// failedPreviewForReport returns a Preview that has failed on a migration, with
// the diagnostic signals the operator populates before captureFailureReport runs.
func failedPreviewForReport() *platformv1alpha1.Preview {
	return &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-77"},
		Spec: platformv1alpha1.PreviewSpec{
			Branch:   "feature/catalog",
			PRNumber: 77,
			Image:    "ghcr.io/acme/app:sha",
			ChangeContext: &platformv1alpha1.ChangeContextSpec{
				DiffRef: platformv1alpha1.DiffRef{Repository: "acme/app", HeadSHA: "abc123"},
				ChangedFiles: []platformv1alpha1.ChangedFile{
					{Path: "migrations/versions/003_fault.py", Type: platformv1alpha1.ChangeFileTypeDatabaseMigration},
				},
			},
		},
		Status: platformv1alpha1.PreviewStatus{
			Phase:         platformv1alpha1.PhaseFailed,
			NamespaceName: "preview-pr-77",
			Diagnostics: &platformv1alpha1.DiagnosticsStatus{
				Reason:     "MigrationFailed",
				Component:  "migration",
				Message:    "Job migration failed",
				LastEvents: []string{"Job/migration: BackoffLimitExceeded"},
			},
		},
	}
}

func TestEnsureFailureReportCreatesReport(t *testing.T) {
	scheme := testAIScheme(t)
	ctx := context.Background()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.FailureReport{}).
		Build()

	fr, err := EnsureFailureReport(ctx, cl, failedPreviewForReport())
	if err != nil {
		t.Fatalf("EnsureFailureReport returned error: %v", err)
	}
	if fr == nil || fr.Name != "pr-77-failure" {
		t.Fatalf("unexpected FailureReport name: %+v", fr)
	}
	if fr.Spec.PRNumber != 77 || fr.Spec.CommitSHA != "abc123" {
		t.Errorf("FailureReport spec not populated from Preview: %+v", fr.Spec)
	}
	if len(fr.Status.EvidenceItems) == 0 {
		t.Error("FailureReport captured no evidence items")
	}

	stored := &platformv1alpha1.FailureReport{}
	if err := cl.Get(ctx, types.NamespacedName{Name: "pr-77-failure"}, stored); err != nil {
		t.Fatalf("FailureReport was not persisted: %v", err)
	}
	if stored.Status.Phase != platformv1alpha1.FailureReportPhaseCaptured {
		t.Errorf("persisted FailureReport phase = %q, want Captured", stored.Status.Phase)
	}
}

func TestEnsureFailureReportIsIdempotent(t *testing.T) {
	scheme := testAIScheme(t)
	ctx := context.Background()
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.FailureReport{}).
		Build()

	preview := failedPreviewForReport()

	// Three captures of the same failed Preview must not create duplicate reports.
	for i := 0; i < 3; i++ {
		if _, err := EnsureFailureReport(ctx, cl, preview); err != nil {
			t.Fatalf("EnsureFailureReport call %d returned error: %v", i+1, err)
		}
	}

	list := &platformv1alpha1.FailureReportList{}
	if err := cl.List(ctx, list); err != nil {
		t.Fatalf("listing FailureReports: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected exactly 1 FailureReport after repeated capture, got %d", len(list.Items))
	}
}
