#!/usr/bin/env python3
"""04-effect-size.py — Vargha–Delaney A12 effect size on the 4 pre-registered
configuration pairs, per `analysis-plan.md §1 L4` and §2 RQ2.

A12 is the non-parametric probability that a random sample from condition X is
larger than a random sample from condition Y (with ties counted as 0.5).
Thresholds from Vargha & Delaney 2000:

| A12 range  | Interpretation |
|------------|----------------|
| ~0.50      | negligible (no practical difference) |
| ≥0.56      | small             |
| ≥0.64      | medium            |
| ≥0.71      | large             |

We compute A12 on per-rep `top1_aligned` (0/1) per (scenario × engine × pair).
Output: one Markdown table with A12 + magnitude tag, per (scenario, engine,
pair) combination.

This complements 03-friedman.py: a Wilcoxon p < 0.05 with A12 ≈ 0.5 is
*statistically significant without practical importance* and is to be
flagged as such in the article.
"""
from __future__ import annotations

import argparse
import csv
import json
import pathlib
import sys
from collections import defaultdict


SCENARIOS = ["F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"]
ENGINES = ["rule-grounded", "llm-grounded", "llm-freeform"]
PAIRS = [("C1", "C4"), ("C4", "C5"), ("C1", "C5"), ("C3", "C4")]


def a12(x: list[float], y: list[float]) -> float:
    """Vargha–Delaney A12. Returns P(X > Y) with ties at 0.5.

    Defined exactly as in Vargha & Delaney 2000:
        A12 = #(x_i > y_j) / (n_x · n_y) + 0.5 · #(x_i == y_j) / (n_x · n_y)
    """
    if not x or not y:
        return float("nan")
    greater = 0
    equal = 0
    for xi in x:
        for yj in y:
            if xi > yj:
                greater += 1
            elif xi == yj:
                equal += 1
    return greater / (len(x) * len(y)) + 0.5 * equal / (len(x) * len(y))


def magnitude(a: float) -> str:
    if a != a:  # NaN
        return "—"
    diff = abs(a - 0.5)
    if diff < 0.06:
        return "negligible"
    if diff < 0.14:
        return "small"
    if diff < 0.21:
        return "medium"
    return "large"


def parse_run_id(run_id: str):
    p = run_id.split("-")
    return (p[0], p[1], p[2], f"{p[-2]}-{p[-1]}")


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
        default="docs/research/failure-provenance/analysis-output/04-effect-size",
    )
    args = ap.parse_args()

    rows = list(csv.DictReader(pathlib.Path(args.in_path).open()))
    matrix: dict[tuple[str, str], dict[tuple[str, str], int]] = defaultdict(dict)
    for r in rows:
        scen, rep, conf, eng = parse_run_id(r["run_id"])
        matrix[(scen, eng)][(rep, conf)] = 1 if r.get("top1_aligned") == "1" else 0

    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    results = []
    for scen in SCENARIOS:
        for eng in ENGINES:
            cells = matrix.get((scen, eng), {})
            reps = sorted({k[0] for k in cells.keys()})
            if not reps:
                continue
            for a, b in PAIRS:
                xa = [cells[(rep, a)] for rep in reps if (rep, a) in cells]
                xb = [cells[(rep, b)] for rep in reps if (rep, b) in cells]
                value = a12(xa, xb)
                results.append(
                    {
                        "scenario": scen,
                        "engine": eng,
                        "pair": f"{a}_vs_{b}",
                        "a12": value,
                        "magnitude": magnitude(value),
                        "interpretation": "X tends to be larger than Y"
                        if value > 0.56
                        else (
                            "Y tends to be larger than X"
                            if value < 0.44
                            else "no practical difference"
                        ),
                    }
                )

    (out_dir / "a12.json").write_text(
        json.dumps(
            results,
            indent=2,
            default=lambda o: None if o != o else o,
        )
        + "\n"
    )

    # Markdown table
    md = [
        "# Vargha–Delaney A12 effect size — pre-registered pairs",
        "",
        "Per (scenario × engine × pair), n = 10 reps per condition. "
        "A12 thresholds: ~0.50 negligible, ≥0.56 small, ≥0.64 medium, "
        "≥0.71 large (Vargha & Delaney 2000).",
        "",
        "| Scenario | Engine | Pair | A12 | Magnitude | Interpretation |",
        "|---|---|---|---|---|---|",
    ]
    for r in results:
        a = r["a12"]
        a_s = "NA" if a != a else f"{a:.3f}"
        md.append(
            f"| {r['scenario']} | {r['engine']} | {r['pair']} | "
            f"{a_s} | {r['magnitude']} | {r['interpretation']} |"
        )
    (out_dir / "a12.md").write_text("\n".join(md) + "\n")
    print(f"wrote {len(results)} A12 rows to {out_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
