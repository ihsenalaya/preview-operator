#!/usr/bin/env python3
"""01-descriptive.py — Layer L1 (descriptive) of analysis-plan.md.

For every (scenario × configuration × engine_mode) cell:
- n: rows in the cell
- top1_correct mean and 95% Wilson CI
- top3_correct mean and 95% Wilson CI
- evidence_precision / evidence_recall median + IQR
- bundle_size_bytes median + IQR

Writes one Markdown table per scenario plus a global summary table.
Output: docs/research/failure-provenance/analysis-output/01-descriptive/
"""
from __future__ import annotations

import argparse
import collections
import csv
import math
import pathlib
import statistics
import sys


SCENARIOS = ["F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"]
CONFIGS = ["C1", "C2", "C3", "C4", "C5"]
ENGINE_MODES = ["rule-grounded", "llm-grounded", "llm-freeform"]


def wilson_ci(successes: int, n: int, z: float = 1.96) -> tuple[float, float]:
    """Wilson score 95% CI for a binomial proportion.

    From Wilson 1927 / Brown-Cai-DasGupta 2001 review — the recommended
    interval for proportions because it stays inside [0,1] and behaves
    well at the boundaries.
    """
    if n == 0:
        return (0.0, 0.0)
    p = successes / n
    denom = 1.0 + z * z / n
    centre = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return (max(0.0, centre - half), min(1.0, centre + half))


def parse_engine_mode(run_id: str) -> str:
    parts = run_id.split("-")
    if len(parts) < 2:
        return ""
    return f"{parts[-2]}-{parts[-1]}"


def fmt_pct(p: float) -> str:
    return f"{100*p:5.1f}%"


def fmt_ci(low: float, high: float) -> str:
    return f"[{100*low:5.1f}%, {100*high:5.1f}%]"


def fmt_iqr(values: list[float]) -> str:
    if not values:
        return "—"
    if len(values) == 1:
        return f"{values[0]:.0f}"
    s = sorted(values)
    q1 = s[len(s) // 4]
    q3 = s[(3 * len(s)) // 4]
    med = statistics.median(s)
    return f"{med:.0f} ({q1:.0f}–{q3:.0f})"


def load_rows(csv_path: pathlib.Path) -> list[dict]:
    with csv_path.open() as fh:
        return list(csv.DictReader(fh))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in",
        dest="in_path",
        default="results-matrix/results-augmented.csv",
    )
    ap.add_argument(
        "--out-dir",
        dest="out_dir",
        default="docs/research/failure-provenance/analysis-output/01-descriptive",
    )
    args = ap.parse_args()

    rows = load_rows(pathlib.Path(args.in_path))
    print(f"loaded {len(rows)} rows from {args.in_path}")

    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    # Bucket rows by (scenario, config, engine_mode).
    cells: dict[tuple[str, str, str], list[dict]] = collections.defaultdict(list)
    for row in rows:
        scen = row.get("scenario_id", "")
        conf = row.get("configuration", "")
        engine = parse_engine_mode(row.get("run_id", ""))
        if scen and conf and engine:
            cells[(scen, conf, engine)].append(row)

    # Per-scenario tables.
    for scen in SCENARIOS:
        lines = [
            f"# {scen} — descriptive statistics (Phase 5a, LLM-A only)",
            "",
            "**n = 10 reps per cell. Wilson 95% CI on proportions, median (IQR) "
            "on bytes.**",
            "",
            "| Engine mode | Cfg | n | top1 mean | top1 95% CI | top3 mean | "
            "ev. precision | ev. recall | bundle bytes |",
            "|---|---|---|---|---|---|---|---|---|",
        ]
        for engine in ENGINE_MODES:
            for conf in CONFIGS:
                cell = cells.get((scen, conf, engine), [])
                n = len(cell)
                if n == 0:
                    lines.append(
                        f"| {engine} | {conf} | 0 | — | — | — | — | — | — |"
                    )
                    continue
                top1 = sum(1 for r in cell if r.get("top1_correct") == "1")
                top3 = sum(1 for r in cell if r.get("top3_correct") == "1")
                p1 = top1 / n
                lo1, hi1 = wilson_ci(top1, n)
                p3 = top3 / n
                prec = [
                    float(r["evidence_precision"])
                    for r in cell
                    if r.get("evidence_precision")
                ]
                rec = [
                    float(r["evidence_recall"])
                    for r in cell
                    if r.get("evidence_recall")
                ]
                size = [
                    float(r["bundle_size_bytes"])
                    for r in cell
                    if r.get("bundle_size_bytes")
                ]
                lines.append(
                    f"| {engine} | {conf} | {n} | {fmt_pct(p1)} | "
                    f"{fmt_ci(lo1, hi1)} | {fmt_pct(p3)} | "
                    f"{fmt_iqr(prec)} | {fmt_iqr(rec)} | {fmt_iqr(size)} |"
                )
        (out_dir / f"{scen}.md").write_text("\n".join(lines) + "\n")

    # Global table: top-1 only, across all scenarios.
    lines = [
        "# All scenarios — top-1 accuracy (Phase 5a, LLM-A only)",
        "",
        "| Scenario | Engine mode | Pool n | top1 mean | Wilson 95% CI |",
        "|---|---|---|---|---|",
    ]
    for scen in SCENARIOS:
        for engine in ENGINE_MODES:
            cell = []
            for conf in CONFIGS:
                cell.extend(cells.get((scen, conf, engine), []))
            n = len(cell)
            if n == 0:
                continue
            top1 = sum(1 for r in cell if r.get("top1_correct") == "1")
            p = top1 / n
            lo, hi = wilson_ci(top1, n)
            lines.append(
                f"| {scen} | {engine} | {n} | {fmt_pct(p)} | {fmt_ci(lo, hi)} |"
            )
    (out_dir / "global-top1.md").write_text("\n".join(lines) + "\n")
    print(f"wrote per-scenario tables + global-top1.md to {out_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
