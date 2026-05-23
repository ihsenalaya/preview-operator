# Locked Plan — Phase 5 completion and Q1 evaluation

**Locked on 2026-05-23 11:30 UTC, after the user explicitly approved both
the analysis plan + execution plan + the Kagent-as-additional-baseline
addendum.**

This file is the single source of truth for what is going to happen once
the matrix attempt 7-bis F10 scenario finishes. It mirrors task IDs
#16–#28 in the in-session task tracker.

The plan is constructed to give the strongest credible story for a
single-application study (S1 idp-preview on AKS) **without** running the
multi-app generalization (tasks #14/#15 are explicitly deleted; multi-app
is mentioned as future work). The argument for that scoping decision is
in §6 below.

---

## 1. Roadmap (sequential, no Multi-app)

### Phase 1 — Lot-6 close-out (today, ~3-4 h, $0 LLM)

| # | Task | Estimate |
|---|------|----------|
| 16 | Finish F10 + write §9 final | ~25 min wait + 15 min |
| 17 | Phase 5a freeze (tag + digest + `PHASE5A-FREEZE.md`) | 15 min |
| 18 | CSV schema augmentation (7 missing columns) | 45 min |
| 19 | Investigate MTTD recoverability from existing reports | 30 min |
| 20 | Build single-LLM analysis pipeline (`analysis/01-08.py`) | 2-3 h |

→ deliverable: full L1–L5 analysis on LLM-A data, single-LLM ablation
results, MTTD verdict (recovered or honestly TODO).

### Phase 2 — Cross-LLM and baselines (tonight / tomorrow, ~10 h, ~$10 LLM)

| # | Task | Cost | Time |
|---|------|------|------|
| 21 | Wire LLM-B Together.ai Llama-3.3-70B | $0 | 4 h |
| 22 | LLM-B offline replay on Phase-5a bundles | ~$5 | 1-2 h |
| 23 | B0 baseline (vanilla LLM on raw kubectl) | ~$1 | 2 h |
| 24 | B2a baseline (K8sGPT v0.4.21 pinned) | ~$1 | 2 h |
| 25 | B2b baseline (Kagent k8s-agent, A2A JSON-RPC) | $0 | 1.5 h |
| 26 | F10 hallucination judge (Claude Sonnet) | ~$3 | 1 h |

→ deliverable: full dataset ~3300 rows × {C1-C5 + B0 + B2a + B2b} × {LLM-A,
LLM-B}, cross-LLM RQ4 derivable, two practitioner-tool baselines, F10
hallucination judged.

### Phase 3 — Synthesis (tomorrow, ~1 h)

| # | Task | Time |
|---|------|------|
| 27 | Write `EVALUATION-DRAFT.md` (article §Evaluation skeleton) | 1 h |
| 28 | **[HUMAN]** Cohen κ on 20% shuffled subsample | 2 h pair-review |

→ deliverable: drop-in Evaluation section for the article with honest
threats-to-validity, plus a κ table once the human reviewer completes #28.

---

## 2. Locked decisions

| Decision | Choice | Where |
|---|---|---|
| External baselines | **K8sGPT AND Kagent k8s-agent** (not just one) | §1 Phase 2 |
| LLM-B | Llama-3.3-70B-Instruct-Turbo via Together.ai | execution-plan §3 |
| Hallucination judge | Claude Sonnet via Anthropic API | analysis-plan §11 |
| Multi-app S2-S5 | **SKIP** for this paper, future-work | §6 below |
| Cluster | AKS only (current `idp-preview-test`), no migration to kind | user pref |
| Annotator κ coverage | 20% shuffled subsample (analysis-plan §11) | task #28 |
| Bootstrap iterations | 10 000 (analysis-plan default) | analysis-plan §11 |
| FWER correction | Holm-Bonferroni p<0.05 across 4 pre-registered pairs | analysis-plan §1 L5 |
| FDR correction | Benjamini-Hochberg ≤0.10 on exploratory pairs | analysis-plan §1 L5 |
| Effect-size thresholds | A12: 0.56 small, 0.64 medium, 0.71 large | Vargha-Delaney 2000 |

---

## 3. The Kagent baseline — why it is added next to K8sGPT

Verified in-session (2026-05-23 11:25 UTC):

1. **Kagent is not in our diagnosis path.** `internal/controller/kagent.go`
   is invoked from `preview_controller.go` for *PR-comment* synthesis only;
   `internal/evidence/` and `internal/diagnosis/` do not reference it. So
   the Kagent generic `k8s-agent` is a clean external baseline, not a
   piece of our own pipeline.
2. **Kagent generic `k8s-agent` exists.** ClusterIP
   `10.0.228.250:8080`, system prompt "KubeAssist — advanced AI agent
   specialized in Kubernetes troubleshooting", tools `GetResources`,
   `DescribeResource`, `GetEvents`, `GetPodLogs`, etc. Invocation via A2A
   JSON-RPC (envelope already implemented in `kagent.go`).
3. **K8sGPT is installable in one curl.** GitHub release v0.4.21,
   OpenAI/Azure backend reuses our existing key. No setup blocker.

Reporting both in the paper lets the reviewer pick the comparator they
trust:

> *We compare against K8sGPT (the CNCF Sandbox tool widely adopted by
> Kubernetes practitioners) and against the Kagent k8s-agent (a 2025
> agentic-tool platform with tool-use, deployed alongside our operator on
> the same cluster). The former is the standard literature comparator;
> the latter is the strongest contemporary practitioner baseline. Both
> are run on the same failed namespaces with no operator pre-processing,
> matching analysis-plan §8 definition of B2.*

If Kagent beats K8sGPT (or our pipeline) on some scenarios, that is a
finding we report honestly — it does not invalidate the operator's
contribution on the other metrics.

---

## 4. What I am NOT going to do (and why)

- **Multi-app S2-S5 (#14, #15 — deleted).** 15 h cluster + ~$20 Azure for
  an additional generalization claim. The marginal value vs LLM-B and the
  two practitioner baselines is lower at the Q1 bar, and multi-app
  introduces a high probability of new per-stack debug rounds (we already
  burned 3-4 h debugging F4, F6, F7 on S1 alone). Documented as future
  work in the article's discussion.
- **Fabricate numbers** for empty cells. Every CSV row must come from a
  real run, per execution-plan §8. Missing data is reported as TODO with
  an honest threat-to-validity entry.
- **Tweak the operator** in the middle of Phase 5b/c/d. The freeze policy
  in execution-plan §4 forbids edits to `internal/evidence/`,
  `internal/controller/preview_controller.go`, `scenarios.yaml`, etc. The
  only allowed edits are the LLM-B integration files (`together.go`).
- **Pre-decide the conclusions.** If the data says "rule diagnoser wins,
  LLM under-performs on vocabulary", that is the paper. Pre-registered
  analysis plan exists precisely so we cannot massage the story to fit a
  preconception.

---

## 5. Honest assessment of expected story

After all of Phase 1-2-3 finishes, the most likely article narrative is:

> "On a single-application AKS deployment with 10 fault-injection scenarios
> and 5 evidence configurations, the operator captures FailureReports
> ahead of teardown at **100% rate, 0/100 lost across 10/10 scenarios
> after the test-Job deadline + FullSuite-bypass fixes** (RQ1, strong).
> Bundle survival across namespace teardown is **100%**.
>
> Diagnostic accuracy is gated by **evidence completeness rather than
> diagnoser sophistication**: the rule-based diagnoser on C1 evidence
> reaches 60-100% top-1 on faults whose signal lies in the captured
> bundle (RQ2). LLM diagnosers under-perform on the strict component-
> vocabulary match (top-1 ≈ 2% on llm-grounded, 0% on llm-freeform)
> because the diagnosis text names the change file's component
> ("backend") rather than the abstract role label ("api-endpoint") —
> this miss is offline-fixable with a vocabulary-alignment pass and
> closes most of the gap.
>
> Cross-LLM (gpt-4o-mini vs Llama-3.3-70B) shows the same vocabulary
> miss in both models, so the issue is **the strict matcher**, not the
> model family (RQ4 partial; full RQ4 requires the Cohen-κ subsample).
>
> Against K8sGPT and Kagent k8s-agent practitioner baselines on the same
> failed namespaces, the operator's evidence bundle reduces the
> diagnosis surface to a labeled, structured artifact — measured here as
> [specific table to be filled in]. The two practitioner tools and our
> pipeline trade strengths per scenario family; the operator's
> contribution is the **capture-before-teardown** guarantee that
> practitioner tools cannot match by construction.
>
> Overhead is **negligible** (median CPU added per preview < 5%, bundle
> size < 16 KB per failure) (RQ5)."

That is a publishable, honest, single-application study. Multi-app
generalization is future work.

---

## 6. Why this scoping is defensible

A single-application study with `(2 LLMs) × (5 + 3 baseline) configurations
× 10 scenarios × 10 reps` is **already** a within-subject design with
~2000 cells. That is denser than RCAEval's per-system splits and
comparable to OpenRCA's per-system tables. The journal/conference
expectation for a first results paper on a new artefact (the operator-
side capture + provenance graph) is *one* deeply-instrumented application
with cross-tool baselines, not breadth across stacks with shallow
baselines. Multi-app would strengthen the generalization claim but is
not required to publish a credible artefact-evaluation paper.

The threats-to-validity table will explicitly call out: "single
application (Flask Python); generalization to other stacks (Go, Django,
Next.js, Spring Boot) is future work, demonstrated as feasible by the
existing multi-app orchestrator (`run-matrix.py`, see
`docs/research/failure-provenance/multi-app-plan.md`)."
