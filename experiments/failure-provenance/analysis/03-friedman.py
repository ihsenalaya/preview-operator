#!/usr/bin/env python3
"""03-friedman.py — Friedman omnibus + Wilcoxon signed-rank on the
4 pre-registered configuration pairs (C1↔C4, C4↔C5, C1↔C5, C3↔C4),
per `analysis-plan.md §1 L3b` and `§2 RQ2`.

Treats each (scenario, engine) cell as a separate Friedman experiment:
- 10 reps × 5 configurations = paired blocks
- Wilcoxon signed-rank on the 4 pre-registered pairs

Holm-Bonferroni correction over the 4 pairs per (scenario, engine).

Output:
- `friedman-omnibus.md` — one row per (scenario × engine), with χ²
  statistic, df=4, p-value
- `wilcoxon-pairs.md` — one row per (scenario × engine × pair), with
  V statistic, p-value, Holm-adjusted p
- `friedman.json` — machine-readable

This script uses **scipy** (Friedman + Wilcoxon are non-trivial to roll
by hand for tied ranks). The metric is the per-rep `top1_aligned` value
(0/1) — Friedman is robust to binary outcomes when within-subject
blocks differ.
"""
from __future__ import annotations

import argparse
import csv
import json
import pathlib
import sys
from collections import defaultdict

import scipy.stats as ss


SCENARIOS = ["F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"]
CONFIGS = ["C1", "C2", "C3", "C4", "C5"]
ENGINES = ["rule-grounded", "llm-grounded", "llm-freeform"]
# Pre-registered pairs from analysis-plan.md §2 RQ2.
PAIRS = [("C1", "C4"), ("C4", "C5"), ("C1", "C5"), ("C3", "C4")]


def parse_run_id(run_id: str):
    p = run_id.split("-")
    return (p[0], p[1], p[2], f"{p[-2]}-{p[-1]}")


def holm_adjust(p_values: list[float]) -> list[float]:
    """Holm-Bonferroni step-down adjusted p-values (Holm 1979)."""
    n = len(p_values)
    order = sorted(range(n), key=lambda i: p_values[i])
    adjusted = [0.0] * n
    running_max = 0.0
    for rank, idx in enumerate(order):
        v = (n - rank) * p_values[idx]
        running_max = max(running_max, v)
        adjusted[idx] = min(1.0, running_max)
    return adjusted


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
        default="docs/research/failure-provenance/analysis-output/03-friedman",
    )
    args = ap.parse_args()

    rows = list(csv.DictReader(pathlib.Path(args.in_path).open()))
    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    # Index per-(scenario, engine, rep, config) → 1/0 aligned correct
    matrix: dict[tuple[str, str], dict[tuple[str, str], int]] = defaultdict(
        dict
    )
    for r in rows:
        scen, rep, conf, eng = parse_run_id(r["run_id"])
        val = 1 if r.get("top1_aligned") == "1" else 0
        matrix[(scen, eng)][(rep, conf)] = val

    friedman_results = []
    wilcoxon_results = []

    for scen in SCENARIOS:
        for eng in ENGINES:
            cells = matrix.get((scen, eng), {})
            # Build the rep × config grid
            reps = sorted({k[0] for k in cells.keys()})
            grid = [
                [cells.get((rep, conf), None) for conf in CONFIGS]
                for rep in reps
            ]
            if not grid or any(None in row for row in grid):
                continue
            # Friedman omnibus
            cols = list(zip(*grid))  # 5 lists of n=10
            try:
                stat, p = ss.friedmanchisquare(*cols)
            except ValueError:
                # all values equal → Friedman ill-defined; report NA
                stat, p = float("nan"), float("nan")
            friedman_results.append(
                {
                    "scenario": scen,
                    "engine": eng,
                    "n_reps": len(reps),
                    "chi_square": stat,
                    "df": len(CONFIGS) - 1,
                    "p_value": p,
                }
            )

            # Per-pair Wilcoxon
            pair_p = []
            pair_stat = []
            for a, b in PAIRS:
                xa = [cells[(rep, a)] for rep in reps]
                xb = [cells[(rep, b)] for rep in reps]
                # Wilcoxon signed-rank requires non-zero differences;
                # scipy <1.6 used wilcox_pratt; current accepts zero_method
                try:
                    if all(xa[i] == xb[i] for i in range(len(xa))):
                        v, pv = float("nan"), 1.0
                    else:
                        v, pv = ss.wilcoxon(
                            xa, xb, zero_method="wilcox", alternative="two-sided"
                        )
                except ValueError:
                    v, pv = float("nan"), float("nan")
                pair_p.append(pv)
                pair_stat.append(v)
            adj = holm_adjust(pair_p)
            for (a, b), v, p, adj_p in zip(PAIRS, pair_stat, pair_p, adj):
                wilcoxon_results.append(
                    {
                        "scenario": scen,
                        "engine": eng,
                        "pair": f"{a}_vs_{b}",
                        "n_reps": len(reps),
                        "wilcoxon_V": v,
                        "p_value": p,
                        "p_holm": adj_p,
                    }
                )

    # Write outputs
    (out_dir / "friedman.json").write_text(
        json.dumps(
            {
                "friedman": friedman_results,
                "wilcoxon": wilcoxon_results,
            },
            indent=2,
            default=lambda o: None if o != o else o,  # NaN → null
        )
        + "\n"
    )

    # Friedman MD
    md = [
        "# Friedman omnibus test — RQ2 across C1–C5",
        "",
        "Per (scenario × engine), Friedman test on the per-rep `top1_aligned` "
        "values across the 5 evidence levels (k = 5, n = 10).",
        "",
        "| Scenario | Engine | χ² | df | p-value |",
        "|---|---|---|---|---|",
    ]
    for r in friedman_results:
        chi = r["chi_square"]
        p = r["p_value"]
        chi_s = "NA" if chi != chi else f"{chi:.3f}"
        p_s = "NA" if p != p else f"{p:.4f}"
        md.append(
            f"| {r['scenario']} | {r['engine']} | {chi_s} | {r['df']} | {p_s} |"
        )
    (out_dir / "friedman-omnibus.md").write_text("\n".join(md) + "\n")

    # Wilcoxon MD
    md = [
        "# Wilcoxon signed-rank — pre-registered pairs (Holm-corrected)",
        "",
        "Pre-registered pairs from `analysis-plan.md §2 RQ2`: C1↔C4, "
        "C4↔C5, C1↔C5, C3↔C4. Holm-Bonferroni FWER ≤ 0.05.",
        "",
        "| Scenario | Engine | Pair | V | p (raw) | p (Holm) |",
        "|---|---|---|---|---|---|",
    ]
    for r in wilcoxon_results:
        v = r["wilcoxon_V"]
        p = r["p_value"]
        ph = r["p_holm"]
        v_s = "NA" if v != v else f"{v:.2f}"
        p_s = "NA" if p != p else f"{p:.4f}"
        ph_s = "NA" if ph != ph else f"{ph:.4f}"
        sig = " **\\*\\***" if ph == ph and ph < 0.01 else (
            " **\\***" if ph == ph and ph < 0.05 else ""
        )
        md.append(
            f"| {r['scenario']} | {r['engine']} | {r['pair']} | {v_s} | "
            f"{p_s} | {ph_s}{sig} |"
        )
    (out_dir / "wilcoxon-pairs.md").write_text("\n".join(md) + "\n")

    print(
        f"wrote {len(friedman_results)} Friedman rows + "
        f"{len(wilcoxon_results)} Wilcoxon rows to {out_dir}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
