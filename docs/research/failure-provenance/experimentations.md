# Failure-Provenance — Experimentation Log

Raw lab notebook for the Lot 6 experiment campaign. **Everything** observed
during builds, smoke tests, baseline runs and the matrix is recorded here:
durations, resources, defects, measurements, anomalies. It is the source
material for the article's *Evaluation*, *Threats to Validity*, *Reproducibility*
and *Lessons Learned* sections.

- **Integrity rule:** only measured facts go here. Unmeasured = stated as such,
  never estimated silently. (`~/CONTINUE-HERE.md` §6.)
- **Updated:** continuously, alongside `PROGRESS.md`, at every milestone.
- **Last updated:** 2026-05-22 17:55 UTC
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
| 17:5x | AI-enrichment fix (`d91607d`); operator build `:fp-aifix2` | ~3.5 min | — |
| — | **Baseline run 3** — pending operator redeploy | — | (pending) |

---

## 4. Defects found (the matrix exposed 7)

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

- **Baseline cleanliness — unconfirmed.** Baseline run 2 confirmed the
  migration now runs but the suite is still noisy (defect #7). Baseline run 3,
  after the AI-enrichment fix `d91607d`, will show whether a seeded catalogue
  finally yields a clean un-faulted preview. Result pending.
- **Baseline run 2 test suite** (no fault, AI seed still failing): smoke
  Succeeded 2/0, contract Failed 0/1, regression Failed 7/2, e2e Failed 1/5 —
  identical to the F1 smoke tests, confirming the failures are seed-noise, not
  fault signal.
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
