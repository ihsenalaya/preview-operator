#!/usr/bin/env python3
"""25-pareto.py — Pareto frontier: bundle size vs top-1 accuracy.

Identifies the (configuration × LLM) cells that lie on the Pareto frontier
of (lower bundle size, higher accuracy). The intuition: a configuration
that gives more accuracy with a smaller bundle is strictly preferable.

Output:
  docs/research/failure-provenance/analysis-output/25-pareto/pareto.png
  docs/research/failure-provenance/analysis-output/25-pareto/pareto.csv
  docs/research/failure-provenance/analysis-output/25-pareto/summary.md
"""
from __future__ import annotations
import csv
import pathlib
import sys
import datetime as dt

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import pandas as pd

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/25-pareto"
OUT.mkdir(parents=True, exist_ok=True)

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from _loader import load_unified


def is_pareto(points: list[tuple[float, float]]) -> list[bool]:
    """For points (cost, value): smaller cost is better, larger value is better."""
    n = len(points)
    flags = [True] * n
    for i in range(n):
        for j in range(n):
            if i == j or not flags[i]:
                continue
            xi, yi = points[i]
            xj, yj = points[j]
            if xj <= xi and yj >= yi and (xj < xi or yj > yi):
                flags[i] = False
                break
    return flags


def main() -> int:
    df = load_unified()
    df = df[df["engine"] == "llm"].copy()
    if df.empty:
        print("no LLM rows", file=sys.stderr)
        return 1

    # Cell summary: mean bundle size and mean accuracy per (C × LLM × mode)
    cells = (
        df.groupby(["configuration", "llm", "mode"], dropna=False)
          .agg(top1=("correct", "mean"),
               bundle=("bundle_size_bytes", "mean"),
               n=("correct", "size"))
          .reset_index()
    )
    cells = cells[cells["bundle"] > 0]
    cells["cell"] = cells["configuration"] + "/" + cells["llm"] + "/" + cells["mode"]

    points = list(zip(cells["bundle"].tolist(), cells["top1"].tolist()))
    flags = is_pareto(points)
    cells["pareto"] = flags

    # Write CSV
    csv_path = OUT / "pareto.csv"
    cells.to_csv(csv_path, index=False)

    # Plot
    fig, ax = plt.subplots(figsize=(7, 5))
    pareto_pts = cells[cells["pareto"]]
    other_pts = cells[~cells["pareto"]]
    ax.scatter(other_pts["bundle"], other_pts["top1"], alpha=0.5, label="non-Pareto")
    ax.scatter(pareto_pts["bundle"], pareto_pts["top1"], color="red", label="Pareto", zorder=5)
    for _, row in cells.iterrows():
        ax.annotate(row["cell"], (row["bundle"], row["top1"]), fontsize=7,
                    xytext=(5, 5), textcoords="offset points")
    ax.set_xlabel("Mean bundle size (bytes)")
    ax.set_ylabel("Mean top-1 accuracy")
    ax.set_title("Pareto frontier of (bundle size, top-1 accuracy)")
    ax.legend()
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    fig.savefig(OUT / "pareto.png", dpi=160)
    plt.close(fig)

    # Summary
    summary = OUT / "summary.md"
    with summary.open("w") as f:
        f.write("# Pareto frontier — bundle size vs top-1 accuracy\n\n")
        f.write(f"Generated {dt.datetime.utcnow().isoformat()}Z. "
                f"{len(cells)} cells, {flags.count(True)} on the Pareto front.\n\n")
        f.write("Cells on the frontier: configurations that no other "
                "configuration strictly dominates on both axes.\n\n")
        f.write("| Cell | Bundle (B) | Top-1 | Pareto |\n|---|---|---|---|\n")
        for _, r in cells.iterrows():
            mark = "✅" if r["pareto"] else ""
            f.write(f"| {r['cell']} | {r['bundle']:.0f} | "
                    f"{r['top1']:.3f} | {mark} |\n")

    print(f"Pareto: {flags.count(True)} cells on front, csv+png in {OUT}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
