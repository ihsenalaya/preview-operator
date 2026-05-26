# Pooled engine-level top-1 with bootstrap 95 % CIs

Percentile bootstrap, 10000 iterations, RNG seed `random.Random(20260523)`. The CI is reported alongside the Wilson CI in 01-descriptive — bootstrap is shown here because it is the resample we use for the paired C5−C1 difference and the engine-vs-engine ranking; reporting the engine totals with the same resample keeps the article internally consistent.

| Engine | n | strict mean | strict 95 % CI | aligned mean | aligned 95 % CI |
|---|---|---|---|---|---|
| rule-grounded | 500 |  24.0% | [ 20.2%,  27.8%] |  40.8% | [ 36.4%,  45.0%] |
| llm-grounded | 500 |   4.0% | [  2.4%,   5.8%] |  31.4% | [ 27.4%,  35.4%] |
| llm-freeform | 500 |   0.0% | [  0.0%,   0.0%] |  23.4% | [ 19.8%,  27.2%] |
