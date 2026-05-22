// fp-diagnose is the diagnostic harness of the failure-provenance evaluation.
// It reads a persisted FailureReport, runs one diagnostic engine in one mode,
// and prints a Result as JSON. It is the executable side of RQ2 (does structured
// evidence improve accuracy) and RQ4 (does grounding reduce hallucination).
//
// Usage:
//
//	fp-diagnose --report report.json --engine rule  --mode grounded
//	fp-diagnose --report report.json --engine llm   --mode freeform --model gpt-4o-mini
//	kubectl get failurereport pr-42-failure -o json | fp-diagnose --engine rule
//
// Engines:
//
//	rule  deterministic, offline rule-based diagnosis (default)
//	llm   LLM-backed diagnosis via an OpenAI-compatible endpoint
//
// Modes:
//
//	grounded  every cited evidence ID must exist in the bundle (default)
//	freeform  no grounding constraint — the RQ4 ablation baseline
//
// The LLM engine reads the endpoint from --ai-base-url / $AI_API_URL and the key
// from --ai-api-key / $AI_API_KEY / $OPENAI_API_KEY. The rule engine needs no
// network access and is fully reproducible.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/diagnosis"
	"github.com/ihsenalaya/preview-operator/internal/evidence"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fp-diagnose: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	reportPath := flag.String("report", "-", "path to a FailureReport JSON file, or - for stdin")
	engineFlag := flag.String("engine", "rule", "diagnostic engine: rule | llm")
	modeFlag := flag.String("mode", "grounded", "diagnosis mode: grounded | freeform")
	model := flag.String("model", envOr("FP_DIAGNOSE_MODEL", "gpt-4o-mini"), "LLM model id (llm engine)")
	aiBaseURL := flag.String("ai-base-url", os.Getenv("AI_API_URL"), "OpenAI-compatible API base URL (llm engine)")
	aiAPIKey := flag.String("ai-api-key", firstEnv("AI_API_KEY", "OPENAI_API_KEY"), "API key (llm engine)")
	levelFlag := flag.String("level", "", "evidence level to diagnose at: C1..C5 (default: the report's own level)")
	outPath := flag.String("out", "-", "path to write the Result JSON, or - for stdout")
	flag.Parse()

	engine := diagnosis.Engine(strings.ToLower(*engineFlag))
	if engine != diagnosis.EngineRule && engine != diagnosis.EngineLLM {
		return fmt.Errorf("unknown engine %q (expected rule | llm)", *engineFlag)
	}
	mode := diagnosis.Mode(strings.ToLower(*modeFlag))
	if mode != diagnosis.ModeGrounded && mode != diagnosis.ModeFreeform {
		return fmt.Errorf("unknown mode %q (expected grounded | freeform)", *modeFlag)
	}

	report, err := readReport(*reportPath)
	if err != nil {
		return err
	}

	// Resolve the evidence level. An explicit --level down-samples the captured
	// bundle (the C1..C5 comparison runs from one operator capture); an empty
	// flag keeps the report's own level.
	level, err := evidence.ParseLevel(*levelFlag)
	if err != nil {
		return err
	}
	var bundle *evidence.Bundle
	if *levelFlag == "" {
		bundle = evidence.BundleFromReport(report)
		level, _ = evidence.ParseLevel(report.Status.EvidenceLevel)
	} else {
		bundle = evidence.BundleFromReportAtLevel(report, level)
	}

	diagnoser, err := buildDiagnoser(engine, mode, *model, *aiBaseURL, *aiAPIKey, report, level)
	if err != nil {
		return err
	}

	result, err := diagnoser.Diagnose(context.Background(), bundle)
	if err != nil {
		return err
	}

	return writeJSON(*outPath, result)
}

// buildDiagnoser constructs the requested engine. The report's provenance graph
// is passed to the LLM engine only at level C5, so the prompt distinguishes C5
// (provenance graph + full bundle) from C4 (full bundle only).
func buildDiagnoser(engine diagnosis.Engine, mode diagnosis.Mode, model, baseURL, apiKey string,
	report *platformv1alpha1.FailureReport, level evidence.Level) (diagnosis.Diagnoser, error) {

	if engine == diagnosis.EngineRule {
		// The rule engine is deterministic and grounded by construction; mode
		// does not change its output, so a freeform request is simply honoured
		// as the same diagnosis.
		return diagnosis.NewRuleDiagnoser(), nil
	}

	if baseURL == "" {
		return nil, fmt.Errorf("llm engine requires --ai-base-url or $AI_API_URL")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("llm engine requires --ai-api-key, $AI_API_KEY or $OPENAI_API_KEY")
	}
	d := diagnosis.NewLLMDiagnoser(diagnosis.NewOpenAIClient(baseURL, apiKey, model), mode)
	if level.IncludesProvenanceGraph() {
		d.Provenance = report.Status.ProvenanceGraph
	}
	return d, nil
}

// readReport decodes a FailureReport from a file or stdin. It accepts either a
// single FailureReport or a FailureReportList (in which case the first item is
// used), so the output of `kubectl get failurereport[s] -o json` works directly.
func readReport(path string) (*platformv1alpha1.FailureReport, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading report: %w", err)
	}

	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parsing report JSON: %w", err)
	}

	if probe.Kind == "FailureReportList" {
		var list platformv1alpha1.FailureReportList
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("parsing FailureReportList: %w", err)
		}
		if len(list.Items) == 0 {
			return nil, fmt.Errorf("FailureReportList is empty")
		}
		return &list.Items[0], nil
	}

	var report platformv1alpha1.FailureReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing FailureReport: %w", err)
	}
	return &report, nil
}

// writeJSON writes v as indented JSON to a file or stdout.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// envOr returns the value of the named environment variable, or def when unset.
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// firstEnv returns the value of the first set environment variable.
func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}
