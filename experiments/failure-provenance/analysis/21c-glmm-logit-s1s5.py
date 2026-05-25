#!/usr/bin/env python3
"""
21c-glmm-logit-s1s5.py — GLMM logit on S1-S5 pooled (extends 21b to multi-app).

Mirrors 21b-glmm-logit.py but reads from
docs/research/failure-provenance/analysis-output/35-unified-rescore/per-subject-results.csv
which pools S1 + S2-S5.

Model:
    top1_aligned ~ C(configuration) * C(engine) + (1 | subject_id) + (1 | scenario_id)
    family = Binomial, link = logit

Output:
  docs/research/failure-provenance/analysis-output/21c-glmm-logit-s1s5/
    glmm_vb_summary.txt
    logit_glm_summary.txt
    odds_ratios.csv
    summary.md
"""
from __future__ import annotations
import datetime as dt
import json
import sys
from pathlib import Path

import numpy as np
import pandas as pd
import statsmodels.api as sm
import statsmodels.formula.api as smf
from statsmodels.genmod.bayes_mixed_glm import BinomialBayesMixedGLM

REPO = Path('/home/azureuser/preview-operator')
CSV = REPO / 'docs/research/failure-provenance/analysis-output/35-unified-rescore/per-subject-results.csv'
OUT = REPO / 'docs/research/failure-provenance/analysis-output/21c-glmm-logit-s1s5'
OUT.mkdir(parents=True, exist_ok=True)


def engine_from_em(em: str) -> str:
    """engine_mode → simplified engine label used by the model.

    35-unified-rescore engine_mode values:
      llm-freeform, llm-grounded     (Azure OpenAI: 'A')
      llm-b-freeform, llm-b-grounded (Cohere:      'B')
      rule-grounded                  (Rule engine)

    Combine A/B into the same 'llm-{mode}' bucket since the question is
    grounded-vs-freeform across LLM families, not which LLM family.
    """
    if em == 'rule-grounded':
        return 'rule-grounded'
    if em in ('llm-freeform', 'llm-b-freeform'):
        return 'llm-freeform'
    if em in ('llm-grounded', 'llm-b-grounded'):
        return 'llm-grounded'
    return 'other'


def main() -> int:
    if not CSV.exists():
        print(f'input not found: {CSV}', file=sys.stderr)
        return 1
    df = pd.read_csv(CSV)
    df['engine'] = df['engine_mode'].apply(engine_from_em)
    df = df[df['engine'] != 'other'].copy()
    df['top1_aligned'] = df['top1_aligned'].astype(int)
    df['configuration'] = pd.Categorical(
        df['configuration'], categories=['C1', 'C2', 'C3', 'C4', 'C5'], ordered=True)
    df['engine'] = pd.Categorical(
        df['engine'], categories=['llm-freeform', 'llm-grounded', 'rule-grounded'])
    df['scenario_id'] = df['scenario_id'].astype(str)
    df['subject_id'] = df['subject_id'].astype(str)

    print(f'n = {len(df)} rows across {df["subject_id"].nunique()} subjects')
    print('subjects:', sorted(df['subject_id'].unique()))
    print('counts by engine:', df['engine'].value_counts().to_dict())
    print('top1_aligned base rate:', df['top1_aligned'].mean().round(4))

    # ------------------------------------------------------------------ #
    # MODEL 1 — Bayesian GLMM (variational Bayes) with random intercept   #
    # ------------------------------------------------------------------ #
    print('\n== GLMM (BinomialBayesMixedGLM, vb) ==')
    formula = 'top1_aligned ~ C(configuration) * C(engine)'
    # Two-level nesting: random intercept per scenario × subject
    random = {
        'subject':  '0 + C(subject_id)',
        'scenario': '0 + C(scenario_id)',
    }
    md = BinomialBayesMixedGLM.from_formula(formula, random, df)
    res_vb = md.fit_vb()
    print(res_vb.summary())
    (OUT / 'glmm_vb_summary.txt').write_text(str(res_vb.summary()))

    # ------------------------------------------------------------------ #
    # MODEL 2 — plain GLM logit with scenario + subject as fixed effects #
    # ------------------------------------------------------------------ #
    print('\n== Logit GLM (subject + scenario as fixed effects) — odds ratios ==')
    m = smf.glm(
        'top1_aligned ~ C(configuration) * C(engine) + C(scenario_id) + C(subject_id)',
        data=df, family=sm.families.Binomial(),
    ).fit()
    print(m.summary())
    (OUT / 'logit_glm_summary.txt').write_text(str(m.summary()))

    coefs = m.params
    conf = m.conf_int()
    or_table = pd.DataFrame({
        'term': coefs.index,
        'estimate_logit': coefs.values,
        'odds_ratio': np.exp(coefs.values),
        'OR_CI_lo': np.exp(conf[0].values),
        'OR_CI_hi': np.exp(conf[1].values),
        'p_value': m.pvalues.values,
    })
    or_table.to_csv(OUT / 'odds_ratios.csv', index=False)
    print('Wrote', OUT / 'odds_ratios.csv')

    md_lines = [
        '# RQ2 — Generalized Linear Mixed-Effects Model (Logit), S1-S5 pooled',
        '',
        f'Generated {dt.datetime.utcnow().isoformat()}Z from `35-unified-rescore/per-subject-results.csv`.',
        f'n = {len(df)} rows across {df["subject_id"].nunique()} subjects: '
        f'{", ".join(sorted(df["subject_id"].unique()))}.',
        '',
        '## Model spec',
        '',
        '```',
        'top1_aligned ~ C(configuration) * C(engine)',
        '              + (1 | subject_id) + (1 | scenario_id)',
        'family = Binomial(), link = logit',
        '```',
        '',
        'Two random intercepts: one per subject (between-app variance), one per',
        'scenario (between-fault variance). LLM-A and LLM-B are pooled into the',
        '`llm-*` engine bucket so the contrast is grounded vs. free-form across',
        'LLM families, not which LLM family.',
        '',
        '## Key odds-ratios (engine × configuration interaction)',
        '',
        '| Term | Odds ratio | 95% CI | p-value |',
        '|---|---|---|---|',
    ]
    # Only print rows where term mentions both 'configuration' and 'engine' or is an engine main effect
    for _, r in or_table.iterrows():
        t = str(r['term'])
        if 'configuration' not in t and 'engine' not in t:
            continue
        if 'subject_id' in t or 'scenario_id' in t:
            continue
        md_lines.append(
            f'| `{t}` | {r["odds_ratio"]:.3f} | '
            f'[{r["OR_CI_lo"]:.3f}, {r["OR_CI_hi"]:.3f}] | {r["p_value"]:.4g} |')

    md_lines += [
        '',
        '## Reading',
        '',
        '- **Odds ratio > 1** means the term increases the odds of correct top-1.',
        '- The `C(configuration)[T.Ck]:C(engine)[T.X]` interaction terms read off',
        '  *how* the configuration × engine interaction differs from the C1 / llm-freeform baseline.',
        '- Two random intercepts (subject + scenario) absorb between-app and between-fault variance, so',
        '  the remaining fixed-effect estimates speak only to the C × engine main effects + interactions.',
        '',
        '## Source',
        '',
        '- Input: `docs/research/failure-provenance/analysis-output/35-unified-rescore/per-subject-results.csv` (11600 rows)',
        '- Script: `experiments/failure-provenance/analysis/21c-glmm-logit-s1s5.py`',
        '',
    ]
    (OUT / 'summary.md').write_text('\n'.join(md_lines))
    print('Wrote', OUT / 'summary.md')
    return 0


if __name__ == '__main__':
    sys.exit(main())
