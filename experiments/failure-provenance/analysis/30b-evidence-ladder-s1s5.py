#!/usr/bin/env python3
"""
30b-evidence-ladder-s1s5.py — Evidence ladder L1→L4 pooled across S1-S5.

Extends 30-evidence-ladder.py to the full multi-app set:
  L1 = C1 (logs only)              35-unified-rescore/per-subject-results.csv
  L2 = B0 (raw kubectl)            10-b0-report (S1) + 29-b0-multiapp-fair (S2-S5)
  L3 = C4 (FailureReport, no PROV) 35-unified-rescore
  L4 = C5 (FailureReport + PROV)   35-unified-rescore

For each (rung × engine × subject) and pooled S1-S5 we report aligned top-1
with Wilson 95 % CI.

Note: L2 (B0) uses the FAIR baseline, i.e. raw kubectl input not operator-
curated evidenceItems. S2-S5 B0 has only 40 rows total (29-b0-multiapp-fair).

Outputs:
  docs/research/failure-provenance/analysis-output/30b-evidence-ladder-s1s5/
    ladder-pooled.csv
    ladder-per-subject.csv
    ladder-pooled.md
    ladder-pooled.png
    ladder-per-subject.png
"""
from __future__ import annotations

import sys
from pathlib import Path

import numpy as np
import pandas as pd
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from scipy.stats import binomtest

REPO = Path('/home/azureuser/preview-operator')
UNIFIED = REPO / 'docs/research/failure-provenance/analysis-output/35-unified-rescore/per-subject-results.csv'
B0_S1 = REPO / 'docs/research/failure-provenance/analysis-output/10-b0-report/results-b0.csv'
B0_MULTI = REPO / 'experiments/failure-provenance/results-matrix/results-b0-multiapp-fair.csv'
OUT = REPO / 'docs/research/failure-provenance/analysis-output/30b-evidence-ladder-s1s5'
OUT.mkdir(parents=True, exist_ok=True)


def engine_from_em(em: str) -> str:
    """Map 35-unified-rescore engine_mode → simplified bucket."""
    if em == 'rule-grounded':
        return 'rule-grounded'
    if em in ('llm-freeform', 'llm-b-freeform'):
        return 'llm-freeform'
    if em in ('llm-grounded', 'llm-b-grounded'):
        return 'llm-grounded'
    return 'other'


def wilson(k: int, n: int) -> tuple[float, float, float]:
    if n == 0:
        return 0.0, 0.0, 0.0
    p = k / n
    ci = binomtest(k, n).proportion_ci(method='wilson')
    return p, ci.low, ci.high


def main() -> int:
    if not UNIFIED.exists():
        print(f'input not found: {UNIFIED}', file=sys.stderr)
        return 1

    # Operator rungs (L1=C1, L3=C4, L4=C5) for all S1-S5 from unified.
    df = pd.read_csv(UNIFIED)
    df['engine'] = df['engine_mode'].apply(engine_from_em)
    df = df[df['engine'].isin({'rule-grounded', 'llm-grounded', 'llm-freeform'})].copy()
    df['top1_aligned'] = df['top1_aligned'].astype(int)
    rung_map = {'C1': 'L1 (logs only)', 'C4': 'L3 (FR, no prov.)', 'C5': 'L4 (FR + prov.)'}
    df_op = df[df['configuration'].isin(rung_map)].copy()
    df_op['rung'] = df_op['configuration'].map(rung_map)

    # B0 rung = L2: combine S1 and S2-S5 B0-fair rows.
    b0_rows = []
    if B0_S1.exists():
        s1b = pd.read_csv(B0_S1)
        # S1 B0 uses top1_aligned column directly
        for _, r in s1b.iterrows():
            b0_rows.append({
                'subject_id': 's1-flask-catalog',
                'scenario_id': str(r.get('scenario_id', '')),
                'rep': str(r.get('rep', '')),
                'top1_aligned': int(r.get('top1_aligned', 0) or 0),
            })
    if B0_MULTI.exists():
        mb = pd.read_csv(B0_MULTI)
        for _, r in mb.iterrows():
            b0_rows.append({
                'subject_id': str(r.get('subject', '')),
                'scenario_id': str(r.get('scenario', '')),
                'rep': str(r.get('rep', '')),
                'top1_aligned': int(r.get('top1_aligned', 0) or 0),
            })
    b0 = pd.DataFrame(b0_rows)
    b0['rung'] = 'L2 (raw kubectl)'
    b0['engine'] = 'B0 (vanilla LLM)'

    # Long-format combined frame
    long_rows = []

    # Operator rungs × engine × subject
    for (subject, rung, engine), g in df_op.groupby(['subject_id', 'rung', 'engine']):
        k = int(g['top1_aligned'].sum())
        n = int(len(g))
        p, lo, hi = wilson(k, n)
        long_rows.append({'subject': subject, 'rung': rung, 'engine': engine,
                          'k': k, 'n': n, 'aligned_top1': p, 'ci_lo': lo, 'ci_hi': hi})

    # B0 rung × subject
    for (subject), g in b0.groupby('subject_id'):
        k = int(g['top1_aligned'].sum())
        n = int(len(g))
        p, lo, hi = wilson(k, n)
        long_rows.append({'subject': subject, 'rung': 'L2 (raw kubectl)',
                          'engine': 'B0 (vanilla LLM)', 'k': k, 'n': n,
                          'aligned_top1': p, 'ci_lo': lo, 'ci_hi': hi})

    per_subj = pd.DataFrame(long_rows)
    per_subj.to_csv(OUT / 'ladder-per-subject.csv', index=False)

    # Pooled across S1-S5
    pooled_rows = []
    for (rung, engine), g in df_op.groupby(['rung', 'engine']):
        k = int(g['top1_aligned'].sum())
        n = int(len(g))
        p, lo, hi = wilson(k, n)
        pooled_rows.append({'rung': rung, 'engine': engine, 'k': k, 'n': n,
                            'aligned_top1': p, 'ci_lo': lo, 'ci_hi': hi})
    for engine, g in b0.groupby('engine'):
        k = int(g['top1_aligned'].sum())
        n = int(len(g))
        p, lo, hi = wilson(k, n)
        pooled_rows.append({'rung': 'L2 (raw kubectl)', 'engine': engine,
                            'k': k, 'n': n, 'aligned_top1': p,
                            'ci_lo': lo, 'ci_hi': hi})
    pooled = pd.DataFrame(pooled_rows)
    pooled.to_csv(OUT / 'ladder-pooled.csv', index=False)

    # Markdown table (pooled S1-S5)
    rungs_order = ['L1 (logs only)', 'L2 (raw kubectl)', 'L3 (FR, no prov.)', 'L4 (FR + prov.)']
    eng_order = ['rule-grounded', 'llm-grounded', 'llm-freeform', 'B0 (vanilla LLM)']
    md_lines = [
        '# Evidence ladder L1 → L4 — pooled S1-S5 (aligned top-1)',
        '',
        'Pooled across all five subjects (S1 flask-catalog, S2 listmonk, S3 healthchecks,',
        'S4 umami, S5 petclinic). Wilson 95 % CI in brackets, n in parentheses.',
        '',
        '| Rung | rule-grounded | llm-grounded | llm-freeform | B0 (vanilla LLM) |',
        '|---|---|---|---|---|',
    ]
    for rung in rungs_order:
        row = [rung]
        for eng in eng_order:
            cell = pooled[(pooled['rung'] == rung) & (pooled['engine'] == eng)]
            if cell.empty:
                row.append('—')
            else:
                r = cell.iloc[0]
                row.append(f'{r["aligned_top1"]:.3f} [{r["ci_lo"]:.3f}, {r["ci_hi"]:.3f}] (n={int(r["n"])})')
        md_lines.append('| ' + ' | '.join(row) + ' |')

    md_lines += [
        '',
        '## Reading',
        '',
        '- **L1 → L2** is the *evidence-volume* effect alone — moving from logs to raw kubectl bundles.',
        '- **L2 → L3** is the *operator-side capture + typed schema* contribution (no PROV linkage).',
        '- **L3 → L4** is the *PROV-graph* contribution on top of the typed bundle.',
        '- B0 uses the FAIR baseline (vanilla LLM reads raw kubectl output, *not* operator',
        '  evidenceItems formatted as kubectl-like text). S2-S5 B0 has only 40 rows (10/subject)',
        '  because that was the manageable cost for the fair-baseline rerun.',
        '',
        '## Per-subject breakdown',
        '',
        'See `ladder-per-subject.csv` and `ladder-per-subject.png` for per-subject ladders.',
        'Pooled numbers above weight subjects by their n (S1 has more captures → more weight).',
        '',
        '## Honesty notes',
        '',
        '- L1, L3, L4 are post-teardown reads of the operator bundle (offline scoring of the',
        '  FailureReport CR contents) — uniform regime across S1-S5.',
        '- L2 is also offline but consumes raw kubectl artefacts (events.txt, pods.txt, jobs.txt, logs/)',
        '  captured before teardown via `collect-results.sh`. Same regime, different input shape.',
        '',
        '## Source',
        '',
        '- L1/L3/L4: `analysis-output/35-unified-rescore/per-subject-results.csv` (11600 rows)',
        '- L2 (S1): `analysis-output/10-b0-report/results-b0.csv`',
        '- L2 (S2-S5): `results-matrix/results-b0-multiapp-fair.csv`',
        '- Script: `experiments/failure-provenance/analysis/30b-evidence-ladder-s1s5.py`',
        '',
    ]
    (OUT / 'ladder-pooled.md').write_text('\n'.join(md_lines))

    # ----------------------- figure: pooled bars -------------------------- #
    fig, ax = plt.subplots(figsize=(8.5, 4.0), dpi=130)
    rungs_short = ['L1\nlogs only', 'L2\nraw kubectl', 'L3\nFR no prov.', 'L4\nFR + prov.']
    colors = ['#2A6F97', '#E07A5F', '#81B29A', '#9C6644']
    x = np.arange(len(rungs_short))
    width = 0.2
    for i, eng in enumerate(eng_order):
        vals, los, his = [], [], []
        for rung in rungs_order:
            cell = pooled[(pooled['rung'] == rung) & (pooled['engine'] == eng)]
            if cell.empty:
                vals.append(np.nan); los.append(np.nan); his.append(np.nan)
            else:
                r = cell.iloc[0]
                vals.append(r['aligned_top1']); los.append(r['ci_lo']); his.append(r['ci_hi'])
        vals = np.array(vals, dtype=float)
        los = np.array(los, dtype=float)
        his = np.array(his, dtype=float)
        err_lo = vals - los
        err_hi = his - vals
        ax.bar(x + (i - 1.5) * width, vals, width=width, label=eng, color=colors[i])
        # Use NaN-safe errorbars
        mask = ~np.isnan(vals)
        ax.errorbar(x[mask] + (i - 1.5) * width, vals[mask],
                    yerr=[err_lo[mask], err_hi[mask]],
                    fmt='none', ecolor='black', capsize=2, linewidth=0.8)

    ax.set_xticks(x)
    ax.set_xticklabels(rungs_short)
    ax.set_ylabel('Aligned top-1')
    ax.set_title('Evidence ladder L1 → L4 (pooled S1–S5)')
    ax.set_ylim(0, max(0.6, np.nanmax(pooled['aligned_top1'].values) * 1.15 if len(pooled) else 0.6))
    ax.legend(loc='upper left', fontsize=8, ncol=2)
    ax.grid(axis='y', alpha=0.3)
    plt.tight_layout()
    plt.savefig(OUT / 'ladder-pooled.png', dpi=130)
    plt.close()

    # --------------------- per-subject heatmap-style figure ---------------- #
    subjects = sorted(per_subj['subject'].unique())
    fig, axes = plt.subplots(1, len(subjects), figsize=(3.0 * len(subjects), 4.0), dpi=130, sharey=True)
    if len(subjects) == 1:
        axes = [axes]
    for ax, subj in zip(axes, subjects):
        sub = per_subj[per_subj['subject'] == subj]
        for i, eng in enumerate(eng_order):
            vals = []
            for rung in rungs_order:
                cell = sub[(sub['rung'] == rung) & (sub['engine'] == eng)]
                vals.append(cell['aligned_top1'].iloc[0] if not cell.empty else np.nan)
            ax.bar(x + (i - 1.5) * width, vals, width=width, color=colors[i],
                   label=eng if subj == subjects[0] else None)
        ax.set_xticks(x)
        ax.set_xticklabels([r.split()[0] for r in rungs_order], fontsize=8)
        ax.set_title(subj, fontsize=9)
        ax.grid(axis='y', alpha=0.3)
    axes[0].set_ylabel('Aligned top-1')
    fig.legend(loc='upper center', ncol=4, fontsize=8, bbox_to_anchor=(0.5, 1.05))
    plt.tight_layout()
    plt.savefig(OUT / 'ladder-per-subject.png', dpi=130, bbox_inches='tight')
    plt.close()

    print(f'wrote ladder-pooled.csv, ladder-per-subject.csv, ladder-pooled.md, '
          f'ladder-pooled.png, ladder-per-subject.png')
    print('Pooled ladder:')
    for _, r in pooled.iterrows():
        print(f'  {r["rung"]:20s} {r["engine"]:25s}: '
              f'{r["aligned_top1"]:.3f} [{r["ci_lo"]:.3f},{r["ci_hi"]:.3f}] (n={r["n"]})')
    return 0


if __name__ == '__main__':
    sys.exit(main())
