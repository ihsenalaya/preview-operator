# L3 — Primary inferential model (LMM)

Generated 2026-05-23T18:06:59.069630Z.

Model: `correct ~ C(configuration) * C(llm)` with random intercept `(1|scenario)`. n = 2000 rows. Fitted via statsmodels MixedLM.

## Configuration × LLM interaction terms

| term | estimate | std_err | p_value |
|---|---|---|---|
| C(configuration)[T.C2]:C(llm)[T.B] | 0.0000 | 0.0132 | 1.0000 |
| C(configuration)[T.C3]:C(llm)[T.B] | 0.0000 | 0.0132 | 1.0000 |
| C(configuration)[T.C4]:C(llm)[T.B] | -0.0500 | 0.0132 | 0.0002 |
| C(configuration)[T.C5]:C(llm)[T.B] | -0.0500 | 0.0132 | 0.0002 |

A significant interaction (p<0.05) is the pre-registered cross-LLM finding for RQ4 (L7). Read the full table in `lmm_top1.txt`.
