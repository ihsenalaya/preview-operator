# Multi-app S2-S5 pooled aligned top-1 by engine

Pooled across listmonk + healthchecks + umami + petclinic, F1+F2+F3+F6+F7, C1-C5, n = 10 reps. (S1 excluded — it is the primary application reported in §3.)

| Engine | n | aligned top-1 | Wilson 95% CI |
|---|---|---|---|
| llm-b-freeform | 2000 |   7.2% (143/2000) | [6.1%, 8.4%] |
| llm-b-grounded | 2000 |   7.8% (157/2000) | [6.8%, 9.1%] |
| llm-freeform | 2000 |  18.4% (368/2000) | [16.8%, 20.2%] |
| llm-grounded | 2000 |  16.8% (335/2000) | [15.2%, 18.5%] |
| rule-grounded | 2000 |   9.5% (190/2000) | [8.3%, 10.9%] |
