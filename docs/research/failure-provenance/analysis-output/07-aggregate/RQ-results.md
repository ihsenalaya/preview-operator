# RQ1–RQ5 results — single-LLM Phase 5a (LLM-A only)

**Frozen commit:** `2e81a648c3244c276dc3d58821a01e61d536901e`  
**Operator digest:** `testagentdevops.azurecr.io/preview-operator@sha256:88cd767f41c07f0eb65d85ea0d95f543926d07839f373b099dbb1c1d591a3a30`  
**LLM-A:** `gpt-4o-mini-2024-07-18` (temperature = 0)  
**Total rows:** 1500

## RQ1 — Evidence preservation

- **Capture rate:** 100.0% (100 / 100 reps captured a FailureReport with phase=Captured).
- **Namespace deletion observed before evidence access:** 100.0% of rows.
- **Evidence survived after teardown:** 100.0% of rows.

The operator captures all 100 / 100 attempted reps and the FailureReport survives the preview teardown 100 % of the time.

## RQ2 — RCA accuracy

### Pooled top-1 by engine (n = 500 each, across 10 scenarios × 5 configs × 10 reps)

| Engine mode | Strict top-1 | Aligned top-1 | newly credited |
|---|---|---|---|
| rule-grounded |  24.0% |  40.8% | +84 |
| llm-grounded |   4.0% |  31.4% | +137 |
| llm-freeform |   0.0% |  23.4% | +117 |

**Strict matcher**: requires the diagnosis component to literally equal the ground-truth label from `scenarios.yaml` (e.g. `migration-job`, `backend`, `frontend`). **Aligned matcher**: accepts documented per-scenario synonyms (`application`, `API`, `database migration`, …; alias table in `08-vocab-rescore.py`). Aligned recovers 22.5 % (338 / 1500) of rows that the strict matcher returned 0 on.

### Per-scenario × engine top-1 (aligned matcher)

| Scenario | rule-grounded | llm-grounded | llm-freeform |
|---|---|---|---|
| F1 | 100.0% [92.9%, 100.0%] | 100.0% [92.9%, 100.0%] |  70.0% [56.2%, 80.9%] |
| F2 |   0.0% [0.0%, 7.1%] |  62.0% [48.2%, 74.1%] |  68.0% [54.2%, 79.2%] |
| F3 |  80.0% [67.0%, 88.8%] |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |
| F4 |  60.0% [46.2%, 72.4%] |  42.0% [29.4%, 55.8%] |  30.0% [19.1%, 43.8%] |
| F5 |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |
| F6 |  54.0% [40.4%, 67.0%] |  52.0% [38.5%, 65.2%] |  28.0% [17.5%, 41.7%] |
| F7 |  54.0% [40.4%, 67.0%] |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |
| F8 |  60.0% [46.2%, 72.4%] |  58.0% [44.2%, 70.6%] |  38.0% [25.9%, 51.8%] |
| F9 |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |
| F10 |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |   0.0% [0.0%, 7.1%] |

**Reading guide.** 0-cells with aligned matcher are *not* vocabulary issues — they are real semantic misclassifications of the root cause. The remaining 0-cells are:

- **F3 llm-***: the LLM never recognises the ImagePullBackOff signature in the bundle.
- **F5 all engines**: diagnosers blame backend / API rather than the frontend; co-failing suites confuse single-hypothesis ranking.
- **F9 / F10 all engines**: LLMs name `test` or `regression` instead of the rubric's `seed-job` / `test-suite`; even aliases do not credit those, which is the honest call.

## RQ3 — MTTD

**Status: NOT MEASURED.** FailureReport.status does not carry a diagnosis_available_at timestamp (verified in task #19, 2026-05-23). MTTD column in the CSV is empty for all 1500 rows. Recovering MTTD requires an operator-side instrumentation pass that emits a diagnosis-completed timestamp, which is a Lot-1 (operator) gap, not a Phase-5a (data) gap.

## RQ4 — Hallucination

**Status: PARTIAL.** Hallucination rate column in the CSV is empty for all rows (strict matcher did not implement claim-level annotation). Per task #26, Claude Sonnet judge on F10 + 20% subsample is still pending. The strict→aligned delta on RQ2 is a *proxy* for one form of hallucination — naming a wrong component — but not for unsupported-claim hallucination per the analysis-plan §1 L6 definition.

## RQ5 — Runtime overhead

- **Bundle size (bytes):** median 3,971 (IQR 3,669–4,167) across all 1500 rows.

- **CPU overhead:** NOT MEASURED — column empty in CSV.
- **Memory overhead:** NOT MEASURED — column empty in CSV.

Bundle size is the only RQ5 metric captured during the live run. CPU/memory overhead requires a paired evidence-on / evidence-off setup that was not part of the Phase 5a run; this is future work flagged in the article's threats-to-validity table.
