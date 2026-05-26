package evidence

import (
	"fmt"
	"strings"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

// Level selects how much failure evidence the operator collects and structures.
// The five levels are the comparison configurations C1..C5 of the controlled
// evaluation — see docs/research/failure-provenance/methodology.md. Lower levels
// supply progressively less context to the diagnostic step; only C5 additionally
// materialises the failure provenance graph. The level is an operator-wide knob
// (the EVIDENCE_LEVEL environment variable) that the experiment harness flips
// between runs to answer RQ2.
type Level string

const (
	// LevelC1 — pod/job logs only.
	LevelC1 Level = "C1"
	// LevelC2 — logs + Kubernetes events.
	LevelC2 Level = "C2"
	// LevelC3 — logs + events + test results.
	LevelC3 Level = "C3"
	// LevelC4 — the full evidence bundle (all collectors).
	LevelC4 Level = "C4"
	// LevelC5 — the full evidence bundle plus the failure provenance graph.
	LevelC5 Level = "C5"
)

// DefaultLevel is the evidence level used when none is configured: the full
// bundle plus the provenance graph, i.e. the operator's complete capability.
const DefaultLevel = LevelC5

// ParseLevel converts a string (case-insensitive, "C1".."C5") to a Level. An
// empty string yields DefaultLevel; any other unrecognised value is an error.
func ParseLevel(s string) (Level, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "":
		return DefaultLevel, nil
	case "C1":
		return LevelC1, nil
	case "C2":
		return LevelC2, nil
	case "C3":
		return LevelC3, nil
	case "C4":
		return LevelC4, nil
	case "C5":
		return LevelC5, nil
	default:
		return "", fmt.Errorf("unknown evidence level %q (expected C1..C5)", s)
	}
}

// Description returns a one-line human-readable description of the level.
func (l Level) Description() string {
	switch l {
	case LevelC1:
		return "logs only"
	case LevelC2:
		return "logs + Kubernetes events"
	case LevelC3:
		return "logs + events + test results"
	case LevelC4:
		return "full evidence bundle"
	case LevelC5:
		return "provenance graph + full evidence bundle"
	default:
		return "unknown"
	}
}

// IncludesProvenanceGraph reports whether the level materialises the failure
// provenance graph. Only C5 does.
func (l Level) IncludesProvenanceGraph() bool { return l == LevelC5 }

// EvidenceTypesForLevel returns the set of evidence types a level admits, or nil
// when the level admits every type (C4, C5, and any unset value).
//
// It is the diagnosis-time counterpart of CollectorsForLevel. The operator
// captures the full bundle once at C5; the experiment harness then down-samples
// that single capture to a lower level by keeping only these types — see
// BundleFromReportAtLevel. This is what lets the C1..C5 comparison for RQ2 run
// from one operator capture instead of five operator restarts.
func EvidenceTypesForLevel(l Level) map[platformv1alpha1.EvidenceType]bool {
	logs := map[platformv1alpha1.EvidenceType]bool{
		platformv1alpha1.EvidenceTypePodLog: true,
		platformv1alpha1.EvidenceTypeJobLog: true,
	}
	switch l {
	case LevelC1:
		return logs
	case LevelC2:
		logs[platformv1alpha1.EvidenceTypeKubernetesEvent] = true
		return logs
	case LevelC3:
		logs[platformv1alpha1.EvidenceTypeKubernetesEvent] = true
		logs[platformv1alpha1.EvidenceTypeTestResult] = true
		return logs
	default: // C4, C5, and any unset value: every type.
		return nil
	}
}

// CollectorsForLevel returns the collector subset that a level enables. The
// subsets are nested: each level adds collectors to the one below it. An unset
// or unrecognised level falls back to the full bundle, so a misconfiguration
// never silently drops evidence.
func CollectorsForLevel(l Level) []Collector {
	logs := LogsCollector{}
	events := EventsCollector{}
	tests := TestResultCollector{}
	switch l {
	case LevelC1:
		return []Collector{logs}
	case LevelC2:
		return []Collector{logs, events}
	case LevelC3:
		return []Collector{logs, events, tests}
	default: // C4, C5, and any unset value: the full bundle.
		return DefaultCollectors()
	}
}
