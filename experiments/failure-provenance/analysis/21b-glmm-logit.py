#!/usr/bin/env python3
"""
21b-glmm-logit.py — Logistic mixed-effects model for top1_aligned.

Addresses the reviewer critique that 21-lmm.py uses MixedLM (Gaussian) on a
binary outcome (top1_aligned ∈ {0,1}). The correct family for a binary outcome
is Binomial with logit link, i.e. a generalized linear mixed-effects model
(GLMM).

We fit:
    top1_aligned ~ C(configuration) * C(engine) + (1|scenario_id)

with:
  - statsmodels.BinomialBayesMixedGLM, variational Bayes fit (fast, stable)
  - cross-check via plain logit with scenario as fixed effect

Output: odds-ratio table + summary markdown.
"""
from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import numpy as np
import pandas as pd
import statsmodels.api as sm
import statsmodels.formula.api as smf
from statsmodels.genmod.bayes_mixed_glm import BinomialBayesMixedGLM

REPO = Path(__file__).resolve().parents[3]
CSV = REPO / "docs/research/failure-provenance/analysis-output/08-vocab-rescore/results-rescored.csv"
OUT = REPO / "docs/research/failure-provenance/analysis-output/21b-glmm-logit"
OUT.mkdir(parents=True, exist_ok=True)


def engine_from_notes(s: str) -> str:
    if pd.isna(s):
        return "unknown"
    if "engine=rule mode=grounded" in s:
        return "rule-grounded"
    if "engine=llm mode=grounded" in s:
        return "llm-grounded"
    if "engine=llm mode=freeform" in s:
        return "llm-freeform"
    return "other"


def main() -> int:
    if not CSV.exists():
        print(f"input not found: {CSV}", file=sys.stderr)
        return 1
    df = pd.read_csv(CSV)
    df["engine"] = df["notes"].apply(engine_from_notes)
    df = df[df["engine"] != "other"].copy()
    df["top1_aligned"] = df["top1_aligned"].astype(int)
    df["configuration"] = pd.Categorical(
        df["configuration"], categories=["C1", "C2", "C3", "C4", "C5"], ordered=True
    )
    df["engine"] = pd.Categorical(
        df["engine"], categories=["llm-freeform", "llm-grounded", "rule-grounded"]
    )
    df["scenario_id"] = df["scenario_id"].astype(str)

    print(f"n = {len(df)} rows")
    print("counts by engine:", df["engine"].value_counts().to_dict())

    # ------------------------------------------------------------------ #
    # MODEL 1 — Bayesian GLMM (variational Bayes) with random intercept   #
    # ------------------------------------------------------------------ #
    print("\n== GLMM (BinomialBayesMixedGLM, vb) ==")
    formula = "top1_aligned ~ C(configuration) * C(engine)"
    random = {"scenario": "0 + C(scenario_id)"}
    md = BinomialBayesMixedGLM.from_formula(formula, random, df)
    res_vb = md.fit_vb()
    print(res_vb.summary())

    # Save VB summary text.
    (OUT / "glmm_vb_summary.txt").write_text(str(res_vb.summary()))

    # ------------------------------------------------------------------ #
    # MODEL 2 — plain GLM logit with scenario fixed effect (cross-check)  #
    # ------------------------------------------------------------------ #
    print("\n== Logit GLM (scenario as fixed effect) — odds ratios ==")
    m = smf.glm(
        "top1_aligned ~ C(configuration) * C(engine) + C(scenario_id)",
        data=df, family=sm.families.Binomial()
    ).fit()
    print(m.summary())
    (OUT / "logit_glm_summary.txt").write_text(str(m.summary()))

    # Extract odds ratios + 95% CIs for configuration and engine main effects + interaction.
    coefs = m.params
    conf = m.conf_int()
    or_table = pd.DataFrame({
        "term": coefs.index,
        "estimate (logit)": coefs.values,
        "odds_ratio": np.exp(coefs.values),
        "OR_CI_lo": np.exp(conf[0].values),
        "OR_CI_hi": np.exp(conf[1].values),
        "p_value": m.pvalues.values,
    })
    # Keep only the rows we care about for the article (drop scenario FEs).
    keep = or_table[~or_table["term"].str.contains("scenario_id")].copy()
    keep.to_csv(OUT / "odds_ratios.csv", index=False)

    # ------------------------------------------------------------------ #
    # MARKDOWN SUMMARY                                                    #
    # ------------------------------------------------------------------ #
    md_lines = [
        "# RQ2 — Generalized Linear Mixed-Effects Model (Logit)",
        "",
        f"Generated from `{CSV.name}` (n = {len(df)} rows, S1 only).",
        "",
        "## Model spec",
        "",
        "```",
        "top1_aligned ~ C(configuration) * C(engine) + (1 | scenario_id)",
        "family = Binomial(), link = logit",
        "```",
        "",
        "Binary outcome `top1_aligned` (0/1) — correctly identifies the failed component "
        "after the per-scenario alias matcher. Reference levels: `configuration = C1`, "
        "`engine = llm-freeform`.",
        "",
        "## Odds ratios (logit GLM cross-check with scenario as fixed effect)",
        "",
        "| Term | Estimate (logit) | Odds ratio | 95 % CI | p |",
        "|---|---:|---:|---:|---:|",
    ]
    for _, r in keep.iterrows():
        if r["term"] == "Intercept":
            continue
        term = r["term"].replace("C(", "").replace(")", "").replace("[T.", "[T.")
        md_lines.append(
            f"| `{term}` | {r['estimate (logit)']:+.3f} | "
            f"{r['odds_ratio']:.2f} | "
            f"[{r['OR_CI_lo']:.2f}, {r['OR_CI_hi']:.2f}] | "
            f"{r['p_value']:.4f} |"
        )

    md_lines += [
        "",
        "## Headline reading",
        "",
        "- Reference cell: `C1 / llm-freeform`. The `Intercept` reflects the baseline "
        "log-odds of a correct alignment-matched diagnosis on F1 (lowest-ID scenario).",
        "- A `C(configuration)[T.Cx]` coefficient > 0 means *adding evidence at level x "
        "increases* the odds of correctness vs C1, all else equal.",
        "- A `C(engine)[T.rule-grounded]` coefficient > 0 means *the rule diagnoser is "
        "more accurate than the free-form LLM* at the reference configuration.",
        "- Interaction terms `C(configuration)[T.Cx]:C(engine)[T.eng]` quantify how the "
        "evidence-level effect *differs* between engines.",
        "",
        "## Why this supersedes `21-lmm.py`",
        "",
        "`21-lmm.py` used `MixedLM` (Gaussian / identity link) on a binary outcome. "
        "While the coefficients there are unbiased on average, the standard errors and "
        "p-values are not correctly calibrated for binary data — predicted probabilities "
        "can fall outside [0,1] and the residual variance is mis-specified. The GLMM "
        "above uses the correct Binomial family with logit link and is the model the "
        "pre-registered analysis plan (`analysis-plan.md §1 L3`) commits to.",
        "",
        "Source CSV: `analysis-output/08-vocab-rescore/results-rescored.csv` (S1 only).",
        "Script: `experiments/failure-provenance/analysis/21b-glmm-logit.py`.",
        "",
    ]
    (OUT / "summary.md").write_text("\n".join(md_lines))

    print(f"\nwrote {OUT / 'summary.md'}")
    print(f"wrote {OUT / 'odds_ratios.csv'}")
    print(f"wrote {OUT / 'glmm_vb_summary.txt'}")
    print(f"wrote {OUT / 'logit_glm_summary.txt'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
