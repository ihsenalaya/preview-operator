#!/usr/bin/env python3
"""05-figures.py — figures for the Evaluation section.

Produces PNG figures from the rescored CSV:

1. `forest-per-scenario.png` — forest plot of per-scenario aligned top-1
   with Wilson 95% CIs, one row per (scenario × engine), grouped by
   scenario. The headline figure for RQ2.

2. `c1-vs-c5-forest.png` — per (scenario × engine) C5 − C1 paired
   difference in aligned top-1, with bootstrap 95% CIs. Shows the
   evidence-completeness gradient directly.

3. `engine-pooled-bars.png` — bar chart of pooled top-1 per engine,
   strict vs aligned matcher (the 22.5% recovery in one image).

Output dir: `docs/research/failure-provenance/analysis-output/05-figures/`
"""
from __future__ import annotations

import argparse
import csv
import math
import pathlib
import random
import sys
from collections import defaultdict

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402


SCENARIOS = ["F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"]
CONFIGS = ["C1", "C2", "C3", "C4", "C5"]
ENGINES = ["rule-grounded", "llm-grounded", "llm-freeform"]
ENGINE_COLOR = {
    "rule-grounded": "#1f77b4",
    "llm-grounded": "#ff7f0e",
    "llm-freeform": "#2ca02c",
}


def wilson_ci(s: int, n: int, z: float = 1.96):
    if n == 0:
        return (0.0, 0.0)
    p = s / n
    denom = 1.0 + z * z / n
    centre = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return (max(0.0, centre - half), min(1.0, centre + half))


def parse_run_id(run_id: str):
    p = run_id.split("-")
    return (p[0], p[1], p[2], f"{p[-2]}-{p[-1]}")


def load(path: pathlib.Path) -> list[dict]:
    return list(csv.DictReader(path.open()))


def fig_forest(rows: list[dict], out: pathlib.Path) -> None:
    cells: dict[tuple[str, str], list[int]] = defaultdict(list)
    for r in rows:
        scen, _, _, eng = parse_run_id(r["run_id"])
        cells[(scen, eng)].append(1 if r.get("top1_aligned") == "1" else 0)

    fig, ax = plt.subplots(figsize=(10, 8))
    y = 0
    yticks = []
    ylabels = []
    for scen in SCENARIOS:
        for eng in ENGINES:
            data = cells.get((scen, eng), [])
            n = len(data)
            if n == 0:
                continue
            s = sum(data)
            p = s / n
            lo, hi = wilson_ci(s, n)
            ax.errorbar(
                p,
                y,
                xerr=[[p - lo], [hi - p]],
                fmt="o",
                color=ENGINE_COLOR[eng],
                markersize=6,
                capsize=3,
            )
            yticks.append(y)
            ylabels.append(f"{scen} · {eng}")
            y -= 1
        y -= 0.5  # gap between scenarios

    ax.set_yticks(yticks)
    ax.set_yticklabels(ylabels, fontsize=8)
    ax.set_xlim(-0.02, 1.02)
    ax.set_xlabel("Aligned top-1 accuracy (Wilson 95% CI), n=50 pooled C1-C5")
    ax.set_title(
        "Per-(scenario × engine) top-1 accuracy — vocabulary-aligned matcher",
    )
    ax.axvline(0.5, color="grey", linestyle=":", linewidth=0.5)
    # Legend
    for eng, color in ENGINE_COLOR.items():
        ax.plot([], [], "o", color=color, label=eng)
    ax.legend(loc="lower right", fontsize=8)
    fig.tight_layout()
    fig.savefig(out, dpi=150, bbox_inches="tight")
    plt.close(fig)


def bootstrap_ci(data: list[float], n_iter: int = 10_000, alpha: float = 0.05):
    if not data:
        return (float("nan"), float("nan"))
    means = []
    rng = random.Random(20260523)
    n = len(data)
    for _ in range(n_iter):
        sample = [data[rng.randrange(n)] for _ in range(n)]
        means.append(sum(sample) / n)
    means.sort()
    lo = means[int(alpha / 2 * n_iter)]
    hi = means[int((1 - alpha / 2) * n_iter)]
    return (lo, hi)


def fig_c5_minus_c1(rows: list[dict], out: pathlib.Path) -> None:
    # Per (scenario, engine, rep) → top1_aligned for C1 and C5
    by_cell: dict[tuple[str, str], dict[tuple[str, str], int]] = defaultdict(dict)
    for r in rows:
        scen, rep, conf, eng = parse_run_id(r["run_id"])
        by_cell[(scen, eng)][(rep, conf)] = (
            1 if r.get("top1_aligned") == "1" else 0
        )

    fig, ax = plt.subplots(figsize=(10, 8))
    y = 0
    yticks = []
    ylabels = []
    for scen in SCENARIOS:
        for eng in ENGINES:
            cells = by_cell.get((scen, eng), {})
            reps = sorted({k[0] for k in cells.keys()})
            if not reps:
                continue
            diffs = [
                cells.get((rep, "C5"), 0) - cells.get((rep, "C1"), 0)
                for rep in reps
            ]
            mean = sum(diffs) / len(diffs)
            lo, hi = bootstrap_ci([float(d) for d in diffs])
            ax.errorbar(
                mean,
                y,
                xerr=[[mean - lo], [hi - mean]],
                fmt="o",
                color=ENGINE_COLOR[eng],
                markersize=6,
                capsize=3,
            )
            yticks.append(y)
            ylabels.append(f"{scen} · {eng}")
            y -= 1
        y -= 0.5

    ax.axvline(0, color="black", linewidth=0.8)
    ax.set_yticks(yticks)
    ax.set_yticklabels(ylabels, fontsize=8)
    ax.set_xlabel(
        "C5 − C1 paired difference in aligned top-1 (bootstrap 95% CI)"
    )
    ax.set_title(
        "Evidence-completeness gradient: C5 − C1 per (scenario × engine)",
    )
    fig.tight_layout()
    fig.savefig(out, dpi=150, bbox_inches="tight")
    plt.close(fig)


def fig_engine_bars(rows: list[dict], out: pathlib.Path) -> None:
    strict_by_eng: dict[str, list[int]] = defaultdict(list)
    aligned_by_eng: dict[str, list[int]] = defaultdict(list)
    for r in rows:
        _, _, _, eng = parse_run_id(r["run_id"])
        strict_by_eng[eng].append(1 if r.get("top1_correct") == "1" else 0)
        aligned_by_eng[eng].append(1 if r.get("top1_aligned") == "1" else 0)

    fig, ax = plt.subplots(figsize=(8, 5))
    x = list(range(len(ENGINES)))
    strict_vals = [
        sum(strict_by_eng[e]) / len(strict_by_eng[e]) if strict_by_eng[e] else 0
        for e in ENGINES
    ]
    aligned_vals = [
        sum(aligned_by_eng[e]) / len(aligned_by_eng[e]) if aligned_by_eng[e] else 0
        for e in ENGINES
    ]
    width = 0.35
    ax.bar(
        [xi - width / 2 for xi in x],
        strict_vals,
        width,
        label="Strict matcher",
        color="#888888",
    )
    ax.bar(
        [xi + width / 2 for xi in x],
        aligned_vals,
        width,
        label="Vocabulary-aligned matcher",
        color="#1f77b4",
    )
    ax.set_xticks(x)
    ax.set_xticklabels(ENGINES)
    ax.set_ylabel("Pooled top-1 accuracy (n = 500 per engine)")
    ax.set_ylim(0, 0.5)
    ax.set_title(
        "RQ2 — strict vs vocabulary-aligned matcher, pooled by engine",
    )
    ax.legend()
    for xi, s, a in zip(x, strict_vals, aligned_vals):
        ax.annotate(
            f"{100*s:.1f}%",
            (xi - width / 2, s),
            textcoords="offset points",
            xytext=(0, 4),
            ha="center",
            fontsize=8,
        )
        ax.annotate(
            f"{100*a:.1f}%",
            (xi + width / 2, a),
            textcoords="offset points",
            xytext=(0, 4),
            ha="center",
            fontsize=8,
        )
    fig.tight_layout()
    fig.savefig(out, dpi=150, bbox_inches="tight")
    plt.close(fig)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in",
        dest="in_path",
        default="docs/research/failure-provenance/analysis-output/"
        "08-vocab-rescore/results-rescored.csv",
    )
    ap.add_argument(
        "--out-dir",
        default="docs/research/failure-provenance/analysis-output/05-figures",
    )
    args = ap.parse_args()

    rows = load(pathlib.Path(args.in_path))
    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    fig_forest(rows, out_dir / "forest-per-scenario.png")
    print(f"wrote {out_dir / 'forest-per-scenario.png'}")
    fig_c5_minus_c1(rows, out_dir / "c5-minus-c1-forest.png")
    print(f"wrote {out_dir / 'c5-minus-c1-forest.png'}")
    fig_engine_bars(rows, out_dir / "engine-pooled-bars.png")
    print(f"wrote {out_dir / 'engine-pooled-bars.png'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
