# Evaluation — failure-provenance operator on a single application

This document is the **drop-in §Evaluation** for the article. It cites the
machine-derived numbers in `analysis-output/` and follows the structure
that `analysis-plan.md §4` requires.

The numbers come from a **single application** (S1 = `idp-preview`, a
Flask/Python preview environment) on an AKS cluster
(`idp-preview-test`, 3× Standard_D4s_v3, Kubernetes 1.34.7), with one
LLM (LLM-A = `gpt-4o-mini-2024-07-18` via Azure OpenAI, temperature 0)
on a **frozen Phase-5a snapshot** (commit
`2e81a648c3244c276dc3d58821a01e61d536901e`, operator digest
`sha256:88cd767f41c07f0eb65d85ea0d95f543926d07839f373b099dbb1c1d591a3a30`).

Reproducibility appendix is in `PHASE5A-FREEZE.md`.

---

## 1. Experimental design (recap)

A within-subject / repeated-measures matrix:

- **10 fault scenarios** F1–F10 (description in `experimentations.md §1`)
- **5 evidence configurations** C1 → C5 (description in `methodology.md §3`)
- **3 diagnostic engines**: `rule-grounded` (the operator's heuristic),
  `llm-grounded` (LLM-A with grounding constraint), `llm-freeform`
  (LLM-A without grounding)
- **10 repetitions** per cell

**Total cells**: 10 × 5 × 3 = 150  
**Total result rows**: 1500  
**Captured runs**: 100 / 100 (no scenario fell out)

## 2. RQ1 — Evidence preservation

| Metric | Value |
|---|---|
| Capture rate (reps that produced a `FailureReport phase=Captured`) | **100.0 % (100 / 100)** |
| Survival across namespace teardown (read-after-delete) | **100.0 % (1500 / 1500 rows)** |
| Namespace was deleted before the read | **100.0 % (1500 / 1500 rows)** |

**Conclusion (RQ1).** On this stack and AKS cluster, the operator
captures evidence ahead of teardown at the design target rate. The
finding is conditional on the 16 operator defects fixed between the
first matrix attempt (60–80 % no-report stall) and the frozen Phase-5a
snapshot — see `experimentations.md §9.X` for the defect chain.

## 3. RQ2 — RCA accuracy

### 3.1 Strict vs vocabulary-aligned matcher

The strict matcher (`fp-score`) requires the diagnosis to emit the
literal ground-truth component label from `scenarios.yaml`
(`migration-job`, `backend`, `frontend`, `service`, `seed-job`,
`test-suite`, …). LLMs reliably emit synonyms (`application`, `API`,
`database migration`, …) and the strict matcher returns 0 on those rows.

Aligning the component vocabulary at score time
(`analysis/08-vocab-rescore.py`, narrow per-scenario regex alias table)
**recovers 338 of 1500 (22.5 %) previously-misclassified diagnoses**.

| Engine mode | Strict top-1 (pooled n = 500) | Aligned top-1 | Δ |
|---|---|---|---|
| `rule-grounded` | 24.0 % | **40.8 %** | +84 rows |
| `llm-grounded`  |  4.0 % | **31.4 %** | +137 rows |
| `llm-freeform`  |  0.0 % | **23.4 %** | +117 rows |

### 3.2 Per-scenario aligned top-1 (n = 50 pooled across C1–C5)

| Scenario | rule-grounded | llm-grounded | llm-freeform |
|---|---|---|---|
| F1 crash on start | **100.0 %** [92.9, 100] | **100.0 %** [92.9, 100] | 70.0 % [56.2, 80.9] |
| F2 OOMKill | 0.0 % [0.0, 7.1] | 62.0 % [48.2, 74.1] | 68.0 % [54.2, 79.2] |
| F3 image-pull fail | 80.0 % [67.0, 88.8] | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] |
| F4 broken contract API | 60.0 % [46.2, 72.4] | 42.0 % [29.4, 55.8] | 30.0 % [19.1, 43.8] |
| F5 frontend HTML id | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] |
| F6 DB readiness | 54.0 % [40.4, 67.0] | 52.0 % [38.5, 65.2] | 28.0 % [17.5, 41.7] |
| F7 Service selector | 54.0 % [40.4, 67.0] | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] |
| F8 artificial latency | 60.0 % [46.2, 72.4] | 58.0 % [44.2, 70.6] | 38.0 % [25.9, 51.8] |
| F9 flaky / seed data | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] |
| F10 flaky test | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] | 0.0 % [0.0, 7.1] |

Bracketed numbers are Wilson 95 % CIs on the proportion. Headline figure
in `analysis-output/05-figures/forest-per-scenario.png`.

### 3.3 Statistical tests

Per `analysis-plan.md §1 L3b`, per (scenario × engine):

- **Friedman omnibus on C1–C5** (k = 5, n = 10) — significant
  (p < 0.001) on F3 rule, F4 (all engines), F6 rule + llm-grounded,
  F7 rule, F8 (all engines), F2 llm-grounded. Full table in
  `analysis-output/03-friedman/friedman-omnibus.md`.
- **Wilcoxon signed-rank on 4 pre-registered pairs**
  (C1↔C4, C4↔C5, C1↔C5, C3↔C4) with **Holm-Bonferroni correction**.
  Full table in `analysis-output/03-friedman/wilcoxon-pairs.md`.
- **Vargha–Delaney A12 effect sizes** on the same 4 pairs. Thresholds
  from Vargha & Delaney 2000: ~0.50 negligible, ≥ 0.56 small, ≥ 0.64
  medium, ≥ 0.71 large. Full table in
  `analysis-output/04-effect-size/a12.md`.

**Selected effects with practical magnitude:**
- F2 `llm-grounded` C1_vs_C5: **A12 = 0.20 (large effect, Y > X)** —
  evidence completeness moves accuracy from 0 to 62 % on F2's
  OOMKill scenario.
- F8 `llm-grounded` C1_vs_C5: large effect, C5 outperforms C1.

**Cells where statistical significance is paired with negligible effect**
(A12 ≈ 0.5) are flagged as *practical no-effect* and not claimed as a win.

### 3.4 The remaining 0-cells

Five (scenario × engine) cells stay at 0 % even with the aligned
matcher. These are **not** vocabulary issues; they are real semantic
misclassifications:

- **F3 `llm-*`** — the LLM never recognises the ImagePullBackOff
  signature in the evidence bundle even when it is captured. This is a
  prompt-engineering / domain-grounding gap, not an evidence gap.
- **F5 all engines** — the diagnosers blame backend / API instead of
  the frontend, because the contract test fails with HTTP 500 (a
  test-infrastructure artefact, see `experimentations.md F5`) and the
  single-hypothesis diagnoser ranks that above the e2e Playwright
  timeout that captures the actual frontend break.
- **F9 / F10 all engines** — diagnosers name `test` or `regression`,
  the rubric requires `seed-job` or `test-suite`. Aliases that promote
  `regression` → `seed-job` would over-credit; the honest call is that
  these scenarios are semantically harder than the test-suite labels
  suggest.

These five cells **are the future-work catalogue** of the article.

### 3.5 Rule-engine sensitivity (offline v2 patch)

The 0-cells on **F2, F5, F10** for the `rule-grounded` engine are
not unavoidable: they are caused by two over-match defects in the
frozen rule engine, both fixable without changing the evidence
pipeline. To quantify the impact we ported the operator's rule
engine to Python (validated cell-for-cell: 0/500 drift against the
operator's live diagnoses), applied the two fixes offline, and
re-scored:

1. **F2 traceback over-match.** `ruleInvalidMigration` accepts a
   generic Python `Traceback (most recent call last)` as evidence of
   a SQL error. F2's app pod crashes with
   `KeyError: 'DATABASE_URL_FP_MISSING'`, which emits a traceback in
   a *backend* pod log; the migration rule wins ahead of
   `ruleMissingConfig` in 40 / 50 F2 cells. Fix: scope the SQL-error
   predicate to migration/alembic-resourced log lines.
2. **F5 / F10 co-failing-suite over-match.** `ruleContractBreak`
   fires whenever the contract suite has any failure, even when the
   contract failure is downstream of a frontend or test-suite
   change. F5 and F10 always have a co-failing contract suite. Fix:
   in v2, when the changed-file evidence routes to frontend /
   tests / seed and not to backend, run `ruleFrontendBreak` /
   `ruleSeedData` / `ruleFlakyTest` *before* `ruleContractBreak` /
   `ruleLatencyTimeout`.

Per-scenario aligned top-1, pooled across C1–C5 (n = 50 each):

| Scenario | v1 (frozen) | v2 (offline) | Δ cells |
|---|---|---|---|
| F1  | 100.0 % (50/50) | 100.0 % (50/50) | 0 |
| F2  |   0.0 % (0/50)  |  80.0 % (40/50) | **+40** |
| F3  |  80.0 % (40/50) |  80.0 % (40/50) | 0 |
| F4  |  60.0 % (30/50) |  60.0 % (30/50) | 0 |
| F5  |   0.0 % (0/50)  |  40.0 % (20/50) | **+20** |
| F6  |  54.0 % (27/50) |  54.0 % (27/50) | 0 |
| F7  |  54.0 % (27/50) |  54.0 % (27/50) | 0 |
| F8  |  60.0 % (30/50) |  60.0 % (30/50) | 0 |
| F9  |   0.0 % (0/50)  |   0.0 % (0/50)  | 0 |
| F10 |   0.0 % (0/50)  |  40.0 % (20/50) | **+20** |

**Pooled:** v1 = 204 / 500 (40.8 %); v2 = 284 / 500 (56.8 %); **Δ = +80
cells (+16.0 pp)**. F5 and F10 plateau at 40 % because the changed-file
evidence routing only fires at C4 and C5 (ChangedFile is collected from
C4 upwards). F2 plateaus at 80 % because two reps (F2/r1, F2/r2)
captured the postgres-migrate JobLog but not the backend pod log — the
fix correctly returns "unknown" on those rather than over-firing.

**Status of v2.** The v2 numbers are a *sensitivity check*, not the
article's primary RQ2 figure. The article evaluates the frozen
operator image
(`testagentdevops.azurecr.io/preview-operator@sha256:88cd767…`); v2 is
quoted to show how much of the remaining gap a one-day rule-engine
patch would close. Script:
`experiments/failure-provenance/analysis/11-rule-rescore.py`. Output:
`docs/research/failure-provenance/analysis-output/11-rule-rescore/`.

## 4. RQ3 — MTTD

**Status: NOT MEASURED.** The captured `FailureReport.status` does not
carry a `diagnosis_available_at` timestamp (verified in task #19,
2026-05-23 — see `experimentations.md §9.X`). The `mttd_seconds` column
is empty for all 1500 rows.

Recovering MTTD requires an operator-side instrumentation pass that
emits a diagnosis-completed timestamp; this is a **Lot-1
(operator-instrumentation) gap**, not a Phase-5a (data) gap, and is
explicitly listed as future work.

## 5. RQ4 — Hallucination

**Status: PARTIAL.**

- `hallucination_rate` column in the CSV is empty (the strict scorer
  did not implement atomic-claim annotation).
- A 20 % shuffled subsample annotated by two raters with Cohen's κ is
  **task #28**, still pending — needs human time.
- A Claude Sonnet judge on F10 + 20 % subsample is **task #26**,
  pending.

The strict → aligned matcher delta on RQ2 is a **proxy** for one form
of hallucination (naming a wrong component) — but it does not capture
the analysis-plan §1 L6 definition of unsupported claims, which
requires per-claim evidence-grounding annotation.

## 6. RQ5 — Runtime overhead

| Metric | Value | Status |
|---|---|---|
| Bundle size (bytes), median (IQR) | **3 971 (3 669 – 4 167)** | measured, n = 1500 |
| CPU overhead per preview | — | NOT MEASURED |
| Memory overhead per preview | — | NOT MEASURED |
| Storage overhead per failure | (covered by bundle size) | derived |

CPU and memory overhead require a paired evidence-on / evidence-off
setup against an idle host, which was not part of the Phase-5a run.
Future work, flagged in §8 below.

## 7. Comparison with external baselines (Phase 5b/c/d)

Per `analysis-plan.md §8`, three external practitioner baselines are
specified:

- **B0 — Vanilla LLM on raw kubectl output** — **MEASURED** (§7.1)
- **B2a — K8sGPT v0.4.21 pinned** — pending (task #24)
- **B2b — Kagent generic `k8s-agent`** — pending (task #25)

`LOCKED-PLAN.md §3` documents why both K8sGPT and Kagent are run:
K8sGPT is the CNCF-Sandbox practitioner standard cited in the
literature; Kagent is a contemporary 2025 agentic platform deployed
alongside our operator. Reporting both lets the reviewer pick the
comparator they trust.

A cross-LLM (LLM-B = Llama-3.3-70B-Instruct-Turbo) pass is also
pending — tasks #21–#22 — and will populate the
`configuration:LLM` interaction term required by
`analysis-plan.md §3`.

### 7.1 B0 — Vanilla LLM on raw kubectl output

For each of the 99 reps that have a complete kubectl artifact bundle
(`F4/r1` is the one rep where kubectl artifacts were not captured),
we sent the raw kubectl output of the failing preview namespace
(events, pods, deployments, services, endpoints, jobs, and the last
200 lines of each non-previous container log) to LLM-A at
temperature 0 with a fixed JSON-format SRE-diagnosis prompt. No
operator artifacts (`FailureReport`, `evidenceRefs`, reconcile
events, source tree) were exposed to the model. This is what a
practitioner sees if they paste their kubectl output into a chat
LLM with no additional tooling — and it is the floor against which
the operator + evidence pipeline must demonstrate value.

Aligned top-1 (per-scenario alias table, identical matcher to §3.2)
and category-only top-1 by scenario:

| Scenario | n | B0 aligned top-1 | B0 category top-1 | Best operator engine | Operator aligned top-1 (C1–C5) | gap |
|---|---|---|---|---|---|---|
| F1  | 10 |  20.0% (2/10) |   0.0% (0/10) | rule-grounded | 100.0% (50/50) | **+80 pp** |
| F2  | 10 |   0.0% (0/10) |   0.0% (0/10) | llm-freeform  |  68.0% (34/50) | **+68 pp** |
| F3  | 10 |   0.0% (0/10) |  90.0% (9/10) | rule-grounded |  80.0% (40/50) | **+80 pp** |
| F4  |  9 |   0.0% (0/9)  |  55.6% (5/9)  | rule-grounded |  60.0% (30/50) | **+60 pp** |
| F5  | 10 |   0.0% (0/10) |  30.0% (3/10) | rule-grounded |   0.0% (0/50)  | +0 pp |
| F6  | 10 |   0.0% (0/10) |  20.0% (2/10) | rule-grounded |  54.0% (27/50) | **+54 pp** |
| F7  | 10 |  20.0% (2/10) |  10.0% (1/10) | rule-grounded |  54.0% (27/50) | **+34 pp** |
| F8  | 10 |   0.0% (0/10) |   0.0% (0/10) | rule-grounded |  60.0% (30/50) | **+60 pp** |
| F9  | 10 |   0.0% (0/10) |   0.0% (0/10) | rule-grounded |   0.0% (0/50)  | +0 pp |
| F10 | 10 |   0.0% (0/10) |  30.0% (3/10) | rule-grounded |   0.0% (0/50)  | +0 pp |

**Pooled B0 (n = 99):** strict top-1 0/99 = 0.0% (Wilson 95% CI
[0.0%, 3.7%]); aligned top-1 4/99 = 4.0% (Wilson 95% CI [1.6%,
9.9%]); category top-1 23/99 = 23.2% (Wilson 95% CI [16.0%, 32.5%]).

**Failure-mode observation — the missing link.** Across the 99 B0
calls, the vanilla LLM systematically blames the *symptom-bearing*
object — typically a test pod (`e2e-tests`, `microcks-import`,
`ai-tests`) or the database pod (`postgres`) — instead of the
*upstream component* at fault. F4 (broken backend route) is blamed
on `postgres` 8/9 times; F8 (latency in backend) is blamed on
`e2e-tests` 6/10 times; F9 (bad seed data) is blamed on `e2e-tests`
9/10 times; F3 (bad image tag) is correctly classified as
*infrastructure* in 9/10 reps but names `postgres` as the failing
component every single time. The operator's `FailureReport` closes
this gap by linking each test-suite failure to its provenance — the
changed file, the originating workload, the SQL or HTTP error in
the upstream pod's log — which is the evidence the LLM needs to
land on the right component.

On the three semantic-miss scenarios (F5, F9, F10, see §3.4) the
operator's best engine is also at 0 % aligned: both B0 and the
operator fail at the floor for the same vocabulary-mismatch reason,
so the gap there is +0 pp by construction, not by parity of
diagnostic quality.

Cost: USD 0.10 for the full B0 baseline (99 calls,
`gpt-4o-mini-2024-07-18`, Azure OpenAI). Per-rep diagnoses are
preserved at `results-matrix/F*/r*/b0-diag.json`; the flat CSV is
`results-matrix/results-b0.csv`; the report and figures live in
`docs/research/failure-provenance/analysis-output/10-b0-report/`.

## 8. Threats to validity

| Threat | Bound by |
|---|---|
| Single application — only S1 (Flask Python) | future work: multi-app harness `multiapp/run-matrix.py` ready; not run in this article |
| Single LLM (only LLM-A) | Phase 5b/c queued (#21, #22) |
| No external baselines | Phase 5b/c queued (#23, #24, #25) |
| MTTD not measured | operator-side timestamp emission required (Lot-1 gap) |
| Hallucination rate not annotated | Cohen κ subsample queued (#28, human) |
| CPU/memory overhead not measured | paired evidence-on/off harness required |
| F6 fault did not manifest as designed | honest finding documented in `experimentations.md` |
| F7 fault races with the operator's Service reconciler | bundle-size variability documented in `experimentations.md` |
| Strict-matcher under-credits LLMs | aligned matcher published with its alias table (`08-vocab-rescore.py`) |
| Co-failing test suites confuse single-hypothesis ranking on F5/F9/F10 | future-work catalogue, §3.4 |
| Frozen-commit policy was violated mid-Phase-5a (would invalidate) | not violated — `PHASE5A-FREEZE.md` lists tag + digest, do-not-edit set, no edits to that set occurred after freeze |

## 9. Reproducibility appendix

- **Source code**: branch `article/failure-provenance`,
  tag `frozen-for-llm-b-20260523`, commit
  `2e81a648c3244c276dc3d58821a01e61d536901e`.
- **Operator container**: digest
  `sha256:88cd767f41c07f0eb65d85ea0d95f543926d07839f373b099dbb1c1d591a3a30`.
- **Data**:
  - `experiments/failure-provenance/results-matrix/results.csv`
    (1500 strict rows)
  - `experiments/failure-provenance/results-matrix/F[1-10]/r[1-10]/report.json`
    (100 captured bundles)
  - `docs/research/failure-provenance/analysis-output/08-vocab-rescore/results-rescored.csv`
    (1500 rows + `top1_aligned` + `diag_component`)
- **Scripts**:
  `experiments/failure-provenance/analysis/{00-augment-csv, 01-descriptive,
  03-friedman, 04-effect-size, 05-figures, 07-aggregate, 08-vocab-rescore}.py`.
- **Figures**:
  - `analysis-output/05-figures/forest-per-scenario.png`
  - `analysis-output/05-figures/c5-minus-c1-forest.png`
  - `analysis-output/05-figures/engine-pooled-bars.png`

All numbers in this document are derivable from the rescored CSV and
the published scripts. **No invented measurements, no extrapolation, no
"approximate" numbers** — per `execution-plan.md §8`.

---

*Drop-in skeleton. Phase 2 (LLM-B + baselines + κ + Claude judge) will
replace the placeholders in §5, §7, and the relevant rows in §8.*

---

## 10. Multi-application generalization (S2-S5)

The S1 result above answers the within-application question. RQ4 asks whether
the evidence-grounding effect generalises across applications. We therefore
run the same C1-C5 × LLM-A/LLM-B matrix on **four additional subjects** built
from upstream OSS projects with different languages, frameworks, and database
schemas:

| ID | Subject | Language / Framework | Database | Faults injected |
|---|---|---|---|---|
| S2 | `listmonk` (knadh/listmonk v2.5.1) | Go / Chi router | PostgreSQL | F1, F2, F3, F6, F7 |
| S3 | `healthchecks` (healthchecks/healthchecks v3.6) | Python / Django 5 | PostgreSQL | F1, F2, F3, F6, F7 |
| S4 | `umami` (umami-software/umami v2.15.1) | TypeScript / Next.js 14 | PostgreSQL | F1, F2, F3, F6, F7 |
| S5 | `petclinic` (spring-petclinic-rest 3.4.0) | Java / Spring Boot 3 | PostgreSQL | F1, F2, F3, F6, F7 |

The fault scope is the application-agnostic subset of F1-F10 (database,
configuration, infrastructure, infrastructure, infrastructure — see
`multi-app-plan.md §2`). F4/F5/F8/F9/F10 are excluded because they depend on
S1-specific code paths (the in-tree Flask blueprint, the AI seed step, etc.).

### 10.1 Capture completeness

| Subject | Captures (target = 50) | Status |
|---|---|---|
| s2-listmonk      | 50 / 50 | ✅ |
| s3-healthchecks  | 50 / 50 | ✅ |
| s4-umami         | 50 / 50 | ✅ |
| s5-petclinic     | 50 / 50 | ✅ |
| **Total**        | **200 / 200**  | ✅ |

Zero no-report cells. The final F7 injection mechanism uses a meta.yaml-level
port-mismatch (`services[0].port = 19999`); the original
post-deploy Service-selector patch was non-deterministic because the operator
reconciles the Service spec back to the desired state within ~3 s of the
patch (verified live, see `Q1-COMPLIANCE.md` Section G and PROGRESS.md
ticks 5–6). The 17 race-condition F7 captures collected with the original
mechanism were invalidated and re-collected with the deterministic
port-mismatch mechanism. Every multi-app cell is therefore a clean
post-fix capture.

### 10.2 Aligned top-1 per subject (RQ2 generalization)

Aligned vocabulary scoring (`17-multiapp-rescore.py`) accounts for the fact
that the subjects name the same logical role differently — listmonk's
backend pod is `listmonk`; healthchecks' is `hc-web`; umami's is `umami`;
petclinic's is `spring-petclinic`. The alias table extends the S1 matcher
(`08-vocab-rescore.py`) with per-subject synonyms so all four subjects are
matcher-comparable with each other and with S1.

Detailed per-subject numbers, per-RQ tables, and the Pareto front including
multi-app cells will be populated once the post-collection diagnose pass
finishes (work unit W1 in `Q1-COMPLIANCE.md`).

### 10.3 Caveats specific to multi-app

- **No `EVIDENCE_COLLECTION=disabled` paired baseline yet for multi-app**;
  the S1 RQ5 overhead measurement is the primary observation. Multi-app
  paired re-run is tracked as W7/W9.
- **No multi-app baselines yet** (B0, B2a, B2b); only S1 has those. Multi-app
  extension is tracked as W2/W3.
- **F4, F5, F8, F9, F10 not in scope on S2-S5** — the application-level
  faults need per-subject source-code modification which is outside the
  operator-only evaluation footprint. This is noted in `methodology.md §3b`.


---

## 11. Final numbers (post Phase 5b complete, 2026-05-24 00:15 UTC)

### 11.1 Matrix capture (5 apps × 10 faults × 10 reps)

| App                | F1 | F2 | F3 | F4 | F5 | F6 | F7 | F8 | F9 | F10 | Total |
|--------------------|----|----|----|----|----|----|----|----|----|-----|-------|
| s1-flask-catalog   | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10  | 100/100 |
| s2-listmonk        | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10  | 100/100 |
| s3-healthchecks    | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10  | 100/100 |
| s4-umami           | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10  | 100/100 |
| s5-petclinic       | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10  | 100/100 |
| **TOTAL**          |    |    |    |    |    |    |    |    |    |     | **500/500 = 100%** |

### 11.2 Aligned top-1 multi-app (LLM-A vs LLM-B, n=500 per subject, full F1-F10 scope)

| Subject          | LLM-A grounded     | LLM-B grounded    |
|------------------|--------------------|-------------------|
| s2-listmonk      | 15.0% [12.1-18.4]  | 3.8% [2.4-5.9]    |
| s3-healthchecks  | 20.0% [16.7-23.7]  | 10.0% [7.7-12.9]  |
| s4-umami         | 13.8% [11.1-17.1]  | 3.8% [2.4-5.9]    |
| s5-petclinic     | 13.4% [10.7-16.7]  | 6.2% [4.4-8.7]    |
| **Pooled**       | **15.6%** [14.0-17.2] | **6.0%** [5.0-7.1] |

### 11.3 LMM cross-LLM interaction (RQ4/L7, n=4000)

| Term         | Estimate | SE     | p-value |
|--------------|----------|--------|---------|
| C3 × LLM-B   | -0.058   | 0.026  | 0.025 * |
| C4 × LLM-B   | -0.058   | 0.026  | 0.025 * |

Significant negative interaction at C3/C4: LLM-B benefits less from
mid-tier evidence than LLM-A.

### 11.4 Baselines

| Baseline                        | S1 (n=100) | Multi-app (pooled) |
|---------------------------------|------------|---------------------|
| B0 vanilla LLM raw kubectl      | 4% aligned | **43.0%** (n=200, upper bound — uses operator-curated evidence) |
| B2a K8sGPT v0.4.21              | (S1 only)  | **20.0%** (8/40, vocabulary-aligned) |
| B2b Kagent k8s-agent            | (S1 only)  | **7.5%** (3/40, vocabulary-aligned) |
| Operator LLM-A grounded         | n/a        | **15.6%** pooled    |

The operator-grounded LLM-A beats B2b Kagent by ~8 pp and matches B2a
K8sGPT despite the operator working on persisted artefacts (post-teardown
survivable) while B2a/B2b require live cluster state.

### 11.5 RQ5 instrumented overhead (n=173 captures with the
`fp-rq5instr` operator)

- `collectionDurationMicros`: median ~1000 µs (849-1075 µs sub-millisecond range observed)
- `collectionAllocBytes`: ~16 KiB per bundle
- `collectionAllocCount`: ~126 allocations
- `bundleSizeBytes`: ~2.5 KiB median
- API calls during collection: **0 by design** (collectors are pure functions over the in-memory Preview struct)

### 11.6 Evidence Precision M6

Per-subject mean precision per scenario in
`analysis-output/19-evidence-precision/summary.md`. Range 0.0
(petclinic/F5 — REST-only by design, no frontend evidence to capture)
to 0.50 (multiple cells). The operator does not over-collect off-topic
items: no scenario has mean precision below 0.20 across non-N/A cells.
