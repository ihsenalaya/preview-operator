#!/usr/bin/env python3
"""
30-evidence-ladder.py — L1 → L4 controlled comparison.

Reviewer critique #2: collapse the C1-C5 + B0 + B2 baselines into a single,
reader-friendly evidence ladder that isolates the contribution of
*provenance structure* vs *more evidence* vs *raw kubectl* vs *logs only*.

The ladder:

  L1  logs only ............ C1 of the operator's matrix (pod/job logs only)
  L2  raw kubectl .......... B0 — vanilla LLM on raw `kubectl get/logs/...`
                              dumps (no operator artefact, no provenance graph)
  L3  FailureReport, no prov. C4 of the operator's matrix (typed bundle, no
                              PROV graph edges)
  L4  FailureReport + prov.  C5 of the operator's matrix (typed bundle + PROV
                              graph: ChangeContext → Resource → Evidence)

For each ladder rung × engine, we report aligned top-1 (S1, n=100 per cell)
with Wilson 95 % CIs.

Output:
  analysis-output/30-evidence-ladder/
    ladder-S1.csv         (raw long-format)
    ladder-S1.md          (markdown summary table)
    ladder-S1.png         (bar plot — 4 rungs × engines)
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

REPO = Path(__file__).resolve().parents[3]
RESCORED = REPO / "docs/research/failure-provenance/analysis-output/08-vocab-rescore/results-rescored.csv"
B0_CSV = REPO / "docs/research/failure-provenance/analysis-output/10-b0-report/results-b0.csv"
OUT = REPO / "docs/research/failure-provenance/analysis-output/30-evidence-ladder"
OUT.mkdir(parents=True, exist_ok=True)


def engine_from_notes(s) -> str:
    if pd.isna(s):
        return "unknown"
    if "engine=rule mode=grounded" in s:
        return "rule-grounded"
    if "engine=llm mode=grounded" in s:
        return "llm-grounded"
    if "engine=llm mode=freeform" in s:
        return "llm-freeform"
    return "other"


def wilson(k: int, n: int) -> tuple[float, float, float]:
    if n == 0:
        return 0.0, 0.0, 0.0
    p = k / n
    ci = binomtest(k, n).proportion_ci(method="wilson")
    return p, ci.low, ci.high


def main() -> int:
    df = pd.read_csv(RESCORED)
    df["engine"] = df["notes"].apply(engine_from_notes)
    df = df[df["engine"].isin({"rule-grounded", "llm-grounded", "llm-freeform"})].copy()
    df["top1_aligned"] = df["top1_aligned"].astype(int)

    # Map C1 -> L1, C4 -> L3, C5 -> L4
    rung_map = {"C1": "L1 (logs only)", "C4": "L3 (FR, no prov.)", "C5": "L4 (FR + prov.)"}
    df_op = df[df["configuration"].isin(rung_map)].copy()
    df_op["rung"] = df_op["configuration"].map(rung_map)

    # B0 = L2
    b0 = pd.read_csv(B0_CSV)
    b0_engine_label = "llm-grounded"  # B0 is the vanilla LLM consuming raw kubectl
    b0 = b0.copy()
    b0["engine"] = "B0 (vanilla LLM)"
    b0["rung"] = "L2 (raw kubectl)"
    b0["top1_aligned"] = b0["top1_aligned"].astype(int)
    # Keep one row per scenario × rep at the bundle level (no configuration concept).

    # Long-format combined frame.
    long_rows = []
    # Operator rungs
    for (rung, engine), g in df_op.groupby(["rung", "engine"]):
        k = int(g["top1_aligned"].sum())
        n = int(len(g))
        p, lo, hi = wilson(k, n)
        long_rows.append({"rung": rung, "engine": engine, "k": k, "n": n,
                          "aligned_top1": p, "ci_lo": lo, "ci_hi": hi})

    # B0 rung
    for engine, g in b0.groupby("engine"):
        k = int(g["top1_aligned"].sum())
        n = int(len(g))
        p, lo, hi = wilson(k, n)
        long_rows.append({"rung": "L2 (raw kubectl)", "engine": engine,
                          "k": k, "n": n, "aligned_top1": p,
                          "ci_lo": lo, "ci_hi": hi})

    long = pd.DataFrame(long_rows)
    long.to_csv(OUT / "ladder-S1.csv", index=False)

    # Markdown table.
    md_lines = [
        "# Evidence ladder L1 → L4 (S1 only, aligned top-1)",
        "",
        "Controlled comparison addressing reviewer critique on RCA accuracy. The ",
        "ladder isolates the contribution of *provenance structure* vs *typed evidence* "
        "vs *raw kubectl* vs *logs only*:",
        "",
        "| Rung | What the diagnoser sees | Source |",
        "|---|---|---|",
        "| **L1** | Pod / job logs only | C1 of operator matrix |",
        "| **L2** | Raw `kubectl get/logs/...` dumps (no operator artefact) | B0 vanilla-LLM baseline |",
        "| **L3** | Full typed `FailureReport` bundle (no PROV graph) | C4 of operator matrix |",
        "| **L4** | `FailureReport` + W3C-PROV provenance graph | C5 of operator matrix |",
        "",
        "Each cell shows aligned top-1 with Wilson 95 % CI; n is the number of "
        "(scenario × rep) pairs.",
        "",
        "## Aligned top-1 by rung × engine",
        "",
        "| Rung | rule-grounded | llm-grounded | llm-freeform | B0 (vanilla LLM) |",
        "|---|---|---|---|---|",
    ]

    rungs_order = ["L1 (logs only)", "L2 (raw kubectl)",
                   "L3 (FR, no prov.)", "L4 (FR + prov.)"]
    eng_order = ["rule-grounded", "llm-grounded", "llm-freeform", "B0 (vanilla LLM)"]
    for rung in rungs_order:
        row = [rung]
        for eng in eng_order:
            cell = long[(long["rung"] == rung) & (long["engine"] == eng)]
            if cell.empty:
                row.append("—")
            else:
                r = cell.iloc[0]
                row.append(f"{r['aligned_top1']:.3f} [{r['ci_lo']:.3f}, {r['ci_hi']:.3f}] (n={int(r['n'])})")
        md_lines.append("| " + " | ".join(row) + " |")

    md_lines += [
        "",
        "## Reading",
        "",
        "- **L1 → L2** is the *evidence-volume* effect alone (no operator-side ",
        "  structuring). Each delta isolates *what richer raw evidence buys you* ",
        "  before structuring.",
        "- **L2 → L3** is the contribution of **operator-side capture + typed schema** ",
        "  *without* PROV linkage. Reads off how much *structuring* (vs raw kubectl) buys.",
        "- **L3 → L4** is the contribution of the **provenance graph** on top of the ",
        "  typed bundle. Reads off how much *PROV linkage* (vs flat typed evidence) buys.",
        "- L2 is reported under `engine = B0`, which is the vanilla LLM (`gpt-4o-mini`) ",
        "  reading raw `kubectl` output. The operator engines (rule / llm-grounded / ",
        "  llm-freeform) consume the operator's bundle at the appropriate level.",
        "",
        "## Honesty notes (regime symmetry)",
        "",
        "- L1, L3, and L4 are **post-teardown** (the operator persists the bundle in a ",
        "  cluster-scoped CR; the diagnoser reads it offline). L2 (raw kubectl) is ",
        "  captured **before** teardown but read offline — the artefacts are stored in ",
        "  `results-matrix/F*/r*/artifacts/{events,pods,deployments,logs}.txt`. ",
        "  So all four rungs operate on **frozen text** in the same regime; the ",
        "  cluster is not interrogated again.",
        "- The K8sGPT / Kagent comparators reported in §5.8 of the article run on the ",
        "  *live* cluster and are kept as a separate practitioner contextualisation. ",
        "  A post-teardown K8sGPT run on the same `evidenceItems` is queued as future ",
        "  work (script: `28-k8sgpt-post-teardown.py`).",
        "",
        "Source: `analysis-output/08-vocab-rescore/results-rescored.csv` (operator),",
        "        `analysis-output/10-b0-report/results-b0.csv` (B0 baseline).",
        "Script: `experiments/failure-provenance/analysis/30-evidence-ladder.py`.",
        "",
    ]
    (OUT / "ladder-S1.md").write_text("\n".join(md_lines))

    # --------------- figure: grouped bar plot --------------------------- #
    fig, ax = plt.subplots(figsize=(7.5, 3.8), dpi=130)
    rungs_short = ["L1\nlogs only", "L2\nraw kubectl", "L3\nFR no prov.", "L4\nFR + prov."]
    engines = ["rule-grounded", "llm-grounded", "llm-freeform", "B0 (vanilla LLM)"]
    colors = ["#2A6F97", "#E07A5F", "#81B29A", "#9C6644"]

    x = np.arange(len(rungs_short))
    width = 0.2
    for i, eng in enumerate(engines):
        vals, los, his = [], [], []
        for rung_full in rungs_order:
            cell = long[(long["rung"] == rung_full) & (long["engine"] == eng)]
            if cell.empty:
                vals.append(np.nan); los.append(np.nan); his.append(np.nan)
            else:
                r = cell.iloc[0]
                vals.append(r["aligned_top1"]); los.append(r["ci_lo"]); his.append(r["ci_hi"])
        vals = np.array(vals, dtype=float)
        los = np.array(los, dtype=float)
        his = np.array(his, dtype=float)
        # error bars relative to mean
        yerr_lo = np.where(np.isnan(vals), 0, vals - los)
        yerr_hi = np.where(np.isnan(vals), 0, his - vals)
        offset = (i - 1.5) * width
        bar_pos = x + offset
        mask = ~np.isnan(vals)
        ax.bar(bar_pos[mask], vals[mask], width, label=eng, color=colors[i],
               edgecolor="white", linewidth=0.7,
               yerr=[yerr_lo[mask], yerr_hi[mask]], capsize=2, ecolor="#333", alpha=0.92)

    ax.set_xticks(x)
    ax.set_xticklabels(rungs_short, fontsize=9)
    ax.set_ylabel("aligned top-1", fontsize=10)
    ax.set_ylim(0, 0.75)
    ax.set_title("Evidence ladder — S1, aligned top-1 with Wilson 95 % CIs", fontsize=10)
    ax.legend(loc="upper left", fontsize=8, framealpha=0.95)
    ax.grid(axis="y", linestyle=":", alpha=0.4)
    plt.tight_layout()
    plt.savefig(OUT / "ladder-S1.png", bbox_inches="tight")
    plt.savefig(REPO / "docs/research/failure-provenance/latex/figures/ladder-S1.png",
                bbox_inches="tight")
    plt.close()

    print("wrote:", OUT / "ladder-S1.csv")
    print("wrote:", OUT / "ladder-S1.md")
    print("wrote:", OUT / "ladder-S1.png (+ latex/figures/)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
