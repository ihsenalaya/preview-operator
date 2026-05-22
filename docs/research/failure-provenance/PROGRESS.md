# Failure-Provenance — Progress Tracker

Live status of the experiment-programming roadmap from `~/CONTINUE-HERE.md` §5.
This file is updated at every milestone (and at least hourly while work is
active). Each commit on `article/failure-provenance` is a checkpoint.

- **Branch:** `article/failure-provenance` (pushed to GitHub)
- **Base commit (Phases 1–4):** `8cc635a`
- **Last updated:** 2026-05-22 16:55 UTC
- **Currently working on:** Lot 6 attempted. The first matrix run exposed a
  chain of harness-fidelity defects (see §6). Three are fixed and committed;
  two more are being fixed now. The matrix is **not** producing valid data yet.

---

## 1. Where things stand

| Lot | Title | Status | Commit |
|-----|-------|--------|--------|
| Lot 1 | Operator measurement instrumentation | ✅ done | `b4b6817` |
| Lot 3 | Diagnostic harness (rule + LLM, grounded/free-form) | ✅ done | `dcff904` |
| Lot 2 | Fault injectors F1–F10 | ⚠️ defects found | `4c93bf3` |
| Lot 4 | Automated scoring | ✅ done | `200b2a6` |
| Lot 5 | End-to-end orchestration | ⚠️ defects found | `f7ef0e8` |
| Lot 6 | Run the 10×10 matrix on the cluster | 🔄 blocked on §6 | — |

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

### Open defects — STOP, needs a decision

| # | Defect | Effect | Status |
|---|--------|--------|--------|
| 4 | **Baseline image unpullable.** `--baseline-image` defaults to `ghcr.io/ihsenalaya/idp-preview:latest`; the cluster gets `ErrImagePull`. | Non-code faults F3/F7/F9 cannot deploy a working app. | A working image was built: `testagentdevops.azurecr.io/idp-preview:baseline` (ACR run `ca8`). The harness default still needs changing to use it. |
| 5 | **Migration/seed never enabled.** The generator emits `spec.database` without a `migration`/`seed` block; the operator runs those jobs only when `spec.database.migration.enabled: true`. No webhook/defaulter auto-enables them. | F1 (bad migration) and F9 (bad seed) faults are **inert**. The app also never gets its DB schema → test suite fails as **baseline noise**. | Not fixed. Needs a design choice — harness post-processes the manifest, or the generator is fixed upstream. |
| 6 | **`seed.py` is missing.** The README's seed command is `python scripts/seed.py`, but no `seed.py` exists anywhere in `ihsenalaya/idp-preview`. | F9 (seed-data fault) **cannot run at all** — the seed job would fail to find its script regardless of any fix. | Not fixable in this repo — needs a change to `ihsenalaya/idp-preview`, or F9 dropped. |

### Why work paused here

The first matrix launch revealed that Lots 2 and 5 were never validated
end-to-end. Six distinct defects surfaced, each found only by running the
harness on the cluster; three were clear-cut and are fixed, but #5 and #6 are
not clear-cut — they need decisions that are outside a single bug fix:

- **#5** — where the migration/seed config belongs (harness vs the idp-preview
  generator), and whether enabling migrations for every scenario is the
  intended experiment design.
- **#6** — `seed.py` is missing from a *different repository*. F9 cannot be run
  without either writing that script (a change to `ihsenalaya/idp-preview`) or
  redefining/dropping F9.
- **Baseline noise is still unconfirmed** — the baseline run never reached the
  test suite (it died at #4's `ErrImagePull`). Whether an un-faulted preview is
  clean once #4/#5 are fixed is unknown, and it determines whether *all ten*
  scenarios are confounded or only F1/F9.

Per the agreed autonomy boundary (fix clear-cut bugs; pause when a problem is
ambiguous or needs a real decision), work stopped here for review rather than
spending more cluster credits on a harness of unknown remaining depth.

### To resume — options for the reviewer

1. **Fix the harness fully** — change the baseline-image default to the ACR
   image; have the harness inject `database.migration`/`seed`; add `seed.py`
   to `ihsenalaya/idp-preview` (or drop F9). Then re-run baseline + F1/F3/F9
   smoke tests before the matrix.
2. **Reduce scope** — run the matrix with the scenarios that already work
   (the non-DB faults), drop F1/F9, and report a 8-scenario evaluation.
3. **Review scenario fidelity first** — confirm with a clean baseline whether
   the demo app's test suite is stable before committing to the full matrix.

### Root cause (process)

Lots 2 and 5 were marked "done" in this tracker on 2026-05-22 14:59 without an
end-to-end cluster run. `inject-fault.sh --list` and `go build` passing is not
the same as a preview actually failing the intended way on the cluster.
