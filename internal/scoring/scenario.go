// Package scoring scores one failure-provenance experiment run against its
// scenario ground truth. It computes the metrics that can be derived
// mechanically from a captured FailureReport and a diagnosis Result — Top-1/3
// RCA accuracy (RQ2), structural hallucination rate (RQ4), type-level evidence
// recall (RQ1/RQ2), time to diagnosis (RQ3) and the persisted-artifact size
// (RQ5).
//
// Metrics that require human annotation (evidence precision, recommendation
// usefulness) or cluster measurement (CPU/memory overhead, namespace teardown)
// are deliberately NOT invented here — they are left empty in the CSV row for a
// human or the orchestration layer to fill. See
// docs/research/failure-provenance/metrics.md and the scoring-rubric.md.
package scoring

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// GroundTruth is the fixed, known root cause of a scenario.
type GroundTruth struct {
	Component string `json:"component"`
	Category  string `json:"category"`
}

// Scenario is one fault-injection scenario, parsed from scenarios.yaml. Only the
// fields the scorer needs are modelled.
type Scenario struct {
	ID                  string      `json:"id"`
	Family              string      `json:"family"`
	Description         string      `json:"description"`
	ExpectedFailedSuite string      `json:"expected_failed_suite"`
	ExpectedEvidence    []string    `json:"expected_evidence"`
	GroundTruth         GroundTruth `json:"ground_truth"`
}

// scenarioMatrix mirrors the top level of scenarios.yaml.
type scenarioMatrix struct {
	Scenarios []Scenario `json:"scenarios"`
}

// LoadScenarios parses a scenarios.yaml file and returns the scenarios keyed by
// ID (F1..F10).
func LoadScenarios(path string) (map[string]Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading scenarios file: %w", err)
	}
	var matrix scenarioMatrix
	if err := yaml.Unmarshal(data, &matrix); err != nil {
		return nil, fmt.Errorf("parsing scenarios YAML: %w", err)
	}
	if len(matrix.Scenarios) == 0 {
		return nil, fmt.Errorf("scenarios file %s defines no scenarios", path)
	}
	out := make(map[string]Scenario, len(matrix.Scenarios))
	for _, s := range matrix.Scenarios {
		if s.ID == "" {
			return nil, fmt.Errorf("scenarios file %s has a scenario with no id", path)
		}
		out[s.ID] = s
	}
	return out, nil
}
