# L3 — Primary inferential model (LMM)

Generated 2026-05-23T22:10:21.632770Z.

Model: `correct ~ C(configuration) * C(llm)` with random intercept `(1|scenario)`. n = 4000 rows. Fitted via statsmodels MixedLM.

## Configuration × LLM interaction terms

| term | estimate | std_err | p_value |
|---|---|---|---|
| C(configuration)[T.C2]:C(llm)[T.B] | -0.0125 | 0.0257 | 0.6261 |
| C(configuration)[T.C3]:C(llm)[T.B] | -0.0575 | 0.0257 | 0.0250 |
| C(configuration)[T.C4]:C(llm)[T.B] | -0.0575 | 0.0257 | 0.0250 |
| C(configuration)[T.C5]:C(llm)[T.B] | -0.0350 | 0.0257 | 0.1725 |

A significant interaction (p<0.05) is the pre-registered cross-LLM finding for RQ4 (L7). Read the full table in `lmm_top1.txt`.
