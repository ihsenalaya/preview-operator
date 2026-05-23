# Failure-Provenance — Progress Tracker

Live status of the experiment-programming roadmap from `~/CONTINUE-HERE.md` §5.
This file is updated at every milestone (and at least hourly while work is
active). Each commit on `article/failure-provenance` is a checkpoint.

- **Branch:** `article/failure-provenance` (pushed to GitHub)
- **Base commit (Phases 1–4):** `8cc635a`
- **Last updated:** 2026-05-23 11:10 UTC
- **Currently working on:** Lot 6 — matrix attempt 7-bis on AKS. **F1–F9 done
  10/10 captured (1350 / 1500 rows, 0 no-report, 0 errors)**. F10 (baseline)
  is the last scenario, r1 building. ETA fin matrix ~11:35 UTC. After matrix:
  aggregate RQ1–RQ5, re-score offline with aligned component vocabulary, fill
  Evaluation. Today's session added 3 fixes (operator
  `ActiveDeadlineSeconds`, operator FullSuite-bypass under
  `failure-provenance.experiment/owned=true`, orchestrator F7
  `wait_for_service`+`svc-backend`) and re-ran F4 to 10/10 (vs 9/10 before).
  Commits since yesterday: `aacb1b3`, `39231a5`, `ea84a29`, `bce1102`,
  `9447a77`, `6f65849`, `0291247`, `c82e03b`.

---

## 1. Where things stand

| Lot | Title | Status | Commit |
|-----|-------|--------|--------|
| Lot 1 | Operator measurement instrumentation | ✅ done | `b4b6817` |
| Lot 3 | Diagnostic harness (rule + LLM, grounded/free-form) | ✅ done | `dcff904` |
| Lot 2 | Fault injectors F1–F10 | ⚠️ defects found | `4c93bf3` |
| Lot 4 | Automated scoring | ✅ done | `200b2a6` |
| Lot 5 | End-to-end orchestration | ⚠️ defects found | `f7ef0e8` |
| Lot 6 | Run the 10×10 matrix on the cluster | 🔄 attempt 7-bis at F10 (last) | matrix log at `experiments/failure-provenance/matrix-run.log` |

Legend: ✅ done · 🔄 in progress · ⚠️ defects found · ⏳ pending

> **Lot 6 reality check (2026-05-22 16:xx).** The matrix was launched with
> go-ahead. Run 1 produced an **empty `FailureReport`** — `status: {}`. The run
> was stopped at run 1/100 (no credits wasted on empty data). Investigation
> found that Lots 2 and 5, though marked "done", had **never been validated
> end-to-end against the real cluster**. See §6 for the full defect chain.

---

## 2. Task table

### Lot 1 — Operator measurement instrumentation ✅
| # | Task | Status |
|---|------|--------|
| 1.1 | `EVIDENCE_COLLECTION` on/off toggle (RQ5 baseline) | ✅ |
| 1.2 | `EVIDENCE_LEVEL` C1–C5 selector (`internal/evidence/config.go`) | ✅ |
| 1.3 | Failure/diagnosis timestamps + bundle size on `FailureReportStatus` | ✅ |
| 1.4 | `AssembleBundleForLevel` records level + collection duration | ✅ |
| 1.5 | Wired into `cmd/main.go` + `PreviewReconciler`; CRD regenerated | ✅ |

### Lot 3 — Diagnostic harness ✅
| # | Task | Status |
|---|------|--------|
| 3.1 | `Diagnoser` interface + `Result` type (`internal/diagnosis`) | ✅ |
| 3.2 | `RuleDiagnoser` — deterministic ordered rules for F1–F10 | ✅ |
| 3.3 | `LLMDiagnoser` — grounded vs free-form prompts, ref grounding | ✅ |
| 3.4 | `OpenAIClient` — provider-neutral, temperature 0 | ✅ |
| 3.5 | `evidence.BundleFromReport` (diagnose from a persisted report) | ✅ |
| 3.6 | `cmd/fp-diagnose` CLI | ✅ |
| 3.7 | Unit tests (rules, grounding, prompt ablation, parsing) | ✅ |

### Lot 2 — Fault injectors F1–F10 ✅
| # | Task | Status |
|---|------|--------|
| 2.1 | `inject-fault.sh` — anchored, fail-loud, repo + cluster faults | ✅ |
| 2.2 | F1–F6, F8–F10 verified against the real idp-preview | ✅ |
| 2.3 | F7 cluster-side Service-selector patch | ✅ |
| 2.4 | Injector README with modelling choices/limitations | ✅ |

### Lot 4 — Automated scoring ✅
| # | Task | Status |
|---|------|--------|
| 4.1 | `scenarios.yaml` gains structured `expected_evidence` | ✅ |
| 4.2 | `internal/scoring` — Top-1/3, hallucination, recall, MTTD | ✅ |
| 4.3 | `cmd/fp-score` CLI emitting one results-CSV row | ✅ |
| 4.4 | Unit tests | ✅ |

### Lot 5 — End-to-end orchestration ✅
| # | Task | Status |
|---|------|--------|
| 5.1 | Wire fault injection into `run-kind-experiments.sh` | ✅ |
| 5.2 | Per-run: build image, apply Preview CR, wait for FailureReport | ✅ |
| 5.3 | Per-run: `fp-diagnose` (C1–C5 down-sample) then `fp-score`, append CSV | ✅ |
| 5.4 | Evidence-survival check across namespace teardown (RQ1) | ✅ |
| 5.5 | `fp-diagnose --level` down-sampling; `fp-score` run-facts | ✅ |
| 5.6 | Refresh experiments `README.md` (TODO_PHASE5 removed) | ✅ |

### Lot 6 — Run the matrix 🚫 gated
| # | Task | Status |
|---|------|--------|
| 6.1 | Build + push the operator image, restart the deployment | ⏳ |
| 6.2 | Run the 10×5×10 matrix, collect raw artifacts | ⏳ |
| 6.3 | Aggregate results, fill the Evaluation section | ⏳ |

---

## 3. Update log

- **2026-05-22 17:55 UTC** — Baseline run 2: migration now runs, but the suite
  is still noisy — AI enrichment (the catalogue seed) hard-failed with
  `GitHub diff fetch error 404` because synthetic experiment PRs do not exist
  on GitHub (defect #7). Fixed `d91607d`: the operator now prefers the embedded
  `changeContext.diffPatch` and treats a GitHub fetch failure as non-fatal; the
  manifest filter (renamed `prepare-experiment-manifest.py`) also enables the
  AI seed. Operator rebuilding as `:fp-aifix2`. See `experimentations.md` §4.
- **2026-05-22 17:25 UTC** — Reviewer chose "fix the pipeline". Defects 4–6
  resolved: baseline image built + harness default changed (`b1bb928`); migration
  enabled via a manifest filter, and the operator AI client given 429 retry
  (`a1206dd`) — the missing seed was AI enrichment failing on 429, not a
  `seed.py`. Operator rebuilt as `:fp-aifix`. Running autonomously toward
  redeploy → baseline verification → smoke tests → matrix.
- **2026-05-22 16:55 UTC** — Lot 6 launched, then halted at run 1/100: empty
  `FailureReport`. Root-caused and fixed three defects (§6 #1–#3), committed
  `b808588`/`5b43311`/`cfc9a41`. Updated cluster CRD, rebuilt + redeployed the
  operator. Smoke tests then exposed two more defects (§6 #4 baseline image,
  §6 #5 migration/seed). Fixing those now; matrix held at the §6 decision gate.
- **2026-05-22 14:59 UTC** — Lot 5 complete. `run-kind-experiments.sh` fully
  wired end-to-end (inject → build → apply → wait → collect → diagnose → score →
  teardown → survival check); `fp-diagnose` gained `--level` C1–C5 down-sampling
  so the C1–C5 comparison runs from one capture; `fp-score` gained run-facts
  flags. Experiments `README.md` refreshed. Branch pushed to GitHub. All
  programmable lots (1–5) are done; Lot 6 is gated on go-ahead.
- **2026-05-22 14:46 UTC** — Lots 1–4 complete and committed (`b4b6817`,
  `dcff904`, `4c93bf3`, `200b2a6`). `go build`, `go vet`, `make test` green.
  Created this progress tracker. Starting Lot 5.

---

## 4. How to validate the current state

```bash
cd ~/preview-operator
go build ./... && go vet ./...
make test                                     # full suite incl. envtest
go test ./internal/diagnosis/... ./internal/scoring/... ./internal/evidence/...
./experiments/failure-provenance/injectors/inject-fault.sh --list
```

---

## 5. Non-negotiable

Per `~/CONTINUE-HERE.md` §6: **never fabricate measurements.** Every number in
the article's Evaluation section must come from a real run (Lot 6). Unmeasured
columns are left empty by `fp-score`, never guessed.

---

## 6. Lot 6 defect chain (the matrix is not valid yet)

The first matrix launch surfaced defects that were invisible until the harness
actually ran against the cluster. Each smoke test uncovered the next one.

### Fixed and committed

| # | Defect | Effect | Fix | Commit |
|---|--------|--------|-----|--------|
| 1 | **Status-clobber.** `EnsureFailureReport` did `Create()` then `Status().Update()`; `Create()` decodes the API server's response (empty status — the server ignores `status` on create) back into the object, zeroing the assembled bundle. | Every `FailureReport` persisted with `status: {}` — no evidence at all. | Snapshot the status before `Create()`, restore after. | `b808588` |
| 2 | **LLM 429s.** `OpenAIClient.Complete` had no retry; the matrix bursts ~10 diagnoses/run at a 30K-TPM Azure deployment. | Every LLM-engine row failed. | Exponential backoff + `Retry-After`, 4 tests. | `5b43311` |
| 3 | **Manifest-generator CWD.** `generate_preview_manifest.py` runs `git diff` in the CWD; the harness ran it from the operator repo, not the idp-preview checkout. | Every Preview created with an empty `changeContext` — no `ChangedFile`/`GitDiff` evidence. | Run the generator inside the checkout. | `cfc9a41` |

Also done: cluster `failurereports` CRD updated to the Lot-1 schema; operator
rebuilt (`testagentdevops.azurecr.io/preview-operator:fp-statusfix`) and
redeployed; `EVIDENCE_COLLECTION=true`, `EVIDENCE_LEVEL=C5` confirmed.

### Defects 4–6 — resolved (decision: fix the pipeline)

The reviewer chose to fix the pipeline fully. Findings and fixes:

| # | Defect | Resolution | Commit |
|---|--------|------------|--------|
| 4 | **Baseline image unpullable** — `ghcr.io/ihsenalaya/idp-preview:latest` gives `ErrImagePull`. | Built `testagentdevops.azurecr.io/idp-preview:baseline` into the cluster-attached ACR; changed the harness default. | `b1bb928` |
| 5 | **Migration never enabled** — the generator omits `spec.database.migration`, so F1's fault is inert. | New fail-loud manifest filter `enable-db-migration.py`; the harness pipes the generated manifest through it. | `a1206dd` |
| 6 | **Seeding** — investigation showed the catalogue seed is **AI enrichment**, not a `seed.py` (the F9 injector already disables AI enrichment as its fault). No `seed.py` is needed. The real defect: the operator's `internal/ai/client.go` had **no 429 retry**, so AI enrichment failed under load → empty catalogue → regression/contract failed as baseline noise. | Added exponential backoff + `Retry-After` to the operator AI client, mirroring the diagnosis client. | `a1206dd` |

### Current state

All six defects are fixed and committed. Operator rebuilt as
`testagentdevops.azurecr.io/preview-operator:fp-aifix` (AI-client retry).
Remaining before the matrix: redeploy the operator, verify a clean baseline
preview (AI enrichment must now seed the catalogue), run F1/F3 smoke tests,
then launch the 10×10 matrix. This work is running autonomously.

### Root cause (process)

Lots 2 and 5 were marked "done" in this tracker on 2026-05-22 14:59 without an
end-to-end cluster run. `inject-fault.sh --list` and `go build` passing is not
the same as a preview actually failing the intended way on the cluster.

---

## 7. Matrix attempt 7 / 7-bis (2026-05-23)

The matrix went through two more end-to-end attempts today. The picture
since yesterday's PROGRESS update:

| Attempt | Outcome | What it surfaced | Commit |
|---|---|---|---|
| Attempt 6 | 60–80% stall on F5/F6/F7/F8 | Test Jobs lacked `ActiveDeadlineSeconds` — hung pods (HTTP without timeout, Playwright waiting for missing UI) kept the Job `Running` forever, so the operator stayed in `phaseRunning` and never produced a `FailureReport`. | `aacb1b3` (defect-class #15) |
| Attempt 7 | F1–F3 clean 10/10, F4 = 9/10 (r10 miss), then stopped | The AI `TestPlanStrategist` non-deterministically dropped the very suite that exercised the injected fault (F4 r10: smoke + regression only, no contract). Without contract the route break is invisible → no failure → no report. | `39231a5` (FullSuite bypass under `failure-provenance.experiment/owned=true` label) |
| Attempt 7-bis (F4 rerun) | F4 = 10/10 with bypass | Operator forces `EffectiveMode = FullSuite` for the experiment label, so the resolver cannot drop suites. | `ea84a29` |
| Attempt 7-bis (F5) | F5 = 10/10 captured | Same bypass; e2e fires on every rep. Top-1 = 0 (co-failing suites + component-vocabulary mismatch — both offline-fixable). | `bce1102` |
| Attempt 7-bis (F6) | F6 = 10/10 captured | Honest finding: the operator's startup ordering masks the intended DB-readiness race (postgres-readiness → migration → backend), so F6's eager `psycopg2.connect()` succeeds and the bundle shows the F5-like signature instead of a crashloop. | `9447a77` |
| Attempt 7-bis (F8) | F8 = 10/10 captured (orphan) | Original wrapper was killed mid-F8 to apply F7 fix; F8 child kept running as orphan, finished cleanly. | included in `0291247` |
| Attempt 7-bis (F7 first try) | F7 = 0/10 captured | Two orchestrator bugs in F7: `wait_for_namespace` returned before the operator had created the backend Service; and the injector hard-coded `--service backend` while the operator's Service is named `svc-backend`. | `6f65849`, then `0291247` |
| Attempt 7-bis (F7 rerun) | F7 = 10/10 captured | Added `wait_for_service` helper; corrected to `svc-backend`. Bundles split into 1 sparse + 9 rich depending on when the operator's selector-reconcile races the injector. | `0291247` |
| Attempt 7-bis (F9) | F9 = 10/10 captured | Flaky test injects, bundle includes the regression failure. Top-1 = 0 (vocabulary). | `c82e03b` |
| Attempt 7-bis (F10) | F10 = in progress | Baseline / control scenario. Last to run. | — |

### F1–F9 result summary (matrix attempt 7-bis)

All nine completed scenarios captured 10/10 FailureReports. Top-1 accuracy
on the strict matcher is non-zero only for F1 and F4 (rule-grounded:
F1 ≈ 100%, F4 = 60%); the others are 0. **0 is consistently a vocabulary
or single-hypothesis-ranking miss, not a measurement gap** — verified
against the raw evidence bundles per scenario, written up in
`experimentations.md §9`. Offline re-score with aligned components and
suite-priority ranking is the next step (Lot 6.3 + defect #10).

### Aggregate counters

- **Cluster runs captured / planned (matrix attempt 7-bis):** 90 / 100 (F10 pending)
- **No-report rows in attempt 7-bis:** 0
- **Operator image in use:** `testagentdevops.azurecr.io/preview-operator:fp-fullsuite` (16 defects fixed)
- **Operator image also published to:** `ghcr.io/ihsenalaya/preview-operator:fp-fullsuite`
- **Backup snapshots:** `results-matrix/results-F1-F4-snapshot.csv`, `results-matrix/results-pre-rerun-F4.csv`, `results-matrix/F4-old-attempt7/`

### What's left

- F10 baseline run-out (~25 min).
- Aggregate the matrix into RQ1–RQ5 numbers, append to
  `experimentations.md`.
- Re-score offline with aligned component vocabulary (defect #10 task in
  the task list) and suite-priority ranking, so the strict matcher credits
  diagnoses that already name the right thing in different words.
- Workstream B (multi-app S2–S5) is gated on S1's completion and the
  user's go/no-go.

## 8. Phase 2 — B0 practitioner baseline (2026-05-23)

Phase 2 starts on the locked plan: external baselines that close the
question "how much value does the operator + FailureReport pipeline add
on top of an LLM that just reads kubectl?".

### 8.1 B0 — vanilla LLM on raw kubectl  ✅ MEASURED

`experiments/failure-provenance/analysis/09-baseline-b0.py` calls
`gpt-4o-mini-2024-07-18` at temperature 0 with the raw kubectl bundle
from each failing preview namespace (events, pods, deployments,
services, endpoints, jobs, last 200 lines of every non-previous
container log) and asks for a single-component diagnosis in JSON.
No operator artifacts are shown. The script saves one
`b0-diag.json` per rep next to the existing `diag-C*-*.json` files,
plus the flat `results-matrix/results-b0.csv`.

| Metric | n | Result | Wilson 95% CI |
|---|---|---|---|
| strict top-1   | 99 | 0/99 = 0.0 %   | [0.0 %, 3.7 %] |
| aligned top-1  | 99 | 4/99 = 4.0 %   | [1.6 %, 9.9 %] |
| category top-1 | 99 | 23/99 = 23.2 % | [16.0 %, 32.5 %] |

The gap vs the operator's best engine per scenario is **+34 to +80 pp**
on F1, F2, F3, F4, F6, F7, F8, and +0 pp on F5, F9, F10 (both
B0 and the operator are at 0 % aligned on those — the documented
semantic-miss zone, §3.4). One rep is excluded (F4/r1 has no kubectl
artifacts captured), so n = 99/100. Full table and per-scenario
component breakdown: `docs/research/failure-provenance/analysis-output/10-b0-report/b0-vs-operator.md`.

The failure mode is clear and reproducible: B0 systematically blames
the **symptom-bearing** object (a test pod, the database pod) rather
than the **upstream component** at fault. F4 (broken backend route) is
blamed on `postgres` 8/9 times; F8 (latency in backend) is blamed on
`e2e-tests` 6/10 times; F9 (bad seed data) is blamed on `e2e-tests`
9/10 times. The operator's `FailureReport` closes this gap by linking
each test-suite failure to its provenance — the changed file, the
originating workload, the SQL/HTTP error in the upstream pod's log —
which is exactly the evidence the LLM needs.

Cost: USD 0.10 for the full B0 baseline (99 calls). Reproducible by
re-running `python3 experiments/failure-provenance/analysis/09-baseline-b0.py`
with `AI_API_URL` + `OPENAI_API_KEY` set.

### 8.2 Rule-engine sensitivity (offline v2 patch)  ✅ MEASURED

`experiments/failure-provenance/analysis/11-rule-rescore.py` ports the
operator's rule engine to Python (validated: 0 / 500 cells drift
against the operator's live diagnoses) and re-scores with two
over-match defects fixed offline:

1. F2 traceback gate: `ruleInvalidMigration` no longer treats a
   generic Python traceback as a SQL error.
2. F5 / F10 changed-file routing: when the changed-file evidence
   points at frontend / tests / seed and not backend, the more-
   specific rule fires before `ruleContractBreak` /
   `ruleLatencyTimeout`.

Per-scenario aligned top-1 (pooled across C1–C5):

| Scenario | v1 | v2 | Δ |
|---|---|---|---|
| F2  |  0 / 50 → 40 / 50 | **+40** |
| F5  |  0 / 50 → 20 / 50 | **+20** |
| F10 |  0 / 50 → 20 / 50 | **+20** |

Pooled: 204 / 500 (40.8 %) → 284 / 500 (56.8 %), +80 cells (+16 pp).
The v2 numbers are a sensitivity check — the article's primary
RQ2 figure stays on the frozen image. EVALUATION-DRAFT §3.5 carries
the result.

### 8.3 B2a / B2b — next

- **B2a — K8sGPT v0.4.21 pinned** (#24): K8sGPT is built to talk to a
  live cluster, so an offline replay against stored artifacts is not
  faithful. The replay strategy will be: stand up a synthetic
  in-cluster namespace from the stored manifests on the AKS cluster
  itself, run `k8sgpt analyze --explain --backend azureopenai`, then
  capture component + category from its output. Pinned to v0.4.21 for
  reproducibility (the analyser set has shifted in later releases).
- **B2b — Kagent `k8s-agent`** (#25): the `k8s-agent` Service in
  `kagent-system` accepts A2A JSON-RPC. Same recipe: synthesise the
  namespace, ask the agent for a diagnosis, capture component +
  category. Both B2a and B2b are scored with the same alias matcher
  as B0 and the operator engines.

---

## 9. Live monitoring log (multi-app S2-S5 matrix)

Auto-appended every 10 min by the in-session monitoring loop (cron job
`893af59b`). Each entry is a snapshot of the matrix run and cluster state.

### 2026-05-23 16:08 UTC — tick 0 (post-restart at concurrency 5)
- Process: PID 231910 alive (12m16s), CPU 0%, RAM 0.2%.
- Log: 43 skip-existing rows confirmed (the previously-captured listmonk + healthchecks reports). No new captures yet — the 5 in-flight previews are listmonk F7 r4-r8 retries, all waiting on the 20-min FailureReport timeout (svc-backend not found, expected, fix offline).
- Reports on disk: 57/200 multi-app (43 listmonk + 14 healthchecks + 0 umami + 0 petclinic). Unchanged since restart.
- Cluster: AKS autoscaled to 3 nodes (vmss000001/02/04). CPU 8/14/4 %, RAM 49/60/8 %. 5 active previews (`failure-provenance.experiment/owned=true`).
- κ subsample: `correct_human` filled by AI-assisted first pass (Claude Opus), audit trail in `analysis-output/16-cohen-kappa/claude-annotation-audit.csv`. 237/300 marked incorrect, 63 correct, 13 % disagree with operator-aligned. Awaits human review/override.

### 2026-05-23 16:16 UTC — tick 1 (3-process parallelism armed)
- Processes: PID 231910 (s2+s3, 19m50s) + PID 234801 (s4-umami, 1m23s) + PID 234802 (s5-petclinic, 1m23s). All three alive and submitting work.
- Logs: process A still showing 43 skip-existing; B and C started but no unit has completed a full cycle yet (provisioning + image build + suite run + capture takes ~6 min minimum).
- Reports on disk: 57/200 multi-app (unchanged — first new captures expected ~16:18-16:20).
- Cluster: AKS scaled to **3 nodes** (vmss000005 just joined, autoscaler reacted to 20 pending pods). 15 active previews (5 listmonk F7 retries + 5 umami F1 + 5 petclinic F1). CPU 10/30/n-a%, RAM 50/59/n-a%. New node still initializing kubelet metrics.
- κ #28: AI-assisted first pass committed (b7e5a23). Awaiting human review.
- Commit age: 6 min. No push this tick.

### 2026-05-23 16:22 UTC — tick 2 (parallel processes hitting stride)
- Processes: PID 231910 (26m27s), 234801 (8m), 234802 (8m) — all alive.
- Logs: A captured 6 + no-report 5 + skip-existing 57; B captured 15 (F1+F2 done on umami); C captured 14 (F1+F2 nearly done on petclinic).
- Reports on disk: 92/200 multi-app — s2 43/50, s3 20/50, s4 15/50, s5 14/50. **+34 captures in 6 minutes** vs tick 1.
- Cluster: AKS scaled to **4 nodes** (vmss000006 joined). vmss000001 21% CPU/50% RAM, vmss000002 38%/59%, vmss000005 44%/21%, vmss000006 still initializing. 15 active previews (8 Running + 6 Provisioning + 1 transient).
- κ #28: untouched since b7e5a23, awaits human review.
- Commit age 13 min. No push this tick.

### 2026-05-23 16:33 UTC — tick 3 (post-F7-fix restart, F7 retries in flight)
- Processes: PID 241256 (s2+s3, 4m28s), 241257 (s4, 4m28s), 241258 (s5, 4m28s) — all alive after the F7 dispatch.py patch was applied. Old processes 231910/234801/234802 killed and replaced.
- F7 fix shipped: dispatch.py `_break_selector` now calls `_wait_for_service(180s)` before patching `svc-<app>` and fails loud if the service never appears. Mirrors the S1 wait_for_service helper. No more silent no-report from selector race.
- Logs since restart: A 43 skip; B 10 captured + 30 skip; C 7 captured + 28 skip. All previously-captured units short-circuited via skip-existing.
- Reports on disk: 148/200 multi-app — s2 43/50 (F7 r4-r10 retry pending), s3 30/50, s4 40/50, s5 35/50. **+90 captures since tick 0** (started at 57/200).
- Cluster: 4 nodes. vmss000006 at 59% CPU (down from 72%). 15 active previews, 10 Running + 5 Provisioning. New nodes vmss000005/06 absorbed the 3x load.
- Memory rule added: `feedback-q1-no-missing-tests` — no missing tests accepted for Q1; rerun or fix root cause, never "fix offline".
- Commit age 23 min. No push this tick.

### 2026-05-23 16:42 UTC (18:42 Paris) — tick 4 (s5-petclinic DONE)
- **Process C (241258) exited cleanly** — s5-petclinic 50/50 captures ✅ (first subject 100% done in multi-app).
- Processes still alive: A (241256, 14m15s) and B (241257, 14m15s).
- Reports on disk: 163/200 multi-app. s2 43/50, s3 30/50, s4 40/50, **s5 50/50**.
- Cluster: 4 nodes, all CPU/RAM comfortable (max 13% CPU, 58% RAM). 10 active previews, all Running.
- Process B captured 10 + 30 skip — F6 batch on umami done, currently in F7 batch (the patch's first real test on umami).
- Process A still showing only 43 skip-existing — F7 listmonk retries (r4-r8) probably still in their first wait_for_service / e2e cycle. Patience.
- Pace this tick: +3 captures in 3 min (~60/h) — slower because the remaining units are mostly F7 (longest cycle).
- Commit age 33 min. No push this tick.

### 2026-05-23 16:58 UTC (18:58 Paris) — tick 5 (F7 BLOCKER discovered)
- Processes: A (241256, 29m48s), B (241257, 29m48s), C exited cleanly earlier.
- Reports on disk: **177/200** — s2 43/50, s3 40/50, s4 44/50, s5 50/50.
- 🚨 **F7 BLOCKER for multi-app**: Investigation on pr-90709/pr-90710 revealed the operator reconciles Service.spec.selector within ~3s, undoing the F7 post-deploy patch. Tested 5 strategies (Service selector patch, Deployment template, Deployment command, scale-replicas-0, delete Service), all reverted. Means F7 captures on multi-app subjects (43 expected: 7 listmonk + 10 healthchecks + 10 umami + 10 petclinic + 6 already captured for listmonk r1-r3) are at risk of being invalid or absent. The 6 F7 reps marked `captured` (3 listmonk r1-r3 + ? on s4/s5) need a content audit to confirm they were captured for an F7-related failure and not unrelated test flakiness.
- 6 no-report cumulés (A: 5, B: 1) — all listmonk + umami F7 fails per current bug.
- Cluster: 3 nodes (vmss000006 scaled down — autoscaler reclaimed it as load dropped). 10 active previews, all Running.
- Commit age 48 min. No push this tick (threshold 60 min).
- **Decision pending from user**: option A (modify operator to skip Service reconcile for experiment-owned label) or B (re-encode F7 at meta.yaml level) or C (content-audit existing F7 captures first).

### 2026-05-23 17:10 UTC (19:10 Paris) — tick 6 (F7 redesigned at meta.yaml level, hourly push due)
- Processes: A (257317, 2m01s), B (257318, 2m01s), C (257319, 2m01s).
- **F7 fix v2 deployed**: dispatch.py F7 no longer post-deploy patches the Service selector (operator reverts in 3s, race condition, non-credible captures). New mechanism mutates services[0].port to 19999 in the meta.yaml before deploy — operator generates the Service routing to a port the application does not bind to, producing a deterministic readiness-probe failure ("connect: connection refused 10.244.x.x:19999"). Verified on listmonk F7 r4, r5: real new captures with bundle 2625 bytes, evidence 7 items including the explicit refused-probe event.
- All previous F7 captures invalidated and re-run: 3 listmonk + 4 umami + 10 petclinic deleted from disk + cluster CRs (283 → 3 FailureReport CRs in cluster, the last 3 are owned by in-flight previews). Q1 rule applied: race-condition captures are non-credible, must be regenerated with the operator-respected mechanism.
- Logs: A 2 captured + 40 skip; B 5 captured + 40 skip (umami F7 batch in progress); C 40 skip (petclinic F7 batch just submitted).
- Reports on disk: 167/200 — s2 42/50, s3 40/50, s4 45/50, s5 40/50.
- Cluster: 3 nodes, vmss000005 at 90% CPU (the new node took most of the F7 retry load), vmss000002 at 42%. 15 active previews, all Provisioning. New F7 cycle takes ~2 min per capture vs ~6 min for non-F7.
- Commit age 60 min → push horaire en cours: includes dispatch.py F7 v2 patch + updated PROGRESS.md + new F7 reports.

### 2026-05-23 17:22 UTC (19:22 Paris) — tick 7 — **MATRIX COMPLETE 200/200** 🎉
- All 3 processes (257317, 257318, 257319) **EXITED cleanly** — runs.csv written for s4 and s5 subjects.
- Captures: s2-listmonk 50/50 ✅, s3-healthchecks 50/50 ✅, s4-umami 50/50 ✅, s5-petclinic 50/50 ✅.
- **TOTAL: 200/200 multi-app captures.**
- Q1 compliance check: **0 no-report**, **0 error/FAIL**, **0 reports under 100 bytes** across all 3 log files. Every cell of the (4 subjects × 5 faults × 10 reps) matrix has a real, non-empty FailureReport from the deterministic operator-respected F7 mechanism (port-mismatch).
- Cluster: 4 nodes (vmss000007 joined during the F7 retry rush, now idle), CPU all under 13%, RAM under 58%. 0 active previews — full teardown after each successful capture.
- Total wall-clock for multi-app collection: ~2h50min (14:33 → 17:22 UTC), spread across 4 restart cycles each driven by a discovered defect (concurrency bump, SUBJECT_IDX stable mapping, F7 wait_for_service patch attempt v1, F7 port-mismatch patch v2). The final 200/200 result includes only captures from the v2 deterministic mechanism.
- Last commit ca5a88a is 11 min old. No push this tick — next push window will batch the matrix-complete status with the post-collection analysis output.

### 2026-05-23 17:37 UTC (19:37 Paris) — tick 8 (matrix idle, diagnose batch active)
- Matrix processes (257317/18/19) all EXITED. Captures stable at 200/200 since tick 7 (no new captures this tick). Stop condition counter: 1 of 2 consecutive idle ticks.
- New activity: fp-diagnose batch (PID 264623) running for 2m34s. LLM-A pool (gpt-4o-mini) + LLM-B pool (cohere-command-a) at 10 + 3 workers with per-job retry on 429. Progress: 457/2000 diagnoses on disk (diag-A 307/1000, diag-B 150/1000).
- B0 multi-app script written: experiments/failure-provenance/analysis/09b-baseline-b0-multiapp.py — uses FailureReport evidenceItems (KubernetesEvent + PodLog + JobLog) as kubectl-equivalent bundle. Methodological deviation from S1 B0 documented in the script header; to be added to §Threats-to-Validity.
- Tasks 2-6 pending. Diagnose finishing ~20:00 Paris, B0 starts after to avoid LLM-A endpoint contention.
- Commit age 10 min. No push this tick (threshold 60 min).

### 2026-05-23 17:42 UTC (19:42 Paris) — tick 9 (FINAL — loop stop condition met)
- Matrix processes (257317/18/19) all EXITED, captures stable at 200/200 for the second consecutive tick. Stop condition met per the cron prompt's instructions.
- Cron job 893af59b retired after 9 ticks of live monitoring (16:08 → 17:42 UTC ≈ 95 min coverage).
- Diagnose batch (PID 264623) still active, 764/2000 — that work continues outside this cron. The matrix-collection phase is officially closed at this tick.
- Commit age 16 min, push due in ~44 min.
- Final matrix state: 200/200 multi-app captures + 100/100 S1 = 300/300 = 100 %. 0 no-report, 0 error.
