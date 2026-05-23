# Vocabulary-aligned re-score — strict vs aligned top-1

Per (scenario × engine) cell, 50 reps pooled across C1–C5 (LLM-A only).

| Scenario | Engine | n | strict top-1 | aligned top-1 | gain |
|---|---|---|---|---|---|
| F1 | rule-grounded | 50 | 100.0% (50/50) | 100.0% (50/50) | +0 |
| F1 | llm-grounded | 50 |  38.0% (19/50) | 100.0% (50/50) | +31 |
| F1 | llm-freeform | 50 |   0.0% (0/50) |  70.0% (35/50) | +35 |
| F2 | rule-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F2 | llm-grounded | 50 |   0.0% (0/50) |  62.0% (31/50) | +31 |
| F2 | llm-freeform | 50 |   0.0% (0/50) |  68.0% (34/50) | +34 |
| F3 | rule-grounded | 50 |  80.0% (40/50) |  80.0% (40/50) | +0 |
| F3 | llm-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F3 | llm-freeform | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F4 | rule-grounded | 50 |  60.0% (30/50) |  60.0% (30/50) | +0 |
| F4 | llm-grounded | 50 |   2.0% (1/50) |  42.0% (21/50) | +20 |
| F4 | llm-freeform | 50 |   0.0% (0/50) |  30.0% (15/50) | +15 |
| F5 | rule-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F5 | llm-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F5 | llm-freeform | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F6 | rule-grounded | 50 |   0.0% (0/50) |  54.0% (27/50) | +27 |
| F6 | llm-grounded | 50 |   0.0% (0/50) |  52.0% (26/50) | +26 |
| F6 | llm-freeform | 50 |   0.0% (0/50) |  28.0% (14/50) | +14 |
| F7 | rule-grounded | 50 |   0.0% (0/50) |  54.0% (27/50) | +27 |
| F7 | llm-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F7 | llm-freeform | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F8 | rule-grounded | 50 |   0.0% (0/50) |  60.0% (30/50) | +30 |
| F8 | llm-grounded | 50 |   0.0% (0/50) |  58.0% (29/50) | +29 |
| F8 | llm-freeform | 50 |   0.0% (0/50) |  38.0% (19/50) | +19 |
| F9 | rule-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F9 | llm-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F9 | llm-freeform | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F10 | rule-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F10 | llm-grounded | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
| F10 | llm-freeform | 50 |   0.0% (0/50) |   0.0% (0/50) | +0 |
