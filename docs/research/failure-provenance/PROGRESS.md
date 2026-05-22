# Failure-Provenance — Progress Tracker

Live status of the experiment-programming roadmap from `~/CONTINUE-HERE.md` §5.
This file is updated at every milestone (and at least hourly while work is
active). Each commit on `article/failure-provenance` is a checkpoint.

- **Branch:** `article/failure-provenance` (pushed to GitHub)
- **Base commit (Phases 1–4):** `8cc635a`
- **Last updated:** 2026-05-22 14:59 UTC
- **Currently working on:** Lots 1–5 done. Lot 6 (run the matrix) is gated —
  awaiting go-ahead (cluster + credit cost).

---

## 1. Where things stand

| Lot | Title | Status | Commit |
|-----|-------|--------|--------|
| Lot 1 | Operator measurement instrumentation | ✅ done | `b4b6817` |
| Lot 3 | Diagnostic harness (rule + LLM, grounded/free-form) | ✅ done | `dcff904` |
| Lot 2 | Fault injectors F1–F10 | ✅ done | `4c93bf3` |
| Lot 4 | Automated scoring | ✅ done | `200b2a6` |
| Lot 5 | End-to-end orchestration | ✅ done | _this commit_ |
| Lot 6 | Run the 10×10 matrix on the cluster | 🚫 gated | — |

Legend: ✅ done · 🔄 in progress · ⏳ pending · 🚫 gated (needs approval/cluster)

Lot 6 is **gated**: it runs the real experiment matrix on the AKS cluster, which
costs credits (`~/CONTINUE-HERE.md` §3 warns of a credit cap). It will not start
without explicit go-ahead.

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
