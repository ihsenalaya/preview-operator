// fp-score scores one failure-provenance experiment run. It reads the captured
// FailureReport and the diagnosis Result (produced by fp-diagnose), looks up the
// scenario ground truth in scenarios.yaml, and emits one results-CSV row — the
// executable side of the scoring rubric for RQ1-RQ5.
//
// It computes only what is mechanically derivable: Top-1/3 RCA correctness,
// structural hallucination rate, type-level evidence recall, time to diagnosis
// and persisted-artifact size. Columns that need human annotation (evidence
// precision, recommendation score) or cluster measurement (CPU/memory overhead,
// teardown) are left empty — never fabricated.
//
// Usage:
//
//	fp-score --report report.json --result diag.json --scenario F1 \
//	         --run-id F1-C4-003 --cluster-type kind
//	fp-score --csv-header                 # print the CSV header and exit
//
// Default output is one CSV row; --format json prints the full Scorecard.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/diagnosis"
	"github.com/ihsenalaya/preview-operator/internal/scoring"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fp-score: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	reportPath := flag.String("report", "", "path to the FailureReport JSON")
	resultPath := flag.String("result", "", "path to the diagnosis Result JSON (from fp-diagnose)")
	scenariosPath := flag.String("scenarios", "experiments/failure-provenance/scenarios.yaml",
		"path to scenarios.yaml")
	scenarioID := flag.String("scenario", "", "scenario id (F1..F10)")
	runID := flag.String("run-id", "", "run identifier for the CSV row")
	clusterType := flag.String("cluster-type", "", "cluster type for the CSV row (e.g. kind, aks)")
	format := flag.String("format", "csv", "output format: csv | json")
	outPath := flag.String("out", "-", "output path, or - for stdout")
	csvHeader := flag.Bool("csv-header", false, "print the results CSV header and exit")
	flag.Parse()

	if *csvHeader {
		return writeCSVRow(*outPath, scoring.CSVHeader)
	}

	if *reportPath == "" || *resultPath == "" || *scenarioID == "" {
		return fmt.Errorf("--report, --result and --scenario are required")
	}

	report, err := readReport(*reportPath)
	if err != nil {
		return err
	}
	result, err := readResult(*resultPath)
	if err != nil {
		return err
	}

	scenarios, err := scoring.LoadScenarios(*scenariosPath)
	if err != nil {
		return err
	}
	scenario, ok := scenarios[strings.ToUpper(*scenarioID)]
	if !ok {
		return fmt.Errorf("scenario %q not found in %s", *scenarioID, *scenariosPath)
	}

	card := scoring.Score(result, report, scenario)

	if *format == "json" {
		data, err := json.MarshalIndent(card, "", "  ")
		if err != nil {
			return err
		}
		return writeRaw(*outPath, append(data, '\n'))
	}
	return writeCSVRow(*outPath, card.CSVRecord(*runID, *clusterType))
}

// readReport decodes a FailureReport, accepting a single object or a List (first
// item), so `kubectl get failurereport[s] -o json` works directly.
func readReport(path string) (*platformv1alpha1.FailureReport, error) {
	data, err := os.ReadFile(path)
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

// readResult decodes a diagnosis Result.
func readResult(path string) (*diagnosis.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading result: %w", err)
	}
	var result diagnosis.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing diagnosis Result: %w", err)
	}
	return &result, nil
}

// writeCSVRow writes one properly quoted CSV record.
func writeCSVRow(path string, record []string) error {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	if err := w.Write(record); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return writeRaw(path, []byte(sb.String()))
}

// writeRaw writes bytes to a file or stdout.
func writeRaw(path string, data []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
