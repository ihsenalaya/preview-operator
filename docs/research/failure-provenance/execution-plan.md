# Execution Plan — Phase 5 Run Order

**Audience.** Any Claude instance (or human) picking up the Phase-5 experimental
campaign. Self-contained: read this first, no prior conversation needed.

**Companion docs (read in this order if you need context):**
1. `methodology.md` — design (within-subject, C1–C5, ≥10 reps, ground truth).
2. `metrics.md` — what is measured (11 metrics).
3. `llm-selection.md` — why two LLMs (LLM-A `gpt-4o-mini`, LLM-B `Llama-3.3-70B`).
4. `analysis-plan.md` — how results will be analysed (LMM primary,
   Friedman robustness, A12, Holm, κ, cross-LLM interaction).
5. `experimentations.md` — what has already been run (raw lab log).

This file decides **the order and concurrency** of the remaining runs and the
**live-vs-offline-replay split** between LLM-A and LLM-B.

---

## 1. State at the time of writing

- Cluster `idp-preview-test` (AKS, 3× D4s_v3, k8s 1.34.7) running this fork's
  operator `testagentdevops.azurecr.io/preview-operator:fp`.
- `FailureReport` CRD installed and wired into the operator failure path.
- LLM-A = `gpt-4o-mini-2024-07-18` via Azure OpenAI is the **only** LLM
  currently wired into `internal/diagnosis/` (`openai.go`). Retry/backoff on
  429s is in place.
- LLM-B = `Llama-3.3-70B-Instruct-Turbo` via Together.ai is **specified** in
  `llm-selection.md` but **not yet integrated** in `internal/diagnosis/`.
- Lot 6 matrix has captured F1–F7 (with various per-scenario caveats in
  `experimentations.md`); F8/F9/F10 and the multi-app subjects S2–S5 are still
  pending.
- An **offline scorer** exists: `experiments/failure-provenance/multiapp/bin/fp-diagnose`
  (commit `8234276 feat(experiments): offline scorer for the multi-app matrix`).
  Captured `FailureReport` bundles can be re-fed to any diagnostic backend
  without re-injecting the fault.

---

## 2. The two concurrency tiers — never mix them in one CSV row

`methodology.md §6` already requires this; this file restates it operationally.

| Tier | What runs concurrently | Which metrics use these rows | CSV tag |
|---|---|---|---|
| **A — bulk** | 5–8 previews in parallel; both LLMs called in parallel per preview (once LLM-B is wired) | RQ1 preservation, RQ2 Top-1/3, RQ4 hallucination, evidence precision/recall | `concurrency_label=parallel-fast` |
| **B — isolated** | 1 preview at a time, 1 LLM call at a time, cluster otherwise idle | RQ3 MTTD, RQ5 overhead (operator CPU/RAM/storage/API-calls) | `concurrency_label=isolated` |

**Hard rule.** No table or statistical test in the article mixes
`parallel-fast` and `isolated` rows in the same cell. If a graph reports MTTD,
it is `isolated` only. If a graph reports Top-1 accuracy, it should *be* the
`parallel-fast` rows (where the sample is largest), with the `isolated`
subset reported in a sensitivity column.

**Practical envelope on AKS (3× D4s_v3, 12 vCPU total).**
- Tier A: up to **5–8 concurrent previews** before apiserver / operator
  reconciliation queue starts to dominate latency. Watch `kubectl top nodes`.
- Tier A LLM bursts: LLM-A endpoint is rate-limited (~30 K TPM, defect #2);
  the existing exponential backoff handles bursts but inflates wall-clock —
  fine in Tier A, fatal in Tier B.
- Tier B: enforce **one preview at a time, one LLM call at a time**, no
  parallel scenarios, no concurrent multi-app subjects.

---

## 3. LLM execution order — sequential, with offline replay for LLM-B

Do **not** run two live LLM passes back-to-back. Instead:

### Phase 5a — LLM-A live (pass 1)

This is the current run. Finish what is in flight:

- **Complete:** F8, F9, F10 on the primary application `idp-preview`.
- **Complete:** S2–S5 multi-app subjects (per `multi-app-plan.md`) — LLM-A
  only on S2–S5, per the existing scope.
- **Re-confirm if needed:** F5, F6 (currently 4/10 and 2/10 valid runs;
  decide whether to re-run for ≥10 valid).
- **Mode:** Tier A for the bulk; a separate **150-run subsample** in Tier B
  (3 reps × 10 scenarios × 5 configs) **before moving to Phase 5b**, so
  RQ3/RQ5 has clean LLM-A numbers.

### Phase 5b — wire LLM-B (Llama via Together.ai)

One-time integration work, ~½ day. **Do not start until Phase 5a is fully
captured and freeze-tagged (§4).**

- Add a `together.go` (or similar) backend in `internal/diagnosis/` following
  the same interface as `openai.go`. Pin
  `model=meta-llama/Llama-3.3-70B-Instruct-Turbo`, `temperature=0`.
- Wire a CLI flag / env var so the harness picks LLM-A or LLM-B per run.
- Smoke-test against **one** stored bundle (any F1 bundle from Phase 5a) and
  confirm a structured diagnosis is returned.
- Verify the Together.ai rate limit for the chosen plan with a 1-request
  probe; record headers.
- Commit `llm-selection.md` (currently untracked) alongside the integration.

### Phase 5c — LLM-B offline replay (cheap, no cluster)

The Phase-5a `FailureReport` bundles are LLM-independent (the operator
captures evidence *before* the diagnostic step). So most of LLM-B's work is
**offline re-scoring**, not new cluster runs.

- Iterate `fp-diagnose` over every `FailureReport` produced in Phase 5a;
  invoke LLM-B for each; write a new CSV row with `llm_model_version=LLM-B`
  and `concurrency_label=offline-replay` (a third tag — see §5).
- This is sufficient for **RQ1, RQ2, RQ4** on LLM-B. No fault injection, no
  preview, no apiserver load.
- Runtime: dominated by the LLM endpoint throughput, not cluster scheduling.

### Phase 5d — LLM-B live (small subset, RQ3 only)

The only metric that needs LLM-B *live on a cluster* is **MTTD (RQ3)** —
because MTTD is wall-clock from failure detection to diagnosis availability,
and an offline replay short-circuits that timeline.

- Sample: 3 reps × 10 scenarios × 5 configs = **150 live runs**.
- Mode: Tier B, strict serial.
- All other metrics inherit from Phase 5c.

### Why this order works

- LLM-A keeps its full sample for the metrics it dominates.
- LLM-B gets a comparable sample for RQ1/RQ2/RQ4 *without* re-burning ~70 %
  of the cluster time.
- The `configuration:LLM` interaction term required by `analysis-plan.md`
  §3 (cross-LLM cross-validation) is fully derivable.

---

## 4. Freeze policy — between Phase 5a and Phase 5c/d

Anything that changes between LLM-A and LLM-B contaminates the cross-LLM
interpretation. Before starting Phase 5b:

- **Tag the commit** that produced the Phase-5a bundles. Suggested tag:
  `frozen-for-llm-b-YYYYMMDD`. Record the tag's commit SHA in the results
  CSV header.
- **Pin the operator image** (`preview-operator:fp` digest, not tag). Note:
  `:fp` is a mutable tag — record the digest from
  `docker manifest inspect testagentdevops.azurecr.io/preview-operator:fp`
  or from `kubectl get pod -n preview-operator-system -o json | jq ...`.
- **Do not edit**:
  - `scenarios.yaml`
  - `internal/evidence/*`
  - `api/v1alpha1/failurereport_types.go`
  - The scoring rubric (`experiments/failure-provenance/scoring-rubric.md`)
  - The collector implementations in `internal/evidence/collectors.go`
- **Do edit** (these are LLM-B-only changes, not shared with LLM-A bundles):
  - `internal/diagnosis/together.go` (new file)
  - Diagnostic-step prompt wiring (only if both LLMs receive identical
    prompts — record any prompt difference per row)
  - Harness flag in `run-kind-experiments.sh` to select LLM-B

If any "do not edit" file is touched, **discard the LLM-B numbers and
restart from a fresh Phase-5a snapshot.** This is not negotiable for the
cross-LLM claim.

---

## 5. CSV schema — required columns to make this auditable

Every row must carry, in addition to the existing
`(scenario_id, run_id, cluster_type, configuration)` key:

| Column | Values | Why |
|---|---|---|
| `concurrency_label` | `parallel-fast`, `isolated`, `offline-replay` | Separates Tier A / Tier B / Phase-5c rows |
| `llm_model_version` | e.g. `gpt-4o-mini-2024-07-18`, `meta-llama/Llama-3.3-70B-Instruct-Turbo` | Pin model snapshot |
| `llm_provider` | e.g. `api.openai.com`, `api.together.xyz` | Provider audit |
| `llm_temperature` | `0` | Determinism control |
| `operator_image_digest` | sha256 | Freeze-policy audit |
| `frozen_commit_sha` | git SHA of `frozen-for-llm-b-…` tag | Cross-LLM provenance |
| `called_at_utc` | ISO-8601 | Time-drift detection |

These columns are already specified piecewise in `llm-selection.md` §6 and
`analysis-plan.md` §4 — this file consolidates them.

---

## 6. After all runs land — analysis pickup

When Phase 5d finishes, the analyst (Claude or human) proceeds with the
flow in `analysis-plan.md`:

1. L1 (descriptive) and L2 (Wilson / bootstrap CIs) on every cell — fast,
   no contention.
2. L3 (mixed-effects model) with
   `outcome ~ configuration * LLM + (1|scenario) + (1|run_id)`.
3. L3b Friedman robustness check.
4. L4 pairwise contrasts via `emmeans` + Wilcoxon + A12.
5. L5 Holm / Benjamini-Hochberg correction.
6. L6 Cohen's κ on the two-annotator hallucination subsample.
7. L7 read off the `configuration:LLM` interaction.
8. §8 external-baseline comparison (B0, B1, B2 — only if those runs were
   collected; otherwise document the gap honestly).
9. §9 cost-benefit synthesis (Pareto frontier).
10. §10 qualitative error analysis (open-coding on incorrect C5 rows).

---

## 7. Decisions still open (escalate to the human author before deciding)

These are listed in `analysis-plan.md` §11 and reproduced here for the
operational hand-off:

1. **External baselines** (B0 vanilla LLM, B1 rule-only, B2 K8sGPT,
   optional B3 devops). Without these, the article cannot claim "better
   than existing approaches" for a top-tier venue. Decision needed before
   Phase 5d: do we add B0/B2 to the live runs?
2. **F10 LLM judge.** Which model arbitrates "the LLM hallucinated on a
   flaky test"? Default proposal: a different model from both LLM-A and
   LLM-B (e.g. Claude Sonnet) on 100 % of F10 rows.
3. **Two-annotator coverage.** 20 % shuffled subsample for all metrics, or
   100 % only for hallucination?
4. **Cost-budget envelope for LLM-B.** At ≈ $0.88 / 1M tokens and ~10
   diagnoses per run, what is the absolute LLM-token budget across the full
   Phase-5c replay + Phase-5d live runs?
5. **Bootstrap iterations** for median CIs (default 10 000).

---

## 8. Non-negotiable research integrity rule

Every number in the article's Evaluation section must come from a real run
recorded in the CSV. **No invented measurements, no extrapolation, no
"approximate" numbers.** A single fabricated row kills the paper. When
unmeasured, the cell is left empty / marked TODO and the limitation is
reported honestly in `threats-to-validity.md`.

---

## Phase 5a closure + Phase 5b completion log

**Last refresh:** 2026-05-23 22:45 UTC.

This execution-plan document is a static contract; the Phase-5 campaign
has since closed Phase 5a (S1 idp-preview, 100/100 captures, frozen
2026-05-23 ~11:32 UTC at commit `2e81a648`) AND a Phase 5b
(multi-application generalisation on s2-listmonk, s3-healthchecks,
s4-umami, s5-petclinic).

For the live state of the Phase-5 work-unit closure plan, see
**`Q1-COMPLIANCE.md`** (Sections I + J + per-file checklist). For the
chronology of multi-app defects + fixes, see **`experimentations.md §10`**.
For per-tick status, see **`PROGRESS.md §9` (Live monitoring log)**, which
records every 10-minute cron tick since 2026-05-23 16:08 UTC.

### LLM substitution chain (replaces this doc's original "LLM-B = Llama-3.3-70B")

The Together.ai Llama-3.3-70B endpoint was unreachable during the
campaign (regional capacity outage). The cross-LLM RQ4 reading therefore
uses:

| Slot         | Pre-registered | Actually run                  |
|--------------|----------------|-------------------------------|
| LLM-A        | gpt-4o-mini-2024-07-18 | Same                           |
| LLM-B        | Llama-3.3-70B (Together.ai)   | **cohere-command-a (Azure AI Foundry)** |
| Judge        | Claude Sonnet 4.5      | **Mistral-Large-3 (Azure AI Foundry)** |

Three disjoint provider families (OpenAI / Cohere / Mistral) preserved.
Documented in `llm-selection.md §3.1`.
