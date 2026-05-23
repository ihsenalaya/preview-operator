#!/usr/bin/env python3
"""22-tukey.py — L4 pairwise contrasts (Tukey HSD).

Pre-registered in analysis-plan §1 L4. Runs Tukey's Honest Significant
Difference across the C1..C5 configurations, per LLM, on the top-1
binary outcome. Treats each (scenario × rep) cell as the replicate.

Output:
  docs/research/failure-provenance/analysis-output/22-tukey/tukey_llm_a.csv
  docs/research/failure-provenance/analysis-output/22-tukey/tukey_llm_b.csv
  docs/research/failure-provenance/analysis-output/22-tukey/summary.md
"""
from __future__ import annotations
import csv
import pathlib
import sys
import datetime as dt

import pandas as pd
from statsmodels.stats.multicomp import pairwise_tukeyhsd

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/22-tukey"
OUT.mkdir(parents=True, exist_ok=True)

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from _loader import load_unified


def main() -> int:
    df = load_unified()
    df = df[df["engine"] == "llm"].copy()
    if df.empty:
        print("no LLM rows", file=sys.stderr)
        return 1

    summary_path = OUT / "summary.md"
    with summary_path.open("w") as smd:
        smd.write("# L4 — Tukey HSD pairwise contrasts\n\n")
        smd.write(f"Generated {dt.datetime.utcnow().isoformat()}Z.\n\n")
        for llm in sorted(df["llm"].unique()):
            sub = df[df["llm"] == llm]
            res = pairwise_tukeyhsd(endog=sub["correct"].astype(float),
                                     groups=sub["configuration"],
                                     alpha=0.05)
            csv_path = OUT / f"tukey_llm_{llm.lower()}.csv"
            with csv_path.open("w", newline="") as f:
                w = csv.writer(f)
                w.writerow(["group1", "group2", "meandiff", "p_adj",
                            "lower", "upper", "reject"])
                for row in res._results_table.data[1:]:
                    w.writerow(row)
            smd.write(f"## LLM={llm}\n\n")
            smd.write(f"n = {len(sub)}\n\n```\n")
            smd.write(str(res))
            smd.write("\n```\n\n")
    print(f"Tukey done: {OUT}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
