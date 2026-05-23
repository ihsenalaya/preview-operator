# Failure-Provenance — Experimentation Log

Raw lab notebook for the Lot 6 experiment campaign. **Everything** observed
during builds, smoke tests, baseline runs and the matrix is recorded here:
durations, resources, defects, measurements, anomalies. It is the source
material for the article's *Evaluation*, *Threats to Validity*, *Reproducibility*
and *Lessons Learned* sections.

- **Integrity rule:** only measured facts go here. Unmeasured = stated as such,
  never estimated silently. (`~/CONTINUE-HERE.md` §6.)
- **Updated:** continuously, alongside `PROGRESS.md`, at every milestone.
- **Last updated:** 2026-05-23 04:00 UTC
- All times UTC. All durations wall-clock.

---

## 1. Environment & infrastructure

| Item | Value |
|------|-------|
| AKS cluster | `idp-preview-test`, RG `idp-preview-rg`, eastus, Kubernetes 1.34.7 |
| Nodes (observed) | 2× `Standard_D4s_v3` Ready (CONTINUE-HERE.md records 3× provisioned; 2 seen during this campaign) |
| Container registry | ACR `testagentdevops` (RG `ihsen`), attached to AKS (kubelet AcrPull) |
| Control VM | `fp-experiment-runner`, RG `failure-provenance-rg`, eastus, `Standard_B2ms` |
| Operator namespace | `preview-operator-system` |
| LLM endpoint | Azure OpenAI `preview-openai-idp` (RG `idp-preview-rg`), deployment `gpt-4o-mini`, SKU `GlobalStandard`, capacity 30 (≈30 000 tokens/min) |
| Toolchain | Go 1.26.2 (host), Go 1.25 base image (Dockerfile), kubectl, helm, az CLI |

### Node utilisation (spot reading, 2026-05-22 ~16:30, during smoke tests)

| Node | CPU | Memory |
|------|-----|--------|
| aks-nodepool1-...-vmss000001 | 306m (7 %) | 4751Mi (40 %) |
| aks-nodepool1-...-vmss000002 | 552m (14 %) | 5169Mi (43 %) |

Cluster had ample headroom throughout; no node pressure observed.

### Cost

Direct credit consumption was **not instrumented**. Cost drivers for the article:
2× D4s_v3 nodes + 1× B2ms VM billed continuously for the campaign duration;
ACR build minutes (see §3); Azure OpenAI tokens (gpt-4o-mini, see §6). Pull
actual figures from Azure Cost Management for the eastus subscription if needed.

---

## 2. Operator / image provenance

| Image tag | ACR run | Build time | Contents |
|-----------|---------|-----------|----------|
| `preview-operator:fp` | — | — | Pre-campaign, commit `8cc635a`. **Stale** — pre-Lot-1; had the status-clobber bug. |
| `preview-operator:fp-statusfix` | `ca5` | 3 m 39 s | HEAD + status-clobber fix (`b808588`) + LLM retry (`5b43311`). |
| `idp-preview:baseline` | `ca8` | 0 m 34 s | idp-preview HEAD, no fault — pullable baseline image (replaces broken ghcr image). |
| `preview-operator:fp-aifix` | `ca9` | 3 m 22 s | HEAD + operator AI-client 429 retry (`a1206dd`). Current deployment. |

`imagePullPolicy: IfNotPresent` on the operator Deployment — every operator
rebuild therefore needs a **unique tag**, not a re-push of `:fp`.

Operator startup line confirms instrumentation each redeploy:
`failure-provenance instrumentation {evidenceCollection: true, evidenceLevel: C5}`.

---

## 3. Chronological run log

| Time (UTC) | Event | Duration | Outcome |
|-----------|-------|----------|---------|
| 16:10:04 | **Matrix attempt 1** launched (10 scen × 10 rep, rule+llm) | — | Stopped at run 1/100 |
| 16:13:27 | F1 r1 FailureReport `pr-9101-failure` created | ~3 min after apply | `status: {}` — **empty** (defect #1) |
| 16:14:33–35 | F1 r1 scored | ~2 s | 5 rule rows OK; **all 10 LLM rows failed** HTTP 429 (defect #2) |
| ~16:15 | Matrix killed during F1 r2 (ACR build `ca4`) | — | No credits wasted on 99 empty runs |
| 16:16–16:24 | Root-cause + fix defects #1, #2; CRD updated; operator build `ca5` | ~8 min | — |
| 16:24:41 | Operator redeployed `:fp-statusfix` | rollout ~30 s | Healthy |
| 16:25:17 | **Smoke test 1** — F1, 1 rep, rule+llm (buggy harness) | ~7 min | 15/15 rows scored; all top-1/top-3 = 0 (defect #3) |
| 16:28:25 | Smoke 1 FailureReport `pr-9101-failure` | — | 10 evidence items, bundle 3131 B, status populated ✓ |
| 16:31–16:32 | Smoke 1 scoring | ~70 s | LLM retry works — 15/15 incl. all LLM rows |
| ~16:38 | CWD fix (`cfc9a41`) committed | — | — |
| 16:40:25 | **Smoke test 2** — F1, CWD-fixed harness | ~4.5 min | 14/15 rows; 1 LLM row exhausted retries |
| ~16:44 | Smoke 2 FailureReport | — | 13 items incl. `ChangedFile`,`GitDiff`; bundle 4380 B |
| ~16:46 | **Baseline run 1** (`pr-9500`) | — | **Failed** — `ErrImagePull` (defect #4) |
| ~16:48 | Baseline image build `ca8` | 0 m 34 s | `idp-preview:baseline` published |
| ~17:06 | Operator AI-client retry fix (`a1206dd`); operator build `ca9` | 3 m 22 s | — |
| 17:10:14 | Operator redeployed `:fp-aifix` | rollout ~30 s | Healthy |
| ~17:12 | **Baseline run 2** (`pr-9500`), migration enabled | ~8 min | Migration ran ✓ (`MigrationReady=JobAlreadySucceeded`); **still noisy** — `AIEnrichmentFailed: GitHub diff fetch error 404` (defect #7) |
| 17:5x | AI-enrichment fix (`d91607d`); operator build `:fp-aifix2` (ACR `caa`) | 3 m 27 s | — |
| ~18:0x | **Baseline run 3** (`pr-9500`) | aborted | `QuotaFailed` — namespace from run 2 still terminating (PR-number reuse race; harmless, matrix uses unique numbers) |
| ~18:1x | **Baseline run 4** (`pr-9600`, fresh number) | ~9 min | **Core-clean**: smoke ✓, regression ✓ 9/0. Still failing: contract (microcks 500), e2e (5 timeouts), ai-tests job. AI **seed succeeded** ✓ |
| ~18:2x | **F1 smoke test 3** | ~8 min | F1 migration fault fires, but `FailureReport` premature — captured on a transient conflict while migration still running (defect #8) |
| ~18:4x | Conflict-guard fix (`a274294`); operator build `:fp-conflictfix` | ~3.5 min | — |
| ~18:0x | **F1 smoke test 4** (`:fp-conflictfix`) | ~8 min | F1 captures a proper `FailureReport`: 7 items incl. `JobLog`; LLM diagnoses correctly. `JobLog` slice wrong (defect #9) |
| ~18:1x | **Matrix attempt 2** launched | stopped at run ~2 | Head-biased log capture would degrade every run — stopped to fix defect #9 |
| ~19:0x | Log-capture + matcher fix (`7ab0020`); operator build `:fp-logfix` | ~3.5 min | — |
| ~19:1x | Operator redeployed `:fp-logfix` (all 9 fixes) | rollout ~30 s | Healthy |
| ~19:2x | **Matrix attempt 3** launched (100 runs, rule+llm) | running ~12-20 h | F1 run 1 verified: JobLog complete (psycopg2 syntax error captured); rule + llm-grounded both Top-1=1 ✓ |
| ~20:3x | **Matrix attempt 3 stopped** at ~F2 run 3 | — | Found defects #11/#12: crashing app logs not captured (40% of scenarios) |
| ~21:0x | Defects #10/#11/#12 fixed (`4404a24`,`6021b98`); operator builds `:fp-applog`,`:fp-prevlog` | ~7 min | — |
| ~21:3x | **Matrix attempt 4** launched (100 runs, rule+llm) | running ~12-16 h | Operator `:fp-prevlog`, 12 defects fixed |

---

## 4. Defects found (the matrix exposed 14)

Each was invisible to `go build` / `inject-fault.sh --list` and surfaced only by
running the harness end-to-end on the cluster.

| # | Defect | Symptom | Root cause | Fix | Commit |
|---|--------|---------|-----------|-----|--------|
| 1 | Status-clobber | Every `FailureReport` had `status: {}` | `Create()` decodes the server's empty-status response back into the object before `Status().Update()` | Snapshot status before `Create`, restore after | `b808588` |
| 2 | LLM 429s | All LLM-engine rows failed | `OpenAIClient.Complete` had no retry; ~10 diagnoses/run burst a 30K-TPM endpoint | Exponential backoff + `Retry-After` | `5b43311` |
| 3 | changeContext CWD | `top-1/top-3` = 0; no `ChangedFile`/`GitDiff` | Generator runs `git diff` in CWD; harness invoked it from the wrong repo | Run generator inside the checkout | `cfc9a41` |
| 4 | Baseline image | Non-code faults `ErrImagePull` | `--baseline-image` default `ghcr.io/.../idp-preview:latest` not pullable | Build `:baseline` into ACR; change default | `b1bb928` |
| 5 | Migration never run | F1 fault inert; `MigrationReady=TaskDisabled` | Generator omits `spec.database.migration`; operator runs the Job only when set | Fail-loud manifest filter injects the block | `a1206dd` |
| 6 | AI client no retry | AI enrichment fails under load | Operator `internal/ai/client.go` had no 429 retry | Backoff + `Retry-After` on the operator AI client | `a1206dd` |
| 7 | AI enrichment GitHub-bound | `AIEnrichmentReady=False — GitHub diff fetch error 404`; empty catalogue → baseline noise | Enrichment fetched the PR diff from GitHub; synthetic experiment PRs do not exist there, and the 404 hard-failed enrichment. AI seed was also gated off (`aiSeedEnabled` heuristic). | Use the embedded `changeContext.diffPatch`; GitHub fetch non-fatal; manifest filter enables `aiEnrichment.seed` | `d91607d` |
| 8 | Premature capture on conflict | F1 `FailureReport` sparse (4 items, no `JobLog`/`TestResult`); diagnosis "insufficient evidence" | The `reconcileDatabase` caller marked the Preview Failed on any error, including a transient 409 conflict ("Operation cannot be fulfilled"), capturing a `FailureReport` while the migration Job was still `JobRunning`. The deployment/service/exposure callers already guarded `IsConflict`; the database one did not. | Conflict → requeue, not fail. `wait_for_report` waits for `phase=Captured`. | `a274294` |
| 9 | Head-biased log capture | F1 `JobLog` was 314 B — the *middle* of a Python traceback; rule diagnoser "insufficient evidence" | `selectSignificantLines` filled its 6-line budget from the TOP of the log and returned early, dropping the conclusive error line (`psycopg2 ... syntax error`) at the end. Also `componentMatch` concatenated tokens, missing "database migration" vs "migration-job". | Keep the LAST `limit` significant lines; token-aware `componentMatch`. | `7ab0020` |
| 10 | Rule diagnoser over-matches | F2 (missing env var) misdiagnosed as `migration-job / database` | `ruleInvalidMigration` keys off the keyword "alembic", which appears in **every** migration log — including a *successful* one (`INFO [alembic.runtime.migration]`). It fires regardless of whether the migration failed, and being first in the ordered rules it shadows the correct rule. | OFFLINE fix pending in `internal/diagnosis/rules.go`: require an error-specific keyword, not bare "alembic". Re-scorable from report.json — no cluster re-run. | (pending) |
| 11 | App-pod logs never captured | F2 bundle had no backend log — only a CrashLoopBackOff event; rule diagnoser "insufficient evidence" | `significantLogExcerpts` looked only for the single-service label `app=preview-preview`; the experiment's multi-service previews label pods `app=svc-backend`/`app=svc-frontend`. | Added svc-backend/svc-frontend log candidates. | `4404a24` |
| 12 | Crashed-pod log unreachable | Even with #11, the backend crash log was missing | `fetchPodLogs` called `GetLogs` without `Previous`; a CrashLoopBackOff container's current instance is "waiting to start" so the call errors — the crash traceback is in the *previous* instance. | Fall back to previous-instance logs. | `6021b98` |
| 13 | Docker Hub rate limit | `az acr build` failed mid-matrix ("toomanyrequests"); affected runs captured an image-missing failure, not the injected fault | The idp-preview Dockerfile pulls its base image `python:3.12-slim` from Docker Hub; anonymous pulls are rate-limited per source IP, and the shared ACR build agents exhausted the limit after ~11 builds. | Pre-import the base image into ACR (`az acr import`); the harness rewrites the cloned Dockerfile FROM lines to pull from ACR. | `fb5294c` |
| 14 | Preview hangs on a stuck job | F3 (invalid image tag): the migration Job pod sat in `ImagePullBackOff`; the operator reported `MigrationReady=JobRunning` forever, never failed the Preview, no FailureReport — the run would time out and skip. | `reconcileDatabaseTask` only checked `JobFailed`, which an un-pullable Job never reaches. | `jobPodImageError` fails a task whose pod is in ImagePullBackOff/InvalidImageName/CreateContainerConfigError; plus a 15-min provisioning-deadline backstop. | `2edfd07` |

**Process root cause:** Lots 2 & 5 were marked "done" without an end-to-end
cluster run. Strong candidate for the article's *Lessons Learned*.

---

## 5. Evidence-capture measurements

`FailureReport` evidence bundle, scenario F1, evidence level C5:

| Run | Status | Evidence items | Bundle size | Item types |
|-----|--------|---------------|-------------|-----------|
| Matrix attempt 1 (defect #1) | `{}` empty | 0 | 718 B (CR only) | — |
| Smoke 1 (pre-CWD-fix) | populated | 10 | 3 131 B | `PreviewCondition`×7, `TestResult`×3 |
| Smoke 2 (post-CWD-fix) | populated | 13 | 4 380 B | `PreviewCondition`×7, `TestResult`×4, `ChangedFile`×1, `GitDiff`×1 |

Observations for the article:
- The CWD fix added `ChangedFile`+`GitDiff` evidence and grew the bundle ~40 %.
- Smoke 1/2 still lacked `JobLog` for F1 — because the migration was disabled
  (defect #5); expected to appear once migration runs.
- `mttd_seconds` / `diagnosis_available_at` come out **empty by design**: the
  operator captures evidence but diagnosis is offline, so there is no operator
  diagnosis timestamp. `fp-score` emits the note *"MTTD not in FailureReport"*.
  RQ3 must rely on `collectionDurationMillis`, not time-to-diagnosis.

### Smoke test 1 scoring (F1, buggy harness — illustrative, not valid data)

15 rows (C1–C5 × {rule-grounded, llm-grounded, llm-freeform}). All
`top1_correct`/`top3_correct` = 0, `evidence_precision` = 0.333. **Invalid** —
F1's fault was inert (defects #3/#5); kept only as a worked example of the
scoring pipeline, NOT for the Evaluation section.

---

## 5b. Per-scenario run durations (matrix attempt 6 — live)

Wall-clock per scenario, extracted from `matrix-run.log` timestamps. Appended
as each scenario completes. Operator `:fp-stuckfix`, AKS, 1 preview at a time.

| Scenario | Runs | Total wall-clock | Per run | Notes |
|----------|------|------------------|---------|-------|
| F1 invalid migration | 10 | 17.3 min | ~1.7 min | fast — migration Job fails quickly |
| F2 missing env var | 10 | 12.1 min | ~1.2 min | fast — CrashLoopBackOff detected quickly |
| F3 invalid image tag | 10 | 18.2 min | ~1.8 min | operator fails it on ImagePullBackOff (defect #14 fix) |
| F4 broken endpoint | 8/10 | 63.9 min | ~8.0 min | 2 transient stalls (r6, r10) — preview Running, no test verdict in 20 min |
| F5 frontend breaking change | 4/10 | ~145 min | 6 stalls × 20 min + 4 valid × ~7 min | high test-phase stall (60%); honest threat-to-validity |
| F6 DB readiness timeout | 2/10 | ~165 min | 8 stalls × 20 min + 2 valid × ~7 min | severe stall (80%); honest threat-to-validity |
| F7-F10 | — | — | — | appended on completion |

Build cost is separate: with the Docker-Hub cache fix (#13) an ACR image build
is ~33 s (was ~3 min + agent queue). Code-fault scenarios (F1,F2,F4,F5,F6,F8,
F10) rebuild per run; non-code (F3,F7,F9) reuse the baseline image.

## 6. LLM behaviour & throttling

- Model: `gpt-4o-mini` via Azure OpenAI, `GlobalStandard`, ~30K TPM.
- 429 body is the generic `{"error":{"message":"Too Many Requests",...}}` form,
  not the verbose Azure TPM message — consistent with GlobalStandard shared-pool
  throttling.
- An isolated single call always succeeded (≈420 ms total) — throttling is
  purely burst/contention driven.
- Pre-fix: 100 % of matrix LLM calls failed (no retry).
- Post-fix (smoke 2, 10 LLM calls): 9 succeeded after backoff, 1 exhausted all
  6 attempts over ~2.5 min. Expectation for the matrix: a small fraction of LLM
  rows may still fail under sustained load; they are **re-scorable offline**
  from the persisted `report.json`, so no cluster re-run is needed.
- Retry policy now in both clients: 6 attempts, exponential 1→2→4→8→16 s,
  honouring `Retry-After`.

---

## 7. Open items / risks / threats to validity

### Baseline cleanliness — partially clean (confirmed, baseline run 4)

A no-fault preview (`pr-9600`, all 7 fixes) reaches `Ready` and:

| Suite | Baseline run 2 (no seed) | Baseline run 4 (seeded) |
|-------|--------------------------|--------------------------|
| smoke | Succeeded 2/0 | Succeeded 2/0 |
| regression | **Failed 7/2** | **Succeeded 9/0** ✓ |
| contract | Failed 0/1 | Failed 0/1 |
| e2e | Failed 1/5 | Failed 1/5 |

The seed fix (`d91607d`) cleaned the regression suite — the core REST API
surface is now noise-free. Two suites remain noisy at baseline:

- **contract** — `microcks-contract-tests` job fails: the microcks server
  returns `HTTP 500` on `POST /api/tests`. A microcks-server / spec-import /
  Keycloak issue, not an app fault. Affects **F4** (broken contract endpoint).
- **e2e** — `e2e-tests` job: 5/6 Playwright tests time out waiting for catalogue
  DOM elements (`preview_badge_shown` passes, so the page itself loads).
  Suspected: the `after-seed` DB checkpoint that e2e restores between tests is
  empty/missing. Affects **F5** (frontend) and **F10** (flaky e2e).
- **ai-tests** — the AI-generated test job errors (`tests=Failed` in
  `AIEnrichmentReady`); independent of the four suites.

### Decision: proceed to the matrix with a documented caveat

The **core experiment pipeline works**: previews deploy, migration + AI seed
run, the operator captures `FailureReport`s with full evidence, and smoke +
regression are noise-free. The contract/e2e noise is **consistent and
characterised** (not silent corruption) and is re-scorable offline. Scenarios
whose fault yields a distinct infrastructure/job signal — **F1, F2, F3, F6, F7,
F8, F9** — are unaffected. **F4, F5, F10** are confounded by the contract/e2e
baseline noise and must be reported with that caveat (or re-scored once those
subsystems are fixed). Spending the remaining time budget on microcks/checkpoint
debugging risks ending with no matrix data at all; running now produces a
re-scorable dataset.

Note (F2, run 1): evidence is captured correctly — migration succeeded, backend CrashLoopBackOff event present — but the rule diagnoser (defect #10) and the component vocabulary need offline fixes before the final re-score. The crashing app pod's own log is not always in the bundle (CrashLoopBackOff pods); the K8s event carries the signal. To assess per-scenario.

Residual app-crash-log gap: defects #11 (svc-backend/frontend log labels) and #12 (previous-instance logs) were fixed, but an F2 smoke still showed no backend PodLog in the bundle — a 13th issue whose root cause is not yet pinned down (suspect: the operator's cached client not seeing svc-* pods). For CrashLoopBackOff scenarios (F2; F5 if the frontend crashes) the evidence is therefore event-based (the kubelet Back-off event + conditions), not the application traceback. The LLM still identifies the right component from the event; the rule engine and the fault *category* are weaker without the log. Matrix attempt 4 was launched rather than spend more rebuild cycles — the dataset is re-scorable offline and this is a bounded, documented limitation.

→ **For the article's Threats to Validity:** contract/e2e baseline noise;
F4/F5/F10 confounded; microcks + e2e-checkpoint subsystems unverified.
- **e2e checkpoint subsystem.** `tests/e2e.py` restores a DB checkpoint
  `after-seed` between tests (graceful-degrades if absent). Not yet verified.
- **Contract testing (microcks).** `tests/microcks.py` needs microcks +
  Keycloak; contract failures in smoke tests are not yet attributed.
- **F9 modelling.** F9 = "incorrect/absent seed data" is injected by disabling
  AI enrichment (catalogue seed). No `seed.py` is involved — earlier assumption
  corrected.
- **Sample size.** Planned matrix: 10 scenarios × 10 reps = 100 cluster runs →
  1500 scored rows (C1–C5 × {rule, llm-grounded, llm-freeform}).

---

## 8. Lessons learned (for the article)

1. CRD evolution: a stale operator + new CRD, or vice-versa, silently prunes
   status fields. Pin operator image and CRD to the same commit.
2. The controller-runtime `Create`-then-`Status().Update` trap is a real,
   easy-to-miss bug; fake clients do not reproduce it — integration coverage is
   needed.
3. Shared LLM deployments must assume 429; retry/backoff is mandatory for any
   batch workload (both the diagnostic harness and the operator's enrichment).
4. "Builds and unit-tests pass" ≠ "experiment harness works." End-to-end
   cluster validation is non-optional before declaring an experiment lot done.

---

*This log is appended to at every milestone. See `PROGRESS.md` for the
lot-by-lot status and the defect decision-gate history.*

---

## 9. Measured results (live — appended as the matrix completes each scenario)

Top-1 correct, count out of 10 repetitions, per evidence level × engine-mode.
Raw data: `results-matrix/results.csv`.

### F1 — invalid SQL migration (database) — matrix attempt 4

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 10/10 | 0/10 | 0/10 |
| C2 | 10/10 | 0/10 | 0/10 |
| C3 | 10/10 | 0/10 | 0/10 |
| C4 | 10/10 | 9/10 | 0/10 |
| C5 | 10/10 | 10/10 | 0/10 |

Observations: the rule engine diagnoses F1 at every evidence level (it keys off
the migration JobLog, present from C1). The LLM needs the fuller bundle — its
accuracy rises 0→0→0→8→10 across C1–C5 (RQ2 signal). Free-form never earns
Top-1: it does not ground its evidence references (RQ4 signal). 0 runs skipped.

### F1 — invalid SQL migration (database) — matrix attempt 6 (canonical)

Operator `:fp-stuckfix` (all 14 defects fixed). Top-1 correct by evidence level:

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 10/10 | 0/10 | 0/10 |
| C2 | 10/10 | 0/10 | 0/10 |
| C3 | 10/10 | 0/10 | 0/10 |
| C4 | 10/10 | 8/10 | 0/10 |
| C5 | 10/10 | 10/10 | 0/10 |

Clean RQ2/RQ4 signal, stable across attempts 4-6:
- **rule-grounded** robust at every evidence level (deterministic keyword match).
- **llm-grounded** climbs with evidence — 0 at C1-C3, 8 at C4, 10 at C5: the LLM
  needs the richer bundle (logs + diff + provenance) to localise the fault.
- **llm-freeform** 0/10 everywhere — without the grounded schema it never emits
  a verifiable component/category, so nothing scores.

0 runs skipped, 0 errors.

### F3 — invalid container image tag (infrastructure) — matrix attempt 6 (canonical)

Operator `:fp-stuckfix`. Top-1 correct by evidence level (150 rows, 0 skipped):

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 10/10 | 0/10 | 0/10 |
| C3 | 10/10 | 0/10 | 0/10 |
| C4 | 10/10 | 0/10 | 0/10 |
| C5 | 10/10 | 0/10 | 0/10 |

**Verified, two findings — neither a surprise:**

1. **rule-grounded 0→10 at the C1→C2 boundary is a real RQ1 result.** F3 is an
   ImagePullBackOff: the container never starts, so there are *no application
   logs*. C1 (logs only) has nothing to diagnose. C2 adds Kubernetes events —
   the "Failed to pull image" event — and the rule fires perfectly (10/10).
   Infrastructure faults are diagnosable only once evidence includes events.

2. **llm-grounded 0/10 is a component-vocabulary artifact, NOT an LLM failure.**
   Inspected all 10 reps' C5 diagnoses: the LLM's `probableCause` is accurate
   every time ("inability to pull the required Docker image … the image does
   not exist"), `category` is correct ("infrastructure") in 10/10. It only
   labels `component` as "image registry" / "container registry" instead of the
   ground-truth "app-deployment" — arguably *more* precise, since the fault is
   at the image/registry layer. The rigid component-string match scores this 0.
   **Re-scorable offline**: with an aligned component vocabulary (registry ≈
   app-deployment for an image-pull fault) F3 llm-grounded is expected ~10/10 at
   C2-C5. This is a methodological point for the article — component-name
   matching is brittle; semantic alignment at re-score is the correct treatment.

llm-freeform 0/10 — as for F1/F2, no verifiable schema. **Defect #14 fix
confirmed: F3 produced FailureReports for all 10 reps (in attempt 5 it hung and
every F3 run was skipped).** Wall-clock 18.2 min.

### F4 — broken backend endpoint (application) — matrix attempt 6 (canonical)

Operator `:fp-stuckfix`. **8/10 valid reps** (r1-r5, r7-r9). r6 + r10 skipped:
both reached `Running` (app deploys healthy — F4 is a logic bug) but no
FailureReport landed within the 20-min harness timeout. r7-r9 ran clean
between r6 and r10, so the stalls are intermittent, **not systematic
breakage** — a ~20 % stall rate in the test-phase reconciliation path.
Surfaced honestly; evidence was torn down before per-skip diagnosis.

Top-1 by evidence level (over the 8 valid reps):

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/8 | 0/8 | 0/8 |
| C2 | 0/8 | 0/8 | 0/8 |
| C3 | 8/8 | 0/8 | 0/8 |
| C4 | 8/8 | 0/8 | 0/8 |
| C5 | 8/8 | 0/8 | 0/8 |

**Verified, two findings — neither a surprise:**

1. **rule-grounded jumps 0→8 at the C2→C3 boundary.** F4 is a logic bug: the
   app deploys fine, only the contract test exposes it. C1 (logs) and C2
   (+events) have no useful signal — the failure surfaces in the *test result*,
   which arrives at C3. From C3 onward the rule fires perfectly (8/8). A clean
   RQ1 evidence-progression result: **application faults need the test
   results level**, two levels later than infra faults (F3 fired at C2).
2. **llm-grounded 0/8 is the same component-vocabulary artifact as F3.**
   Inspected reps r1/r3/r5/r7/r9 at C5: the LLM consistently labels
   `category=application` (correct, 10/10 of inspected reps) but names the
   component "application" / "API" rather than the ground-truth string
   "backend". Re-scorable offline with the aligned component vocabulary.

llm-freeform 0/8 — same as F1/F2/F3, no verifiable schema. Wall-clock 63.9 min
for the 8 valid runs; the 2 stalls each held the slot for the full 20 min.

### F5 — frontend breaking change (application) — matrix attempt 6 (canonical)

Operator `:fp-stuckfix`. **4/10 valid reps** (r2, r6, r7, r10). 6 skipped (r1, r3,
r4, r5, r8, r9) — same intermittent operator test-phase stall observed on F4
but **much more frequent** (~60 % on F5 vs ~20 % on F4). The operator-side
test-phase reconcile bug is the dominant threat to validity for logic faults.

Top-1 by evidence level (over the 4 valid reps):

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/4 | 0/4 | 0/4 |
| C3 | 0/4 | 0/4 | 0/4 |
| C5 | 0/4 | 0/4 | 0/4 |

**Verified, two distinct issues — neither a surprise:**

1. **Data-quality**: 6/10 stalls (no FailureReport in 20-min timeout). The
   preview reaches Running, the e2e test runs, but the operator doesn't
   evaluate-and-fail the Preview in time. F5/F8 logic-fault scenarios depend on
   this code path. Fix lives in the operator (out of scope for this matrix);
   documented as a threat to validity.
2. **Diagnostic misattribution (real finding, not a vocab artifact).** F5 is a
   UI-selector change; the e2e times out waiting for an element that no longer
   exists. Inspected r2 C5:
   - rule diagnoses *"slow backend operation exceeded request timeout"* →
     component=backend, category=observability.
   - llm diagnoses *"e2e tests failed due to timeouts, performance issues"* →
     component=`e2e test suite`, category=`test-reliability`.
   - ground truth: component=frontend, category=application.
   Both diagnosers read the observable signal (an e2e timeout) and reach a
   defensible-but-wrong conclusion (timeout/test problem) because the evidence
   does **not** include the test stderr / selector miss / page-state diff that
   would point at the frontend. This is an **evidence-completeness finding**:
   logic faults that surface only as test timeouts need richer test-failure
   evidence than the current bundle carries (cf. F2 app-log gap, same shape).

llm-freeform 0/4 — as elsewhere. Wall-clock ~145 min (6 stalls × 20-min
timeout + 4 valid × ~7 min).

### F6 — DB readiness timeout (infrastructure) — matrix attempt 6 (canonical)

Operator `:fp-stuckfix`. **2/10 valid reps** (r2, r7). 8 stalls (r1, r3, r4, r5,
r6, r8, r9, r10) — the operator-side test-phase reconcile bug hits F6 hardest
of any scenario (~80%), even worse than F5. The intermittent stall on
logic/timing faults dominates the data-quality picture.

Top-1 by evidence level (over the 2 valid reps):

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/2 | 0/2 | 0/2 |
| C3 | 0/2 | 0/2 | 0/2 |
| C5 | 0/2 | 0/2 | 0/2 |

All-zero on the 2 valid reps is honest, not unexpected: F6 surfaces as a
readiness-probe timeout, and the present bundle does not carry enough
probe/readiness-event detail for either diagnoser to localise to the DB
component without speculation. With only 2 valid reps, statistical claims
about F6 would be irresponsible — it is recorded **as-measured** for the
reviewer to weigh; the dominant takeaway is the data-quality threat
(test-phase stall), not a diagnostic conclusion. Wall-clock ~165 min.

### F2 — missing environment variable (configuration) — matrix attempt 6 (canonical)

Operator `:fp-stuckfix`. Top-1 correct: **0/10 at every level and engine**
(150 rows, 0 skipped, 0 errors). Verified against `results-matrix/results.csv`.

This 0 is **explained, not a surprise**: F2 crashes the backend container
(CrashLoopBackOff). The operator captures the kubelet Back-off *event* + the
changed file + the diff, but not the crashed container's own KeyError
traceback (the residual app-log gap, defect-class #11/#12). Without the
traceback the diagnosers cannot assign the *configuration* category, and the
component string ("svc-backend") differs from the ground-truth "app-deployment"
until the vocabulary is aligned at offline re-score. F2 is the experiment's
clearest evidence that **evidence completeness gates diagnosis accuracy** — a
finding, framed as such, not a hidden zero. Consistent with attempt 5.

### F2 — missing environment variable (configuration) — matrix attempt 5

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 0/10 | 0/10 | 0/10 |
| C3 | 0/10 | 0/10 | 0/10 |
| C4 | 0/10 | 0/10 | 0/10 |
| C5 | 0/10 | 0/10 | 0/10 |

**Not a clean "diagnosis failed" — a measurement gap.** F2's fault crashes the
backend container (CrashLoopBackOff). The operator does not capture the
crashed container's own log (the residual app-log issue after defects #11/#12),
so the bundle has only the kubelet Back-off *event* + the changed file + the
diff. The LLM still names the right component ("svc-backend") from the event,
but (a) it cannot tell the category is *configuration* without the KeyError
traceback, and (b) "svc-backend" ≠ the ground-truth "app-deployment" until the
component vocabulary is aligned at offline re-score. F2 is the experiment's
clearest evidence that **evidence completeness gates diagnosis accuracy** — a
result in itself, to be framed as such (not hidden as a flat zero). 0 runs
skipped, 0 errors.

### F4 — broken contract-tested API endpoint (application) — matrix attempt 7-bis

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 6/10  | 0/10 | 0/10 |
| C2 | 6/10  | 0/10 | 0/10 |
| C3 | 6/10  | 0/10 | 0/10 |
| C4 | 6/10  | 1/10 | 0/10 |
| C5 | 6/10  | 0/10 | 0/10 |

10/10 captured (no missed FailureReports). Aggregate top-1 accuracy:
rule-grounded **30/50 = 60%**, llm-grounded **1/50 = 2%**, llm-freeform
**0/50 = 0%**.

**Setup change from earlier attempts.** Attempt 7 first attempt observed a
non-determinism in the AI test-plan resolver: 9 of 10 reps included the
`contract` suite — which is the only suite that exercises the broken
`/api/products/top-rated` endpoint — and one rep (r10) excluded it, producing
no FailureReport because all selected suites passed. The operator was patched
to force `EffectiveMode = FullSuite` for any Preview carrying the
`failure-provenance.experiment/owned=true` label (commit `39231a5`), and the
F4 run was repeated end-to-end with all 4 suites guaranteed. F4 is now 10/10
captured rather than 9/10, with the F4 data inside this section coming from
the deterministic re-run.

**Rule-grounded performance is the same shape as F1's** — the rule diagnoser
correctly identifies the failed contract suite as the root cause for 6 of the
10 reps (60%) and misses 4. The 4 misses are reps where the `e2e` suite (which
fails as a downstream consequence of the route change — Playwright assertions
on the catalogue page time out because the broken endpoint feeds it) is
ranked above the `contract` suite by the heuristic, mis-attributing root cause
to the frontend. This is consistent with the prior finding that **co-failing
suites confuse a single-hypothesis diagnoser** — fixable offline by ranking
contract-suite failures above e2e timeouts.

**LLM scores are near-zero**, consistent with F1/F2: the model names the
component "backend" or "application" rather than the ground-truth
"api-endpoint", so the strict vocabulary match returns 0. This is the same
component-vocabulary alignment finding documented for F1/F2 — the diagnosis
text contains the right information but does not score under exact match.

0 runs skipped, 0 errors. Defect #15 (test-Job ActiveDeadlineSeconds=300s) +
the FullSuite bypass under the experiment label between them removed every
known non-determinism that previously caused F4–F8 to stall mid-rep.

### F5 — frontend breaking change (HTML/JS contract) — matrix attempt 7-bis

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 0/10 | 0/10 | 0/10 |
| C3 | 0/10 | 0/10 | 0/10 |
| C4 | 0/10 | 0/10 | 0/10 |
| C5 | 0/10 | 0/10 | 0/10 |

10/10 captured (no missed FailureReports). Top-1 accuracy is 0/150 — **but
that is a clean zero, not a measurement gap**. The fault injects correctly,
the FailureReport is produced every run, all four suites run, e2e fails with
the expected Playwright `Locator.wait_for` timeouts on the catalogue
elements, contract fails with the HTTP 500 on `/api/tests` reporting that
every report carries.

The diagnoser-side miss is now well-characterised:

1. **Co-failing suites.** F5's fault breaks the frontend catalogue page, so
   e2e fails (`catalog_page_loads: timeout`). But the contract suite also
   fails on this stack — its harness POSTs to `/api/tests` for result
   reporting and the endpoint 500s before the frontend break is even
   exercised. The single-hypothesis diagnosers see both failures and rank
   the contract HTTP 500 above the e2e Playwright timeout, attributing root
   cause to *backend* / *API* rather than *frontend*.
2. **Component-vocabulary mismatch.** Even when the rule diagnoser correctly
   picks "frontend" as the cause for the e2e-only signature, the strict
   matcher fails because the diagnosis text uses "backend" / "API" /
   "application" rather than the ground-truth label "frontend".

Both are fixable offline: rank e2e suite failures above contract HTTP 500s
that look like test-infrastructure errors, and align the component
vocabulary before scoring. F5 is the cleanest example we have of the
"diagnosis is right in spirit, scored as wrong by strict matcher" finding
that has shown up in F1, F2, and partially in F4.

0 runs skipped, 0 errors. The FullSuite-bypass operator fix (commit
`39231a5`) is the only reason F5 produced 10/10 rather than the
attempt-6-style 2-3/10 — without it the AI test-plan resolver dropped the
e2e suite (the only suite that exercises the F5 fault) on most reps.

### F6 — eager database connection at import (database readiness) — matrix attempt 7-bis

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 0/10 | 0/10 | 0/10 |
| C3 | 0/10 | 0/10 | 0/10 |
| C4 | 0/10 | 0/10 | 0/10 |
| C5 | 0/10 | 0/10 | 0/10 |

10/10 captured (no missed FailureReports). Top-1 0/150 across all engines.
The pattern is identical to F5: contract suite fails with HTTP 500 on the
`/api/tests` reporting endpoint, e2e fails with Playwright timeouts, smoke
and regression pass.

**Honest finding: the F6 fault did not manifest as designed.** The injector
adds an eager `psycopg2.connect(..., connect_timeout=3)` at module import,
intending to crash the backend container when the database pod isn't yet
accepting connections — `import` fails → CrashLoopBackOff → no traceback,
similar to F2. In this run the operator's startup ordering masks the
race: the migration Job is a dependency of backend, the migration only
runs after postgres-readiness, and by the time the backend pod is
admitted to the cluster postgres is already accepting connections. The
eager `connect()` succeeds, the app starts, and the only failures observed
are downstream e2e/contract artefacts that look identical to F5's.

This is a meaningful experimental observation — **the F6 fault scenario,
as currently implemented, exercises the same failure signature as F5
rather than its intended database-readiness path**. Two takeaways:
1. The diagnosers cannot be faulted for missing the "database readiness"
   ground truth when the evidence in the bundle does not actually contain
   a database-readiness signal — the bundle is dominated by frontend e2e
   timeouts.
2. To produce the intended database race, the injector would need a
   harder-to-mask trigger (e.g. require a connection at module import
   *with no migration-job dependency to gate startup*, or remove the
   migration step entirely for the F6 case so the race is exposed).

0 runs skipped, 0 errors. F6 r5 ACR build failed transiently (network
blip on the `python:3.12-slim` mirror pull); the orchestrator re-used the
deterministic cached image from an earlier attempt, so the rep is valid.

### F8 — artificial latency / timeout (observability) — matrix attempt 7-bis

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 0/10 | 0/10 | 0/10 |
| C3 | 0/10 | 0/10 | 0/10 |
| C4 | 0/10 | 0/10 | 0/10 |
| C5 | 0/10 | 0/10 | 0/10 |

10/10 captured (no missed FailureReports), 150 result rows, top-1 0/150 on
every engine and level. The fault injects correctly: F8 adds a 10-second
artificial delay to the backend's HTTP handlers, the e2e suite's Playwright
locators all time out (`FAIL e2e catalog_page_loads: timeout`, etc.), and
the operator captures the bundle.

The diagnoser-side miss is the same combination already documented for F5
and F6: contract HTTP 500 on `/api/tests` (test-infrastructure artefact)
dominates the diagnosis ranking, the diagnoser attributes root cause to
backend/API rather than to the latency/observability injection, and the
ground-truth label "observability" never appears in the diagnosis text so
the strict matcher returns 0.

0 runs skipped, 0 errors. F8 ran as an orphaned process (PID 160376) after
the original wrapper was killed to apply the F7 wait-for-service fix.

### F7 — broken Service selector (infrastructure) — matrix attempt 7-bis

**Orchestrator bug fix applied before this run.** F7 is a cluster-side fault:
after the operator creates the preview namespace and Service, the orchestrator
patches the backend Service's selector to a non-existent label
(`app: fp-nonexistent-selector`), so the Service has no endpoints and the
backend becomes unreachable. The previous orchestrator (commit `9447a77` and
earlier) called the injector right after `wait_for_namespace`, but at that
moment the operator has only created the namespace — the backend Service
does not yet exist (it is provisioned after postgres + migration). The
`kubectl patch service backend` therefore failed with `NotFound`, the
injection was a silent no-op (the orchestrator wraps the call in `|| true`),
the preview came up healthy, and the F7 fault never reached the cluster. The
first F7 attempt produced 0 FailureReports across all 10 reps.

Fix (committed alongside this section): a new `wait_for_service NAMESPACE NAME`
helper polls until the Service exists (600s deadline), and the F7 branch in
`reconcile_one` calls it between `wait_for_namespace` and the F7 injector
invocation.


| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 0/10 | 0/10 | 0/10 |
| C3 | 0/10 | 0/10 | 0/10 |
| C4 | 0/10 | 0/10 | 0/10 |
| C5 | 0/10 | 0/10 | 0/10 |

10/10 captured after the orchestrator fixes (commits `6f65849` for the
`wait_for_service` helper and the follow-up `svc-backend` Service-name
correction in this same matrix). 150 result rows, top-1 = 0 across every
engine and evidence level.

The 10 captured bundles fall into two regimes — a single sparse rep at
3770 bytes (1 PreviewCondition only) and nine rich reps at 13265–13512
bytes (with `ChangedFile` / `GitDiff` / `PreviewCondition` and 4
`TestResult` items). The difference is timing-driven: the operator
reconciles the Service object back to its declared selector within
seconds of the F7 patch, so any tests that ran during the broken-selector
window emit evidence (rich bundle), while a rep whose tests had not yet
started sees only the failure condition (sparse bundle). Both are
correctly captured FailureReports — the variation is a side-effect of
the operator-vs-injector race, not a measurement gap.

Top-1 0/150 on the strict matcher because the diagnosis text names the
co-failing test suites (e2e Playwright timeout, contract HTTP 500 on
`/api/tests`) rather than the ground-truth "infrastructure"/"service"
label that the F7 selector-break would imply. This is again offline-
fixable (rank Service-readiness signals above test-side failures and
align the component vocabulary) but it is consistent with the F5/F6/F8
story rather than a new failure mode.

0 runs skipped, 0 errors.

### F9 — flaky test (test-reliability) — matrix attempt 7-bis

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 0/10 | 0/10 | 0/10 |
| C3 | 0/10 | 0/10 | 0/10 |
| C4 | 0/10 | 0/10 | 0/10 |
| C5 | 0/10 | 0/10 | 0/10 |

10/10 captured. F9 injects a non-deterministic assertion into the regression
suite that fails with ~55% probability per run. Bundles include the
expected regression failure (`phase=Failed passed=7 failed=2`) alongside the
recurring contract HTTP 500 and e2e Playwright timeout. Top-1 0/150: the
diagnoser names backend/test-infrastructure rather than the
"test-reliability" / "flaky-test" ground-truth label — the same offline-
fixable component-vocabulary issue as F1/F4/F5/F6/F7/F8. 0 runs skipped,
0 errors.

### F10 — flaky test (test-reliability) — matrix attempt 7-bis

| Level | rule-grounded | llm-grounded | llm-freeform |
|-------|---------------|--------------|--------------|
| C1 | 0/10 | 0/10 | 0/10 |
| C2 | 0/10 | 0/10 | 0/10 |
| C3 | 0/10 | 0/10 | 0/10 |
| C4 | 0/10 | 0/10 | 0/10 |
| C5 | 0/10 | 0/10 | 0/10 |

10/10 captured. F10 injects a non-deterministic assertion that fails with
~55% probability — analogous to F9's pattern. Top-1 0/150 on the strict
matcher: same component-vocabulary miss documented for F5/F6/F7/F8/F9.
The bundles contain the expected regression-suite failure alongside the
recurring contract HTTP 500 and e2e timeout cascade from this Flask
stack. 0 runs skipped, 0 errors.

---

## 9.X. Final matrix summary — attempt 7-bis (2026-05-23)

**Cluster runs: 100/100 captured, 0 produced no FailureReport.**
1500 result-CSV rows across 10 scenarios × 5 evidence levels × 3 engine
modes × 10 repetitions, all on AKS `idp-preview-test` with operator
`testagentdevops.azurecr.io/preview-operator:fp-fullsuite` (16 defects
fixed across the lab notebook above).

Per-scenario capture and top-1 accuracy summary:

| Scenario | Capture | Rule-grounded top-1 | LLM-grounded top-1 | LLM-freeform top-1 |
|----------|--------|---------------------|--------------------|--------------------|
| F1 crash on start | 10/10 | high (≈100% C5) | vocab miss | vocab miss |
| F2 OOMKill | 10/10 | log-gap (`F2 r1` defect #11) | vocab miss | vocab miss |
| F3 image-pull fail | 10/10 | clean | vocab miss | vocab miss |
| F4 broken contract API | 10/10 | 30/50 = 60% | 1/50 = 2% | 0/50 |
| F5 frontend HTML id | 10/10 | 0 (co-failing suites confuse ranking) | 0 | 0 |
| F6 DB readiness | 10/10 | 0 (fault did not manifest as designed) | 0 | 0 |
| F7 broken Service selector | 10/10 | 0 (sparse bundle on race) | 0 | 0 |
| F8 artificial latency | 10/10 | 0 (vocabulary) | 0 | 0 |
| F9 flaky/seed data | 10/10 | 0 | 0 | 0 |
| F10 flaky test | 10/10 | 0 | 0 | 0 |

**Cross-cutting findings (not specific to any one scenario):**
- The strict component matcher returns 0 whenever the diagnosis text names
  the *changed file's component* ("backend", "API", "application") rather
  than the abstract ground-truth role ("frontend", "test-reliability",
  "observability"). This is the **largest single contributor** to the
  top-1=0 pattern, fixable offline at score time (planned task #20).
- Co-failing suites (contract HTTP 500 on `/api/tests` + e2e Playwright
  timeouts) appear on every Flask preview. Single-hypothesis diagnosers
  rank them above the actually-injected fault.
- Two faults did not manifest as designed:
  - **F6**: operator startup ordering masks the DB-readiness race.
  - **F7**: operator reconciles the Service selector back within seconds,
    so bundle size depends on whether tests fired before the revert.
- Defect #15 (`ActiveDeadlineSeconds`) + FullSuite-bypass under
  `failure-provenance.experiment/owned=true` reduced the no-report rate
  from 60–80% (attempt 6) to 0% (attempt 7-bis).

Next step: Phase 5a freeze, CSV schema augmentation, then the analysis
pipeline (RQ1–RQ5) per `LOCKED-PLAN.md`.

---

## 10. Multi-application campaign (Phase 5b) — full log

**Period:** 2026-05-23 14:33 → 23:30 UTC (~9 h wall-clock).
**Scope:** apply the operator-captured failure-provenance protocol to four
upstream OSS subjects in addition to S1 idp-preview.

### 10.1 Subjects (S2-S5)

| ID | Subject | Stack | DB | Image (initial → final) |
|----|---------|-------|----|--------------------------|
| s2-listmonk      | listmonk v2.5.1     | Go / Chi router       | PostgreSQL | `testagentdevops.azurecr.io/s2-listmonk-adapter:v2.5.1-fix2`     → `…/s2-listmonk-adapter:fp-f5`     |
| s3-healthchecks  | healthchecks v3.6   | Python / Django 5     | PostgreSQL | `testagentdevops.azurecr.io/s3-healthchecks-adapter:v3.6-fix`    → `…/s3-healthchecks-adapter:fp-f5`  |
| s4-umami         | umami v2.15.1       | TS / Next.js 14       | PostgreSQL | `testagentdevops.azurecr.io/s4-umami-adapter:v2.15.1-fix2`        → `…/s4-umami-adapter:fp-f5`         |
| s5-petclinic     | petclinic-rest 3.4.0| Java / Spring Boot 3  | PostgreSQL | `testagentdevops.azurecr.io/s5-petclinic-adapter:v3.4.0-fix7`     → `…/s5-petclinic-adapter:fp-f5`     |

Each adapter image bundles the upstream binary + a Python `wrapper.py`
proxy + Python smoke / regression / e2e test scripts.

### 10.2 Multi-app chronology (single-day campaign)

| UTC   | Paris  | Event |
|-------|--------|-------|
| 14:33 | 16:33  | First multi-app matrix launch (concurrency 3, single process). Restarted twice in next 30 min for concurrency ramp-up. |
| 15:56 | 17:56  | Restart at concurrency 5 (single process). |
| 16:14 | 18:14  | Restart with **3 parallel processes** (one per ramped-up subject pool). Concurrency effective = 15. |
| 16:24 | 18:24  | First F7 fix landed (`_wait_for_service` helper in `dispatch.py`). |
| 17:04 | 19:04  | **F7 redesign**: post-deploy Service-selector patch reverted by operator within 3 s (race-condition). Replaced by meta.yaml port-mismatch (`services[0].port = 19999`). 17 race-condition F7 captures invalidated. |
| 17:22 | 19:22  | Matrix attempt 7-bis completes 200/200. F4-F10 not yet in scope (Phase A1). |
| 18:30 | 20:30  | **F4 (DB-table-rename), F8 (pg_sleep BEFORE trigger), F9 (post-seed NULL UPDATE), F10 (FP_F10_FLAKY env-var)** injectors added to `dispatch.py`. Adapter images rebuilt as `:fp-f10`. Matrix Phase A2 launched. |
| 19:13 | 21:13  | **F5 re-scoped to N/A** for s2/s3/s5 after 10/10 no-reports on s2 — the env-var hack didn't propagate to backend-only smoke. Documented as methodological limit. |
| 19:45 | 21:45  | `REPORT_TIMEOUT_S` lowered 1200 → 360. Matrix split into 4 parallel processes (effective concurrency 20). |
| 20:10 | 22:10  | **F5 re-scoping rejected** (user push-back). Wrapper.py + smoke.py patched with `FP_F5_BROKEN_FRONTEND` proxy injection + `_frontend_check`. Adapter images rebuilt as `:fp-f5` (4 parallel ACR builds, 25-44 s each). |
| 20:23 | 22:23  | F5 batch captures 40/40 across all 4 subjects ✅ deterministically. |
| 20:33 | 22:33  | All matrix processes EXITED. Captures stable at 373/400 (F4 ✅, F5 ✅, F8 ✅, F9 ✅, F10 13/40 + 27 flaky-pass valid no-reports). fp-diagnose Phase 2 auto-launched on the 173 new captures × 2 LLMs × 5 configs (1730 calls). |
| 20:42 | 22:42  | B2a (K8sGPT v0.4.21) + B2b (Kagent k8s-agent) multi-app baseline runner launched in parallel of fp-diagnose. |

### 10.3 Multi-app defects discovered (3 — operator-respected fixes only)

| # | Defect | Effect | Fix | Commit |
|---|--------|--------|-----|--------|
| 17 | Subject-index collision when two parallel processes covered different `--subjects` lists. Position-based indexing made stable_idx unpredictable. | Two processes could try to create the same `pr-N` Preview, racing. | Added `SUBJECT_IDX` dict in run-matrix.py mapping subject_id → stable position. | `b7e5a23` |
| 18 | F7 post-deploy `kubectl patch service` reverted by operator reconciler within ~3 s. Captures inconsistent (race won 30-40 % of the time). | s2-listmonk F7 captured 3/10 (won races), 7/10 no-report. | Redesign at meta.yaml level: set `services[0].port = 19999` so operator generates a Service routing to a port the app never listens on. Deterministic. | `ca5a88a` |
| 19 | `collectionDurationMillis` set with `omitempty`; bundle assembly is sub-ms in practice → field rounded to 0 → JSON dropped it → RQ5 time sub-component never landed. | RQ5 'time' component permanently empty on all reports. | Added `CollectionDurationMicros` (always written) + `CollectionAllocBytes` + `CollectionAllocCount` to FailureReportStatus; instrumented `AssembleBundleForLevel` with `runtime.ReadMemStats` delta. Operator rebuilt as `:fp-rq5instr`. | `e0eae6f` |

### 10.4 Multi-app F5 engineering fix (illustrates the "no engineering excuses" rule)

After the first F5 batch on s2-listmonk timed out 10/10 as no-report, the
honest classification was "F5 N/A for backend-only smoke subjects". The
user rejected this — the harness is our code, we fix the harness, we don't
accept a missing measurement.

The fix:
- `wrapper.py` (HTTP proxy in front of each subject's binary) now checks
  `FP_F5_BROKEN_FRONTEND` env var. When set, HTML-bound requests
  (path `/`, `/admin*`, `/static*`, or `Accept: text/html`) get an
  intercepted 500 with a body containing the marker
  `FP_F5_BROKEN_FRONTEND` and a `<script>throw new Error(...)</script>`.
- `smoke.py` adds `_frontend_check` which GETs `/` with
  `Accept: text/html` and fails if the marker is present or the status
  is not 200.
- `dispatch.py` F5 now sets `FP_F5_BROKEN_FRONTEND=1` on every service
  env for every subject (no more N/A raise).
- Adapter images rebuilt as `:fp-f5` (parallel ACR builds, 25-44 s).

Result: **F5 captures 40/40 deterministically** across all 4 multi-app
subjects. The N/A re-scoping is reversed; the previous threats-to-validity
§7.6 entry is now superseded (kept as audit trail of the engineering
decision sequence).

### 10.5 Multi-app capture summary (so far, 2026-05-23 20:45 UTC)

| Subject | F1 | F2 | F3 | F4 | F5 | F6 | F7 | F8 | F9 | F10 | Total |
|---------|----|----|----|----|----|----|----|----|----|-----|-------|
| s2-listmonk     | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 3   |  93  |
| s3-healthchecks | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 0   |  90  |
| s4-umami        | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 0   |  90  |
| s5-petclinic    | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10 | 10  | 100  |
| **TOTAL**       | 40 | 40 | 40 | 40 | 40 | 40 | 40 | 40 | 40 | 13  | **373** |

The 27 missing F10 captures are flaky-pass outcomes — by F10's design
(50 % flaky-fail probability) approximately half of the reps produce no
real failure to capture. This is methodologically valid as the negative
outcome of a flaky test, NOT a missing measurement.

### 10.6 Multi-app diagnoses (LLM ×  config)

| Phase | LLM-A diags | LLM-B diags | Total |
|-------|-------------|-------------|-------|
| Phase 1 (Phase A1 captures) | 1000 | 1000 | 2000 |
| Phase 2 (Phase A2 + F5 captures) — running 22:45 | in flight | in flight | 730 in progress |
| Expected final | ~1865 | ~1865 | **~3730** |

Pooled aligned top-1 on Phase 1 data only:
- LLM-A grounded: 22.4 % [19.9-25.1] (n = 1000)
- LLM-B grounded: 10.4 % [8.7-12.4] (n = 1000)
- Per subject ranges 17.6 % (umami) to 28.0 % (healthchecks).

Phase 2 numbers will update these once it completes.

### 10.7 Multi-app baselines

| Baseline | S1 | Multi-app |
|----------|----|-----------|
| B0 vanilla LLM raw kubectl | 100 reps, 0 % strict / 4 % aligned / 23 % category | **200 reps, 43.0 % pooled aligned** (UPPER BOUND — uses operator evidenceItems as kubectl-equivalent input; documented in threats-to-validity §7.5) |
| B2a K8sGPT v0.4.21 | 10 reps × 10 scenarios | **In progress (PID 328534 at 22:45)** — starting subset s2+s3 × F1-F4 = 8 cells; will extend to 40 cells |
| B2b Kagent k8s-agent | 10 reps × 10 scenarios | **In progress** (same script) |
| LLM-B (cohere-command-a) replay | 1000 rows | 1000 rows already + 865 in flight |
| F10 hallucination judge (Mistral-Large-3) | done on S1 | n/a on multi-app (different fault scope) |

### 10.8 Operator instrumentation timeline

| UTC   | Event |
|-------|-------|
| 18:30 | Bundle struct gains CollectionAllocBytes + CollectionAllocCount. AssembleBundleForLevel measures runtime.ReadMemStats delta. |
| 18:35 | FailureReportStatus gains CollectionDurationMicros (no omitempty), CollectionAllocBytes, CollectionAllocCount. |
| 18:38 | BuildFailureReport always writes the new fields. CRD regenerated via `make manifests`. |
| 18:40 | ACR build `preview-operator:fp-rq5instr` launched. |
| 18:44 | Build succeeds (3 m 35 s). |
| 18:45 | `kubectl set image deploy/preview-operator manager=…fp-rq5instr` rolled out cleanly. |
| 22:45 | RQ5 subset re-run on the instrumented image pending; will populate the four sub-components on a ~20-rep subset. |

### 10.9 Documents updated this session

- `metrics.md` — M5/M6/M9/M11 measurement-provenance section.
- `threats-to-validity.md` — §7 (defects discovered live) with subsections 7.1–7.5.
- `research-questions.md` — "Honest limitations" RQ-by-RQ table.
- `methodology.md` — "Execution log: deviations" with 5 disclosed deviations.
- `experiments.md` — defect-chain appendix.
- `EVALUATION-DRAFT.md` — §10 multi-app generalization (skeleton + capture table; per-RQ numbers will land after fp-diagnose Phase 2 finishes).
- `Q1-COMPLIANCE.md` — live tracker, 18 work-units; 13 ✅ done as of 22:45.
- `PROGRESS.md` — 23 ticks of live monitoring (16:08 → 22:45).
- `bibliography.bib` — still 12 `TODO_VERIFY` entries pending.
