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

- Kubernetes version: **1.34.7** — pinned on the AKS `idp-preview-test` cluster
  used for the full campaign (S1 + multi-app). Recorded in `experimentations.md §1`.
  Local kind cluster runs the same minor version when used for smoke tests.
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

---

## §X — Execution log: deviations from the methodology

This section is appended after the experiment was run, to disclose the
deviations from the methodology described above. None of them change
the headline conclusions; each is bounded and quantified.

1. **RQ5 paired ON / OFF baseline** — the methodology described a paired
   run of each scenario with `EVIDENCE_COLLECTION=on` and `=off` for
   overhead measurement. Only the ON arm was run on the S1 + multi-app
   matrix. The OFF arm is pending the instrumented-operator subset
   re-run (W7 in `Q1-COMPLIANCE.md`). The published storage component
   of M10 (`bundleSizeBytes`) is unaffected; the time / memory
   components are derived from `runtime.ReadMemStats` deltas captured
   by the instrumented operator.

2. **F7 fault model on multi-app subjects** — the original injector
   patched the Service selector post-deploy. The operator's reconciler
   reverted the patch within ~3 s on multi-app subjects (operator-owned
   Service spec), producing a race condition. The fault model was
   redesigned to set `services[0].port = 19999` (an unbound port) at
   the meta.yaml level *before* deploy, which yields a deterministic
   "connection refused" failure consistent with the F7 infrastructure
   class. The semantic class of F7 is preserved. Documented in
   `threats-to-validity.md §7.1`.

3. **Recommendation Usefulness scoring** — operationalised as
   LLM-as-judge (Mistral-Large-3, blind) rather than the
   originally-considered manual rubric, to keep the protocol
   reproducible. Disagreements above the κ < 0.6 floor with the human
   reviewer block the row from primary analysis (consistent with M8).

4. **Cross-LLM substitution chain** — the originally pre-registered
   LLM-B was Llama-3.3-70B (Together.ai). It was substituted to
   Cohere `command-a` (Azure AI Foundry) after a regional capacity
   outage on the Foundry Llama deployment. The substitution preserves
   the methodological requirement of three disjoint provider families
   (OpenAI / Cohere / Mistral). Documented in `llm-selection.md §3.1`.

5. **F4, F5, F8, F9, F10 not in scope on S2–S5** — the operator-only
   evaluation footprint requires injection at the manifest level (no
   per-subject source-code patches). F4/F5/F8/F9/F10 rely on
   application-level code paths that are S1-specific. The multi-app
   scope is the F1, F2, F3, F6, F7 subset. The article's RQ4 cross-LLM
   reading uses the same fault subset, so multi-app and S1 are still
   matcher-comparable on those five.
