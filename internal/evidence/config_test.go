package evidence

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    Level
		wantErr bool
	}{
		{"", DefaultLevel, false},
		{"C1", LevelC1, false},
		{"c2", LevelC2, false},
		{" C3 ", LevelC3, false},
		{"C4", LevelC4, false},
		{"C5", LevelC5, false},
		{"C6", "", true},
		{"full", "", true},
	}
	for _, tc := range cases {
		got, err := ParseLevel(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q): expected an error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q): unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIncludesProvenanceGraph(t *testing.T) {
	for _, l := range []Level{LevelC1, LevelC2, LevelC3, LevelC4} {
		if l.IncludesProvenanceGraph() {
			t.Errorf("level %s must not materialise the provenance graph", l)
		}
	}
	if !LevelC5.IncludesProvenanceGraph() {
		t.Error("level C5 must materialise the provenance graph")
	}
}

func TestCollectorsForLevelAreNested(t *testing.T) {
	// Each level must enable at least as many collectors as the one below it.
	prev := -1
	for _, l := range []Level{LevelC1, LevelC2, LevelC3, LevelC4, LevelC5} {
		n := len(CollectorsForLevel(l))
		if n < prev {
			t.Errorf("level %s enables %d collectors, fewer than the level below (%d)", l, n, prev)
		}
		prev = n
	}
	if got := len(CollectorsForLevel(LevelC1)); got != 1 {
		t.Errorf("C1 must enable exactly the logs collector, got %d collectors", got)
	}
	if got := len(CollectorsForLevel(LevelC5)); got != len(DefaultCollectors()) {
		t.Errorf("C5 must enable the full collector set (%d), got %d", len(DefaultCollectors()), got)
	}
}

func TestBundleFromReportAtLevelDownsamples(t *testing.T) {
	// A full C5-style capture with one item of each level-relevant type.
	report := &platformv1alpha1.FailureReport{
		Status: platformv1alpha1.FailureReportStatus{
			EvidenceLevel: "C5",
			EvidenceItems: []platformv1alpha1.FailureEvidenceItem{
				{ID: "podlog-1", Type: platformv1alpha1.EvidenceTypePodLog},
				{ID: "joblog-1", Type: platformv1alpha1.EvidenceTypeJobLog},
				{ID: "event-1", Type: platformv1alpha1.EvidenceTypeKubernetesEvent},
				{ID: "test-1", Type: platformv1alpha1.EvidenceTypeTestResult},
				{ID: "file-1", Type: platformv1alpha1.EvidenceTypeChangedFile},
			},
		},
	}
	cases := []struct {
		level Level
		want  int
	}{
		{LevelC1, 2}, // logs only
		{LevelC2, 3}, // + events
		{LevelC3, 4}, // + test results
		{LevelC4, 5}, // everything
		{LevelC5, 5}, // everything
	}
	for _, tc := range cases {
		b := BundleFromReportAtLevel(report, tc.level)
		if b.Len() != tc.want {
			t.Errorf("level %s: kept %d items, want %d", tc.level, b.Len(), tc.want)
		}
		if b.Level != tc.level {
			t.Errorf("level %s: bundle.Level = %q", tc.level, b.Level)
		}
	}

	// C1 must never carry an event or a changed file.
	c1 := BundleFromReportAtLevel(report, LevelC1)
	if c1.Has("event-1") || c1.Has("file-1") {
		t.Error("C1 down-sample leaked a non-log evidence item")
	}
}

func TestAssembleBundleForLevelRecordsLevel(t *testing.T) {
	for _, l := range []Level{LevelC1, LevelC2, LevelC3, LevelC4, LevelC5} {
		b := AssembleBundleForLevel(failedPreview(), l)
		if b.Level != l {
			t.Errorf("AssembleBundleForLevel(%s): bundle.Level = %q", l, b.Level)
		}
	}
}

func TestLevelControlsEvidenceBreadth(t *testing.T) {
	// C1 (logs only) must capture strictly fewer evidence types than C4 (full).
	c1 := AssembleBundleForLevel(failedPreview(), LevelC1)
	c4 := AssembleBundleForLevel(failedPreview(), LevelC4)

	hasType := func(b *Bundle, want platformv1alpha1.EvidenceType) bool {
		for _, it := range b.Items() {
			if it.Type == want {
				return true
			}
		}
		return false
	}
	if hasType(c1, platformv1alpha1.EvidenceTypeKubernetesEvent) {
		t.Error("C1 must not capture Kubernetes events")
	}
	if hasType(c1, platformv1alpha1.EvidenceTypeGitDiff) {
		t.Error("C1 must not capture the git diff")
	}
	if !hasType(c4, platformv1alpha1.EvidenceTypeKubernetesEvent) {
		t.Error("C4 must capture Kubernetes events")
	}
	if c1.Len() >= c4.Len() {
		t.Errorf("C1 captured %d items, expected fewer than C4's %d", c1.Len(), c4.Len())
	}
}

func TestReportProvenanceGraphOnlyAtC5(t *testing.T) {
	for _, l := range []Level{LevelC1, LevelC2, LevelC3, LevelC4} {
		r := BuildFailureReport(AssembleBundleForLevel(failedPreview(), l))
		if r.Status.ProvenanceGraph != nil {
			t.Errorf("level %s: report must not carry a provenance graph", l)
		}
		if r.Status.EvidenceLevel != string(l) {
			t.Errorf("level %s: report EvidenceLevel = %q", l, r.Status.EvidenceLevel)
		}
	}
	r5 := BuildFailureReport(AssembleBundleForLevel(failedPreview(), LevelC5))
	if r5.Status.ProvenanceGraph == nil {
		t.Error("level C5: report must carry a provenance graph")
	}
	if r5.Status.BundleSizeBytes <= 0 {
		t.Error("level C5: report must record a non-zero bundle size")
	}
}

func TestReportRecordsDiagnosisTiming(t *testing.T) {
	b := AssembleBundleForLevel(failedPreview(), LevelC5)
	// Pin the failure-detection time 90s in the past so the time-to-diagnosis is
	// a stable, observable interval (RQ3).
	b.FailureDetectedAt = metav1.NewTime(time.Now().Add(-90 * time.Second))

	known := b.Items()[0].ID
	if err := b.SetDiagnosis(&platformv1alpha1.FailureDiagnosis{
		ProbableCause: "invalid SQL in migration",
		EvidenceRefs:  []string{known},
	}); err != nil {
		t.Fatalf("SetDiagnosis: %v", err)
	}

	r := BuildFailureReport(b)
	if r.Status.DiagnosisAvailableAt == nil {
		t.Fatal("report must record DiagnosisAvailableAt once a diagnosis is set")
	}
	if r.Status.TimeToDiagnosisMillis < 80_000 || r.Status.TimeToDiagnosisMillis > 110_000 {
		t.Errorf("TimeToDiagnosisMillis = %d, want ~90000", r.Status.TimeToDiagnosisMillis)
	}

	// Without a diagnosis the timing fields stay empty.
	noDiag := BuildFailureReport(AssembleBundleForLevel(failedPreview(), LevelC5))
	if noDiag.Status.DiagnosisAvailableAt != nil || noDiag.Status.TimeToDiagnosisMillis != 0 {
		t.Error("report without a diagnosis must not carry diagnosis-timing fields")
	}
}
