# S1-S5 pooled aligned top-1 by engine

Pooled across all five subjects (flask-catalog, listmonk, healthchecks, umami, petclinic). S1 has the full F1-F10 scope (100 captures); S2-S5 each cover F1+F2+F3+F6+F7 (15 captures).

| Engine | n | aligned top-1 | Wilson 95% CI |
|---|---|---|---|
| llm-b-freeform | 2200 |   3.8% (84/2200) | [3.1%, 4.7%] |
| llm-b-grounded | 2500 |   5.3% (133/2500) | [4.5%, 6.3%] |
| llm-freeform | 2200 |  18.0% (396/2200) | [16.5%, 19.7%] |
| llm-grounded | 2500 |  18.7% (468/2500) | [17.2%, 20.3%] |
| rule-grounded | 2200 |  15.3% (337/2200) | [13.9%, 16.9%] |
