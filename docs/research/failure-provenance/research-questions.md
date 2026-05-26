# Research Questions

This article defines **five research questions, RQ1–RQ5**, following `prompt.txt`. They
form a single focused thread: operator-captured failure provenance for ephemeral
preview environments.

## Scope — what is NOT in this article

The `preview-operator` also supports a separate study on **PostgreSQL inter-suite
isolation overhead** and **AI seed-data quality** (mutation detection). That study is
the subject of a **companion article** `[postgres_isolation_companion]` and is
**explicitly out of scope here**, to avoid any overlap between the two papers.

Consequence for readers of the source code:

> Source-code comments that mention "RQ3" (database isolation) and "RQ4" (AI seed
> quality) — `api/v1alpha1/preview_types.go:66`, `api/v1alpha1/preview_types.go:226`,
> `internal/controller/checkpoint.go:519` and `:564` — use the **companion PostgreSQL
> article's** numbering. They are **not** the RQ3/RQ4 of this article. Do not renumber
> them; they belong to the other paper.

RQ5 below covers the runtime overhead **of failure evidence collection only**. The
overhead of database isolation (`spec.database.isolationMode`: `restore` vs
`migration`) is measured in the companion article, not here.

---

## RQ1 — Evidence Preservation

> **To what extent can a Kubernetes operator preserve failure evidence from an
> ephemeral preview environment before namespace teardown?**

- **Motivation.** When a preview namespace is deleted, namespace-scoped evidence (pod
  logs, Kubernetes events, transient resource state) is destroyed. The operator runs
  in the reconcile path and can capture this evidence while it still exists.
- **Hypothesis (H1).** An operator that collects evidence on failure detection, before
  the teardown path runs, preserves a high fraction of the expected evidence items for
  each fault class, and the persisted artifact survives namespace deletion.
- **Measurement.** *Evidence Preservation Rate* and *Snapshot Survival Rate* (see
  `metrics.md`). For each scenario, compare the set of evidence items captured in the
  `FailureReport` / bundle against the per-scenario *expected evidence* list in
  `experiments.md`. Then delete the namespace and re-measure what is still readable.
- **Expected result.** Preservation Rate ≥ 0.9 for fault classes whose evidence is
  pod/job/event based; Survival Rate = 1.0 for everything written to the persisted
  artifact (the artifact is cluster-scoped or externally stored).
- **Risk / limitation.** Some evidence is inherently racy (a pod that restarts before
  logs are read). Trace evidence depends on app-level OpenTelemetry instrumentation
  and an external trace backend, which the operator does not itself provide — report
  trace preservation separately and do not over-claim.

---

## RQ2 — RCA Accuracy

> **Does structured failure evidence improve root cause diagnosis accuracy compared
> with logs-only analysis?**

- **Motivation.** Diagnosis quality is expected to depend on the richness and structure
  of the evidence supplied to the diagnostic step (rule-based or LLM-based).
- **Hypothesis (H2).** Diagnostic accuracy increases monotonically (non-strictly) from
  configuration C1 (logs only) to C5 (provenance graph + full bundle).
- **Measurement.** *Top-1* and *Top-3 RCA Accuracy* (see `metrics.md`) across the five
  comparison configurations C1–C5 (`methodology.md`), over all scenarios F1–F10, with
  ≥10 repetitions each. A diagnosis is correct only if it identifies the right
  *component*, the right *failure category*, and at least one valid *evidence item*.
- **Expected result.** C4/C5 materially outperform C1; the largest gains appear on
  multi-signal failures (F4, F6, F7) where logs alone are ambiguous.
- **Risk / limitation.** LLM non-determinism inflates variance — fix the model and
  version, set temperature 0 for the diagnostic step, repeat, and report confidence
  intervals. Accuracy is partly a construct-validity question (see
  `threats-to-validity.md`).

---

## RQ3 — Time to Diagnosis

> **Does failure provenance reduce the time required to identify the probable root
> cause?**

- **Motivation.** In an ephemeral environment, diagnosis competes with the TTL clock.
  Faster, structured evidence delivery should shorten time to a probable cause.
- **Hypothesis (H3).** Mean Time To Diagnosis (MTTD) is lower when the diagnostic step
  consumes a pre-assembled evidence bundle than when it must gather evidence ad hoc.
- **Measurement.** *Mean Time To Diagnosis* = `diagnosis_available_timestamp −
  failure_detected_timestamp` (see `metrics.md`), per configuration, with CIs.
- **Expected result.** MTTD for C4/C5 is lower and more *predictable* (smaller
  variance) than for C1–C3, because evidence collection is amortised into the
  reconcile loop instead of performed on demand.
- **Risk / limitation.** MTTD includes evidence-collection time, which is itself part
  of RQ5 overhead — report the two consistently and avoid double-counting. Wall-clock
  time depends on cluster load; isolate runs.

---

## RQ4 — Hallucination Control

> **Does requiring each AI-generated diagnosis to reference observable evidence reduce
> unsupported or hallucinated explanations?**

- **Motivation.** LLM-based RCA can produce fluent but unsupported explanations. The
  operator can enforce a *grounding constraint*: every hypothesis must cite evidence
  item IDs that exist in the bundle.
- **Hypothesis (H4).** The grounding constraint lowers the Hallucination Rate without
  a large drop in Top-1 accuracy.
- **Measurement.** *Hallucination Rate* = `unsupported_diagnostic_claims /
  total_diagnostic_claims` (see `metrics.md`). Compare two ablations: (a) free-form
  LLM diagnosis, (b) grounded diagnosis where each claim must reference an evidence ID
  present in the bundle; claims referencing non-existent IDs are rejected/flagged.
  Annotate claims with the scoring rubric (`experiments/.../scoring-rubric.md`),
  ideally two annotators with an inter-rater agreement statistic.
- **Expected result.** Grounded diagnosis has a substantially lower Hallucination Rate;
  Top-1 accuracy is stable or slightly improved.
- **Risk / limitation.** "Unsupported" is an annotation judgement — construct validity.
  Use a fixed rubric and report inter-rater agreement (e.g. Cohen's κ).

---

## RQ5 — Runtime Overhead

> **What is the additional runtime, CPU, memory and storage overhead introduced by
> failure evidence collection?**

- **Motivation.** Evidence collection adds cost to the preview lifecycle; the article
  must quantify it honestly so the approach can be judged practical.
- **Hypothesis (H5).** Failure evidence collection adds a small, bounded overhead
  relative to total preview lifetime.
- **Measurement.** *Collection Overhead* (see `metrics.md`): additional seconds, CPU,
  memory, storage size of the persisted artifact, and number of Kubernetes API calls —
  measured as the difference between evidence collection *enabled* and *disabled*, on
  an otherwise idle host. Where feasible, also report behaviour under 1, 5, and 10
  concurrent previews.
- **Expected result.** Collection overhead is a single-digit-percentage of total
  preview lifetime and scales gracefully with concurrency.
- **Risk / limitation.** Overhead is cluster- and image-dependent; Kind and AKS will
  differ — report them separately. **Out of scope:** the overhead of database
  inter-suite isolation (`restore` vs `migration`) is *not* an RQ of this article; it
  is measured in the companion PostgreSQL article `[postgres_isolation_companion]`.
  Avoid double-counting: if a run uses DB isolation, that cost is attributed to the
  companion study, not to RQ5.

---

## Summary

| RQ | Question | Primary metric | Configurations |
|----|----------|----------------|----------------|
| RQ1 | Evidence preservation before teardown | Preservation Rate, Survival Rate | F1–F10 |
| RQ2 | Structured evidence vs logs-only accuracy | Top-1 / Top-3 Accuracy | C1–C5 |
| RQ3 | Provenance and time to diagnosis | Mean Time To Diagnosis | C1–C5 |
| RQ4 | Grounding constraint vs hallucination | Hallucination Rate | grounded vs free-form |
| RQ5 | Evidence-collection overhead | Collection Overhead | collection on / off |

RQ1–RQ4 are the **core** failure-provenance questions and anchor the article. RQ5 is
the **overhead** question that establishes practicality. Database-isolation overhead
and AI seed-data quality are deliberately excluded — they belong to the companion
PostgreSQL article.

---

## Honest limitations — what we did and did not measure for each RQ

For full Q1 transparency, the following is the actual measurement
coverage relative to the pre-registration. The corresponding sensitivity
analyses are in `analysis-output/` and the closure plan for each gap is
in `Q1-COMPLIANCE.md`.

| RQ | Pre-registered measurement | What landed | Closure plan |
|----|----------------------------|-------------|--------------|
| RQ1 | Evidence Preservation Rate, Snapshot Survival Rate, Recall | Fully measured (1500/1500 S1 + 200/200 multi-app) | — |
| RQ2 | Top-1, Top-3, Precision, Recall, Recommendation Usefulness | Top-1, Top-3, Recall fully; Precision via external compute (`analysis-output/19`); Recommendation Usefulness pending (W6) | W6 LLM-as-judge with κ floor |
| RQ3 | MTTD median + IQR + bootstrap 95% CI | Operator-side latency: sub-second on all 1500+200 cells. Diagnosis-side: external estimator (`analysis-output/18`) with documented queueing caveat; clean wrap-and-time on instrumented operator pending (W7) | W7 instrumented re-run subset |
| RQ4 | Hallucination Rate; Cohen κ ≥ 0.6 gate; LLM-A vs LLM-B interaction | Hallucination rate fully; κ partial (proxy Mistral-Large-3 done; canonical human reviewer pending W15); interaction estimable via LMM (`analysis-output/21`) | W15 human review of κ subsample |
| RQ5 | Time / CPU / memory / storage / API-calls overhead, paired ON vs OFF | Storage (`bundleSizeBytes`): full coverage. Time / memory / alloc count: operator patched to capture sub-millisecond resolution; subset re-run pending (W7) | W7 instrumented re-run + paired OFF baseline |

The matrix collection phase is complete (1500 S1 + 200 multi-app =
1700 captures, 0 no-report, 0 fabricated values). The remaining gaps
are measurement *coverage* of the existing captures, not measurement
*existence*. Each gap is bounded in size and has a concrete closure
work unit in `Q1-COMPLIANCE.md`.
