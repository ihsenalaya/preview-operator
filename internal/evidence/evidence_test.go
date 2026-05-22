package evidence

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// failedPreview returns a Preview that has failed on an invalid SQL migration
// (scenario F1), with the diagnostic signals the operator would have populated.
func failedPreview() *platformv1alpha1.Preview {
	return &platformv1alpha1.Preview{
		ObjectMeta: metav1.ObjectMeta{Name: "pr-42"},
		Spec: platformv1alpha1.PreviewSpec{
			Branch:   "feature/catalog",
			PRNumber: 42,
			Image:    "ghcr.io/acme/app:abc123",
			ChangeContext: &platformv1alpha1.ChangeContextSpec{
				DiffRef: platformv1alpha1.DiffRef{
					Repository:        "acme/app",
					PullRequestNumber: 42,
					HeadSHA:           "deadbeefcafe",
				},
				ChangedFiles: []platformv1alpha1.ChangedFile{
					{Path: "migrations/versions/002_add_index.py", Type: platformv1alpha1.ChangeFileTypeDatabaseMigration},
					{Path: "README.md", Type: platformv1alpha1.ChangeFileTypeDocs},
				},
				DiffPatch: "--- a/migrations/versions/002_add_index.py\n+++ b/migrations/versions/002_add_index.py\n+CREATE INDX broken",
			},
		},
		Status: platformv1alpha1.PreviewStatus{
			NamespaceName: "preview-pr-42",
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "MigrationFailed", Message: "migration job failed"},
			},
			Diagnostics: &platformv1alpha1.DiagnosticsStatus{
				Reason:    "MigrationFailed",
				Component: "migration",
				Message:   "Job migration failed",
				LastEvents: []string{
					"Job/migration: BackoffLimitExceeded",
				},
				SignificantLogs: []platformv1alpha1.DiagnosticLogExcerpt{
					{
						Component: "migration",
						Source:    "pod/migration-xyz container/migration",
						Lines:     []string{`psycopg2.errors.SyntaxError: syntax error at or near "INDX"`},
					},
				},
				PodLogs: []string{
					"Traceback (most recent call last):",
					"config DB_PASSWORD=topsecretvalue loaded",
					"SyntaxError",
				},
			},
			Tests: &platformv1alpha1.TestSuiteStatus{
				Phase:     "Failed",
				Smoke:     platformv1alpha1.TestResult{Phase: "Succeeded", Passed: 2},
				Migration: platformv1alpha1.TestResult{Phase: "Failed", Failed: 1, Output: []string{"FAIL migration 002: syntax error"}},
			},
		},
	}
}

func TestEvidenceIDDeterministic(t *testing.T) {
	a := EvidenceID(platformv1alpha1.EvidenceTypePodLog, "logs-collector", "pod/app", "boom")
	b := EvidenceID(platformv1alpha1.EvidenceTypePodLog, "logs-collector", "pod/app", "boom")
	if a != b {
		t.Fatalf("EvidenceID is not deterministic: %q != %q", a, b)
	}
	if !strings.HasPrefix(a, "podlog-") {
		t.Errorf("EvidenceID = %q, want a lowercased type prefix 'podlog-'", a)
	}
	if c := EvidenceID(platformv1alpha1.EvidenceTypePodLog, "logs-collector", "pod/app", "different"); c == a {
		t.Errorf("EvidenceID collision: different content produced the same ID %q", a)
	}
	if d := EvidenceID(platformv1alpha1.EvidenceTypeJobLog, "logs-collector", "pod/app", "boom"); d == a {
		t.Errorf("EvidenceID collision: different type produced the same ID %q", a)
	}
}

func TestAssembleBundleFromPreview(t *testing.T) {
	b := AssembleBundle(failedPreview())

	if b.PreviewName != "pr-42" || b.PRNumber != 42 || b.Namespace != "preview-pr-42" {
		t.Errorf("bundle identity not seeded from Preview: %+v", b)
	}
	if b.CommitSHA != "deadbeefcafe" {
		t.Errorf("CommitSHA = %q, want deadbeefcafe", b.CommitSHA)
	}

	want := map[platformv1alpha1.EvidenceType]int{
		platformv1alpha1.EvidenceTypeGitDiff:          1,
		platformv1alpha1.EvidenceTypeChangedFile:      2,
		platformv1alpha1.EvidenceTypePreviewCondition: 1,
		platformv1alpha1.EvidenceTypeKubernetesEvent:  1,
		platformv1alpha1.EvidenceTypeJobLog:           1,
		platformv1alpha1.EvidenceTypePodLog:           1,
		platformv1alpha1.EvidenceTypeTestResult:       2,
	}
	got := map[platformv1alpha1.EvidenceType]int{}
	for _, it := range b.Items() {
		got[it.Type]++
		if it.ID == "" {
			t.Errorf("evidence item has empty ID: %+v", it)
		}
	}
	for typ, n := range want {
		if got[typ] != n {
			t.Errorf("evidence type %s: got %d items, want %d", typ, got[typ], n)
		}
	}
}

func TestAssembleBundleNilPreview(t *testing.T) {
	if b := AssembleBundle(nil); b == nil || b.Len() != 0 {
		t.Errorf("AssembleBundle(nil) must return an empty, non-nil bundle")
	}
}

func TestBundleNoDuplicateItemsOnRepeatedReconcile(t *testing.T) {
	p := failedPreview()
	first := AssembleBundle(p)
	second := AssembleBundle(p)

	if first.Len() != second.Len() {
		t.Fatalf("repeated reconcile produced different bundle sizes: %d vs %d", first.Len(), second.Len())
	}
	for _, it := range first.Items() {
		if !second.Has(it.ID) {
			t.Errorf("evidence ID %q not stable across reconciles", it.ID)
		}
	}

	// Adding the same logical item again must be a no-op.
	before := first.Len()
	sample := first.Items()[0]
	first.Add(sample)
	first.Add(sample)
	if first.Len() != before {
		t.Errorf("duplicate evidence item was added: len %d -> %d", before, first.Len())
	}
}

func TestBundleGroundingAcceptsKnownRefs(t *testing.T) {
	b := AssembleBundle(failedPreview())
	known := b.Items()[0].ID

	err := b.SetDiagnosis(&platformv1alpha1.FailureDiagnosis{
		ProbableCause: "invalid SQL in migration",
		Component:     "migration-job",
		Category:      platformv1alpha1.FailureCategoryDatabase,
		Confidence:    "high",
		EvidenceRefs:  []string{known},
	})
	if err != nil {
		t.Fatalf("grounded diagnosis with a known ref was rejected: %v", err)
	}
	if b.Diagnosis() == nil {
		t.Error("diagnosis was not attached to the bundle")
	}
}

func TestBundleGroundingRejectsUnknownRefs(t *testing.T) {
	b := AssembleBundle(failedPreview())

	err := b.SetDiagnosis(&platformv1alpha1.FailureDiagnosis{
		ProbableCause: "fabricated cause",
		EvidenceRefs:  []string{"podlog-000000000000"},
	})
	if err == nil {
		t.Fatal("diagnosis referencing an unknown evidence ID must be rejected")
	}
	if b.Diagnosis() != nil {
		t.Error("ungrounded diagnosis must not be attached to the bundle")
	}
}

func TestRedactionAppliedByCollectors(t *testing.T) {
	b := AssembleBundle(failedPreview())
	var podLog *platformv1alpha1.FailureEvidenceItem
	for i, it := range b.Items() {
		if it.Type == platformv1alpha1.EvidenceTypePodLog {
			podLog = &b.Items()[i]
			break
		}
	}
	if podLog == nil {
		t.Fatal("expected a PodLog evidence item")
	}
	if !podLog.Redacted {
		t.Error("PodLog item carrying a secret must be flagged Redacted")
	}
	if strings.Contains(podLog.Message, "topsecretvalue") {
		t.Errorf("secret leaked into evidence: %q", podLog.Message)
	}
}

func TestBuildFailureReportDeterministic(t *testing.T) {
	p := failedPreview()
	r1 := BuildFailureReport(AssembleBundle(p))
	r2 := BuildFailureReport(AssembleBundle(p))

	if r1.Name != r2.Name {
		t.Fatalf("FailureReport name not deterministic: %q vs %q", r1.Name, r2.Name)
	}
	if r1.Name != FailureReportName("pr-42") {
		t.Errorf("FailureReport name = %q, want %q", r1.Name, FailureReportName("pr-42"))
	}
	if len(r1.Status.EvidenceItems) != len(r2.Status.EvidenceItems) {
		t.Fatalf("FailureReport evidence count not stable: %d vs %d",
			len(r1.Status.EvidenceItems), len(r2.Status.EvidenceItems))
	}
	for i := range r1.Status.EvidenceItems {
		if r1.Status.EvidenceItems[i].ID != r2.Status.EvidenceItems[i].ID {
			t.Errorf("evidence item %d ID not stable: %q vs %q",
				i, r1.Status.EvidenceItems[i].ID, r2.Status.EvidenceItems[i].ID)
		}
	}
	if r1.Spec.PRNumber != 42 || r1.Spec.CommitSHA != "deadbeefcafe" {
		t.Errorf("FailureReport spec not populated from bundle: %+v", r1.Spec)
	}
	if r1.Status.Phase != platformv1alpha1.FailureReportPhaseCaptured {
		t.Errorf("FailureReport phase = %q, want Captured", r1.Status.Phase)
	}
}
