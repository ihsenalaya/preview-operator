# Failure-Provenance Experiment Harness

Fault-injection harness for the article *Operator-Captured Failure Provenance for
Ephemeral Kubernetes Preview Environments*. It drives the ten scenarios F1–F10
across the five evidence configurations C1–C5 and collects the data needed to
answer RQ1–RQ5.

The research design is in [`docs/research/failure-provenance/`](../../docs/research/failure-provenance/).
This directory is the *executable* side of it.

## Files

| File | Purpose |
|------|---------|
| `scenarios.yaml` | Machine-readable F1–F10 matrix (authoritative for the harness) |
| `run-kind-experiments.sh` | Orchestrates the experiment runs (dry-run by default) |
| `collect-results.sh` | Read-only collection of evidence artifacts for one run |
| `scoring-rubric.md` | How to score RCA correctness, evidence, hallucination, recommendations |
| `results-template.csv` | Empty results CSV — one row per run |
| `README.md` | This file |

## Prerequisites

- `kubectl`, `kind`, `docker` (required); `jq`, `yq` (recommended).
- A Kubernetes cluster (Kind for the primary results; AKS optional).
- The `preview-operator` installed on the cluster, including the `FailureReport`
  CRD (`config/crd/bases/platform.company.io_failurereports.yaml`).
- The `idp-preview` demo application reachable for synthetic PRs.

## Quick start

```bash
# 1. See the full plan — contacts nothing, changes nothing:
./run-kind-experiments.sh --dry-run

# 2. Plan a single cell:
./run-kind-experiments.sh --dry-run --scenario F1 --config C4 --repetitions 10

# 3. Run the safe steps against a cluster:
./run-kind-experiments.sh --execute --scenario F1 --config C4

# 4. Collect artifacts for one preview (read-only):
./collect-results.sh --preview pr-42
```

`run-kind-experiments.sh` **defaults to `--dry-run`**: it prints the plan and
touches nothing. `collect-results.sh` is **read-only**: it only ever runs
`kubectl get` / `kubectl logs`.

## Comparison configurations

| Config | Evidence supplied to the diagnostic step |
|--------|-------------------------------------------|
| C1 | Logs only |
| C2 | Logs + Kubernetes events |
| C3 | Logs + events + test results |
| C4 | Full evidence bundle |
| C5 | Provenance graph + full evidence bundle |

The full run is 10 scenarios × 5 configurations × ≥10 repetitions = **≥500 runs**
on Kind. Budget cluster time accordingly; run the cells in batches.

## Status — what works now, what needs Phase 5

Working today:

- `--dry-run` prints the complete run plan.
- `collect-results.sh` collects real artifacts from a cluster.
- `scenarios.yaml`, `scoring-rubric.md`, `results-template.csv` are complete.

Needs the **Phase-5 integration** (marked `TODO_PHASE5` in `run-kind-experiments.sh`):

1. Fault injection into synthetic `idp-preview` pull requests (one injector per
   scenario — see the `injection` block of each scenario in `scenarios.yaml`).
2. Wiring `EnsureFailureReport` (`internal/controller/failurereport.go`) onto the
   operator's Preview failure path, before namespace teardown, so a `FailureReport`
   is actually produced for each failed preview.

Until those are wired, `--execute` runs the safe steps (dependency and cluster
checks, results-CSV setup) and stops cleanly at the unwired step.

## Reproducibility

- Pin and record the Kubernetes version, the operator image, and — for
  LLM-in-the-loop runs — the model and model version (temperature 0).
- Keep Kind and AKS results in separate CSVs; never pool them.
- Every CSV row is keyed by `(scenario_id, run_id, cluster_type, configuration)`
  so any number can be traced back to its raw artifacts.
