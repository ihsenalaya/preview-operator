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

**Status: not yet collected — Phase 2 of the locked plan.**

Per `analysis-plan.md §8`, this article will compare against:

- **B0 — Vanilla LLM on raw kubectl output** (task #23)
- **B2a — K8sGPT v0.4.21 pinned** (task #24)
- **B2b — Kagent generic `k8s-agent`** (task #25)

`LOCKED-PLAN.md §3` documents why both K8sGPT and Kagent are run:
K8sGPT is the CNCF-Sandbox practitioner standard cited in the
literature; Kagent is a contemporary 2025 agentic platform deployed
alongside our operator. Reporting both lets the reviewer pick the
comparator they trust.

A cross-LLM (LLM-B = Llama-3.3-70B-Instruct-Turbo) pass is also
pending — tasks #21–#22 — and will populate the
`configuration:LLM` interaction term required by
`analysis-plan.md §3`.

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
