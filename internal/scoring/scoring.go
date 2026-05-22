package scoring

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/diagnosis"
)

// CSVHeader is the results CSV column order. It MUST match
// experiments/failure-provenance/results-template.csv.
var CSVHeader = []string{
	"scenario_id", "run_id", "cluster_type", "configuration",
	"failure_detected_at", "diagnosis_available_at", "mttd_seconds",
	"top1_correct", "top3_correct",
	"evidence_precision", "evidence_recall", "hallucination_rate",
	"recommendation_score", "bundle_size_bytes",
	"cpu_overhead", "memory_overhead", "storage_overhead",
	"namespace_deleted", "evidence_survived", "notes",
}

// Scorecard holds every metric the scorer could compute for one run, plus the
// provenance needed to audit the numbers. Fields that are computable are filled;
// fields that need human annotation or cluster measurement stay zero/false and
// are emitted as an empty CSV cell, never as a fabricated value.
type Scorecard struct {
	ScenarioID    string `json:"scenarioId"`
	Configuration string `json:"configuration"`
	Engine        string `json:"engine"`
	Mode          string `json:"mode"`
	Model         string `json:"model,omitempty"`

	// RCA correctness (RQ2). Top1Correct holds only when all three parts hold.
	ComponentMatch      bool `json:"componentMatch"`
	CategoryMatch       bool `json:"categoryMatch"`
	HasValidEvidenceRef bool `json:"hasValidEvidenceRef"`
	Top1Correct         bool `json:"top1Correct"`
	Top3Correct         bool `json:"top3Correct"`

	// Hallucination (RQ4). Structural rate: cited evidence IDs absent from the
	// bundle, over all cited IDs. Decomposition into atomic claims is a human
	// step (scoring-rubric.md §4); this is its mechanical lower bound.
	TotalRefs         int     `json:"totalRefs"`
	HallucinatedRefs  int     `json:"hallucinatedRefs"`
	HallucinationRate float64 `json:"hallucinationRate"`
	Overclaim         bool    `json:"overclaim"` // F10 negative-control over-claim

	// Evidence breadth (RQ1/RQ2), type-level.
	ExpectedEvidenceTypes []string `json:"expectedEvidenceTypes,omitempty"`
	CapturedEvidenceTypes []string `json:"capturedEvidenceTypes,omitempty"`
	EvidenceRecall        float64  `json:"evidenceRecall"`
	HasEvidenceRecall     bool     `json:"hasEvidenceRecall"`

	// Timing (RQ3) and persisted-artifact size (RQ5).
	FailureDetectedAt    string  `json:"failureDetectedAt,omitempty"`
	DiagnosisAvailableAt string  `json:"diagnosisAvailableAt,omitempty"`
	MTTDSeconds          float64 `json:"mttdSeconds"`
	HasMTTD              bool    `json:"hasMTTD"`
	BundleSizeBytes      int64   `json:"bundleSizeBytes"`

	// Notes records partial matches, modelling caveats and anything a human
	// scorer should see — it is the CSV `notes` column.
	Notes []string `json:"notes,omitempty"`
}

// Score computes a Scorecard for one run from the diagnosis Result, the
// FailureReport whose evidence was diagnosed, and the scenario ground truth.
func Score(result *diagnosis.Result, report *platformv1alpha1.FailureReport, scenario Scenario) *Scorecard {
	sc := &Scorecard{
		ScenarioID:            scenario.ID,
		ExpectedEvidenceTypes: scenario.ExpectedEvidence,
	}
	if result != nil {
		sc.Engine = string(result.Engine)
		sc.Mode = string(result.Mode)
		sc.Model = result.Model
		sc.Configuration = result.EvidenceLevel
	}
	if sc.Configuration == "" && report != nil {
		sc.Configuration = report.Status.EvidenceLevel
	}

	// Record the engine/mode/model first in notes: the results CSV has no
	// dedicated column for them, and scoring-rubric.md §6 requires the LLM model
	// and version to be recorded on every LLM-in-the-loop row.
	desc := fmt.Sprintf("engine=%s mode=%s", sc.Engine, sc.Mode)
	if sc.Model != "" {
		desc += " model=" + sc.Model
	}
	sc.Notes = append(sc.Notes, desc)

	evidenceIDs := evidenceIDSet(report)
	capturedTypes := capturedEvidenceTypes(report)
	sc.CapturedEvidenceTypes = capturedTypes
	sc.BundleSizeBytes = bundleSize(report)

	scoreRCA(sc, result, scenario, evidenceIDs)
	scoreHallucination(sc, result, scenario)
	scoreEvidenceRecall(sc, scenario, capturedTypes)
	scoreTiming(sc, report)

	return sc
}

// scoreRCA fills the three-part RCA-correctness verdict (methodology.md §4).
func scoreRCA(sc *Scorecard, result *diagnosis.Result, scenario Scenario, evidenceIDs map[string]bool) {
	if result == nil || result.Diagnosis == nil {
		sc.Notes = append(sc.Notes, "no diagnosis produced")
		return
	}
	d := result.Diagnosis

	sc.ComponentMatch = componentMatch(d.Component, scenario.GroundTruth.Component)
	sc.CategoryMatch = strings.EqualFold(string(d.Category), scenario.GroundTruth.Category)

	for _, ref := range d.EvidenceRefs {
		if evidenceIDs[ref] {
			sc.HasValidEvidenceRef = true
			break
		}
	}

	sc.Top1Correct = sc.ComponentMatch && sc.CategoryMatch && sc.HasValidEvidenceRef
	// The rule and LLM engines emit a single ranked hypothesis, so Top-3
	// collapses onto Top-1. This is recorded so the article does not over-read
	// the Top-3 column.
	sc.Top3Correct = sc.Top1Correct
	sc.Notes = append(sc.Notes, "single-hypothesis engine: top3==top1")

	switch {
	case sc.Top1Correct:
		// correct — nothing to flag
	case sc.ComponentMatch && !sc.CategoryMatch:
		sc.Notes = append(sc.Notes, fmt.Sprintf("partial: component ok, category %q != %q",
			d.Category, scenario.GroundTruth.Category))
	case sc.CategoryMatch && !sc.ComponentMatch:
		sc.Notes = append(sc.Notes, fmt.Sprintf("partial: category ok, component %q != %q",
			d.Component, scenario.GroundTruth.Component))
	case (sc.ComponentMatch && sc.CategoryMatch) && !sc.HasValidEvidenceRef:
		sc.Notes = append(sc.Notes, "partial: component+category ok but no valid evidence reference")
	}
}

// scoreHallucination fills the structural hallucination rate and the F10
// negative-control over-claim flag (scoring-rubric.md §4).
func scoreHallucination(sc *Scorecard, result *diagnosis.Result, scenario Scenario) {
	if result == nil || result.Diagnosis == nil {
		return
	}
	d := result.Diagnosis

	cited := map[string]bool{}
	for _, ref := range d.EvidenceRefs {
		cited[ref] = true
	}
	for _, ref := range result.HallucinatedRefs {
		cited[ref] = true
	}
	sc.TotalRefs = len(cited)
	sc.HallucinatedRefs = len(result.HallucinatedRefs)

	switch {
	case sc.TotalRefs > 0:
		sc.HallucinationRate = float64(sc.HallucinatedRefs) / float64(sc.TotalRefs)
	case strings.TrimSpace(d.ProbableCause) != "":
		// A probable cause asserted with no evidence reference at all is wholly
		// unsupported (metrics.md §8).
		sc.HallucinationRate = 1.0
		sc.Notes = append(sc.Notes, "diagnosis cites no evidence: treated as fully unsupported")
	}

	// F10 negative control: confidently blaming a real component is a
	// hallucination by construction — there is no such cause.
	if scenario.ID == "F10" && !strings.EqualFold(string(d.Category), "test-reliability") {
		if strings.EqualFold(d.Confidence, "medium") || strings.EqualFold(d.Confidence, "high") {
			sc.Overclaim = true
			sc.HallucinationRate = 1.0
			sc.Notes = append(sc.Notes,
				"F10 over-claim: confidently blamed a non-test-reliability cause")
		}
	}
}

// scoreEvidenceRecall fills the type-level evidence recall (metrics.md §1, §7):
// the fraction of the scenario's expected evidence types that were captured.
func scoreEvidenceRecall(sc *Scorecard, scenario Scenario, capturedTypes []string) {
	if len(scenario.ExpectedEvidence) == 0 {
		sc.Notes = append(sc.Notes, "no expected_evidence for scenario: recall not scored")
		return
	}
	captured := map[string]bool{}
	for _, t := range capturedTypes {
		captured[strings.ToLower(t)] = true
	}
	hit := 0
	var missing []string
	for _, want := range scenario.ExpectedEvidence {
		if captured[strings.ToLower(want)] {
			hit++
		} else {
			missing = append(missing, want)
		}
	}
	sc.EvidenceRecall = float64(hit) / float64(len(scenario.ExpectedEvidence))
	sc.HasEvidenceRecall = true
	if len(missing) > 0 {
		sc.Notes = append(sc.Notes, "missing expected evidence types: "+strings.Join(missing, ","))
	}
}

// scoreTiming fills the time-to-diagnosis and persisted-artifact-size metrics
// from the operator-written FailureReport.
func scoreTiming(sc *Scorecard, report *platformv1alpha1.FailureReport) {
	if report == nil {
		return
	}
	if t := report.Status.FailureDetectedAt; t != nil && !t.IsZero() {
		sc.FailureDetectedAt = t.UTC().Format(time.RFC3339)
	}
	if t := report.Status.DiagnosisAvailableAt; t != nil && !t.IsZero() {
		sc.DiagnosisAvailableAt = t.UTC().Format(time.RFC3339)
	}
	switch {
	case report.Status.TimeToDiagnosisMillis > 0:
		sc.MTTDSeconds = float64(report.Status.TimeToDiagnosisMillis) / 1000.0
		sc.HasMTTD = true
	case report.Status.FailureDetectedAt != nil && report.Status.DiagnosisAvailableAt != nil:
		d := report.Status.DiagnosisAvailableAt.Sub(report.Status.FailureDetectedAt.Time)
		if d > 0 {
			sc.MTTDSeconds = d.Seconds()
			sc.HasMTTD = true
		}
	}
	if !sc.HasMTTD {
		sc.Notes = append(sc.Notes, "MTTD not in FailureReport (no operator diagnosis timestamp)")
	}
}

// RunFacts carries the per-run facts the scorer cannot derive from the
// FailureReport or the diagnosis Result — they are known only to the
// orchestration layer (run identity, and whether the artifact survived
// namespace teardown).
type RunFacts struct {
	RunID       string
	ClusterType string
	// NamespaceDeleted and EvidenceSurvived are "true"/"false", or "" when the
	// orchestration did not reach the teardown / survival-check step.
	NamespaceDeleted string
	EvidenceSurvived string
}

// CSVRecord renders the scorecard as one results-CSV row, in CSVHeader order.
// Run identity and the teardown/survival facts come from RunFacts; columns the
// scorer cannot measure (evidence precision, recommendation score, CPU/memory
// overhead) are emitted empty so a human fills them — never fabricated.
func (sc *Scorecard) CSVRecord(f RunFacts) []string {
	return []string{
		sc.ScenarioID,
		f.RunID,
		f.ClusterType,
		sc.Configuration,
		sc.FailureDetectedAt,
		sc.DiagnosisAvailableAt,
		optFloat(sc.MTTDSeconds, sc.HasMTTD),
		boolCell(sc.Top1Correct),
		boolCell(sc.Top3Correct),
		"", // evidence_precision — human-annotated (scoring-rubric.md §2)
		optFloat(sc.EvidenceRecall, sc.HasEvidenceRecall),
		strconv.FormatFloat(sc.HallucinationRate, 'f', 4, 64),
		"", // recommendation_score — human-annotated (scoring-rubric.md §5)
		strconv.FormatInt(sc.BundleSizeBytes, 10),
		"", // cpu_overhead — cluster measurement (RQ5)
		"", // memory_overhead — cluster measurement (RQ5)
		strconv.FormatInt(sc.BundleSizeBytes, 10), // storage_overhead = persisted artifact size
		f.NamespaceDeleted,
		f.EvidenceSurvived,
		strings.Join(sc.Notes, "; "),
	}
}

// --- helpers ---------------------------------------------------------------

// evidenceIDSet returns the set of evidence item IDs in the report.
func evidenceIDSet(report *platformv1alpha1.FailureReport) map[string]bool {
	ids := map[string]bool{}
	if report == nil {
		return ids
	}
	for _, it := range report.Status.EvidenceItems {
		ids[it.ID] = true
	}
	return ids
}

// capturedEvidenceTypes returns the distinct, sorted evidence types in the report.
func capturedEvidenceTypes(report *platformv1alpha1.FailureReport) []string {
	if report == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, it := range report.Status.EvidenceItems {
		if it.Type != "" {
			seen[string(it.Type)] = true
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// bundleSize returns the recorded persisted-artifact size, falling back to the
// evidence-item count's absence (0) when the operator did not record it.
func bundleSize(report *platformv1alpha1.FailureReport) int64 {
	if report == nil {
		return 0
	}
	return report.Status.BundleSizeBytes
}

// componentMatch reports whether a diagnosed component matches the ground-truth
// component. Both are normalised to lowercase alphanumerics; a match is exact
// equality or one being a substring of the other, which tolerates wording like
// "migration job" vs "migration-job" or "deployment" vs "app-deployment".
func componentMatch(diag, truth string) bool {
	d, t := normToken(diag), normToken(truth)
	if d == "" || t == "" {
		return false
	}
	return d == t || strings.Contains(d, t) || strings.Contains(t, d)
}

// normToken keeps only lowercase alphanumeric characters of s.
func normToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// boolCell renders a boolean as the 1/0 the results CSV uses.
func boolCell(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// optFloat renders a float when present, or an empty cell when not measured.
func optFloat(v float64, present bool) string {
	if !present {
		return ""
	}
	return strconv.FormatFloat(v, 'f', 4, 64)
}
