# RQ2 — Generalized Linear Mixed-Effects Model (Logit), S1-S5 pooled

Generated 2026-05-25T13:15:06.878750Z from `35-unified-rescore/per-subject-results.csv`.
n = 11600 rows across 5 subjects: s1-flask-catalog, s2-listmonk, s3-healthchecks, s4-umami, s5-petclinic.

## Model spec

```
top1_aligned ~ C(configuration) * C(engine)
              + (1 | subject_id) + (1 | scenario_id)
family = Binomial(), link = logit
```

Two random intercepts: one per subject (between-app variance), one per
scenario (between-fault variance). LLM-A and LLM-B are pooled into the
`llm-*` engine bucket so the contrast is grounded vs. free-form across
LLM families, not which LLM family.

## Key odds-ratios (engine × configuration interaction)

| Term | Odds ratio | 95% CI | p-value |
|---|---|---|---|
| `C(configuration)[T.C2]` | 2.296 | [1.382, 3.813] | 0.001327 |
| `C(configuration)[T.C3]` | 6.885 | [4.336, 10.930] | 2.822e-16 |
| `C(configuration)[T.C4]` | 8.392 | [5.308, 13.268] | 8.793e-20 |
| `C(configuration)[T.C5]` | 7.981 | [5.043, 12.630] | 7.386e-19 |
| `C(engine)[T.llm-grounded]` | 1.213 | [0.706, 2.084] | 0.4855 |
| `C(engine)[T.rule-grounded]` | 0.824 | [0.385, 1.761] | 0.6172 |
| `C(configuration)[T.C2]:C(engine)[T.llm-grounded]` | 0.665 | [0.336, 1.316] | 0.2414 |
| `C(configuration)[T.C3]:C(engine)[T.llm-grounded]` | 0.798 | [0.434, 1.468] | 0.4691 |
| `C(configuration)[T.C4]:C(engine)[T.llm-grounded]` | 0.937 | [0.514, 1.710] | 0.8326 |
| `C(configuration)[T.C5]:C(engine)[T.llm-grounded]` | 0.993 | [0.544, 1.813] | 0.9825 |
| `C(configuration)[T.C2]:C(engine)[T.rule-grounded]` | 2.599 | [1.083, 6.236] | 0.0324 |
| `C(configuration)[T.C3]:C(engine)[T.rule-grounded]` | 2.187 | [0.956, 5.001] | 0.06366 |
| `C(configuration)[T.C4]:C(engine)[T.rule-grounded]` | 1.794 | [0.787, 4.092] | 0.1647 |
| `C(configuration)[T.C5]:C(engine)[T.rule-grounded]` | 1.887 | [0.827, 4.306] | 0.1316 |

## Reading

- **Odds ratio > 1** means the term increases the odds of correct top-1.
- The `C(configuration)[T.Ck]:C(engine)[T.X]` interaction terms read off
  *how* the configuration × engine interaction differs from the C1 / llm-freeform baseline.
- Two random intercepts (subject + scenario) absorb between-app and between-fault variance, so
  the remaining fixed-effect estimates speak only to the C × engine main effects + interactions.

## Source

- Input: `docs/research/failure-provenance/analysis-output/35-unified-rescore/per-subject-results.csv` (11600 rows)
- Script: `experiments/failure-provenance/analysis/21c-glmm-logit-s1s5.py`
