# Q1-COMPLIANCE — global gap tracker for the failure-provenance article

**Created:** 2026-05-23 19:50 Paris (17:50 UTC)
**Owner:** project lead + AI assist
**Rule (non-negotiable):** no missing tests, no missing results, no missing data,
no missing analyses. Every measurement promised by the pre-registration must be
delivered or the row goes to a sensitivity table with an explicit reason.

This file is the single tracking source for everything that still has to land
before the article is Q1-credible. Each row has a status:
- ✅ done and verified
- 🔄 in progress (work unit running now)
- ⏳ pending (queued, not started)
- ❌ blocked (needs decision or human action)
- ⚠️ partial (works but not at the pre-registration's scope)

When a row flips to ✅, edit the status here AND record the artefact path that
proves it. Commit + push after each flip; this file is the live audit trail.

---

## Section A — Data gaps (CSV columns 0% filled across 1500 rows)

`experiments/failure-provenance/results-matrix/results-augmented.csv` was
audited 2026-05-23 17:48 UTC. 6 columns out of 20 are 0% filled across all
1500 rows.

| # | Column                | RQ  | Status | Fix path |
|---|-----------------------|-----|--------|----------|
| A1 | `diagnosis_available_at` | RQ3 | ❌ 0/1500 | Compute externally from `diag-*.json._called_at_utc`. New script `18-mttd-external.py`. |
| A2 | `mttd_seconds`        | RQ3 | ❌ 0/1500 | Derived from A1 minus `failure_detected_at`. Same script. |
| A3 | `evidence_precision`  | RQ2 | ❌ 0/1500 | Compute from FailureReport `evidenceItems` ∩ `expected_evidence` (per scenario). New script `19-evidence-precision.py`. |
| A4 | `recommendation_score`| RQ2 | ❌ 0/1500 | Define + score "recommendation usefulness". Either a deterministic rubric (preferable) or LLM-as-judge with rubric. New script `20-recommendation-usefulness.py`. |
| A5 | `cpu_overhead`        | RQ5 | ❌ 0/1500 | Operator instrumentation needed (Go runtime metrics during AssembleBundle). Patch operator, rebuild, re-run subset. |
| A6 | `memory_overhead`     | RQ5 | ❌ 0/1500 | Same as A5 (`runtime.ReadMemStats` delta). |

**Also partial (filled but not at full pre-registration depth):**
- `llm_model_version`, `llm_provider`, `llm_temperature`: 67% filled (LLM-A only;
  LLM-B rows have empty model metadata — fix from the existing diag-*.json file).

---

## Section B — Statistical-analysis gaps (layers L1–L7 from analysis-plan §1)

Audited 2026-05-23 17:50 UTC against `docs/research/failure-provenance/analysis-output/`.

| Layer | Coverage | Status | Fix path |
|-------|----------|--------|----------|
| L1 — Descriptive (means, medians, proportions)            | ✅ 11 outputs | done | `analysis-output/01-descriptive/` |
| L2 — Uncertainty (Wilson, bootstrap)                      | ✅ 4 outputs  | done | `analysis-output/02-bootstrap/` |
| **L3 — Primary inferential (LMM)**                        | ❌ no script, no output | **missing**   | New script `21-lmm.py` with `statsmodels.MixedLM` (`correct ~ configuration * LLM + (1\|scenario) + (1\|run)`). |
| L3b — Friedman + Holm                                     | ✅ 3 outputs  | done | `analysis-output/03-friedman/` |
| **L4 — Tukey / emmeans pairwise**                         | ❌ no output  | **missing**   | New script `22-tukey.py`, `statsmodels.stats.multicomp.pairwise_tukeyhsd`. |
| L4 — Vargha-Delaney + Wilcoxon                            | ✅ 2 outputs  | done | `analysis-output/04-effect-size/` |
| L5 — Holm / BH FWER correction                            | ✅ embedded   | done | inside L3b and L4 outputs. |
| L6 — Inter-rater κ (Cohen)                                | ⚠️ proxy only | **partial**   | Canonical κ requires the human reviewer (see Section E). |
| **L7 — Cross-LLM consistency (LMM interaction)**          | ❌ blocked on L3 | **missing**   | Derive from L3 LMM `configuration:LLM` interaction term once L3 is done. |
| **McNemar (RQ4 grounded vs free-form, paired binary)**    | ❌ no code, no output | **missing**   | New script `23-mcnemar.py`, `statsmodels.stats.contingency_tables.mcnemar` per (scenario × LLM × {C4,C5}). |
| **CD diagrams (Critical-Difference, Friedman post-hoc)**  | ❌ no output  | **missing**   | New script `24-cd-diagrams.py` (Demsar 2006 procedure, matplotlib). |
| **Pareto analysis** (cost-of-evidence vs accuracy gain)   | ❌ no output  | **missing**   | New script `25-pareto.py`. Plot bundle-size or collection-time on x, top-1 gain on y, identify the Pareto front of (Cn, LLM) cells. |
| **Qualitative analysis** (case studies of representative failures) | ❌ no output | **missing**   | Manual pass over a curated 10-row sample (1 per scenario), narrative description. Document in `analysis-output/26-qualitative/`. |

---

## Section C — Metric inventory (11 metrics from metrics.md)

| # | Metric                          | RQ  | Status | Reason |
|---|---------------------------------|-----|--------|--------|
| M1 | Evidence Preservation Rate     | RQ1 | ✅ done | derived from `evidence_recall` + `expected_evidence` |
| M2 | Snapshot Survival Rate         | RQ1 | ✅ done | `evidence_survived` column |
| M3 | Top-1 RCA Accuracy             | RQ2 | ✅ done | `top1_correct` |
| M4 | Top-3 RCA Accuracy             | RQ2 | ✅ done | `top3_correct` |
| **M5** | **MTTD**                  | RQ3 | ❌ missing | column 0/1500. Fixed via A1+A2. |
| **M6** | **Evidence Precision**    | RQ2 | ❌ missing | column 0/1500. Fixed via A3. |
| M7 | Evidence Recall                | RQ2 | ✅ done | `evidence_recall` |
| M8 | Hallucination Rate             | RQ4 | ⚠️ partial | rate filled, but the κ that gates inclusion in the primary table is proxy-only |
| **M9** | **Recommendation Usefulness** | RQ2 | ❌ missing | column 0/1500. Fixed via A4. |
| **M10** | **Collection Overhead**  | RQ5 | ⚠️ 1/5 components | storage ✅; time, CPU, memory, API-calls all missing. Fixed via A5+A6 + operator instrumentation. |
| **M11** | **Reconcile Idempotency** | RQ1 | ❌ missing | run reconcile twice for the same Preview, diff `evidenceItems` set; expect identical set. New script `27-idempotency-check.py` + cluster subset re-run. |

---

## Section D — LOCKED-PLAN.md deliverables (8 items)

`LOCKED-PLAN.md` promises three Phase deliverables totalling ~8 items. Status as of now:

| Deliverable                                                       | Status |
|-------------------------------------------------------------------|--------|
| Phase 1 — Full L1-L5 analysis on LLM-A data                       | ⚠️ partial (L3 + L4-Tukey missing) |
| Phase 1 — Single-LLM ablation results                             | ⚠️ partial (no L3 interaction term) |
| Phase 1 — MTTD verdict (recovered or honestly TODO)               | ❌ MTTD is honestly TODO right now; A1 fixes |
| Phase 2 — Full dataset ~3300 rows × {C1-C5 + B0 + B2a + B2b} × {LLM-A, LLM-B} | ⚠️ partial (S1 has it; multi-app baselines B0/B2a/B2b NOT yet) |
| Phase 2 — Cross-LLM RQ4 derivable                                 | ❌ blocked on L3 LMM |
| Phase 2 — Two practitioner-tool baselines (K8sGPT, Kagent)        | ⚠️ S1 only; multi-app pending (Task #3) |
| Phase 2 — F10 hallucination judged                                | ✅ done for S1; multi-app has no F10 (not in scope) |
| Phase 3 — `EVALUATION-DRAFT.md` (Evaluation section)              | ⚠️ S1 written; multi-app section pending |
| Phase 3 — Cohen κ table once human reviewer completes #28         | ⚠️ proxy only; human review pending (Section E) |

---

## Section E — Human-only blockers (cannot be substituted)

| Item | Owner | Time | Status |
|------|-------|------|--------|
| Cohen κ #28 — review 300 rows blind in `analysis-output/16-cohen-kappa/claude-annotation-audit.csv` and produce the canonical `correct_human` column | **PROJECT LEAD** | ~2 h | ❌ pending |
| Bibliography verification — resolve 12 `TODO_VERIFY` entries in `bibliography.bib` against primary sources | lead or assistant | ~45 min | ❌ pending |

---

## Section F — Stale documents (last touched 2026-05-22, pre-experiment)

These five docs must be updated before submission to match what actually
landed in the run:

| Doc | Stale because… | Fix |
|-----|----------------|-----|
| `metrics.md`                  | no concrete formula / column for M6, M9, M11 | add the formulas + the column that holds the value |
| `threats-to-validity.md`      | does not mention F7-race-condition (closed), CollectionDuration NULL, κ AI-assisted, CPU/memory not measured, paired ON/OFF not done | append a section per real threat |
| `experiments.md`              | pre-experiment, ignores the 16 defects discovered live | append a "defect chain" appendix |
| `research-questions.md`       | claims RQ3 + RQ5 fully covered; reality is partial | add an explicit "limitations" sub-section |
| `methodology.md`              | describes RQ5 paired ON/OFF; not yet executed | either run the paired subset (preferred) or rewrite the claim |

---

## Section G — Operator instrumentation work (RQ5 components M10 + RQ1 M11)

The operator currently does not surface the four sub-components of M10
(time, CPU, memory, API calls) nor M11 (idempotency check). Fix path:

1. **API** (`api/v1alpha1/failurereport_types.go`):
   - Drop `omitempty` on `CollectionDurationMillis`, OR add a new
     `CollectionDurationMicros int64` field (no omitempty) for sub-ms resolution.
   - Add `BundleAllocBytes int64` (alloc delta during bundle assembly).
   - Add `BundleAllocCount int32` (mallocs count).
   - Add `PersistDurationMicros int64` (the Create + Status().Update wall-clock).
2. **Code** (`internal/evidence/evidence.go` + `internal/evidence/report.go`):
   - Record `runtime.ReadMemStats` before and after `AssembleBundleForLevel`.
   - Capture the persist time inside `EnsureFailureReport` (between the
     Create/Update and the Status.Update calls).
3. **Build + deploy**: `az acr build -r testagentdevops -t preview-operator:fp-rq5instr` + `kubectl -n preview-operator-system rollout restart deploy/preview-operator`.
4. **Re-run subset**: 50–100 reps with the instrumented image so the
   above fields land in the FailureReport.
5. **Paired ON/OFF**: re-run the same subset with
   `EVIDENCE_COLLECTION=disabled` so the operator skips the bundle and the
   reconcile-duration difference can be measured. The operator's overall
   reconcile duration is exported as a Prometheus metric already; if not,
   capture wall-clock from controller logs.

---

## Section H — Multi-app baselines (S2-S5) still missing

| Subject | B0 (vanilla LLM raw kubectl) | B2a (K8sGPT) | B2b (Kagent k8s-agent) | LLM-B replay |
|---------|------------------------------|--------------|------------------------|--------------|
| s1-flask-catalog | ✅ `results-b0.csv`   | ✅ `results-b2.csv` | ✅ `results-b2.csv` | ✅ `results-llmb.csv` |
| s2-listmonk      | ❌ pending                  | ❌ pending          | ❌ pending          | ⚠️ in fp-diagnose batch now |
| s3-healthchecks  | ❌ pending                  | ❌ pending          | ❌ pending          | ⚠️ in fp-diagnose batch now |
| s4-umami         | ❌ pending                  | ❌ pending          | ❌ pending          | ⚠️ in fp-diagnose batch now |
| s5-petclinic     | ❌ pending                  | ❌ pending          | ❌ pending          | ⚠️ in fp-diagnose batch now |

Scripts ready: `09b-baseline-b0-multiapp.py` (B0); `14-cluster-baselines.py`
(B2a + B2b for S1 — needs a multi-app port).

---

## Section I — Live work units (parallelism-safe)

These can run concurrently; each item has a unique resource lane.

| ID  | Title                                              | Lane                | ETA   | Status |
|-----|----------------------------------------------------|---------------------|-------|--------|
| W1  | fp-diagnose multi-app × LLM-A × LLM-B × C1–C5      | Azure OpenAI + Foundry quota | ~15 min from 17:50 | 🔄 running (PID 264623) |
| W2  | B0 baseline multi-app (200 calls LLM-A)            | Azure OpenAI (post-W1) | 10 min | ⏳ queued |
| W3  | B2a + B2b cluster baselines on multi-app           | AKS namespaces, separate | 1-2 h | ⏳ queued |
| W4  | MTTD external compute (A1 + A2)                    | local Python only   | 30 min | ⏳ ready to start in parallel to W1 |
| W5  | Evidence precision compute (A3)                    | local Python only   | 30 min | ⏳ ready to start in parallel |
| W6  | Recommendation usefulness scoring (A4)             | local + small LLM   | 1 h    | ⏳ queued |
| W7  | Operator instrumentation patch + rebuild + redeploy + subset rerun (A5, A6, M10, M11) | code + AKS subset | 1.5–2 h | ⏳ queued |
| W8  | L3 LMM script (statsmodels MixedLM)                | local Python        | 1 h    | ⏳ ready to start in parallel |
| W9  | L4 Tukey emmeans                                   | local Python        | 30 min | ⏳ ready (depends on W8 for full picture) |
| W10 | McNemar grounded vs freeform                       | local Python        | 20 min | ⏳ ready |
| W11 | CD diagrams (Friedman post-hoc visual)             | local Python + matplotlib | 30 min | ⏳ ready |
| W12 | Pareto front (cost-vs-accuracy)                    | local Python + matplotlib | 30 min | ⏳ ready |
| W13 | Qualitative analysis (10 case studies)             | manual + narrative  | 1 h    | ⏳ ready |
| W14 | 5-doc update (metrics, threats-to-validity, experiments, research-questions, methodology) | docs | 1–2 h | ⏳ after results land |
| W15 | Cohen κ — human reviewer                           | **HUMAN**           | 2 h    | ❌ blocked on lead |
| W16 | Bibliography 12 TODO_VERIFY                        | human or assistant  | 45 min | ⏳ queued |
| W17 | `EVALUATION-DRAFT.md` multi-app section + final polish | docs            | 1 h    | ⏳ after results land |
| W18 | Phase 5b/6 freeze (tag + digest)                   | git                 | 15 min | ⏳ very last step |

---

## File-by-file compliance checklist

| File                                                       | Q1-ready ? | What needs fixing |
|------------------------------------------------------------|------------|-------------------|
| `experiments/failure-provenance/results-matrix/results-augmented.csv` | ⚠️ 14/20 cols filled | A1-A6 |
| `experiments/failure-provenance/results-matrix/results-b0.csv`        | ✅ S1 done  | multi-app extension (Section H) |
| `experiments/failure-provenance/results-matrix/results-b2.csv`        | ✅ S1 done  | multi-app extension (Section H) |
| `experiments/failure-provenance/results-matrix/results-llmb.csv`      | ✅ S1 done  | multi-app extension (Section H) |
| `experiments/failure-provenance/results-matrix/results-f10-judge.csv` | ✅ done     | — |
| `experiments/failure-provenance/results-multiapp/<subject>/<F>/<r>/report.json` (200 files) | ✅ 200/200  | — |
| `experiments/failure-provenance/results-multiapp/<...>/diag-*.json`   | 🔄 producing | W1 in progress |
| `analysis-output/01-descriptive/`                          | ✅ done    | — |
| `analysis-output/02-bootstrap/`                            | ✅ done    | — |
| `analysis-output/03-friedman/`                             | ✅ done    | — |
| `analysis-output/04-effect-size/`                          | ✅ done    | — |
| `analysis-output/07-aggregate/`                            | ✅ done    | — |
| `analysis-output/08-vocab-rescore/`                        | ✅ done    | — |
| `analysis-output/10-b0-report/`                            | ✅ S1 done | extend for multi-app (Section H) |
| `analysis-output/11-rule-rescore/`                         | ✅ done    | — |
| `analysis-output/14-cluster-baselines/`                    | ✅ S1 done | extend for multi-app (Section H) |
| `analysis-output/16-cohen-kappa/`                          | ⚠️ proxy only | W15 |
| `analysis-output/17-multiapp-rescore/`                     | ⏳ pending | depends on W1+W2+W3 finishing |
| `analysis-output/18-mttd-external/`                        | ✅ done    | W4 closed (operator-side latency sub-second, e2e caveat documented) |
| `analysis-output/19-evidence-precision/`                   | ✅ done    | W5 closed (300 reports, summary.md) |
| **`analysis-output/20-recommendation-usefulness/`**        | ❌ missing | W6 (LLM-blocked on W1) |
| `analysis-output/21-lmm/`                                  | ✅ done    | W8 closed (LLM-A + LLM-B unified, 2000 rows) |
| `analysis-output/22-tukey/`                                | ✅ done    | W9 closed (Tukey HSD per LLM) |
| `analysis-output/23-mcnemar/`                              | ✅ done    | W10 closed (100 tests on S1) |
| `analysis-output/24-cd-diagrams/`                          | ✅ done    | W11 closed (cd_llm_a.png + cd_llm_b.png + Demsar CD) |
| `analysis-output/25-pareto/`                               | ✅ done    | W12 closed (Pareto frontier + 2 cells on front) |
| `analysis-output/26-qualitative/`                          | ✅ done    | W13 closed (30 case-study cards) |
| **`analysis-output/27-idempotency-check/`**                | 🔄 in progress | W7 — operator patched, ACR build in flight |
| `docs/research/failure-provenance/metrics.md`              | ⚠️ stale | W14 |
| `docs/research/failure-provenance/threats-to-validity.md`  | ⚠️ stale | W14 |
| `docs/research/failure-provenance/experiments.md`          | ⚠️ stale | W14 |
| `docs/research/failure-provenance/research-questions.md`   | ⚠️ stale | W14 |
| `docs/research/failure-provenance/methodology.md`          | ⚠️ stale | W14 |
| `docs/research/failure-provenance/EVALUATION-DRAFT.md`     | ⚠️ S1 only | W17 |
| `docs/research/failure-provenance/PROGRESS.md`             | 🔄 live log | append per tick |
| `docs/research/failure-provenance/bibliography.bib`        | ⚠️ 12 TODO_VERIFY | W16 |

---

## Section J — Order of operations (now → submission)

1. NOW: launch W4, W5, W8, W10, W11 in parallel of W1 (different lanes).
   - W4, W5: local Python, no LLM, no cluster
   - W8, W10, W11: local Python with `statsmodels` + `scipy` + `matplotlib`
2. W1 finishes (~18:05 UTC) → launch W2.
3. W2 finishes (~18:15 UTC) → launch W3 (cluster).
4. W7 starts in parallel with W3 (operator patch is code-only; build + deploy then waits for an AKS slot).
5. W9, W12 once W8 outputs land.
6. W13 manual, can be done while W3 runs.
7. W14 once all measured results land.
8. W15 (human κ) and W16 (bibliography) sequenced with the project lead.
9. W17 once W14 is signed off.
10. W18 last.

---

## Section K — Update log of this file

- 2026-05-23 17:50 UTC — first draft. Captures all known gaps as of the end of
  the multi-app matrix collection phase.
- 2026-05-23 18:15 UTC — first flip pass: W4, W5, W8, W9, W10, W11, W12, W13
  closed. Operator instrumentation (W7) patch landed in code (evidence.go +
  collectors.go + report.go + failurereport_types.go) and CRD regenerated;
  rebuild + redeploy + subset rerun pending. EVALUATION-DRAFT.md §10
  multi-app section drafted.
- 2026-05-23 21:53 UTC — **MATRIX 500/500 = 100 % MILESTONE**:
  - F4 multi-app via DB-table-rename: 40/40 ✅
  - F5 multi-app via wrapper.py FP_F5_BROKEN_FRONTEND proxy: 40/40 ✅
  - F8 multi-app via pg_sleep BEFORE trigger: 40/40 ✅
  - F9 multi-app via post-seed NULL UPDATE: 40/40 ✅
  - F10 multi-app via wrapper.py FP_F10_FLAKY proxy injection (v2): 40/40 ✅
  - 13 stale F10 captures (env-only mechanism) invalidated and re-collected
  - 27 work units listed in §I; W2-W14 all closed, W3 B2a/B2b ✅ done
    (40/40 cells via 14b + 14c rescore), W15/W16 human-only remain.
  - RQ5 cpu/memory/microseconds: 173 instrumented captures already on
    disk (Phase A2 + F5 phases ran under the `:fp-rq5instr` operator).
    Sample: collectionDurationMicros ≈ 849-1075 µs (sub-millisecond
    sustained), collectionAllocBytes ≈ 16 KB, AllocCount ≈ 126. RQ5 'API
    calls' = 0 by design (collectors are pure functions over Preview).
  - Pending machine work tonight: fp-diagnose Phase 3 (400 calls for the
    new F10 captures × 2 LLMs × 5 configs), B2 s5/F10 redo (4 cells lost
    in F10-dir cleanup), final commit + push + cron stop.
  - Human-only items remaining: W15 (κ canonical, ~2 h), W16 (bibliography
    12 TODO_VERIFY, ~45 min).

- 2026-05-25 08:00--12:30 UTC — **Phase 5c S1--S5 unified treatment**:
  - W17 (new) — `report.json → failurereport.yaml` synthesis on 340
    S2--S5 captures (`/tmp/convert-reports.py`); inventory now 449/449
    captures with CRD YAML on disk. ✅
  - W18 (new) — K8sGPT post-teardown replay extended to S1
    (`analysis/k8sgpt-replay.py --subset s1+s2+s3+s4+s5 --max-reps 20
    --force`). Sequential, ~10 s/cell, ~85 min wall-clock for 509
    capture-cells. 🔄
  - W19 (new) — Kagent post-teardown replay extended to S1
    (`analysis/kagent-replay.py --subset s1+s2+s3+s4+s5 --max-reps 20
    --force`). Agentic chain, ~25--30 s/cell, ~3 h 30 wall-clock. 🔄
  - W20 (new) — Unified S1--S5 rescoring (`analysis/35-unified-rescore.py`)
    over the diag-output dataset; pooled engine numbers across all five
    subjects ($n=11\,600$): rule 15.32 %, llm-A grounded 18.72 %,
    llm-A freeform 18.00 %, llm-B grounded 5.32 %, llm-B freeform 3.82 %.
    ✅
  - W21 (new) — Unified S1--S5 figures (`analysis/36-unified-figures.py`):
    `engine-pooled-bars-s1s5.png` + `engine-by-subject-heatmap-s1s5.png`.
    ✅
  - W22 (new) — `34-b2-post-teardown-rescore.py` extended to include
    `s1-flask-catalog` in SUBJECT_IDX; emits
    `results-matrix/results-b2-post-teardown.csv` over the five-subject
    pool. Final values pending the `--force` replay completion. 🔄
  - W23 (new) — Article wiring: abstract + §sec:eval-baselines
    (B2a-PT / B2b-PT rows) + §sec:multi-app (`tab:multi-engine-pooled` +
    `fig:engine-pooled-s1s5` + `fig:engine-heatmap-s1s5`) updated for
    five-subject treatment. `overleaf.zip` repacked. Commits
    `8d6f1e9` → `61c136e` → `f4e1d33` → `15f0035`. Final B2a-PT/B2b-PT
    numbers refresh after the replays complete.
  - Q1 zero-fault budget: kept. No skipped captures on the `--force`
    replay; every output is overwritten uniformly so the five subjects
    receive identical processing.
