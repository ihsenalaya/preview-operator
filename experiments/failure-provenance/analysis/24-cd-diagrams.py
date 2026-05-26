#!/usr/bin/env python3
"""24-cd-diagrams.py — Critical-Difference diagrams (Demsar 2006).

For each scenario, ranks the C1..C5 configurations by their top-1 accuracy
within each LLM, then plots the average ranks across scenarios with the
critical-difference (CD) band drawn from the Nemenyi post-hoc.

Output:
  docs/research/failure-provenance/analysis-output/24-cd-diagrams/cd_llm_a.png
  docs/research/failure-provenance/analysis-output/24-cd-diagrams/cd_llm_b.png
  docs/research/failure-provenance/analysis-output/24-cd-diagrams/summary.md
"""
from __future__ import annotations
import math
import pathlib
import sys
import datetime as dt

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/24-cd-diagrams"
OUT.mkdir(parents=True, exist_ok=True)

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from _loader import load_unified


# Critical values q_alpha from Demsar 2006 Table 5 (Nemenyi, alpha=0.05).
# Index = k (number of classifiers/treatments).
Q_ALPHA_005 = {2: 1.960, 3: 2.343, 4: 2.569, 5: 2.728, 6: 2.850,
               7: 2.949, 8: 3.031, 9: 3.102, 10: 3.164}


def nemenyi_cd(k: int, n: int, alpha: float = 0.05) -> float:
    q = Q_ALPHA_005[k] if alpha == 0.05 else Q_ALPHA_005[k]
    return q * math.sqrt(k * (k + 1) / (6.0 * n))


def cd_diagram(ranks: dict[str, float], cd: float, title: str, path: pathlib.Path) -> None:
    """Minimal CD diagram: horizontal axis = avg rank, labelled treatments,
    a 'CD' bar at the top showing the critical-difference width."""
    treatments = list(ranks.keys())
    avg = np.array([ranks[t] for t in treatments])
    order = np.argsort(avg)
    treatments = [treatments[i] for i in order]
    avg = avg[order]
    k = len(treatments)

    fig, ax = plt.subplots(figsize=(7, 2.5))
    # Axis
    lo, hi = 1, k
    ax.set_xlim(hi + 0.5, lo - 0.5)  # reverse: best rank (smallest) on right
    ax.set_ylim(0, 6)
    ax.axhline(2, color="k", linewidth=1)
    for i in range(int(lo), int(hi) + 1):
        ax.text(i, 1.7, str(i), ha="center", va="top", fontsize=9)

    # CD bar
    ax.plot([lo, lo + cd], [4.3, 4.3], color="k", linewidth=2)
    ax.text(lo + cd / 2, 4.7, f"CD = {cd:.2f}", ha="center", fontsize=9)

    # Label each treatment
    for i, t in enumerate(treatments):
        x = avg[i]
        side = "left" if i < k / 2 else "right"
        ax.plot([x, x], [2.0, 2.6], color="k", linewidth=1)
        if side == "left":
            ax.plot([x, hi + 0.3], [2.6, 2.6 + 0.4 * i], color="k", linewidth=0.8)
            ax.text(hi + 0.35, 2.6 + 0.4 * i, f"{t}  ({x:.2f})", va="center", ha="left", fontsize=9)
        else:
            ax.plot([x, lo - 0.3], [2.6, 2.6 + 0.4 * (k - 1 - i)], color="k", linewidth=0.8)
            ax.text(lo - 0.35, 2.6 + 0.4 * (k - 1 - i), f"{t}  ({x:.2f})", va="center", ha="right", fontsize=9)

    ax.axis("off")
    ax.set_title(title, fontsize=10)
    fig.tight_layout()
    fig.savefig(path, dpi=160, bbox_inches="tight")
    plt.close(fig)


def main() -> int:
    df = load_unified()
    df = df[df["engine"] == "llm"].copy()
    if df.empty:
        print("no LLM rows; cannot produce CD diagrams", file=sys.stderr)
        return 1

    summary_path = OUT / "summary.md"
    with summary_path.open("w") as smd:
        smd.write("# CD diagrams — Friedman + Nemenyi post-hoc\n\n")
        smd.write(f"Generated {dt.datetime.utcnow().isoformat()}Z.\n\n")
        for llm in sorted(df["llm"].unique()):
            sub = df[df["llm"] == llm]
            # Per-(scenario × C) mean accuracy
            tab = sub.groupby(["scenario", "configuration"])["correct"].mean().unstack("configuration")
            tab = tab.fillna(0.0)
            if tab.shape[1] < 3:
                smd.write(f"## LLM={llm}: not enough configurations ({tab.shape[1]}), skipped\n\n")
                continue
            # Rank within each scenario (higher = better → lower rank number)
            ranks = tab.rank(axis=1, ascending=False, method="average")
            avg_ranks = ranks.mean(axis=0).to_dict()
            n_scenarios = tab.shape[0]
            k = len(avg_ranks)
            cd = nemenyi_cd(k, n_scenarios)
            png = OUT / f"cd_llm_{llm.lower()}.png"
            cd_diagram(avg_ranks, cd, f"LLM-{llm} — avg rank across {n_scenarios} scenarios (Nemenyi CD={cd:.2f}, α=0.05)", png)
            smd.write(f"## LLM={llm}\n\n")
            smd.write(f"Friedman ranks across {n_scenarios} scenarios over "
                      f"{k} configurations. Critical Difference (Nemenyi, "
                      f"α=0.05) = {cd:.3f}.\n\n")
            smd.write("| C | avg rank |\n|---|---|\n")
            for c, r in sorted(avg_ranks.items(), key=lambda x: x[1]):
                smd.write(f"| {c} | {r:.3f} |\n")
            smd.write(f"\nFigure: `{png.name}`\n\n")
    print(f"CD diagrams in {OUT}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
