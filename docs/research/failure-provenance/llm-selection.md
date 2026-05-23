# LLM Selection for the Diagnostic Harness

This document records the criteria, the rationale, and the limitations of the LLM
selection for the diagnostic harness used in the failure-provenance evaluation
(Contribution C3 — *Operator-Controlled, Evidence-Grounded RCA* — and research
question RQ4 — *Hallucination Control*).

It is referenced from `methodology.md` and will be cited in the article's
methodology section.

---

## 1. Why this document exists

The "evidence-grounded RCA" contribution (C3) and the hallucination-control
question (RQ4) depend directly on LLM behaviour. **A single-LLM evaluation cannot
disentangle whether the grounding constraint is the mechanism reducing hallucination
or whether the result is an artefact of the chosen model.** The community standard
for LLM-based diagnostic / RCA work therefore evaluates across multiple LLMs:

- **OpenRCA (ICLR 2025)** [`openrca_2025`] — the closest published benchmark for
  LLM-based RCA on software failures — evaluates **six** LLMs spanning three
  proprietary models (Claude 3.5, GPT-4o, Gemini 1.5 Pro) and three open-source
  models (Mistral Large 2, Command R+, Llama 3.1 Instruct).
- The **SE empirical-study guidelines for LLMs** [`se_llm_guidelines`,
  arXiv:2508.15503] explicitly recommend:
  - **Guideline 2** — report exact model versions, configurations, and
    customisations.
  - **Guideline 6** — *include an open LLM as a baseline*.
  - **Guideline 7** — use suitable baselines, benchmarks, and metrics.
- Open-source LLMs are documented to confer specific reproducibility advantages
  over closed-source ones (no opaque hyperparameter / version drift)
  [`llm_open_advantage`, arXiv:2412.12004].

This study evaluates **two LLMs** — a deliberate trade-off between methodological
strength and experimental cost. The criteria below justify the choice and document
its limitations honestly.

---

## 2. Selection criteria (synthesised from the literature)

| # | Criterion | Why it matters | Source |
|---|---|---|---|
| C1 | **Provider diversity** | Different providers use different training data, alignment methods, and RLHF processes; varying the provider helps show the *mechanism* (grounding) is what reduces hallucination, not the specifics of one training pipeline. | [`openrca_2025`] |
| C2 | **Open vs closed source** | Closed-source LLMs pose reproducibility problems (no access to weights, version drift, deprecation); including an open LLM provides a reproducibility floor. | [`se_llm_guidelines`, Guideline 6] [`llm_open_advantage`] |
| C3 | **Comparable capability tier** | Comparing across capability extremes confounds the *grounding* effect with raw model capability; the chosen models should be roughly comparable to keep RQ4 interpretable. | [`llm_eval_app_driven`] |
| C4 | **Versioned, deterministic access** | The model must support a pinned version identifier and `temperature=0` to be reproducible across runs and re-runs by independent researchers. | [`se_llm_guidelines`, Guideline 2] |
| C5 | **Sufficient context window** | A complete failure evidence bundle (PR diff, K8s state, log excerpts, events, telemetry, conditions) can exceed 20k tokens; the model must accept this without truncation. | study-specific |
| C6 | **Cost feasibility** | The evaluation calls the LLM many times per scenario × configuration × repetition; per-call cost dominates the experimental budget. | study-specific |
| C7 | **API stability** | Multi-week evaluation campaigns require a stable endpoint; experimental/preview endpoints risk mid-experiment version drift. | study-specific |

---

## 3. The two selected LLMs

| Aspect | LLM-A (primary, retained) | LLM-B (added) |
|---|---|---|
| Model identifier | `gpt-4o-mini-2024-07-18` | `meta-llama/Llama-3.3-70B-Instruct-Turbo` |
| Provider | OpenAI | Meta (open weights), served via Together.ai (alternative: Groq) |
| Source type | **Closed-source / proprietary** | **Open-source (weights public)** |
| Context window | 128k tokens | 128k tokens |
| Capability tier | mid-tier frontier-adjacent | mid-tier frontier (open) |
| Temperature pinned | `0` | `0` |
| Reproducibility | conditional on OpenAI not deprecating the version | weights public; identical model can be self-hosted |
| Cost (May 2026) | ≈ $0.15 / 1M input tokens, $0.60 / 1M output | ≈ $0.88 / 1M tokens (Together.ai, May 2026) |

This pair satisfies all seven criteria:

| Criterion | LLM-A | LLM-B | Coverage |
|---|---|---|---|
| C1 Provider diversity | OpenAI | Meta / Together.ai | ✅ two independent training lineages |
| **C2 Open + closed source** | closed | **open** | ✅ **Guideline 6 satisfied** |
| C3 Capability tier | mid frontier-adjacent | mid frontier | ✅ comparable |
| C4 Versioned + temp 0 | ✅ | ✅ | ✅ |
| C5 ≥ 20k context | 128k ✅ | 128k ✅ | ✅ |
| C6 Cost feasible | ✅ | ✅ | ✅ |
| C7 API stability | ✅ stable | ✅ stable (Together.ai is established) | ✅ |

---

## 4. Rationale — why exactly these two

- **GPT-4o-mini** is the primary diagnostic model already used throughout the
  existing matrix-attempt 4–6 runs (commits `b4b6817…`). Retaining it as one of the
  two preserves continuity with results already produced and minimises rework.
- **Llama 3.3 70B Instruct** was selected as the second model because it:
  - is **open-source** (weights publicly released by Meta), directly satisfying
    Guideline 6 [`se_llm_guidelines`];
  - comes from a **different provider lineage** (Meta, with independent training
    data and post-training pipeline);
  - is at a **comparable capability tier** to GPT-4o-mini for SE/RCA tasks
    (both mid-tier frontier-class — see e.g. OpenRCA leaderboard for relative
    performance on RCA-style tasks [`openrca_2025`]);
  - is accessible via stable hosted endpoints (Together.ai, Groq) at a cost
    competitive with proprietary APIs, without requiring our own GPU
    infrastructure (which would be a confound on overhead measurements for RQ5);
  - has wide ecosystem stability — released by a major AI lab, broadly hosted,
    long support track record relative to other open models.

### Why not three LLMs?

Three LLMs (e.g. adding Claude Sonnet) would further strengthen the
generalisability claim and approach the OpenRCA precedent of six. The decision to
stop at two reflects:
- The LLM-in-the-loop matrix is run only on the **primary application
  (idp-preview)** — *not* crossed with the multi-app subjects (`S2–S5`), which use
  only LLM-A. This partition is documented in `methodology.md` and prevents
  combinatorial explosion of the experimental budget.
- Two LLMs across the closed/open boundary is the **minimum credible
  configuration** that distinguishes "the grounding constraint is the mechanism"
  from "the result is a property of one model family".
- Adding a third would multiply the LLM-in-the-loop runs by 1.5× for marginal
  gain on the central claim. It is identified as future work in §6.

### Why Llama and not Mistral / DeepSeek / Command R+?

All four are defensible open-source choices. Llama 3.3 was selected for:
- **Ecosystem breadth**: more hosted endpoints, broader replication possibility.
- **Reproducibility track**: longer history of versioned, archived weights.
- **Documented context window of 128k**, matching LLM-A.

A study comparing additional open models is in scope for an extended replication
package; the two-LLM choice is designed to minimise scope creep while satisfying
the methodological floor.

### Why not Claude Sonnet (Anthropic)?

Claude Sonnet was considered. It would provide additional **provider diversity**
(OpenAI → Anthropic) at the cost of remaining within the closed-source category.
Llama was preferred over Claude Sonnet because Guideline 6 [`se_llm_guidelines`]
specifically calls for the open-source dimension; that dimension is more
methodologically valuable, for our claim, than a second closed-source proprietary
model. If the closed/open dimension is later judged less important for the chosen
venue, Claude Sonnet remains a viable substitute.

---

## 5. Limitations acknowledged

- **No frontier-tier evaluation.** Neither GPT-4o (full) nor Claude Opus 4.5 nor
  Gemini 2.x is included. Results on larger frontier models could differ in either
  direction — more capacity might either reduce baseline hallucination (making the
  grounding constraint less impactful) or expose new failure modes (making it more
  necessary). Reported in *threats to validity* (`threats-to-validity.md` §1).
- **Two LLMs is the minimum, not the complete picture.** A full six-LLM
  evaluation as in OpenRCA would be stronger; two LLMs across the closed/open
  boundary is the *minimum that lets RQ4 distinguish "mechanism" from "model
  artefact"*. The hallucination-reduction effect is asserted with this caveat.
- **Hosted Llama endpoint dependence.** Llama 3.3 is open-weights, but our
  evaluation calls a hosted endpoint (Together.ai) for cost reasons. A self-hosted
  replication on our AKS cluster (using the existing GPU-less node pool would
  require additional hardware) would remove that dependence — future work.
- **No small / distilled model.** It is plausible that the grounding constraint
  helps weaker models proportionally more (more upside) or less (less able to
  follow the constraint). Not tested here.
- **Version drift risk for LLM-A.** GPT-4o-mini snapshot
  `gpt-4o-mini-2024-07-18` is pinned in every result row, but OpenAI may
  eventually retire it; the replication package documents the snapshot for
  archive purposes.

---

## 6. Reproducibility — what each result row records

Every row in `experiments/failure-provenance/results-template.csv` records, in
the `notes` column:

```
model=<exact-id>; temperature=0; provider=<endpoint>;
called_at=<UTC-ISO8601>
```

For example:
```
model=gpt-4o-mini-2024-07-18; temperature=0; provider=api.openai.com;
  called_at=2026-05-23T09:14:02Z
model=meta-llama/Llama-3.3-70B-Instruct-Turbo; temperature=0;
  provider=api.together.xyz; called_at=2026-05-23T09:14:11Z
```

This satisfies Guideline 2 [`se_llm_guidelines`] and supports auditability across
the multi-month evaluation window.

---

## 7. Future work

- Extend to **3-6 LLMs** for a published replication / extension study.
- Add a **self-hosted Llama** replication to remove the hosted-endpoint
  dependence.
- Add a **frontier-tier model** (GPT-4o, Claude Opus, Gemini 2) to test whether
  the grounding effect scales with capability.
- Add a **small / distilled model** (e.g. Llama 3.1 8B or Phi-4) to test whether
  the grounding effect is stronger or weaker on lower-capacity models.

---

## 8. References

- `openrca_2025` — *OpenRCA: Can Large Language Models Locate the Root Cause of
  Software Failures?* ICLR 2025. <https://openreview.net/forum?id=M4qNIzQYpd>
- `se_llm_guidelines` — *Evaluation Guidelines for Empirical Studies in Software
  Engineering involving LLMs*. arXiv:2508.15503.
  <https://arxiv.org/abs/2508.15503> · living resource: <https://llm-guidelines.org>
- `llm_eval_app_driven` — *A-Eval: Application-driven Evaluation for Large
  Language Models*. arXiv:2406.10307. <https://arxiv.org/abs/2406.10307>
- `llm_open_advantage` — Manchanda, J. et al. *The Open-Source Advantage in Large
  Language Models*. arXiv:2412.12004. <https://arxiv.org/abs/2412.12004>

(These BibTeX entries are to be added to `bibliography.bib` — currently
`TODO_VERIFY` for the four kubernetes.io entries already verified; the four
references above are new and authoritative.)
