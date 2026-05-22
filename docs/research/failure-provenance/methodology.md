# Methodology

This chapter defines the experimental methodology used to answer RQ1–RQ5
(`research-questions.md`). It is written to be reproducible: another researcher should
be able to re-run the evaluation from this description plus the artifact in
`experiments/failure-provenance/`.

---

## 1. Experimental environments

### 1.1 Kind local cluster (primary, reproducibility)

The primary environment is a local Kubernetes cluster created with **Kind** (Kubernetes
in Docker). Kind is chosen because it is single-machine, scriptable, and produces a
deterministic, disposable cluster — ideal for reproducible fault injection.

- Kubernetes version: **TODO_VERIFY** — pin one version and record it (e.g. the version
  used by the operator's `make test-e2e`).
- Node layout: single control-plane node unless a scenario needs more.
- Host machine specification (CPU model, cores, RAM, OS) **must be recorded** in the
  results, because overhead metrics (RQ5) are hardware-dependent.

### 1.2 AKS cluster (optional, cloud validation)

A managed **Azure Kubernetes Service (AKS)** cluster is used, if available, to check
whether results transfer from a local single-node cluster to managed multi-node
Kubernetes. AKS results are reported **separately** from Kind results — never pooled —
because cluster topology, networking, and storage differ.

- If AKS is unavailable for a submission, the article reports Kind only and lists
  cloud validation as future work (this is an explicit external-validity threat — see
  `threats-to-validity.md`).

---

## 2. Core components under test

All components below already exist in the `preview-operator` repository unless marked
*(new)*.

| Component | Role | Source |
|-----------|------|--------|
| `preview-operator` | The Kubernetes operator under study | `cmd/`, `internal/controller/` |
| `Preview` CRD | Cluster-scoped resource describing one PR preview environment | `api/v1alpha1/preview_types.go` |
| Ephemeral namespace | Per-`Preview` isolated namespace, deleted at teardown | created by the controller |
| PostgreSQL (ephemeral) | Per-preview database | `spec.database` |
| Migration job | One-shot DB migration before app deploy | `spec.database.migration` |
| Seed job | One-shot data seeding after migration | `spec.database.seed` / AI enrichment |
| Smoke / contract / regression / e2e / migration tests | Sequential test pipeline | `internal/controller/tests.go`, `TestSuiteSpec` |
| `TestPlan` / `TestRun` CRDs | Test selection and per-run results | `api/v1alpha1/` |
| `ReconcileEvent` CRD | Append-only reconcile audit trail | `api/v1alpha1/reconcileevent_types.go` |
| OpenTelemetry wiring | Auto-instrumentation annotations on the app pod | `spec.telemetry` |
| kagent / AI diagnostic | diff-analyzer, troubleshooter, test-strategist agents | `internal/controller/kagent.go` |
| GitHub Actions PR workflow | Creates the `Preview` CR, runs the diff classifier | `.github/workflows/preview-env.yml` |
| Evidence collectors *(new)* | Assemble the failure evidence bundle | Phase 2 scaffolding |
| `FailureReport` artifact *(new)* | The persisted provenance artifact | Phase 3 (CRD or status extension) |

**Honesty note.** Trace evidence depends on app-level OpenTelemetry instrumentation and
an external trace backend; the operator only wires auto-instrumentation. Experiments
that rely on trace evidence (notably F8) must record whether a trace backend was
deployed; if not, trace evidence is reported as *not available* rather than *missing*.

---

## 3. Experimental flow

Each experimental run follows the same twelve steps:

1. **Create a synthetic PR.** A scripted change against the demo application
   (`demo-app/`). The PR is synthetic and deterministic — no human authoring.
2. **Inject a controlled fault.** Exactly one fault from the matrix F1–F10
   (`experiments.md`) is applied. Each fault has a single, known root cause.
3. **CI creates the `Preview` CR.** The GitHub Actions workflow (or its scripted
   equivalent for offline runs) classifies the diff and applies a `Preview` resource.
4. **Operator reconciles the environment.** Namespace, quota, database, migration,
   deployment, services, ingress.
5. **Tests execute.** The configured test pipeline runs (smoke → contract → regression
   → e2e, plus migration tests when relevant).
6. **Failure occurs.** The injected fault causes a deterministic failure in a known
   phase/suite. `failure_detected_at` is recorded.
7. **Operator captures the Failure Evidence Bundle.** Collectors gather change context,
   Kubernetes state, test results, logs, events, and (if available) telemetry.
8. **Operator creates/updates the `FailureReport` / `Preview` status.** The bundle and
   provenance graph are persisted. Generation is idempotent (no duplicate items on
   repeated reconcile — see RQ-related metric *Reconcile Idempotency*).
9. **AI diagnosis is produced from structured evidence.** The diagnostic step
   (rule-based and/or kagent) consumes the bundle. `diagnosis_available_at` is recorded.
10. **Namespace is deleted.** Either by TTL expiry or by deleting the `Preview` CR; the
    finalizer sequences persistence before namespace teardown.
11. **Verify evidence survives deletion.** After namespace deletion, the persisted
    artifact is re-read and compared to what was captured (Snapshot Survival Rate).
12. **Compare diagnosis with ground truth.** The produced diagnosis is scored against
    the scenario's known root cause (see §4 and `scoring-rubric.md`).

Steps 1–3 are the *setup*; 4–9 are the *system under test*; 10–12 are the *measurement*.

---

## 4. Ground truth design

Every injected fault has a **known root cause**, fixed when the scenario is authored.
Ground truth for a scenario is the tuple:

```
(component, failure_category, expected_evidence_items[])
```

- `component` — the part of the system at fault (e.g. `migration-job`, `app-deployment`,
  `service`, `seed-job`, `test-suite`).
- `failure_category` — one of: `database`, `configuration`, `infrastructure`,
  `application`, `observability`, `test-reliability`.
- `expected_evidence_items` — the evidence items a complete bundle should contain for
  this fault (listed per scenario in `experiments.md`).

**Correctness rule.** A diagnosis is counted **correct** only if it satisfies *all three*:

1. it identifies the correct `component`;
2. it identifies the correct `failure_category`;
3. it references **at least one valid evidence item** that genuinely supports the cause.

Partial matches (right component, wrong category — or no evidence reference) are
recorded as **incorrect** for Top-1/Top-3 accuracy, but the partial breakdown is kept
for the discussion section.

---

## 5. Comparison configurations

Diagnosis quality (RQ2, RQ3, RQ4) is evaluated across five evidence configurations.
Each configuration controls *what evidence the diagnostic step receives*; the failure
itself and the cluster are identical across configurations for a given run.

| Config | Evidence supplied to the diagnostic step |
|--------|-------------------------------------------|
| **C1** | Logs only (pod/job log excerpts) |
| **C2** | Logs + Kubernetes events |
| **C3** | Logs + events + test results |
| **C4** | Full evidence bundle (change context, Kubernetes state, tests, logs, events, telemetry, conditions) |
| **C5** | Provenance graph + full evidence bundle (C4 plus the typed graph linking change → resource → test → evidence → cause) |

- C1 is the **baseline** ("logs-only analysis" in RQ2).
- C4 isolates the value of the **bundle** (Contribution 1).
- C5 isolates the additional value of the **provenance graph** (Contribution 2) on top
  of C4.
- The grounding constraint (RQ4) is evaluated as an **orthogonal ablation** on top of
  C4 and C5: *grounded* vs *free-form* diagnosis.

---

## 6. Repetitions and statistics

- **Repetitions.** Each (scenario, configuration) pair is run **at least 10 times**.
  More repetitions are used for scenarios with high variance (notably F10, the flaky
  test, and any LLM-in-the-loop configuration).
- **LLM control.** For every configuration that uses the kagent LLM diagnostic step:
  fix the model and the model version; set temperature to 0 for the diagnostic step;
  record the model identifier and version in every result row.
- **Concurrency.** Where feasible, run parallel previews with **1, 5, and 10**
  concurrent PRs to measure how capture behaves under load (relevant to RQ5).
- **Statistics.** Report means with **confidence intervals** (e.g. 95% CI) over
  repetitions. For accuracy rates use proportion confidence intervals (e.g.
  Wilson interval). Keep Kind and AKS results separate.
- **Isolation.** Run one experiment at a time on an otherwise idle host for overhead
  measurements (RQ5); concurrency experiments are explicitly labelled.

---

## 7. Data collection

For each run, the harness records one CSV row (schema in
`experiments/failure-provenance/results-template.csv`) plus the raw artifacts:

- the `Preview` object (`spec` and `status`);
- the `FailureReport` artifact (if present);
- Kubernetes events, pod statuses, job statuses;
- collected logs;
- test result artifacts (JUnit / output lines) where available.

Raw artifacts are stored under a per-run directory keyed by `(scenario_id, run_id,
cluster_type, configuration)` so that any reported number can be traced back to its
evidence.

---

## 8. Validity safeguards (summary)

- **Determinism.** Faults are injected by script; no manual editing during a run.
- **Ground truth.** Fixed per scenario before any run.
- **Repetition.** ≥10 runs per cell; CIs reported.
- **Independent scoring.** Diagnosis correctness and hallucination annotation follow
  the fixed rubric in `experiments/failure-provenance/scoring-rubric.md`; where
  resources allow, two annotators score independently and inter-rater agreement is
  reported.
- **Separation.** Kind and AKS reported separately; overhead runs isolated from
  concurrency runs.

Full discussion of remaining threats is in `threats-to-validity.md`.
