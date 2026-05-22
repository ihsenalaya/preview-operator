# Failure-Provenance Experiment Harness

Fault-injection harness for the article *Operator-Captured Failure Provenance for
Ephemeral Kubernetes Preview Environments*. It drives the ten scenarios F1–F10
across the five evidence configurations C1–C5 and collects the data needed to
answer RQ1–RQ5.

The research design is in [`docs/research/failure-provenance/`](../../docs/research/failure-provenance/).
This directory is the *executable* side of it; live progress is tracked in
[`docs/research/failure-provenance/PROGRESS.md`](../../docs/research/failure-provenance/PROGRESS.md).

## Files

| File | Purpose |
|------|---------|
| `scenarios.yaml` | Machine-readable F1–F10 matrix (authoritative for the harness) |
| `injectors/inject-fault.sh` | Applies one fault per scenario (see `injectors/README.md`) |
| `run-kind-experiments.sh` | Orchestrates the whole matrix (dry-run by default) |
| `collect-results.sh` | Read-only collection of evidence artifacts for one run |
| `scoring-rubric.md` | How to score RCA correctness, evidence, hallucination, recommendations |
| `results-template.csv` | Empty results CSV — one row per scored run |
| `README.md` | This file |

The diagnostic harness and scorer the orchestration calls are Go commands in the
operator repository:

| Command | Purpose |
|---------|---------|
| `cmd/fp-diagnose` | Diagnose a captured `FailureReport` — rule or LLM engine, grounded/free-form, at level C1–C5 |
| `cmd/fp-score` | Score one run against its scenario ground truth → one results-CSV row |

## Prerequisites

- `kubectl`, `git`, `go`, `python3` (always); plus `docker` + `kind` (Kind runs)
  or `az` (AKS runs).
- A Kubernetes cluster running **this fork's operator** with the `FailureReport`
  CRD installed, `EVIDENCE_COLLECTION` enabled and `EVIDENCE_LEVEL=C5`.
- The `idp-preview` demo application repository (cloned fresh per run).
- For the LLM engine: `AI_API_KEY` (or `OPENAI_API_KEY`) in the environment.

## Quick start

```bash
# 1. See the full plan — contacts nothing, changes nothing:
./run-kind-experiments.sh --dry-run

# 2. Plan a single scenario, all configs, one repetition:
./run-kind-experiments.sh --dry-run --scenario F1 --repetitions 1

# 3. Run for real against an AKS cluster (rule engine):
./run-kind-experiments.sh --execute --cluster-type aks --scenario F1 --repetitions 10

# 4. Run with the LLM engine as well (needs AI_API_KEY):
./run-kind-experiments.sh --execute --cluster-type aks --engines rule,llm

# 5. Collect artifacts for one preview (read-only):
./collect-results.sh --preview pr-9101
```

`run-kind-experiments.sh` **defaults to `--dry-run`**: it prints the complete
plan and touches nothing. `collect-results.sh` is **read-only**.

## How the matrix runs

A **cluster run** is one `(scenario, repetition)`: the harness clones
`idp-preview`, injects the fault (`injectors/inject-fault.sh`), builds an image
for code faults, applies a synthetic `Preview`, waits for the operator to capture
one full **C5** `FailureReport`, exports it, and tears the preview down.

**Scoring is offline.** For each configuration C1–C5 and each engine/mode,
`fp-diagnose` down-samples that single capture to the level and `fp-score`
appends one results-CSV row. The C1–C5 comparison therefore costs **one** cluster
run, not five:

```
cluster runs = scenarios × repetitions          = 10 × 10  = 100
CSV rows     = cluster runs × configs × engine-modes        = 100 × 5 × (1..3)
```

## Comparison configurations

| Config | Evidence supplied to the diagnostic step |
|--------|-------------------------------------------|
| C1 | Logs only |
| C2 | Logs + Kubernetes events |
| C3 | Logs + events + test results |
| C4 | Full evidence bundle |
| C5 | Provenance graph + full evidence bundle |

## Diagnostic engines and modes

| Engine | Modes | Purpose |
|--------|-------|---------|
| `rule` | grounded | Deterministic, offline baseline — grounded by construction |
| `llm`  | grounded, free-form | RQ4 hallucination ablation (grounded vs free-form) |

## Reproducibility

- Pin and record the Kubernetes version, the operator image, and — for the LLM
  engine — the model and version (temperature 0; `fp-score` records the model in
  the `notes` column).
- Keep Kind and AKS results in separate CSVs; never pool them.
- Every CSV row is keyed by `(scenario_id, run_id, cluster_type, configuration)`
  so any number can be traced back to its raw artifacts under `results/<scenario>/r<n>/`.
- **Research integrity:** `fp-score` fills only the columns it can measure.
  Evidence precision, recommendation score and CPU/memory overhead need human
  annotation or cluster measurement and are left **empty** — never fabricated.
