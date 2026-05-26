#!/usr/bin/env python3
"""36-unified-figures.py — Generate S1-S5 unified figures.

Currently emits:
  engine-pooled-bars-s1s5.png : pooled aligned top-1 per engine across
                                all five subjects, Wilson 95% CIs.
"""
import csv, math, pathlib, sys
from collections import defaultdict
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

CSV_PATH = pathlib.Path('/home/azureuser/preview-operator/docs/research/'
                        'failure-provenance/analysis-output/35-unified-rescore/'
                        'per-subject-results.csv')
OUT_DIR = pathlib.Path('/home/azureuser/preview-operator/docs/research/'
                       'failure-provenance/latex/figures')
OUT_DIR.mkdir(parents=True, exist_ok=True)


def wilson_ci(s: int, n: int, z: float = 1.96):
    if n == 0:
        return (0.0, 0.0)
    p = s / n
    denom = 1.0 + z * z / n
    centre = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return (max(0.0, centre - half), min(1.0, centre + half))


def main() -> int:
    pooled = defaultdict(list)
    per_subject = defaultdict(lambda: defaultdict(list))
    with CSV_PATH.open() as f:
        r = csv.DictReader(f)
        for row in r:
            eng = row['engine_mode']
            v = int(row['top1_aligned'])
            pooled[eng].append(v)
            per_subject[row['subject_id']][eng].append(v)

    engines = ['rule-grounded',
               'llm-grounded', 'llm-freeform',
               'llm-b-grounded', 'llm-b-freeform']
    labels = ['rule\n(grounded)',
              'gpt-4o-mini\n(grounded)', 'gpt-4o-mini\n(freeform)',
              'cohere-cmd-a\n(grounded)', 'cohere-cmd-a\n(freeform)']

    # ---- 1. Pooled S1-S5 per-engine bar chart with Wilson CIs ----
    fig, ax = plt.subplots(figsize=(7.5, 4.2))
    xs = list(range(len(engines)))
    means = []
    lows = []
    highs = []
    ns = []
    for e in engines:
        d = pooled.get(e, [])
        n = len(d)
        s = sum(d)
        p = s / n if n else 0.0
        lo, hi = wilson_ci(s, n)
        means.append(p)
        lows.append(p - lo)
        highs.append(hi - p)
        ns.append(n)

    colours = ['#4C72B0', '#55A868', '#55A868', '#C44E52', '#C44E52']
    hatches = ['', '', '//', '', '//']
    bars = ax.bar(xs, means, color=colours, edgecolor='black',
                  hatch=hatches, alpha=0.85)
    ax.errorbar(xs, means, yerr=[lows, highs], fmt='none',
                ecolor='black', capsize=4, linewidth=1)
    ax.set_xticks(xs)
    ax.set_xticklabels(labels, fontsize=9)
    ax.set_ylabel('Aligned top-1 (pooled S1--S5)')
    ax.set_ylim(0, max(means) * 1.35 + 0.02)
    ax.yaxis.set_major_formatter(
        plt.FuncFormatter(lambda x, _: f'{100*x:.0f}\\%'))
    for xi, (m, n) in enumerate(zip(means, ns)):
        ax.annotate(f'{100*m:.1f}%\n($n$={n})',
                    (xi, m), textcoords='offset points',
                    xytext=(0, 14), ha='center', fontsize=8)
    ax.set_title('Pooled aligned top-1 per engine across S1--S5 '
                 '(Wilson 95\\% CI)', fontsize=10)
    ax.grid(axis='y', linestyle=':', alpha=0.5)
    fig.tight_layout()
    out_p = OUT_DIR / 'engine-pooled-bars-s1s5.png'
    fig.savefig(out_p, dpi=150, bbox_inches='tight')
    print(f'wrote {out_p}')
    plt.close(fig)

    # ---- 2. Per-subject heatmap of aligned top-1 ----
    subjects = ['s1-flask-catalog', 's2-listmonk', 's3-healthchecks',
                's4-umami', 's5-petclinic']
    short = ['S1 flask', 'S2 listmonk', 'S3 healthchecks', 'S4 umami', 'S5 petclinic']
    M = []
    for subj in subjects:
        row = []
        for eng in engines:
            d = per_subject[subj].get(eng, [])
            n = len(d)
            row.append(sum(d) / n if n else 0.0)
        M.append(row)

    fig, ax = plt.subplots(figsize=(8.0, 3.2))
    im = ax.imshow(M, cmap='viridis', vmin=0, vmax=0.5, aspect='auto')
    ax.set_xticks(range(len(engines)))
    ax.set_xticklabels(labels, fontsize=8)
    ax.set_yticks(range(len(subjects)))
    ax.set_yticklabels(short, fontsize=9)
    for i in range(len(subjects)):
        for j in range(len(engines)):
            v = M[i][j]
            ax.text(j, i, f'{100*v:.1f}%', ha='center', va='center',
                    color='white' if v < 0.25 else 'black', fontsize=8)
    cbar = fig.colorbar(im, ax=ax, fraction=0.04, pad=0.02)
    cbar.set_label('aligned top-1', fontsize=8)
    cbar.ax.tick_params(labelsize=8)
    ax.set_title('Aligned top-1 per subject × engine (S1--S5 heatmap)',
                 fontsize=10)
    fig.tight_layout()
    out_p2 = OUT_DIR / 'engine-by-subject-heatmap-s1s5.png'
    fig.savefig(out_p2, dpi=150, bbox_inches='tight')
    print(f'wrote {out_p2}')
    plt.close(fig)

    return 0


if __name__ == '__main__':
    sys.exit(main())
