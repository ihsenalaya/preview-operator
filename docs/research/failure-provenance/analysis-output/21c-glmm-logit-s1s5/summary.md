# RQ2 — Generalized Linear Mixed-Effects Model (Logit), S1-S5 pooled

Generated 2026-05-25T18:04:32.109145Z from `35-unified-rescore/per-subject-results.csv`.
n = 12500 rows across 5 subjects: s1-flask-catalog, s2-listmonk, s3-healthchecks, s4-umami, s5-petclinic.

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
| `C(configuration)[T.C2]` | 3.619 | [2.239, 5.848] | 1.511e-07 |
| `C(configuration)[T.C3]` | 9.654 | [6.143, 15.171] | 8.269e-23 |
| `C(configuration)[T.C4]` | 12.006 | [7.665, 18.806] | 1.876e-27 |
| `C(configuration)[T.C5]` | 11.531 | [7.358, 18.072] | 1.462e-26 |
| `C(engine)[T.llm-grounded]` | 1.134 | [0.643, 1.999] | 0.6648 |
| `C(engine)[T.rule-grounded]` | 0.825 | [0.387, 1.759] | 0.6183 |
| `C(configuration)[T.C2]:C(engine)[T.llm-grounded]` | 0.800 | [0.410, 1.561] | 0.5131 |
| `C(configuration)[T.C3]:C(engine)[T.llm-grounded]` | 0.898 | [0.481, 1.676] | 0.7348 |
| `C(configuration)[T.C4]:C(engine)[T.llm-grounded]` | 0.992 | [0.535, 1.840] | 0.9789 |
| `C(configuration)[T.C5]:C(engine)[T.llm-grounded]` | 1.001 | [0.539, 1.859] | 0.9967 |
| `C(configuration)[T.C2]:C(engine)[T.rule-grounded]` | 2.145 | [0.919, 5.007] | 0.07749 |
| `C(configuration)[T.C3]:C(engine)[T.rule-grounded]` | 1.868 | [0.827, 4.220] | 0.1328 |
| `C(configuration)[T.C4]:C(engine)[T.rule-grounded]` | 1.502 | [0.666, 3.387] | 0.3265 |
| `C(configuration)[T.C5]:C(engine)[T.rule-grounded]` | 1.564 | [0.694, 3.527] | 0.2811 |

## Reading

- **Odds ratio > 1** means the term increases the odds of correct top-1.
- The `C(configuration)[T.Ck]:C(engine)[T.X]` interaction terms read off
  *how* the configuration × engine interaction differs from the C1 / llm-freeform baseline.
- Two random intercepts (subject + scenario) absorb between-app and between-fault variance, so
  the remaining fixed-effect estimates speak only to the C × engine main effects + interactions.

## Source

- Input: `docs/research/failure-provenance/analysis-output/35-unified-rescore/per-subject-results.csv` (11600 rows)
- Script: `experiments/failure-provenance/analysis/21c-glmm-logit-s1s5.py`
