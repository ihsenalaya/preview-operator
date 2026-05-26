# RQ2 — Generalized Linear Mixed-Effects Model (Logit)

Generated from `results-rescored.csv` (n = 1500 rows, S1 only).

## Model spec

```
top1_aligned ~ C(configuration) * C(engine) + (1 | scenario_id)
family = Binomial(), link = logit
```

Binary outcome `top1_aligned` (0/1) — correctly identifies the failed component after the per-scenario alias matcher. Reference levels: `configuration = C1`, `engine = llm-freeform`.

## Odds ratios (logit GLM cross-check with scenario as fixed effect)

| Term | Estimate (logit) | Odds ratio | 95 % CI | p |
|---|---:|---:|---:|---:|
| `configuration[T.C2]` | -0.488 | 0.61 | [0.20, 1.89] | 0.3945 |
| `configuration[T.C3]` | +1.048 | 2.85 | [1.08, 7.53] | 0.0343 |
| `configuration[T.C4]` | +2.118 | 8.32 | [3.25, 21.28] | 0.0000 |
| `configuration[T.C5]` | +1.888 | 6.61 | [2.58, 16.93] | 0.0001 |
| `engine[T.llm-grounded]` | +0.000 | 1.00 | [0.34, 2.90] | 1.0000 |
| `engine[T.rule-grounded]` | -0.674 | 0.51 | [0.16, 1.61] | 0.2501 |
| `configuration[T.C2]:engine[T.llm-grounded]` | +0.631 | 1.88 | [0.40, 8.75] | 0.4212 |
| `configuration[T.C3]:engine[T.llm-grounded]` | +1.070 | 2.92 | [0.76, 11.12] | 0.1172 |
| `configuration[T.C4]:engine[T.llm-grounded]` | +0.615 | 1.85 | [0.50, 6.89] | 0.3595 |
| `configuration[T.C5]:engine[T.llm-grounded]` | +1.171 | 3.22 | [0.86, 12.15] | 0.0836 |
| `configuration[T.C2]:engine[T.rule-grounded]` | +1.909 | 6.75 | [1.39, 32.70] | 0.0177 |
| `configuration[T.C3]:engine[T.rule-grounded]` | +3.662 | 38.92 | [8.89, 170.45] | 0.0000 |
| `configuration[T.C4]:engine[T.rule-grounded]` | +2.591 | 13.35 | [3.15, 56.62] | 0.0004 |
| `configuration[T.C5]:engine[T.rule-grounded]` | +2.821 | 16.80 | [3.95, 71.49] | 0.0001 |

## Headline reading

- Reference cell: `C1 / llm-freeform`. The `Intercept` reflects the baseline log-odds of a correct alignment-matched diagnosis on F1 (lowest-ID scenario).
- A `C(configuration)[T.Cx]` coefficient > 0 means *adding evidence at level x increases* the odds of correctness vs C1, all else equal.
- A `C(engine)[T.rule-grounded]` coefficient > 0 means *the rule diagnoser is more accurate than the free-form LLM* at the reference configuration.
- Interaction terms `C(configuration)[T.Cx]:C(engine)[T.eng]` quantify how the evidence-level effect *differs* between engines.

## Why this supersedes `21-lmm.py`

`21-lmm.py` used `MixedLM` (Gaussian / identity link) on a binary outcome. While the coefficients there are unbiased on average, the standard errors and p-values are not correctly calibrated for binary data — predicted probabilities can fall outside [0,1] and the residual variance is mis-specified. The GLMM above uses the correct Binomial family with logit link and is the model the pre-registered analysis plan (`analysis-plan.md §1 L3`) commits to.

Source CSV: `analysis-output/08-vocab-rescore/results-rescored.csv` (S1 only).
Script: `experiments/failure-provenance/analysis/21b-glmm-logit.py`.
