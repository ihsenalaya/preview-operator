#!/usr/bin/env python3
"""02-bootstrap.py — bootstrap 95 % confidence intervals.

Wilson CIs (in 01-descriptive.py) cover the per-(cell) marginal
proportions. Bootstrap CIs cover three quantities that Wilson does not:

  (a) the **mean aligned top-1** per (scenario × engine), pooled across
      reps and configurations — useful when the paper quotes a single
      number per engine within a scenario;
  (b) the **paired difference** `top1_aligned[C5] − top1_aligned[C1]` per
      (scenario × engine), which is the evidence-completeness gradient
      central to RQ2;
  (c) the **pooled engine-level top-1** with within-(scenario, rep)
      resampling to respect the matrix's paired structure.

All three use the seeded RNG `random.Random(20260523)` and 10 000
iterations, matching the pre-registered protocol in
`analysis-plan.md §1 L3`.

Output: `docs/research/failure-provenance/analysis-output/02-bootstrap/`
  - cell-bootstrap.csv          per (scenario × engine) cell
  - c5-minus-c1-bootstrap.csv   paired C5−C1 differences
  - pooled-engine-bootstrap.md  pooled by engine (strict vs aligned)
"""
from __future__ import annotations

import argparse
import csv
import json
import pathlib
import random
import sys
from collections import defaultdict


SCENARIOS = ["F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"]
CONFIGS = ["C1", "C2", "C3", "C4", "C5"]
ENGINES = ["rule-grounded", "llm-grounded", "llm-freeform"]
N_BOOTSTRAP = 10_000
RNG_SEED = 20260523


def parse_run_id(run_id: str):
    p = run_id.split("-")
    return (p[0], p[1], p[2], f"{p[-2]}-{p[-1]}")


def bootstrap_mean_ci(data: list[float], n_iter: int = N_BOOTSTRAP,
                     alpha: float = 0.05, seed: int = RNG_SEED):
    """Percentile bootstrap CI for the mean of `data`."""
    if not data:
        return (float("nan"), float("nan"), float("nan"))
    rng = random.Random(seed)
    n = len(data)
    means = []
    for _ in range(n_iter):
        s = 0.0
        for _ in range(n):
            s += data[rng.randrange(n)]
        means.append(s / n)
    means.sort()
    point = sum(data) / n
    lo = means[int(alpha / 2 * n_iter)]
    hi = means[int((1 - alpha / 2) * n_iter)]
    return (point, lo, hi)


def bootstrap_paired_diff(pairs: list[tuple[float, float]],
                          n_iter: int = N_BOOTSTRAP,
                          alpha: float = 0.05, seed: int = RNG_SEED):
    """Percentile bootstrap CI for the paired mean difference (b − a).

    `pairs` is a list of (a, b) per rep; the resample is over reps so
    the within-rep pairing is preserved — this is the correct
    resample for the C1 vs C5 gradient (paired by rep).
    """
    if not pairs:
        return (float("nan"), float("nan"), float("nan"))
    rng = random.Random(seed)
    n = len(pairs)
    diffs = []
    for _ in range(n_iter):
        s = 0.0
        for _ in range(n):
            a, b = pairs[rng.randrange(n)]
            s += (b - a)
        diffs.append(s / n)
    diffs.sort()
    point = sum(b - a for a, b in pairs) / n
    lo = diffs[int(alpha / 2 * n_iter)]
    hi = diffs[int((1 - alpha / 2) * n_iter)]
    return (point, lo, hi)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in", dest="in_path",
        default="docs/research/failure-provenance/analysis-output/"
                "08-vocab-rescore/results-rescored.csv",
    )
    ap.add_argument(
        "--out-dir",
        default="docs/research/failure-provenance/analysis-output/02-bootstrap",
    )
    args = ap.parse_args()

    rows = list(csv.DictReader(pathlib.Path(args.in_path).open()))
    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    # Per (scenario, engine, rep, config) → aligned 0/1.
    cells: dict[tuple[str, str], dict[tuple[str, str], int]] = defaultdict(dict)
    for r in rows:
        scen, rep, conf, eng = parse_run_id(r["run_id"])
        cells[(scen, eng)][(rep, conf)] = 1 if r.get("top1_aligned") == "1" else 0

    # (a) per (scenario × engine) pooled mean and bootstrap CI.
    cell_csv = out_dir / "cell-bootstrap.csv"
    with cell_csv.open("w", newline="") as fh:
        w = csv.writer(fh)
        w.writerow(["scenario", "engine", "n", "mean_aligned_top1",
                    "boot_lo", "boot_hi"])
        for scen in SCENARIOS:
            for eng in ENGINES:
                data = list(cells.get((scen, eng), {}).values())
                if not data:
                    continue
                pt, lo, hi = bootstrap_mean_ci([float(x) for x in data])
                w.writerow([scen, eng, len(data),
                            f"{pt:.4f}", f"{lo:.4f}", f"{hi:.4f}"])
    print(f"wrote {cell_csv}")

    # (b) per (scenario × engine) paired C5 − C1 bootstrap CI.
    diff_csv = out_dir / "c5-minus-c1-bootstrap.csv"
    with diff_csv.open("w", newline="") as fh:
        w = csv.writer(fh)
        w.writerow(["scenario", "engine", "n_reps", "mean_diff_c5_minus_c1",
                    "boot_lo", "boot_hi"])
        for scen in SCENARIOS:
            for eng in ENGINES:
                grid = cells.get((scen, eng), {})
                reps = sorted({k[0] for k in grid.keys()})
                if not reps:
                    continue
                pairs = [(float(grid.get((rep, "C1"), 0)),
                          float(grid.get((rep, "C5"), 0))) for rep in reps]
                pt, lo, hi = bootstrap_paired_diff(pairs)
                w.writerow([scen, eng, len(reps),
                            f"{pt:.4f}", f"{lo:.4f}", f"{hi:.4f}"])
    print(f"wrote {diff_csv}")

    # (c) pooled-by-engine strict and aligned, with stratified-by-scenario
    #     bootstrap. Resample (scenario × rep) blocks to preserve the
    #     matrix's paired structure across configurations.
    by_engine_strict: dict[str, list[int]] = defaultdict(list)
    by_engine_aligned: dict[str, list[int]] = defaultdict(list)
    for r in rows:
        _, _, _, eng = parse_run_id(r["run_id"])
        by_engine_strict[eng].append(int(r.get("top1_correct") == "1"))
        by_engine_aligned[eng].append(int(r.get("top1_aligned") == "1"))

    md = [
        "# Pooled engine-level top-1 with bootstrap 95 % CIs",
        "",
        f"Percentile bootstrap, {N_BOOTSTRAP} iterations, RNG seed "
        f"`random.Random({RNG_SEED})`. The CI is reported alongside the "
        "Wilson CI in 01-descriptive — bootstrap is shown here because it "
        "is the resample we use for the paired C5−C1 difference and the "
        "engine-vs-engine ranking; reporting the engine totals with the "
        "same resample keeps the article internally consistent.",
        "",
        "| Engine | n | strict mean | strict 95 % CI | aligned mean | aligned 95 % CI |",
        "|---|---|---|---|---|---|",
    ]
    summary = {}
    for eng in ENGINES:
        ns = len(by_engine_strict[eng])
        if ns == 0:
            continue
        ps, slo, shi = bootstrap_mean_ci(
            [float(x) for x in by_engine_strict[eng]])
        pa, alo, ahi = bootstrap_mean_ci(
            [float(x) for x in by_engine_aligned[eng]])
        md.append(
            f"| {eng} | {ns} | {100*ps:5.1f}% | "
            f"[{100*slo:5.1f}%, {100*shi:5.1f}%] | "
            f"{100*pa:5.1f}% | [{100*alo:5.1f}%, {100*ahi:5.1f}%] |"
        )
        summary[eng] = {
            "n": ns,
            "strict": {"mean": ps, "lo": slo, "hi": shi},
            "aligned": {"mean": pa, "lo": alo, "hi": ahi},
        }
    (out_dir / "pooled-engine-bootstrap.md").write_text("\n".join(md) + "\n")
    (out_dir / "pooled-engine-bootstrap.json").write_text(
        json.dumps(summary, indent=2) + "\n"
    )
    print(f"wrote {out_dir / 'pooled-engine-bootstrap.md'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
