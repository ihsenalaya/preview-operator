# S1-S5 pooled aligned top-1 by engine

Pooled across all five subjects (flask-catalog, listmonk, healthchecks, umami, petclinic). S1 has the full F1-F10 scope (100 captures); S2-S5 each cover F1+F2+F3+F6+F7 (15 captures).

| Engine | n | aligned top-1 | Wilson 95% CI |
|---|---|---|---|
| llm-b-freeform | 2500 |   5.9% (148/2500) | [5.1%, 6.9%] |
| llm-b-grounded | 2500 |   6.8% (171/2500) | [5.9%, 7.9%] |
| llm-freeform | 2500 |  19.4% (485/2500) | [17.9%, 21.0%] |
| llm-grounded | 2500 |  19.7% (492/2500) | [18.2%, 21.3%] |
| rule-grounded | 2500 |  15.8% (394/2500) | [14.4%, 17.2%] |
