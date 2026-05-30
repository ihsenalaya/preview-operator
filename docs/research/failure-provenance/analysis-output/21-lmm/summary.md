# L3 — Primary inferential model (LMM)

Generated 2026-05-30T14:23:38.269841Z.

Model: `correct ~ C(configuration) * C(llm)` with random intercept `(1|scenario)`. n = 10000 rows. Fitted via statsmodels MixedLM.

## Configuration × LLM interaction terms

| term | estimate | std_err | p_value |
|---|---|---|---|
| C(configuration)[T.C2]:C(llm)[T.B] | -0.0370 | 0.0182 | 0.0417 |
| C(configuration)[T.C3]:C(llm)[T.B] | -0.1530 | 0.0182 | 0.0000 |
| C(configuration)[T.C4]:C(llm)[T.B] | -0.1500 | 0.0182 | 0.0000 |
| C(configuration)[T.C5]:C(llm)[T.B] | -0.1430 | 0.0182 | 0.0000 |

A significant interaction (p<0.05) is the pre-registered cross-LLM finding for RQ4 (L7). Read the full table in `lmm_top1.txt`.
