# L3 — Primary inferential model (LMM)

Generated 2026-05-25T18:04:28.352192Z.

Model: `correct ~ C(configuration) * C(llm)` with random intercept `(1|scenario)`. n = 9400 rows. Fitted via statsmodels MixedLM.

## Configuration × LLM interaction terms

| term | estimate | std_err | p_value |
|---|---|---|---|
| C(configuration)[T.C2]:C(llm)[T.B] | -0.0245 | 0.0162 | 0.1299 |
| C(configuration)[T.C3]:C(llm)[T.B] | -0.1213 | 0.0162 | 0.0000 |
| C(configuration)[T.C4]:C(llm)[T.B] | -0.0947 | 0.0162 | 0.0000 |
| C(configuration)[T.C5]:C(llm)[T.B] | -0.0809 | 0.0162 | 0.0000 |

A significant interaction (p<0.05) is the pre-registered cross-LLM finding for RQ4 (L7). Read the full table in `lmm_top1.txt`.
