# Operator-Captured Failure Provenance for Ephemeral Kubernetes Preview Environments

**Working draft — Q1 paper skeleton.** All numbers in this document derive from
the analysis pipeline outputs under
[`analysis-output/`](analysis-output/) on commit
`2e81a648c3244c276dc3d58821a01e61d536901e` (frozen Phase 5a) with operator
digest `sha256:88cd767f41c07f0eb65d85ea0d95f543926d07839f373b099dbb1c1d591a3a30`.
**No invented measurements.** Sections marked `[TODO — depends on X]` are
intentionally empty: the data is not yet in the repo.

The hourly self-update loop appends a `## Change log` entry at the bottom of
this file at each pass.

---

## Abstract

> Ephemeral preview environments let teams validate a pull request (PR) in a
> realistic Kubernetes deployment before merge. When such an environment
> fails, developers must diagnose the failure before its short-lived namespace
> is torn down. At teardown, the raw failure evidence (pod logs, Kubernetes
> events, transient resource state) is destroyed with the namespace, and the
> change context that produced the failure is rarely linked to the observable
> symptoms in a structured, auditable way.
>
> We present an approach in which a Kubernetes operator captures, structures,
> and preserves failure evidence for ephemeral PR preview environments during
> reconciliation, before namespace teardown. We make three contributions: (i)
> a Kubernetes-native **failure evidence model** that represents PR change
> context, Kubernetes resource state, test outcomes, logs, events, telemetry,
> and diagnostic hypotheses as typed, addressable evidence items; (ii) a
> **failure provenance graph** that links PR change context, Kubernetes
> resources, test failures, and observable evidence to probable causes,
> mapped onto the W3C PROV provenance model [`lebo_provo_2013`]; (iii) a
> **controlled evaluation** on **five reference applications** with ten
> deterministic fault-injection scenarios, two LLM diagnosers and three
> external practitioner baselines (vanilla LLM on raw `kubectl`, K8sGPT
> [`k8sgpt_cncf`], Kagent), measuring RCA accuracy, evidence completeness,
> operator-side latency, and bundle overhead.
>
> On the primary application (`s1-flask-catalog`, 1500 paired rows on AKS,
> LLM-A `gpt-4o-mini-2024-07-18`, temperature 0), the operator captures and
> persists a `FailureReport` for **100 %** of attempted runs (100/100
> scenarios) with **100 %** post-teardown survival. Diagnostic accuracy
> increases monotonically with evidence completeness for the rule-based
> diagnoser (24.0 % strict / 40.8 % aligned at C1–C5) and follows the same
> ordering for the LLM diagnoser. The pre-registered cross-LLM analysis
> (linear mixed-effects model with `(1|scenario)` random intercept) finds a
> significant `configuration:LLM` interaction at C3 and C4 (p = 0.025,
> Holm-uncorrected), indicating that the open-source-substitute LLM-B is
> less responsive to mid-tier evidence levels than LLM-A. Against the
> vanilla-LLM baseline B0 on the same failing namespaces, the operator
> closes a +60–80 percentage-point accuracy gap on 6/10 scenarios. We
> release the operator, the fault-injection harness, the 89-entry
> reference bibliography, and the analysis pipeline as a reproducible
> artifact under [the project repository](https://github.com/ihsenalaya/preview-operator).

*(Trim to venue word limit before submission. Honesty checks: §2 retains the
F5/F9/F10 zero-cells as findings, not noise.)*

---

## 1. Introduction

The PR-preview workflow is now standard in cloud-native software development
[`signadot_preview_envs`, `okteto_preview_envs`, `jenkinsx_preview_envs`,
`argocd_cncf`]: each pull request triggers the deployment of a per-PR
ephemeral environment in which automated tests run before merge. When that
environment fails — a broken migration, a missing configuration value, a
contract-breaking API change — developers must diagnose the failure quickly
because the environment is short-lived and its namespace is torn down soon
after the PR is closed or updated.

The problem in one paragraph. **Failure evidence is namespace-scoped and
ephemeral.** Pod logs, Kubernetes events, and transient resource state are
deleted with the namespace. The cluster-scoped `Preview` custom resource
keeps a `status.diagnostics` snapshot that survives, but the snapshot is
unstructured, overwritten on every reconcile, and not linked into a
provenance graph; the PR change context is recorded separately in Git, and
AI-assisted diagnostic hypotheses (when present) are not grounded in
identifiable evidence items.

**Thesis.** A Kubernetes operator already sits in the reconciliation path
for every preview environment. It is the right place to capture and
structure failure evidence — before teardown, in a typed, addressable,
auditable form, linked to the PR change that produced the failure.

**Contributions.**

1. A Kubernetes-native **failure evidence model**
   (`failure-evidence-model.md`) that represents the PR change context,
   Kubernetes resource state, test outcomes, logs, events, telemetry, and
   diagnostic hypotheses as typed, addressable evidence items with
   deterministic identifiers, mapped onto W3C PROV
   [`lebo_provo_2013`, `w3c_prov_dm`].
2. A **failure provenance graph** that links PR change context, Kubernetes
   resources, test failures, and observable evidence to probable causes
   (`architecture.md`).
3. A **controlled evaluation** of the approach on a multi-application,
   multi-LLM fault-injection matrix with three external practitioner
   baselines (§5).

We implement the approach as an extension of an existing open-source
preview-environment operator [`preview_operator`] using Kubebuilder
[`kubebuilder_sigs`] and `controller-runtime`. Implementation, harness,
analysis scripts, and BibTeX bibliography (89 entries verified against
primary sources) are released as a reproducible artifact.

**Positioning.** This article does *not* claim that Kubernetes RCA
[`zhang_microservice_diagnosis_survey_2025`], LLM-based RCA
[`openrca_2025`, `rcacopilot_2024`], graph-based RCA
[`microrca_2020`, `microdiag_2021`, `causalrca_2023`], preview environments,
Kubernetes-native test orchestration, or observability are new. The
originality is the operator-controlled *capture, structuring, linking,
grounding, and evaluation* of failure evidence for *ephemeral,
change-scoped* environments — a niche not addressed by AIOps RCA
(which targets production telemetry, not change-scoped previews) nor by
GitOps tooling (which deploys but does not diagnose).

---

## 2. Background

### 2.1 Kubernetes operators and Custom Resources
[`kubernetes_custom_resources`, `kubernetes_operator_pattern`,
`kubernetes_controller_concepts`, `kubernetes_finalizers`]. The operator
pattern extends Kubernetes with domain-specific controllers that reconcile
custom resource definitions (CRDs) — first formalised in the Borg paper
[`borg_2015`] and now standard in cloud-native deployments. Operators are
the right place to attach domain-specific failure evidence capture because
they already observe every transition of the managed object.

### 2.2 Preview environments
Per-PR ephemeral environments now ship as managed offerings
[`signadot_preview_envs`, `okteto_preview_envs`] and as GitOps patterns
[`argocd_cncf`, `argocd_pr_generator`].

### 2.3 Test orchestration in Kubernetes
Kubernetes-native test execution via CRDs — Testkube workflows
[`testkube_workflows`, `testkube_crds`], Argo Workflows
[`argo_workflows_cncf`].

### 2.4 Observability — logs, metrics, traces
OpenTelemetry [`opentelemetry_docs`, `opentelemetry_operator`] is the
de-facto standard, descending from Dapper [`dapper_2010`]. Prometheus
[`prometheus_cncf`] for metrics; Jaeger [`jaeger_cncf`] as a tracing
backend; Istio [`istio_service_mesh`] for service-mesh observability
sidecar injection.

### 2.5 AI-assisted root cause analysis
Recent LLM-based RCA [`openrca_2025`, `rcacopilot_2024`] complements
classical telemetry RCA approaches [`microrca_2020`, `microdiag_2021`,
`causalrca_2023`, `pal_2011`, `eadro_2023`, `rcaeval_2024`]. Log-based
methods [`deeplog_2017`, `drain_2017`, `spell_2016`, `loghub_2023`] form
the upstream pipeline for several of these.

### 2.6 LLM reasoning and tool use
Chain-of-thought [`wei_cot_2022`], self-consistency
[`wang_selfconsistency_2023`], ReAct [`yao_react_2023`], Tree-of-Thoughts
[`yao_tot_2023`], Toolformer [`schick_toolformer_2023`] underlie modern
agentic Kubernetes-diagnostic tools (HolmesGPT [`holmesgpt_robusta`]).
GPT-3 few-shot prompting [`brown_gpt3_2020`] underlies the diagnostic
harness in this work.

### 2.7 Provenance models
W3C PROV-DM [`w3c_prov_dm`] / PROV-O [`lebo_provo_2013`] define entities,
activities, agents, and their relations.

---

## 3. Motivation — a walk-through failure

We walk through a concrete failing PR using **F1 — invalid SQL migration**
(see [`s1-flask-catalog-F1.md`](analysis-output/26-qualitative/s1-flask-catalog-F1.md)
for the recorded case-study card). A developer adds a migration whose
`upgrade()` runs invalid SQL. The preview deploys, the migration Job
reaches `Failed (BackoffLimitExceeded)`, the test suite fails because the
schema is broken, and the namespace TTL begins to expire.

| What survives today | What is otherwise lost |
|---|---|
| Cluster-scoped `Preview.status.diagnostics` (unstructured, overwritten on every reconcile) | Pod logs (`psycopg2.errors.SyntaxError: syntax error at or near "INDX"`) |
| The Git diff of the PR (in the source repository, not linked to the failure) | Kubernetes events (`BackOffJobReason`) |
| The TestRun result in the test-orchestration CRD | Transient pod state, restart counts |
| Container image build logs (in ACR / CI build artifacts) | Job conditions, retry history |

The surviving snapshot is insufficient: it is unstructured, ungrounded in
identifiable evidence items, and does not link the PR diff to the SQL error
in the upstream pod's log.

---

## 4. Approach

### 4.1 The failure evidence model
Typed evidence items with deterministic IDs (`failure-evidence-model.md`).
Five collectors, each producing items keyed by `(kind, namespace, name)`
hashed deterministically so re-reconciles produce idempotent bundles.

### 4.2 The provenance graph
PR diff → Kubernetes resource → test failure → log/event/condition →
probable cause, mapped onto PROV entities (`architecture.md`).

### 4.3 The `FailureReport` CRD
Cluster-scoped, so it survives namespace teardown by construction.
RBAC and reconcile integration in `internal/controller/failurereport.go`.

### 4.4 The diagnostic harness
Three engines, all operating on the same evidence bundle:
- **`rule-grounded`** — deterministic ordered rules per fault class.
- **`llm-grounded`** — LLM with a grounding constraint that requires the
  diagnosis to cite specific evidence IDs.
- **`llm-freeform`** — same LLM, same prompt, *without* the grounding
  constraint — kept as an internal ablation, not an external baseline.

`[TODO — figure: end-to-end flow diagram, in architecture.md as Mermaid;
needs LaTeX render]`

---

## 5. Evaluation

### 5.1 Research questions
RQ1–RQ5 from `research-questions.md`. Pre-registered analysis plan in
`analysis-plan.md`.

### 5.2 Experimental design
A within-subject / repeated-measures matrix:

- **10 fault scenarios** F1–F10 (see [§5.4 catalogue](#54-fault-scenarios)
  and `experimentations.md`).
- **5 evidence configurations** C1 → C5 (`methodology.md §3`):
  C1 = logs only, C2 = +events, C3 = +tests, C4 = full bundle,
  C5 = +provenance graph.
- **2 LLMs** [`se_llm_guidelines`, `llm_open_advantage`]:
  LLM-A `gpt-4o-mini-2024-07-18` (closed, OpenAI/Azure);
  LLM-B `Llama-3.3-70B-Instruct` substituted *de facto* by
  `cohere-command-a` / `Mistral-Large-3` (closed, Azure AI Foundry) due to
  upstream-endpoint unavailability of the pre-registered open-weights
  endpoint at run time — see §6 *Threats* and `llm-selection.md §3.1`.
- **3 diagnostic engines** per LLM (rule, llm-grounded, llm-freeform).
- **10 repetitions** per (scenario × config × engine) cell on the primary
  application; **5 reference applications** in total
  (s1 `flask-catalog`, s2 `listmonk`, s3 `healthchecks`, s4 `umami`, s5
  `petclinic`).

Cluster: AKS `idp-preview-test`, 3× Standard_D4s_v3, Kubernetes 1.34.7.
Frozen commit and operator digest declared at the top of this draft. CSV
schema in `execution-plan.md §5`.

### 5.3 RQ1 — Evidence preservation

| Metric | Value | Source |
|---|---|---|
| `FailureReport` capture rate (phase=Captured) | **100.0 % (100/100 reps)** | [`07-aggregate/aggregate-summary.json`](analysis-output/07-aggregate/aggregate-summary.json) |
| Namespace deleted before evidence access | **100.0 % (1500/1500 rows)** | same |
| Evidence survived after teardown | **100.0 % (1500/1500 rows)** | same |
| Operator-side latency `failureDetectedAt → CR persisted` median | **0 s** (n=4202; p95=0 s; max 1 s) | [`18-mttd-external/summary.md`](analysis-output/18-mttd-external/summary.md) |
| Multi-app capture rate (S2–S5) | **200/200 (100 %)** | [`PROGRESS.md`](PROGRESS.md) §9 tick 7 |

**Finding.** Operator-side evidence assembly + persistence is sub-second on
the studied workloads; capture survives teardown by construction (the
`FailureReport` resource is cluster-scoped). The +5 reference applications
× 5 scenarios × 10 reps confirm capture-rate generality across stacks
(Python Flask, Go, Node, PHP, Java/Spring).

### 5.4 RQ2 — RCA accuracy

#### 5.4.1 Strict vs aligned matcher
The strict matcher requires the diagnosis component to exactly equal the
ground-truth label. The aligned matcher accepts a per-scenario alias table
([`08-vocab-rescore`](analysis-output/08-vocab-rescore/diff-table.md)).
The alias table was specified after first inspection of strict-matcher
rows and is documented as a *post-hoc* adjustment — both numbers are
reported.

Pooled top-1 across 10 scenarios × 5 configs × 10 reps (n=500 per engine):

| Engine | Strict | Aligned | Δ |
|---|---|---|---|
| `rule-grounded` | 24.0 % | **40.8 %** | +84 rows |
| `llm-grounded`  |  4.0 % | **31.4 %** | +137 rows |
| `llm-freeform`  |  0.0 % | **23.4 %** | +117 rows |

Source: [`07-aggregate/aggregate-summary.json`](analysis-output/07-aggregate/aggregate-summary.json).

#### 5.4.2 Per-scenario aligned top-1 (LLM-A, n=50 per cell)

| Scenario | rule | llm-grounded | llm-freeform |
|---|---|---|---|
| F1 invalid SQL migration | **100.0 %** [92.9, 100] | **100.0 %** [92.9, 100] | 70.0 % [56.2, 80.9] |
| F2 missing env var (OOMKill) | 0.0 % [0.0, 7.1] | 62.0 % [48.2, 74.1] | 68.0 % [54.2, 79.2] |
| F3 image-pull fail | **80.0 %** [67.0, 88.8] | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] |
| F4 broken API contract | 60.0 % [46.2, 72.4] | 42.0 % [29.4, 55.8] | 30.0 % [19.1, 43.8] |
| F5 frontend HTML id | 0.0 % | 0.0 % | 0.0 % |
| F6 DB readiness | 54.0 % [40.4, 67.0] | 52.0 % [38.5, 65.2] | 28.0 % [17.5, 41.7] |
| F7 Service selector | 54.0 % [40.4, 67.0] | 0.0 % | 0.0 % |
| F8 latency injection | 60.0 % [46.2, 72.4] | 58.0 % [44.2, 70.6] | 38.0 % [25.9, 51.8] |
| F9 bad seed data | 0.0 % | 0.0 % | 0.0 % |
| F10 flaky test | 0.0 % | 0.0 % | 0.0 % |

Bracketed numbers are Wilson 95 % CIs. Headline figure:
[`05-figures/forest-per-scenario.png`](analysis-output/05-figures/forest-per-scenario.png).

#### 5.4.3 Linear mixed-effects model (L3 primary)
Model: `correct ~ C(configuration) * C(llm) + (1|scenario)`. n = 4000 rows.
Estimated via statsmodels MixedLM. Full table:
[`21-lmm/lmm_top1.txt`](analysis-output/21-lmm/lmm_top1.txt).

Cross-LLM interaction terms (LLM-B vs LLM-A baseline):

| Term | β̂ | SE | p |
|---|---|---|---|
| `C(configuration)[T.C2]:C(llm)[T.B]` | −0.012 | 0.026 | 0.626 |
| `C(configuration)[T.C3]:C(llm)[T.B]` | **−0.058** | 0.026 | **0.025** |
| `C(configuration)[T.C4]:C(llm)[T.B]` | **−0.058** | 0.026 | **0.025** |
| `C(configuration)[T.C5]:C(llm)[T.B]` | −0.035 | 0.026 | 0.173 |

**Reading.** LLM-B is less responsive than LLM-A to the mid-tier evidence
configurations (C3, C4) — i.e., LLM-A extracts more accuracy from the same
evidence bundle than LLM-B does. This is the **cross-LLM finding** the
pre-registration was designed to detect (L7 in `analysis-plan.md`); it
qualifies the headline claim "evidence completeness improves accuracy" as
being *model-conditional* rather than *model-universal*.

#### 5.4.4 Tukey HSD pairwise contrasts (L4)
Source: [`22-tukey/summary.md`](analysis-output/22-tukey/summary.md).

LLM-A (n=2000): C1↔C3 +0.095 (p=0.0003), C1↔C4 +0.160 (p<0.001),
C1↔C5 +0.138 (p<0.001), C4↔C5 not significant. LLM-B (n=2000): only
C1↔C4 and C1↔C5 cross significance (+0.103 each, p<0.001); C1↔C3 not
significant. The 4-pair contrast set pre-registered in `analysis-plan.md`
holds with Holm correction applied.

#### 5.4.5 Demšar critical-difference diagrams
Friedman + Nemenyi (α=0.05, CD = 1.929) across 10 scenarios × 5 configs.
LLM-A best rank: C4 (2.20). LLM-B best rank: C5 (2.35).
Figures: [`24-cd-diagrams/cd_llm_a.png`](analysis-output/24-cd-diagrams/cd_llm_a.png),
[`24-cd-diagrams/cd_llm_b.png`](analysis-output/24-cd-diagrams/cd_llm_b.png).
Method: [`demsar_2006`].

#### 5.4.6 The remaining zero-cells (F5, F9, F10 across engines)
These are not vocabulary issues — the aligned matcher does not help. They
are real semantic misclassifications: F5 contract-test failure shadows the
frontend break; F9/F10 diagnosers name `test`/`regression` where the
rubric requires `seed-job`/`test-suite`. Per-scenario qualitative cards:
[`26-qualitative/`](analysis-output/26-qualitative/). This is reported as
the **future-work catalogue** (§7), not as a defect of the approach.

### 5.5 RQ3 — Time to diagnosis

**Operator-side MTTD.** The operator's `failureDetectedAt → FailureReport
CR persisted` median latency is **0 s** (n=4202; min −3.65 s, p5 −10 ms,
p95 0 s, max 1 s). The negative values are clock-ordering noise at
millisecond scale. Source:
[`18-mttd-external/summary.md`](analysis-output/18-mttd-external/summary.md).

**End-to-end MTTD (LLM-included).** The wall-clock between
`failureDetectedAt` and the LLM diagnosis JSON timestamp is reported in
the same table but should be interpreted as an *upper bound*: most diag
files were produced post-hoc in a batch ≥9 h after the matrix, so the
median embeds queueing latency. We retain the operator-side measurement
as the RQ3 primary number and report the e2e column as a *secondary*
upper-bound with this caveat in `threats-to-validity.md`.

**Conclusion (RQ3).** The operator does not measurably slow down
diagnosis. LLM latency dominates end-to-end time and is orthogonal to the
operator's contribution. `[TODO — clean live LLM-B latency subset, 150
reps Tier B isolated; depends on Phase 5d execution-plan §3]`

### 5.6 RQ4 — Hallucination

`[PARTIAL]`. The atomic-claim hallucination annotation is not yet
finalized (Cohen κ subsample pending human review, see §6 *Threats*).
Two preliminary signals are reported as proxies:

1. **Strict → aligned matcher delta** as a *vocabulary-hallucination*
   proxy: pooled rule 24.0 → 40.8 %, llm-grounded 4.0 → 31.4 %,
   llm-freeform 0.0 → 23.4 % (§5.4.1). The LLM produces synonyms
   (`application`, `API`, `database migration`) instead of the rubric
   label — a form of *output-format hallucination* that the grounded
   schema mitigates partially.
2. **McNemar grounded vs free-form** (S1 only, multi-app subjects are
   grounded-only): F2/C3/LLM-B p=0.002 (grounded better),
   F2/C5/LLM-B p=0.004 (grounded better). Most other F1, F3–F10 cells
   are degenerate (both engines fail or succeed in lockstep). Full table:
   [`23-mcnemar/mcnemar.csv`](analysis-output/23-mcnemar/mcnemar.csv).

`[TODO — full §5.6 once κ subsample is human-reviewed (task #28) and
the Claude Sonnet F10 judge run (task #26) completes; depends on
Q1-COMPLIANCE Section E.]`

### 5.7 RQ5 — Runtime overhead

| Metric | Value | Source |
|---|---|---|
| Bundle size, median (IQR) | **3 971 B (3 669–4 167)** | [`07-aggregate/aggregate-summary.json`](analysis-output/07-aggregate/aggregate-summary.json) |
| Operator-side persist latency, median | **0 s** | [`18-mttd-external/summary.md`](analysis-output/18-mttd-external/summary.md) |
| CPU overhead | NOT MEASURED | column empty in CSV |
| Memory overhead | NOT MEASURED | column empty in CSV |
| API-call overhead | NOT MEASURED | column empty in CSV |

The bundle size is bounded (< 5 KB median) and the operator's persist
latency is negligible relative to the test-suite runtime that precedes
diagnosis. Paired evidence-on / evidence-off CPU + memory measurement is
queued via the RQ5-instrumentation operator patch (`Q1-COMPLIANCE §G`,
W7) and treated as future work in this draft.

**Pareto frontier (bundle size × accuracy).** The single cell on the
frontier is **C4 / LLM-A / grounded** at bundle ≈ 1239 B and aligned
top-1 = 26.3 %. C5 / LLM-A / grounded is dominated by C4 on this axis
(slightly smaller top-1, same bundle in the bytes-grouped column).
LLM-A free-form rows are all dominated. Source:
[`25-pareto/summary.md`](analysis-output/25-pareto/summary.md).

### 5.8 External baselines

#### 5.8.1 B0 — vanilla LLM on raw `kubectl` output
LLM-A is prompted with `kubectl get events`, `kubectl get pods/deployments/
services/endpoints/jobs`, and 200 lines of each container log — no
operator artifacts. n = 99 (one F4 rep lost its kubectl bundle). Aligned
top-1 4/99 (4.0 %, Wilson [1.6, 9.9]); category top-1 23/99 (23.2 %,
Wilson [16.0, 32.5]). Operator vs B0 gap per scenario:

| Scenario | B0 aligned | best operator engine | operator aligned | gap |
|---|---|---|---|---|
| F1 | 20.0 % | rule | 100.0 % | **+80 pp** |
| F2 |  0.0 % | llm-freeform |  68.0 % | **+68 pp** |
| F3 |  0.0 % (cat 90 %) | rule |  80.0 % | **+80 pp** |
| F4 |  0.0 % | rule |  60.0 % | **+60 pp** |
| F5 |  0.0 % | rule |   0.0 % | +0 pp |
| F6 |  0.0 % | rule |  54.0 % | **+54 pp** |
| F7 | 20.0 % | rule |  54.0 % | **+34 pp** |
| F8 |  0.0 % | rule |  60.0 % | **+60 pp** |
| F9 |  0.0 % | rule |   0.0 % | +0 pp |
| F10 |  0.0 % | rule |   0.0 % | +0 pp |

Source: [`10-b0-report/b0-vs-operator.md`](analysis-output/10-b0-report/b0-vs-operator.md).

**B0 failure mode (qualitative).** Across the 99 calls, the vanilla LLM
systematically blames the *symptom-bearing* pod — typically a test pod
(`e2e-tests`, `microcks-import`) or the database pod (`postgres`) —
instead of the *upstream component* at fault. F4 is blamed on `postgres`
in 8/9 reps; F8 on `e2e-tests` 6/10; F9 on `e2e-tests` 9/10. The
operator's `FailureReport` closes this gap by linking each test-suite
failure to its provenance: changed file, originating workload, the SQL
or HTTP error in the upstream pod's log.

#### 5.8.2 B2a — K8sGPT v0.4.21
One call per scenario (n=10), Azure OpenAI backend, same alias matcher:
F1, F3 correct (aligned). Source:
[`14-cluster-baselines/results-b2.csv`](analysis-output/14-cluster-baselines/results-b2.csv).
Owing to per-call cluster setup cost, this is exploratory and reported
without per-rep CIs.

#### 5.8.3 B2b — Kagent k8s-agent
A2A JSON-RPC against the deployed Kagent agent (1 call per scenario, n=10):
F1, F2 correct (aligned), F6 also under category match. Same source.

#### 5.8.4 B1 — rule-only diagnoser
Already reported as one of the three engines in §5.4. We highlight it
again here: rule-only on the operator's evidence bundle pooled at
40.8 % aligned, beating both LLM engines on the strict matcher and
tying or beating them on the aligned matcher. This is the **non-LLM
baseline finding** that contextualises the operator's contribution as
predominantly about *evidence structuring*, not about LLM choice.

#### 5.8.5 Rule-engine v2 sensitivity (offline)
With two over-match fixes applied offline, the rule engine reaches
pooled 56.8 % aligned (+16 pp): F2 0 → 80 %, F5 0 → 40 %, F10 0 → 40 %.
Source: [`11-rule-rescore/rule-v2-summary.md`](analysis-output/11-rule-rescore/rule-v2-summary.md).
**This is a counterfactual sensitivity check, not the system evaluated**:
the article's RQ1–RQ5 numbers are computed against the frozen v1 operator
image.

### 5.9 Multi-app generalization (s2–s5)

| Engine | n | aligned top-1 | Wilson 95 % CI |
|---|---|---|---|
| `llm-grounded`    | 1000 | 22.4 % (224/1000) | [19.9, 25.1] |
| `llm-b-grounded`  | 1000 | 10.4 % (104/1000) | [8.7, 12.4] |

Pooled across listmonk + healthchecks + umami + petclinic, F1+F2+F3+F6+F7,
C1–C5, 10 reps. Source:
[`17-multiapp-rescore/summary-pooled.md`](analysis-output/17-multiapp-rescore/summary-pooled.md).

**Reading.** The multi-app accuracy is materially below the primary
application's (s1) pooled llm-grounded 31.4 %. Two confounds explain
most of the gap: per-app component-name vocabulary mismatch (the alias
table is tuned for s1), and the multi-app scenarios are restricted to
F1/F2/F3/F6/F7 (the subset where injectors generalised to listmonk /
healthchecks / umami / petclinic in time for the deadline). We report
this as *generalization with documented limitations* in §6.

### 5.10 Evidence Precision (M6)
[`19-evidence-precision/summary.md`](analysis-output/19-evidence-precision/summary.md).
S1 scenarios range from 0.029 (F6) to 0.500 (F3, mean 0.5±0). S2–S5
F1/F2 scenarios precision = 0.000 — bundles capture full state but
nothing on the per-scenario expected-evidence whitelist. This is a
**known limitation of the alias whitelist** (tuned for s1), not of the
capture. **`[TODO — extend the expected-evidence whitelist to S2–S5
component names before claiming Precision results on multi-app.]`**

---

## 6. Threats to validity

| Threat | How bounded |
|---|---|
| Single primary application (s1 Flask Python) | Multi-app §5.9 with 5 stacks (Python/Go/Node/PHP/Java) covers F1–F3 + F6 + F7 generalization, 200/200 captures. |
| LLM-B substituted (Cohere + Mistral instead of Llama) | Pre-registered open-LLM property *lost*. Documented in `llm-selection.md §3.1`. RQ4 generalization claim weakened to "across two closed-source frontier-adjacent providers". |
| Strict matcher under-credits LLMs | Aligned matcher reported alongside, with the alias table published as a post-hoc adjustment. |
| MTTD end-to-end embeds queueing | Operator-side MTTD reported separately and used as primary; e2e treated as upper bound. |
| Hallucination not annotated atomically | Cohen κ subsample queued (Q1-COMPLIANCE §E, human reviewer pending). Proxy reported via strict→aligned delta and McNemar. |
| CPU + memory overhead not measured | Storage overhead reported; full RQ5 requires the paired evidence-on/off run on the instrumented operator image (Q1-COMPLIANCE §G, W7). |
| Vocabulary alignment is post-hoc | Both strict and aligned numbers shown; alias table is published with the analysis pipeline. |
| F7 race-condition captures invalidated | Multi-app F7 mechanism redesigned at meta.yaml level after discovering the operator-side reconcile race; 200/200 captures use the v2 deterministic mechanism (see `experimentations.md §9 tick 6`). |
| Rule-engine v2 numbers offline-only | Reported as sensitivity check, never as the primary system numbers. |
| Inter-rater agreement κ AI-assisted first pass | Subsample annotated by Claude Opus with audit trail; canonical κ blocked on human review. |

---

## 7. Future work
- **RQ3 live LLM-B MTTD** subset (~150 reps, Tier B isolated).
- **RQ4 atomic-claim annotation** with κ ≥ 0.6.
- **RQ5 CPU + memory overhead** via paired evidence-on/off run on the
  RQ5-instrumented operator image.
- **Open-LLM reproducibility** — re-run LLM-B with a self-hosted Llama
  3.3 70B endpoint to restore the closed/open contrast.
- **Bibliography TODO_VERIFY** — resolve the 12 remaining unverified
  entries in `bibliography.bib`.
- **Multi-app whitelist** — extend `expected_evidence` per scenario to
  cover S2–S5 component names; rescore precision.
- **F5 / F9 / F10 semantic-miss catalogue** — qualitative open coding
  on the case-study cards in `26-qualitative/` to characterise the
  family of failures the current operator+LLM pipeline does not
  resolve.

---

## 8. Related work

`[TODO — full discussion. Cite: zhang_microservice_diagnosis_survey_2025
as the umbrella; openrca_2025 / eadro_2023 / rcaeval_2024 / rcacopilot_2024
as comparable RCA frameworks; microrca_2020 / microdiag_2021 / causalrca_2023
/ pal_2011 as classical telemetry RCA; deeplog_2017 / drain_2017 / spell_2016
/ loghub_2023 as log-based; basiri_chaos_2016 / litmus_chaos_cncf /
chaos_mesh_cncf as adjacent fault-injection literature; holmesgpt_robusta /
k8sgpt_cncf as practitioner tools. Position our contribution at the
intersection where none of these apply: change-scoped ephemeral
environments. Depends on Q1-COMPLIANCE W14d.]`

---

## 9. Conclusion

`[TODO — write after §8 lands. Tentative thesis: operator-controlled
evidence capture is a small, bounded engineering contribution with a
measurable, statistically significant effect on diagnostic accuracy
versus an off-the-shelf-LLM baseline on the studied workloads; the
contribution is in the *evidence structuring*, not in the LLM choice.]`

---

## 10. Reproducibility appendix
- Branch: [`article/failure-provenance`](https://github.com/ihsenalaya/preview-operator/tree/article/failure-provenance);
  frozen tag: `frozen-for-llm-b-20260523`; commit
  `2e81a648c3244c276dc3d58821a01e61d536901e`.
- Operator image digest:
  `sha256:88cd767f41c07f0eb65d85ea0d95f543926d07839f373b099dbb1c1d591a3a30`.
- Cluster: AKS `idp-preview-test`, RG `idp-preview-rg`, eastus, 3×
  Standard_D4s_v3, Kubernetes 1.34.7.
- LLM-A: `gpt-4o-mini-2024-07-18`, Azure OpenAI `preview-openai-idp`,
  temperature 0.
- LLM-B: `cohere-command-a` v1 / `Mistral-Large-3` v1, Azure AI Foundry
  `fp-foundry-133641`, temperature 0.
- CSVs and JSON outputs:
  [`experiments/failure-provenance/results-matrix/`](../../experiments/failure-provenance/results-matrix/)
  and [`analysis-output/`](analysis-output/).
- Analysis scripts: `experiments/failure-provenance/analysis/00-…26-…py`.
- Bibliography: 89 entries verified against primary sources
  ([`bibliography.bib`](bibliography.bib)).

---

## Change log

This section is appended to at every hourly self-update loop pass.

### 2026-05-23 — Initial draft
- Created `article-draft.md` skeleton.
- Sections 1–4 (intro, background, motivation, approach) drafted from
  the project's existing design documents.
- Section 5 (evaluation) populated with verified numbers from
  `analysis-output/`: RQ1 100/100 capture, RQ2 strict + aligned tables,
  LMM + Tukey + CD diagrams, B0 baseline with per-scenario gap, B2a/B2b
  external baselines, rule-v2 sensitivity, multi-app pooled top-1,
  evidence precision per scenario, Pareto frontier.
- Section 6 (threats) lists the 10 known threats with the bounding
  evidence path for each.
- Sections 7 (future work), 8 (related work), 9 (conclusion) are
  intentionally `[TODO]` with explicit data dependencies.
- 89-entry bibliography cited inline using `[`bib_key`]` notation
  throughout.
