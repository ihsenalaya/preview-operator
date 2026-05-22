package scoring

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/diagnosis"
)

// reportWith builds a FailureReport carrying the given evidence items.
func reportWith(items ...platformv1alpha1.FailureEvidenceItem) *platformv1alpha1.FailureReport {
	detected := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	available := metav1.NewTime(time.Now().Add(-30 * time.Second))
	return &platformv1alpha1.FailureReport{
		Status: platformv1alpha1.FailureReportStatus{
			EvidenceLevel:         "C4",
			EvidenceItems:         items,
			BundleSizeBytes:       4096,
			FailureDetectedAt:     &detected,
			DiagnosisAvailableAt:  &available,
			TimeToDiagnosisMillis: 90_000,
		},
	}
}

func item(id string, t platformv1alpha1.EvidenceType) platformv1alpha1.FailureEvidenceItem {
	return platformv1alpha1.FailureEvidenceItem{ID: id, Type: t, Resource: "r"}
}

var f1 = Scenario{
	ID:               "F1",
	Family:           "database",
	ExpectedEvidence: []string{"JobLog", "TestResult", "ChangedFile"},
	GroundTruth:      GroundTruth{Component: "migration-job", Category: "database"},
}

func TestScoreCorrectDiagnosis(t *testing.T) {
	report := reportWith(
		item("joblog-1", platformv1alpha1.EvidenceTypeJobLog),
		item("testresult-1", platformv1alpha1.EvidenceTypeTestResult),
		item("changedfile-1", platformv1alpha1.EvidenceTypeChangedFile),
	)
	result := &diagnosis.Result{
		Engine: diagnosis.EngineRule, Mode: diagnosis.ModeGrounded, EvidenceLevel: "C4",
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "migration-job", Category: platformv1alpha1.FailureCategoryDatabase,
			Confidence: "high", EvidenceRefs: []string{"joblog-1"},
		},
	}
	sc := Score(result, report, f1)

	if !sc.Top1Correct || !sc.Top3Correct {
		t.Errorf("expected Top1/Top3 correct, got %v/%v", sc.Top1Correct, sc.Top3Correct)
	}
	if !sc.ComponentMatch || !sc.CategoryMatch || !sc.HasValidEvidenceRef {
		t.Errorf("RCA parts: component=%v category=%v evidence=%v",
			sc.ComponentMatch, sc.CategoryMatch, sc.HasValidEvidenceRef)
	}
	if sc.HallucinationRate != 0 {
		t.Errorf("hallucination rate = %v, want 0", sc.HallucinationRate)
	}
	if !sc.HasEvidenceRecall || sc.EvidenceRecall != 1.0 {
		t.Errorf("evidence recall = %v (has=%v), want 1.0", sc.EvidenceRecall, sc.HasEvidenceRecall)
	}
	if !sc.HasMTTD || sc.MTTDSeconds != 90 {
		t.Errorf("MTTD = %v (has=%v), want 90", sc.MTTDSeconds, sc.HasMTTD)
	}
}

func TestScoreWrongCategoryIsNotCorrect(t *testing.T) {
	report := reportWith(item("joblog-1", platformv1alpha1.EvidenceTypeJobLog))
	result := &diagnosis.Result{
		Engine: diagnosis.EngineRule, Mode: diagnosis.ModeGrounded,
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "migration-job", Category: platformv1alpha1.FailureCategoryInfrastructure,
			Confidence: "high", EvidenceRefs: []string{"joblog-1"},
		},
	}
	sc := Score(result, report, f1)
	if sc.Top1Correct {
		t.Error("diagnosis with wrong category must not be Top1Correct")
	}
	if !sc.ComponentMatch {
		t.Error("component should still match")
	}
	if !containsNote(sc, "partial: component ok") {
		t.Errorf("expected a partial-match note, got %v", sc.Notes)
	}
}

func TestScoreHallucinationRate(t *testing.T) {
	report := reportWith(item("joblog-1", platformv1alpha1.EvidenceTypeJobLog))
	// Free-form-style result: one valid ref kept, one invented ref also kept,
	// the invented one reported in HallucinatedRefs.
	result := &diagnosis.Result{
		Engine: diagnosis.EngineLLM, Mode: diagnosis.ModeFreeform,
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "migration-job", Category: platformv1alpha1.FailureCategoryDatabase,
			Confidence: "high", EvidenceRefs: []string{"joblog-1", "joblog-invented"},
		},
		HallucinatedRefs: []string{"joblog-invented"},
	}
	sc := Score(result, report, f1)
	if sc.TotalRefs != 2 || sc.HallucinatedRefs != 1 {
		t.Errorf("refs: total=%d hallucinated=%d, want 2/1", sc.TotalRefs, sc.HallucinatedRefs)
	}
	if sc.HallucinationRate != 0.5 {
		t.Errorf("hallucination rate = %v, want 0.5", sc.HallucinationRate)
	}
}

func TestScoreNoEvidenceRefIsFullyUnsupported(t *testing.T) {
	report := reportWith(item("joblog-1", platformv1alpha1.EvidenceTypeJobLog))
	result := &diagnosis.Result{
		Engine: diagnosis.EngineLLM, Mode: diagnosis.ModeFreeform,
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "migration-job", Category: platformv1alpha1.FailureCategoryDatabase,
			ProbableCause: "something went wrong",
		},
	}
	sc := Score(result, report, f1)
	if sc.HallucinationRate != 1.0 {
		t.Errorf("a diagnosis citing no evidence must score 1.0, got %v", sc.HallucinationRate)
	}
	if sc.HasValidEvidenceRef {
		t.Error("HasValidEvidenceRef must be false with no refs")
	}
}

func TestScoreF10Overclaim(t *testing.T) {
	f10 := Scenario{
		ID: "F10", Family: "test-reliability",
		ExpectedEvidence: []string{"TestResult"},
		GroundTruth:      GroundTruth{Component: "test-suite", Category: "test-reliability"},
	}
	report := reportWith(item("testresult-1", platformv1alpha1.EvidenceTypeTestResult))

	// Over-claim: confidently blames infrastructure on the flaky-test control.
	bad := &diagnosis.Result{
		Engine: diagnosis.EngineLLM, Mode: diagnosis.ModeFreeform,
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "app-deployment", Category: platformv1alpha1.FailureCategoryInfrastructure,
			Confidence: "high", EvidenceRefs: []string{"testresult-1"},
		},
	}
	sc := Score(bad, report, f10)
	if !sc.Overclaim || sc.HallucinationRate != 1.0 {
		t.Errorf("F10 over-claim not flagged: overclaim=%v rate=%v", sc.Overclaim, sc.HallucinationRate)
	}

	// Correct: reports flakiness — no over-claim.
	good := &diagnosis.Result{
		Engine: diagnosis.EngineRule, Mode: diagnosis.ModeGrounded,
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "test-suite", Category: platformv1alpha1.FailureCategoryTestReliability,
			Confidence: "low", EvidenceRefs: []string{"testresult-1"},
		},
	}
	scGood := Score(good, report, f10)
	if scGood.Overclaim {
		t.Error("correct F10 diagnosis must not be flagged as over-claim")
	}
	if !scGood.Top1Correct {
		t.Error("correct F10 diagnosis should be Top1Correct")
	}
}

func TestScoreEvidenceRecallPartial(t *testing.T) {
	// Only one of the three expected types captured (C1-style logs-only bundle).
	report := reportWith(item("joblog-1", platformv1alpha1.EvidenceTypeJobLog))
	result := &diagnosis.Result{
		Engine: diagnosis.EngineRule, Mode: diagnosis.ModeGrounded,
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "migration-job", Category: platformv1alpha1.FailureCategoryDatabase,
			EvidenceRefs: []string{"joblog-1"},
		},
	}
	sc := Score(result, report, f1)
	want := 1.0 / 3.0
	if sc.EvidenceRecall < want-1e-9 || sc.EvidenceRecall > want+1e-9 {
		t.Errorf("evidence recall = %v, want %v", sc.EvidenceRecall, want)
	}
}

func TestCSVRecordShapeAndEmptyCells(t *testing.T) {
	report := reportWith(item("joblog-1", platformv1alpha1.EvidenceTypeJobLog))
	result := &diagnosis.Result{
		Engine: diagnosis.EngineRule, Mode: diagnosis.ModeGrounded, EvidenceLevel: "C4",
		Diagnosis: &platformv1alpha1.FailureDiagnosis{
			Component: "migration-job", Category: platformv1alpha1.FailureCategoryDatabase,
			EvidenceRefs: []string{"joblog-1"},
		},
	}
	rec := Score(result, report, f1).CSVRecord("F1-C4-001", "kind")
	if len(rec) != len(CSVHeader) {
		t.Fatalf("CSV record has %d fields, header has %d", len(rec), len(CSVHeader))
	}
	idx := map[string]int{}
	for i, h := range CSVHeader {
		idx[h] = i
	}
	// Columns the scorer must NOT fabricate stay empty.
	for _, col := range []string{"evidence_precision", "recommendation_score",
		"cpu_overhead", "memory_overhead", "namespace_deleted", "evidence_survived"} {
		if rec[idx[col]] != "" {
			t.Errorf("column %q must be empty (not measured), got %q", col, rec[idx[col]])
		}
	}
	if rec[idx["scenario_id"]] != "F1" || rec[idx["run_id"]] != "F1-C4-001" {
		t.Errorf("identity columns wrong: %q / %q", rec[idx["scenario_id"]], rec[idx["run_id"]])
	}
	if rec[idx["top1_correct"]] != "1" {
		t.Errorf("top1_correct = %q, want 1", rec[idx["top1_correct"]])
	}
}

func containsNote(sc *Scorecard, substr string) bool {
	for _, n := range sc.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
