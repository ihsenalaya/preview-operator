// Package diagnosis turns a captured failure evidence bundle into a probable
// root cause. It provides two interchangeable diagnostic engines — a
// deterministic rule-based engine and an LLM-backed engine — each runnable in a
// grounded mode (every claim must cite evidence item IDs that exist in the
// bundle) or a free-form mode (no grounding constraint).
//
// The grounded/free-form ablation is what RQ4 measures (does requiring evidence
// grounding reduce hallucinated explanations); the engine choice combined with
// the evidence level (C1..C5) feeds RQ2 (does structured evidence improve
// accuracy). See docs/research/failure-provenance/research-questions.md.
//
// The package performs no Kubernetes API calls: it consumes an evidence.Bundle,
// which the experiment harness reconstructs from a persisted FailureReport with
// evidence.BundleFromReport. This keeps diagnosis reproducible and offline for
// the rule engine, and dependent only on the LLM endpoint for the LLM engine.
package diagnosis

import (
	"context"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/evidence"
)

// Engine selects the diagnostic engine.
type Engine string

const (
	// EngineRule is the deterministic, offline rule-based engine.
	EngineRule Engine = "rule"
	// EngineLLM is the LLM-backed engine.
	EngineLLM Engine = "llm"
)

// Mode selects whether the diagnosis is grounded in evidence IDs.
type Mode string

const (
	// ModeGrounded requires every evidence reference to resolve to an evidence
	// item present in the bundle. References that do not resolve are stripped
	// from the diagnosis and reported as hallucinated.
	ModeGrounded Mode = "grounded"
	// ModeFreeform applies no grounding constraint: references are kept as the
	// engine produced them, and any that do not resolve are still reported.
	ModeFreeform Mode = "freeform"
)

// Diagnoser produces a diagnosis from a failure evidence bundle.
type Diagnoser interface {
	// Diagnose analyses the bundle and returns a Result. It must not mutate the
	// bundle.
	Diagnose(ctx context.Context, b *evidence.Bundle) (*Result, error)
}

// Result is the outcome of one diagnostic run. It is JSON-serialisable so the
// scoring step (experiments/failure-provenance) can consume it directly.
type Result struct {
	// Engine and Mode record how the diagnosis was produced.
	Engine Engine `json:"engine"`
	Mode   Mode   `json:"mode"`

	// Model is the LLM model identifier. It is empty for the rule engine and
	// recorded for the LLM engine so a result is reproducible.
	Model string `json:"model,omitempty"`

	// EvidenceLevel is the C1..C5 configuration of the bundle that was
	// diagnosed, copied through for traceability.
	EvidenceLevel string `json:"evidenceLevel,omitempty"`

	// Diagnosis is the produced diagnosis. In grounded mode every entry of
	// Diagnosis.EvidenceRefs resolves to an item in the bundle.
	Diagnosis *platformv1alpha1.FailureDiagnosis `json:"diagnosis"`

	// HallucinatedRefs lists evidence IDs the engine cited that do not exist in
	// the bundle. It is always empty for the rule engine. For the LLM engine in
	// grounded mode these references are also stripped from Diagnosis.EvidenceRefs;
	// in free-form mode they are reported but left in place. This is the raw
	// signal for the Hallucination Rate metric (RQ4).
	HallucinatedRefs []string `json:"hallucinatedRefs,omitempty"`

	// RawOutput is the unparsed engine output, kept for auditing. It is empty
	// for the rule engine.
	RawOutput string `json:"rawOutput,omitempty"`
}

// applyGrounding splits d.EvidenceRefs into references present in the bundle and
// references that are not. In grounded mode the unknown references are removed
// from d; in free-form mode d is left unchanged. The unknown references are
// returned in both cases so the caller can report them (RQ4).
func applyGrounding(d *platformv1alpha1.FailureDiagnosis, b *evidence.Bundle, mode Mode) []string {
	if d == nil || len(d.EvidenceRefs) == 0 {
		return nil
	}
	known := make([]string, 0, len(d.EvidenceRefs))
	var unknown []string
	for _, ref := range d.EvidenceRefs {
		if b.Has(ref) {
			known = append(known, ref)
		} else {
			unknown = append(unknown, ref)
		}
	}
	if mode == ModeGrounded {
		d.EvidenceRefs = known
	}
	return unknown
}
