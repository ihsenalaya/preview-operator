# Article Outline

**Working title:** Operator-Captured Failure Provenance for Ephemeral Kubernetes Preview Environments

**Status:** Draft outline — to be filled with experimental results from Phase 5.

---

## Positioning statement (read before drafting)

This article does **not** claim that Kubernetes root cause analysis (RCA), LLM-assisted
RCA, graph-based RCA, preview environments, Kubernetes-native test orchestration, or
observability are new. All of these are established. The originality is positioned
precisely as:

> A Kubernetes operator captures, structures, and preserves failure evidence for
> ephemeral pull-request preview environments **before namespace teardown**, linking
> PR diff, Kubernetes resources, test failures, logs, events, traces, and AI-assisted
> diagnostic hypotheses into an **auditable provenance artifact**.

Honesty note for authors: the `Preview` custom resource is *cluster-scoped*, so the
existing `status.diagnostics` snapshot already survives namespace deletion. The article
must therefore **not** claim "evidence is otherwise lost". The defensible gap is that
today's snapshot is *unstructured, overwritten on every reconcile, and not linked into
a provenance graph*, and that AI hypotheses are *not grounded* in identifiable evidence
items. See `state-of-the-art.md` §G and `contributions.md`.

---

## Proposed abstract (English)

> Ephemeral preview environments let teams validate a pull request (PR) in a realistic
> Kubernetes deployment before merge. When such an environment fails — a broken
> migration, a missing configuration value, an incompatible API change — developers
> must diagnose the failure quickly, because the environment is short-lived and its
> namespace is torn down soon after the PR is closed or updated. At teardown, the raw
> failure evidence (pod logs, Kubernetes events, transient resource state) disappears
> with the namespace, and the change context that produced the failure is rarely linked
> to the observable symptoms in a structured, auditable way.
>
> This paper presents an approach in which a Kubernetes operator captures, structures,
> and preserves failure evidence for ephemeral PR preview environments during
> reconciliation, before namespace teardown. We make three contributions. First, we
> define a **Kubernetes-native failure evidence model** that represents PR change
> context, Kubernetes resource state, test outcomes, logs, events, telemetry, and
> diagnostic hypotheses as typed, addressable evidence items. Second, we define a
> **failure provenance graph** that links PR change context, Kubernetes resources, test
> failures, and observable evidence to probable causes and recommendations, mapped onto
> the W3C PROV provenance model. Third, we report a **controlled evaluation** using ten
> deterministic fault-injection scenarios that measures RCA accuracy, evidence
> completeness, hallucination rate, time to diagnosis, and runtime overhead, comparing
> five evidence configurations from logs-only to full provenance graph. We implement
> the approach as an extension of an existing open-source preview-environment operator
> and release the implementation, fault-injection harness, and evaluation scripts as a
> reproducible artifact.

*(Trim to the target venue's word limit. Keep the three contributions explicit.)*

---

## Section structure

### 1. Abstract
See above.

### 2. Introduction
- The PR-preview workflow: per-PR ephemeral environments, short TTL, namespace teardown.
- The problem in one paragraph: failures happen *inside* the ephemeral environment;
  diagnosis competes with teardown; evidence and change context are not linked.
- Thesis: the operator — already in the reconciliation path — is the right place to
  capture and structure failure evidence.
- Contributions list (3 items, verbatim from the abstract).
- Paper roadmap.

### 3. Background
- **3.1 Kubernetes operators.** Custom Resources, the operator pattern, the
  controller reconciliation loop, finalizers.
- **3.2 Preview environments.** Per-PR ephemeral environments; industrial platforms.
- **3.3 Test orchestration in Kubernetes.** Kubernetes-native test execution, CRDs
  for test plans and test runs.
- **3.4 Observability and OpenTelemetry.** Logs, metrics, traces; auto-instrumentation.
- **3.5 AI-assisted root cause analysis.** LLM-based and graph-based RCA.
- **3.6 Provenance models.** W3C PROV / PROV-DM; entities, activities, agents.

### 4. Motivation
- Walk-through of a concrete failing PR (use scenario **F1 — invalid SQL migration**).
- Timeline: failure detected → developer investigates → namespace TTL expires →
  evidence gone. Show what survives today (`status.diagnostics`) and what does not
  (raw logs, events, pod state) and *why the surviving snapshot is insufficient*
  (unstructured, overwritten, ungrounded).

### 5. Problem Statement
- P1: Evidence volatility — raw evidence is namespace-scoped and ephemeral.
- P2: Missing linkage — change context, resources, tests, and evidence are recorded
  separately, not as a connected provenance artifact.
- P3: Ungrounded diagnosis — AI hypotheses are not tied to specific evidence items,
  enabling unverifiable or hallucinated explanations.
- Scope and non-goals (we do not propose a new RCA algorithm or a new LLM).

### 6. Proposed Approach
- The operator as the evidence-capture point: capture occurs in the reconcile loop,
  triggered on failure detection, before the teardown path runs.
- Capture pipeline: detect failure → collect evidence items → assemble bundle →
  build provenance graph → persist artifact → allow teardown.
- Finalizer interaction: how persistence is sequenced before namespace deletion.

### 7. Failure Evidence Bundle
- The evidence model (forward-reference `failure-evidence-model.md`).
- Evidence item structure: typed, addressable (`FailureEvidenceItem`), with relevance
  and redaction flags.
- Evidence types: GitDiff, ChangedFile, KubernetesEvent, PodLog, JobLog, TestResult,
  TraceSpan, Metric, PreviewCondition, ReconcileEvent.
- Redaction of secrets and tokens.

### 8. Failure Provenance Graph
- Nodes and edges; mapping to W3C PROV (Entity / Activity / Agent).
- The provenance chain: PR → commit → changed files → Preview CR → namespace →
  resources → test suite → failed test → observable evidence → probable cause →
  recommendation.
- Why a graph: traceability, grounding of hypotheses, audit.

### 9. Operator Implementation
- The base operator (the `Preview` CRD, phases, reconcile loop).
- Existing diagnostic substrate reused: `DiagnosticsStatus`, `ChangeContextSpec`,
  `ReconcileEvent`, `TestPlan`/`TestRun`, kagent integration.
- New components (implemented): the `internal/evidence/` package — evidence
  collectors, the `Bundle` assembler, deterministic evidence IDs, the redaction
  utility, idempotent report generation; and the `FailureReport` cluster-scoped
  CRD, wired into the operator failure paths so it is produced for every failed
  preview.
- Idempotency: stable evidence IDs, no duplicate items on repeated reconcile —
  unit tested.
- Grounding discipline: each AI hypothesis must reference existing evidence IDs;
  the `Bundle.SetDiagnosis` API rejects a diagnosis with unknown references.
- The provenance graph builder linking evidence items into the typed graph is the
  one component still specified-but-not-implemented — note it honestly as such.

### 10. Experimental Methodology
- Forward-reference `methodology.md`.
- Environments (Kind, optional AKS); fault-injection harness; ground-truth design;
  the five comparison configurations C1–C5.

### 11. Evaluation
- **11.1** RQ1 — Evidence preservation and snapshot survival.
- **11.2** RQ2 — RCA accuracy across C1–C5.
- **11.3** RQ3 — Time to diagnosis.
- **11.4** RQ4 — Hallucination control under the grounding constraint.
- **11.5** RQ5 — Runtime overhead of failure evidence collection.
- Results tables, plots, confidence intervals; Kind vs AKS where available.

### 12. Discussion
- When provenance helps most (which failure classes) and least (flaky tests).
- Cost/benefit of operator-side capture.
- Generality beyond this operator.

### 13. Threats to Validity
- Forward-reference `threats-to-validity.md` (internal, external, construct, conclusion).

### 14. Conclusion
- Restate contributions and headline results; future work (trace-backed evidence,
  cross-PR provenance, multi-cluster).

### 15. References
- Generated from `bibliography.bib`.

---

## Figures and tables (planned)

| Ref | Content | Source file |
|-----|---------|-------------|
| Fig. 1 | End-to-end flow (PR → operator → evidence bundle → report → PR comment) | `architecture.md` Diagram 1 |
| Fig. 2 | Failure Evidence Bundle composition | `architecture.md` Diagram 2 |
| Fig. 3 | Failure Provenance Graph | `architecture.md` Diagram 3 |
| Fig. 4 | Evidence persistence before namespace deletion | `architecture.md` Diagram 4 |
| Fig. 5 | Failure evidence model mapped to W3C PROV | `failure-evidence-model.md` |
| Table 1 | Fault-injection scenario matrix F1–F10 | `experiments.md` |
| Table 2 | Comparison configurations C1–C5 | `methodology.md` |
| Table 3 | RCA accuracy (Top-1 / Top-3) per configuration | results (Phase 5) |
| Table 4 | Overhead breakdown (time, CPU, memory, storage, API calls) | results (Phase 5) |

---

## TODO before submission

- [ ] Fill all `Evaluation` subsections with Phase 5 results.
- [ ] Confirm the target venue and trim the abstract to its word limit.
- [ ] Resolve every `TODO_VERIFY` in `bibliography.bib`.
- [ ] Final pass to ensure no unsupported claim survives (see `threats-to-validity.md`).
