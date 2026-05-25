# Multi-app S2-S5 pooled aligned top-1 by engine

Pooled across listmonk + healthchecks + umami + petclinic, F1+F2+F3+F6+F7, C1-C5, n = 10 reps. (S1 excluded — it is the primary application reported in §3.)

| Engine | n | aligned top-1 | Wilson 95% CI |
|---|---|---|---|
| llm-b-freeform | 1700 |   4.6% (79/1700) | [3.7%, 5.8%] |
| llm-b-grounded | 2000 |   6.0% (119/2000) | [5.0%, 7.1%] |
| llm-freeform | 1700 |  16.4% (279/1700) | [14.7%, 18.2%] |
| llm-grounded | 2000 |  15.6% (311/2000) | [14.0%, 17.2%] |
| rule-grounded | 1700 |   7.8% (133/1700) | [6.6%, 9.2%] |
