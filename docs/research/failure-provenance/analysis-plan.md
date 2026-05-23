# Analysis & Interpretation Plan

This document specifies *how* the results produced by the experiment harness
(`experiments/failure-provenance/`) will be analyzed and interpreted, **before**
the data is in. Locking the analysis plan ahead of the run is required to avoid
selective reporting (a known threat to conclusion validity — see
`threats-to-validity.md` and [`wohlin_experimentation`]).

It is paired with `metrics.md` (what is measured) and `methodology.md` (how runs
are produced). This file answers: *given a results CSV with one row per run,
which numbers do we compute, which tests do we run, and how do we read the
output?*

The experimental design is a **within-subject / repeated-measures matrix**:
10 scenarios (F1–F10) × 5 configurations (C1–C5) × ≥10 repetitions × 2 LLMs
(LLM-A = `gpt-4o-mini`, LLM-B = open-source per `llm-selection.md`).

---

## 1. Hierarchy of analyses (read top-down)

| Layer | Output | Source |
|---|---|---|
| **L1 — Descriptive** | Means, medians, proportions, raw counts; per (scenario × configuration × LLM) cell | every metric in `metrics.md` |
| **L2 — Uncertainty** | Confidence intervals around every reported number | Wilson 95 % for proportions [`wilson_score_ci`]; bootstrap percentile 95 % for non-proportion means |
| **L3 — Primary inferential model** | Effect of configuration on the outcome, controlling for scenario / run / LLM as random effects, with an explicit `configuration:LLM` interaction term | **Linear / generalized linear mixed-effects model** [`bates_lme4_2015`, `baayen_2008`, `brown_lmm_2021`] |
| **L3b — Robustness check** | Same omnibus question with a non-parametric alternative; reported alongside L3 | Friedman test on ranks across C1–C5, per metric [`demsar_2006`] |
| **L4 — Pairwise contrasts** | Which configurations differ from which, with what magnitude? | Estimated marginal means (`emmeans`) from the LMM with Tukey or Bonferroni adjustment; cross-checked by Wilcoxon signed-rank + Vargha–Delaney A12 effect size [`arcuri_briand_2014`, `vargha_delaney_2000`] |
| **L5 — Multiple-comparison control** | Family-wise α correction across the pairwise tests | Holm–Bonferroni step-down (FWER) [`holm_1979`]; Benjamini–Hochberg (FDR) for the exploratory secondary metrics [`benjamini_hochberg_1995`] |
| **L6 — Inter-rater check** | Trustworthiness of every metric that depends on human/LLM scoring | Cohen's κ between the two annotators on the same shuffled sample; report agreement and disagreements [`mchugh_kappa_2012`, `inter_coder_se_2023`] |
| **L7 — Cross-LLM consistency** | Does the effect of evidence configuration replicate across LLM-A and LLM-B? | Read off the `configuration:LLM` interaction term from L3; supplement with the per-LLM L3b/L4 side-by-side plot. A statistically significant interaction *is* the cross-LLM finding for RQ4 |

**Why a mixed-effects model as primary.** The design has crossed/nested random
sources of variance (scenario, repetition, LLM, configuration) that a paired
Friedman test cannot fully express. Mixed-effects models — the de facto
standard for repeated-measures with crossed random factors since
Baayen et al. 2008 [`baayen_2008`] — let us include `(1|scenario)` and
`(1|run)` as random intercepts and model the `configuration:LLM` interaction
that RQ4 hinges on. Friedman is kept as a non-parametric robustness check;
the two should tell the same story.

No statistical test is run at L3–L5 unless L1+L2 already show a non-trivial
descriptive difference — significance without effect size is not a result.

---

## 2. Per-research-question analysis recipe

### RQ1 — Evidence Preservation

- **Primary metrics.** `EvidencePreservationRate` (`metric 1`); `SnapshotSurvivalRate`
  (`metric 2`); `EvidenceRecall` (`metric 7`); `IdempotencyOK` (`metric 11`).
- **Cell.** One cell per (scenario × {bundle = present | C1 evidence-off}).
- **Reporting.** Per-scenario table; Wilson 95 % CI on each ratio.
- **No inferential test** is run on RQ1 in C4/C5 — the *capture* is operator
  behaviour, not a comparison between configurations. RQ1 fails if a cell drops
  below the pre-registered threshold (≥ 0.9 for pod/job/event evidence, exactly
  1.0 for survival of the persisted artifact).

### RQ2 — RCA Accuracy

- **Primary metric.** `Top1Accuracy` (`metric 3`). **Secondary:** `Top3Accuracy`
  (`metric 4`), `EvidencePrecision` (`metric 6`), `EvidenceRecall` (`metric 7`),
  `RecommendationUsefulness` (`metric 9`).
- **Design.** Within-subject across C1–C5, per scenario, per LLM. Each
  (scenario × LLM) gives ≥10 paired observations.
- **Confidence interval.** Wilson 95 % per (scenario × C × LLM) cell.
- **Primary inferential model.** Generalized linear mixed-effects model
  (binomial family, logit link) on the per-run correct/incorrect outcome
  [`bates_lme4_2015`, `baayen_2008`]:
  ```
  correct ~ configuration * LLM + (1 | scenario) + (1 | run_id)
  ```
  Read off main effects of `configuration` and `LLM`, the
  `configuration:LLM` interaction (the heart of RQ4 cross-LLM
  generalization), and per-scenario random-effect variance. Estimated
  marginal means and pairwise contrasts with Tukey adjustment via `emmeans`.
- **Robustness check.** Friedman omnibus on per-scenario mean accuracy across
  C1–C5 (k = 5 conditions, n = 10 scenarios) [`demsar_2006`], followed by
  Wilcoxon signed-rank on the *contrast pairs that are pre-registered*:
  C1 vs C4, C4 vs C5, C1 vs C5, C3 vs C4. **Not** all 10 unordered pairs
  (this preserves statistical power). Mixed model and non-parametric
  results must point in the same direction; any divergence is treated as
  a finding to investigate, not hidden.
- **Effect size.** Vargha–Delaney A12 on each pre-registered pair.
  Interpretation thresholds from the original paper: 0.56 = small,
  0.64 = medium, 0.71 = large [`vargha_delaney_2000`]. Report alongside
  p-values; A12 close to 0.5 means "no practical difference" *regardless of
  p-value*.
- **Multiple comparisons.** Holm–Bonferroni across the 4 pre-registered pairs
  (FWER ≤ 0.05). The exploratory pairs across the remaining 6 contrasts use
  Benjamini–Hochberg (FDR ≤ 0.10) and are reported as secondary.
- **Per-scenario reporting.** Beyond the global LMM, **also report a
  per-scenario summary table**: for each (F1…F10, LLM) cell, the winning
  configuration in Top-1 accuracy, the C5-vs-C1 gap with Wilson 95 % CI, and
  a flag for scenarios where the provenance graph (C5) does **not**
  outperform C4. This per-scenario view is what a reviewer needs to see to
  trust the global effect [`kitchenham_2002`].
- **Visualization.** Demšar critical-difference (CD) diagram across C1–C5
  (one line per LLM) [`demsar_2006`]; plus a forest plot of per-scenario
  C5−C1 differences with CIs.

### RQ3 — Time to Diagnosis (MTTD)

- **Primary metric.** `MTTD` per cell (`metric 5`).
- **Design.** Same within-subject matrix; *MTTD distributions are positively
  skewed*, so report **median + IQR + bootstrap 95 % CI on the median**, not
  mean ± SD. The mean is reported only as a secondary line.
- **Omnibus.** Friedman on per-scenario median MTTD across C1–C5.
- **Post-hoc.** Wilcoxon signed-rank for pre-registered contrast pairs as in
  RQ2; A12 effect size; Holm correction. Same effect-size thresholds.
- **Interpretation rule.** A statistically significant MTTD increase with
  A12 < 0.56 between C4 and C5 is reported as a *practically negligible*
  cost of the provenance graph and is **not** treated as a regression.

### RQ4 — Hallucination Control

- **Primary metric.** `HallucinationRate` (`metric 8`).
- **Crucial sub-design.** RQ4 is the *orthogonal grounded × free-form ablation*
  on top of C4 and C5 (`methodology.md` §5). The cell key is
  `(scenario × {C4, C5} × {grounded, free-form} × LLM)`.
- **Scoring is human-and-LLM-assisted.** Each diagnosis is decomposed into
  atomic claims; each claim is annotated as supported / unsupported. Two
  annotators per row on a 20 % shuffled subsample. Report **Cohen's κ** and
  the per-category disagreement breakdown [`mchugh_kappa_2012`]. Annotation
  disagreements above κ < 0.6 *block* the corresponding row from the primary
  analysis (move it to a sensitivity table) [`inter_coder_se_2023`].
- **LLM-as-judge caveat.** If part of the annotation is delegated to an LLM
  judge (e.g. claim-supported-by-evidence checking), apply the bias controls
  from [`shi_position_bias_2024`]: randomize evidence-vs-claim presentation
  order; *do not* use the same LLM family as the diagnostic step
  (LLM-A diagnoses ⇒ LLM-B judges, and vice-versa); report the **judge ID and
  version** in every row of the results CSV.
- **Faithfulness benchmarking framing.** Hallucination is positioned as a
  *faithfulness* problem in the sense of [`tamber_faithjudge_2025`] — claims
  that contradict, ignore, or extrapolate beyond the evidence bundle.
- **Statistical comparison.** Within (scenario × LLM × {C4, C5}), McNemar's
  test on the matched grounded-vs-free-form binary correctness; report the
  odds ratio and its 95 % CI.
- **Negative control.** **F10 (flaky test)** is the bedrock of RQ4
  interpretation: a confidently asserted component cause on F10 is a
  hallucination by construction. F10 hallucination rate must be reported
  *separately and unaggregated*.

### RQ5 — Runtime Overhead

- **Primary metric.** `CollectionOverhead` (`metric 10`) split into
  time / CPU / memory / storage / API-call components.
- **Design.** Paired: each scenario run twice on the same idle host, one with
  `EVIDENCE_COLLECTION=on`, one `off`. Concurrency variants (1 / 5 / 10
  concurrent PRs) are reported in a separate table — never pooled with the
  isolated overhead.
- **Reporting.** Median + IQR + bootstrap CI. Plus a **bytes-per-evidence-item**
  derived figure for the storage component (storage scales with bundle size,
  not with run time).
- **Effect-size interpretation.** Differences below a pre-registered floor
  (5 % of preview total wall-clock; 10 MiB persisted storage; 50 API calls
  per failure) are reported as *practically negligible* even if statistically
  significant.

---

## 3. Two-LLM cross-validation (the heart of the RQ4 claim)

The two-LLM design from `llm-selection.md` exists to disentangle "the
grounding constraint is the mechanism" from "the result is a property of one
model family". The interpretation rule, written ahead of the data:

| Pattern observed across both LLMs | Conclusion |
|---|---|
| Both LLMs show C5/C4 hallucination << C1, with comparable effect sizes | Mechanism is the grounding/structure — strong RQ4 evidence |
| One LLM shows the effect, the other does not | Mechanism is **model-specific**; RQ4 is reported as conditional and the cross-LLM gap becomes the finding |
| Neither LLM shows the effect | RQ4 hypothesis rejected; the bundle/graph is not the lever for hallucination |
| Effect appears stronger on the open LLM | Stronger reproducibility claim — the lever does not require frontier capacity |
| Effect appears stronger on the closed LLM | Mechanism may interact with model capability; future work flagged in §6 |

This pre-registered table is what we will fill in once both LLMs have run.

---

## 4. Reporting format (what the article tables and figures must look like)

1. **Per-metric headline table:** rows = scenarios, columns = (C1, C2, C3, C4,
   C5) × (LLM-A, LLM-B). Each cell: mean ± Wilson 95 % CI for proportions,
   median (IQR) for durations / bytes. **No bare numbers.**
2. **Per-RQ CD diagram:** one for accuracy, one for MTTD, one for
   hallucination. Required to render the post-hoc structure compactly
   [`demsar_2006`].
3. **Hallucination subsection has its own table:** rows = scenarios incl. F10
   as a separate block, columns = {C4, C5} × {grounded, free-form} × LLM.
4. **Threats-to-validity table** lists, per threat, what was *measured* to
   bound it (annotator κ, Wilson CI width, A12, McNemar OR), not just text
   mitigations. Aligned with [`wohlin_experimentation`].
5. **Reproducibility appendix.** Every row in the CSV carries
   `(scenario_id, run_id, cluster_type, configuration, llm_model_version,
   judge_model_version, called_at_utc)`. Tables in the paper *must* be
   re-derivable from the CSV with the published scoring code.

---

## 5. Pre-registered thresholds (committed before any data is unblinded)

- **Annotator κ floor:** 0.6. Below this, the row is moved to a sensitivity
  table, not the primary analysis [`mchugh_kappa_2012`].
- **Effect-size interpretation:** A12 ≥ 0.56 small, ≥ 0.64 medium, ≥ 0.71
  large [`vargha_delaney_2000`].
- **Primary FWER:** Holm-corrected p < 0.05 across the 4 pre-registered
  pairwise contrasts per RQ [`holm_1979`].
- **Secondary FDR:** Benjamini-Hochberg ≤ 0.10 on all exploratory pairs
  [`benjamini_hochberg_1995`].
- **MTTD / overhead "practical negligibility" floors:** as in §2 above —
  *significance without crossing the floor is reported as no effect*.

---

## 6. What can and cannot be claimed (interpretation guardrails)

- **Can claim:** that the operator captures evidence ahead of teardown at
  measured rates, with measured idempotency and survival; that, *for the
  two studied LLMs and the ten studied fault scenarios*, the evidence
  bundle / provenance graph affects Top-K accuracy, MTTD, and hallucination
  in the directions reported, with effect sizes within the reported CIs.
- **Cannot claim:** generality beyond the ten scenarios; generality across
  LLM families not in the study (frontier models excluded — see
  `llm-selection.md`); independence from the test workload of the demo
  application; statistical *significance* without effect size; or that
  hallucination is "eliminated" — only the *rate* under the studied
  constraints is reported [`tamber_faithjudge_2025`].
- **Must report regardless of direction:** any LLM-as-judge order-effects
  uncovered by the position-bias control [`shi_position_bias_2024`], and
  any cell where the two annotators disagreed before adjudication.

---

## 7. Comparable benchmarks (for the discussion section, *not* head-to-head)

Direct head-to-head numerical comparison against AIOps RCA benchmarks is
**not** in scope — those benchmarks target a different task surface
(production telemetry, not change-scoped ephemeral previews). We position
*qualitatively* against:

- **RCAEval** [`rcaeval_2024`] — 735 failure cases, three microservice
  systems; supplies the vocabulary of Top-k accuracy / Avg@5 / MRR
  that we adopt for `metric 3`/`metric 4`.
- **OpenRCA** [`openrca_2025`] — 335 failures, telemetry-only LLM RCA on
  enterprise systems; the source of the multi-LLM evaluation precedent that
  motivates `llm-selection.md`.
- **Eadro** [`eadro_2023`] — multi-source troubleshooting (logs + metrics +
  traces) on microservice benchmarks; the closest end-to-end framework, but
  *not* ephemeral / change-scoped.
- **RCACopilot** [`rcacopilot_2024`] — LLM-based incident RCA in production
  at Microsoft, deployed at scale; the closest *production* analogue and a
  good reality check on Macro-F1 magnitudes for category prediction.

The discussion section uses these to *contextualize* our numbers (e.g. our
Top-1 should be read against RCAEval Top-1, not against OpenRCA's much harder
telemetry-only setting). No common dataset, so no leaderboard claim is made.

---

## 8. External baselines (the Q1 gap)

Comparing only C1 → C5 is an *internal ablation*: it shows our pipeline
benefits from richer evidence, but it does **not** show the pipeline is
better than what a developer or an existing tool would do today. A reviewer
at a top venue will (correctly) call this "comparing a method against
weakened versions of itself" — the standard answer in empirical SE
[`kitchenham_2002`, `wohlin_experimentation`] is to add at least one
*external* baseline.

We therefore add three baselines, ordered from cheapest to costliest:

| Baseline | What the diagnoser sees | Why this baseline matters |
|---|---|---|
| **B0 — Vanilla LLM** | Raw `kubectl logs` + `kubectl describe` + `kubectl get events` output for the failing namespace, no FailureReport, no graph, no operator-side structuring. Same LLM (LLM-A or LLM-B), same prompt template up to evidence formatting. | Isolates the contribution of operator-side capture + structuring from the LLM itself. A C5 → B0 gap is the clean "operator vs no operator" answer. |
| **B1 — Rule-based diagnoser** | The full evidence bundle (= C4) but the diagnostic step is the **existing rule-based diagnoser** [`internal/diagnosis/rules.go`], not an LLM. | Isolates the "structured evidence is enough" claim from any LLM contribution. If B1 already approaches C5, the LLM is doing less work than it looks. |
| **B2 — `K8sGPT`-style external tool** | The same failing namespace handed to an off-the-shelf CNCF Kubernetes-RCA tool (K8sGPT [`k8sgpt_cncf`], pinned version), with no operator pre-processing. | Establishes how the proposed approach compares to a *deployed practitioner tool* on the same fault. K8sGPT is rule-based + LLM-explainer (CNCF Sandbox), not peer-reviewed, but it is the closest off-the-shelf comparator developers actually use today. |

Optional B3 (devops-on-raw-logs, 1–2 developers per fault on a subsample) is
desirable but expensive; flag it as "if resources permit, on a 20 % shuffled
subsample, with κ between the two devops reported". Without B3, B0 + B1 + B2
are the **minimum credible external comparison set** for a Q1 submission.

**Statistical handling.** Treat each baseline as one additional level of the
`configuration` factor in the L3 mixed-effects model (extending C1–C5 to
C1–C5 + B0 + B1 + B2). The pre-registered contrast pairs that matter for the
external claim are: **C5 vs B0**, **C4 vs B1**, **C5 vs B2**. The other
baseline pairs are exploratory (BH-FDR).

**Honesty caveats.**
- B0 and B1 are within our infrastructure but outside our contribution
  (we built the rule diagnoser but it pre-dates the FailureReport idea).
- B2 (K8sGPT) is a moving target: pin the exact released version, log the
  invocation, and treat it as a *contemporaneous* practitioner baseline,
  not a peer-reviewed comparator.
- No common dataset exists with RCAEval / OpenRCA / Eadro / RCACopilot, so
  those remain *qualitative* contextualization (`analysis-plan.md` §7), not
  numerical baselines.

---

## 9. Cost-benefit synthesis (RQ2 × RQ5)

RQ2 (accuracy) and RQ5 (overhead) are measured separately. A reviewer at a
top venue expects them connected: *is the accuracy gain worth the overhead?*
We answer this with one synthesis figure and one table:

- **Synthesis table.** Per configuration (C1…C5 + B0…B2), report the joint
  cell `(mean Top-1 accuracy ± Wilson CI, median CPU-seconds added per
  preview, median bytes persisted per failure)`. The C1 column is the
  reference point (overhead floor).
- **Pareto frontier figure.** Scatter plot, x = median CPU-seconds added,
  y = mean Top-1 accuracy with CI whiskers; one point per configuration per
  LLM. Annotate the dominated configurations and the Pareto frontier.
- **Headline statistic.** *Accuracy gained per CPU-second of capture* —
  derived as `(Top1(Cx) − Top1(B0)) / overhead(Cx)`. Report with bootstrap
  95 % CI for each Cx and for each LLM separately.
- **Storage line.** Bundle size in bytes does NOT scale with run wall-clock
  but with the failure surface; report `bytes-per-evidence-item` alongside.

This section answers the obvious deployment question and is also the
defensible rebuttal to "C5 only wins because you spent CPU on it" — if the
Pareto frontier still puts C5 above C1 + Bx, the answer is yes.

---

## 10. Qualitative error analysis (where C5 fails despite the graph)

Top-K numbers cannot tell us *why* the proposed approach still gets some
diagnoses wrong. Standard SE methodology [`seaman_1999`] requires a
qualitative complement on a sampled subset of failure rows.

- **Sample.** All C5 rows where the diagnosis was scored incorrect, plus a
  random 10 % sample of correctly scored C5 rows as a counterfactual.
- **Codes (open-coding, two coders).** Pre-seeded categories:
  *(a) missing evidence item* (operator failed to capture);
  *(b) ambiguous evidence* (two plausible causes);
  *(c) vocabulary mismatch* (LLM names the component differently from
  ground truth — already seen in `experimentations.md` F3/F4);
  *(d) overconfident on flaky behaviour* (especially F10);
  *(e) graph link missing* (provenance graph did not connect change → symptom);
  *(f) LLM ignored grounded evidence* — emitted a free-form-style claim
  despite C5 inputs.
- **Output.** A short coded table per failure category, with two examples
  verbatim per code (with redaction). Inter-coder agreement reported with
  Cohen's κ on the codebook [`mchugh_kappa_2012`].
- **Why this matters for Q1.** A reviewer who reads "C5 wins on average"
  next reads "where does it still fail?" If the answer is just a number, it
  reads as evasive. If the answer is six named failure modes with examples,
  it reads as honest engineering.

This is also the section that funds the future-work claims in the
conclusion: each named code is a research direction.

---

## 11. Open questions to settle *before* Phase 5 unblinds data

These remain to be decided collectively, and are blocking the lock-in:

1. **Bootstrap iterations** for median CIs: default 10 000 — keep or raise?
2. **F10 LLM judge selection:** which model arbitrates "the LLM hallucinated
   on a flaky test"? Default proposal: a different model from both LLM-A and
   LLM-B (e.g. Claude Sonnet) on a 100 %-of-F10 sample, with two-human
   adjudication on disagreements.
3. **Two-annotator coverage:** 20 % shuffled subsample for all metrics, or
   100 % only for hallucination?
4. **Cross-cluster pooling:** confirmed *not* pooled (Kind ≠ AKS); does any
   table benefit from a *combined* row labelled "Kind ∪ AKS" with explicit
   variance breakdown, or do we keep them strictly separate everywhere?
5. **Cost-budget envelope for LLM-B:** at ≈$0.88 / 1M tokens (`llm-selection.md`)
   and ~10 diagnoses per run, what is the absolute LLM-token budget across
   the full matrix run? Needed to know which cells survive a budget cut.

Each of these gets a sentence in the article methodology; settling them up
front is a precondition for the pre-registration to be meaningful.
