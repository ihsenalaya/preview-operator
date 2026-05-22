# Failure Provenance — Research Package

Research material for the article:

> **Operator-Captured Failure Provenance for Ephemeral Kubernetes Preview
> Environments**

This directory is the working documentation package for the article. It is
self-contained: the state of the art, research questions, methodology, fault-injection
experiments, metrics, contributions, threats to validity, architecture diagrams, the
failure evidence model, and the bibliography.

## One-line thesis

A Kubernetes operator captures, structures, and preserves failure evidence for
ephemeral pull-request preview environments **before namespace teardown**, linking PR
diff, Kubernetes resources, test failures, logs, events, traces, and AI-assisted
diagnostic hypotheses into an **auditable provenance artifact**.

This article does **not** claim that Kubernetes RCA, LLM-based RCA, graph-based RCA,
preview environments, Kubernetes-native test orchestration, or observability are new.
The contribution is the operator-controlled *capture, structuring, linking, grounding,
and evaluation* of failure evidence for *ephemeral, change-scoped* environments.

## Files

| File | Purpose |
|------|---------|
| [`article-outline.md`](article-outline.md) | Full article structure + proposed abstract + figure/table plan |
| [`state-of-the-art.md`](state-of-the-art.md) | Survey (preview envs, operators, test orchestration, RCA, observability, provenance) + gap analysis |
| [`research-questions.md`](research-questions.md) | RQ1–RQ5 with motivation, hypothesis, measurement, expected result, risk |
| [`methodology.md`](methodology.md) | Environments, components, experimental flow, ground truth, configurations C1–C5, statistics |
| [`experiments.md`](experiments.md) | Fault-injection matrix F1–F10 (one root cause per scenario) |
| [`metrics.md`](metrics.md) | The 11 metrics, each with formula, unit, collection method, RQ mapping |
| [`contributions.md`](contributions.md) | The five contributions and what validates each |
| [`threats-to-validity.md`](threats-to-validity.md) | Internal / external / construct / conclusion validity + mitigations |
| [`architecture.md`](architecture.md) | Four Mermaid diagrams (end-to-end flow, bundle, provenance graph, persistence) |
| [`failure-evidence-model.md`](failure-evidence-model.md) | Entities / activities / agents / relations, mapped to W3C PROV |
| [`bibliography.bib`](bibliography.bib) | BibTeX bibliography (entries needing checks are marked `TODO_VERIFY`) |
| `README.md` | This index |

## Suggested reading order

1. `article-outline.md` — see the whole article shape and the abstract.
2. `contributions.md` — what is claimed (and what is explicitly not claimed).
3. `state-of-the-art.md` — prior work and the precise gap.
4. `failure-evidence-model.md` + `architecture.md` — the approach and its diagrams.
5. `research-questions.md` → `methodology.md` → `experiments.md` → `metrics.md` — the
   evaluation design.
6. `threats-to-validity.md` — honest limitations.

## Research questions (see `research-questions.md` for the full text)

| RQ | Question |
|----|----------|
| RQ1 | Evidence preservation before namespace teardown |
| RQ2 | Does structured evidence improve RCA accuracy vs logs-only? |
| RQ3 | Does provenance reduce time to diagnosis? |
| RQ4 | Does an evidence-grounding constraint reduce hallucination? |
| RQ5 | Runtime overhead of failure evidence collection |

Database inter-suite isolation overhead and AI seed-data quality are **out of scope**:
they belong to a separate, already-prepared **companion PostgreSQL article**
(`[postgres_isolation_companion]`). Source comments that mention "RQ3" / "RQ4" (e.g.
`preview_types.go:66`, `:226`, `checkpoint.go:519/564`) refer to that companion
article's numbering — see the Scope note at the top of `research-questions.md`.

## Relationship to the rest of the repository

This documentation package is **Phase 1**. The companion phases:

- **Phase 2 — implementation scaffolding — DONE.** `internal/evidence/` Go package:
  `Bundle`, `FailureEvidenceItem`, `FailureDiagnosis`, deterministic evidence IDs,
  the `Collector` interface and five collectors, the redaction utility, and
  idempotent report generation — with unit tests (≈89 % statement coverage).
- **Phase 3 — `FailureReport` artifact — DONE.** A new cluster-scoped CRD
  `platform.company.io/v1alpha1, kind: FailureReport`
  (`api/v1alpha1/failurereport_types.go`), with generated CRD YAML and operator
  RBAC. `EnsureFailureReport` is **wired into the operator failure paths**
  (`internal/controller/failurereport.go`, `setFailedStatus`, the test-suite
  failure path), so a `FailureReport` is produced for every failed preview.
- **Phase 4 — experiment harness — DONE.** `experiments/failure-provenance/`:
  `scenarios.yaml` (F1–F10), `run-kind-experiments.sh`, `collect-results.sh`,
  `scoring-rubric.md`, `results-template.csv`.
- **Phase 5 — evaluation — PENDING.** Run the experiments on a cluster and collect
  data. Requires real cluster time and an LLM endpoint; not yet done.
- **Phase 6 — writing — PENDING.** Fill `article-outline.md` with Phase 5 results.

## Status — Phases 1–4 complete

The documentation, the implementation, the `FailureReport` CRD, and the experiment
harness all exist and are validated (`go build`, `go vet`, `make test` all green).
Before the article can be submitted:

- [ ] Resolve every `TODO_VERIFY` (most are in `bibliography.bib` and
      `state-of-the-art.md`).
- [ ] Confirm with the companion PostgreSQL article that database-isolation and
      seed-quality results are not duplicated here (`research-questions.md`, Scope).
- [ ] Choose the target venue and trim the abstract accordingly.
- [ ] Run Phase 5 on a cluster and fill the evaluation sections with real results —
      the results MUST be real; never fabricate measurements.

## Honesty constraints (apply throughout the article)

- The `Preview` CRD is **cluster-scoped**, so `status.diagnostics` already survives
  namespace deletion. Do not claim evidence is otherwise entirely lost — the gap is
  that the surviving snapshot is unstructured, overwritten, and ungrounded.
- The operator wires OpenTelemetry **auto-instrumentation** but does **not** itself
  collect trace spans. Trace evidence is conditional on app-level instrumentation and
  an external backend.
- Database-isolation overhead (`restore` / `migration`) and AI seed-data quality are
  **out of scope** — covered by the companion PostgreSQL article, not this one.
- Any reference that cannot be verified from a primary source stays `TODO_VERIFY` —
  never fabricate metadata.
