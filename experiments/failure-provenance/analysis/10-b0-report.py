#!/usr/bin/env python3
"""10-b0-report.py — emit the B0 baseline report (Markdown + JSON).

Reads `results-b0.csv` (produced by 09-baseline-b0.py) and writes a per-
scenario × engine comparison vs the operator's pooled C5 numbers from the
already-rescored CSV, plus a one-page Markdown summary that lands in the
article's Evaluation section.

Output dir: `docs/research/failure-provenance/analysis-output/10-b0-report/`
"""
from __future__ import annotations

import argparse
import csv
import json
import math
import pathlib
import sys
from collections import defaultdict


SCENARIOS = ["F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"]
ENGINES = ["rule-grounded", "llm-grounded", "llm-freeform"]


def wilson_ci(s: int, n: int, z: float = 1.96):
    if n == 0:
        return (0.0, 0.0)
    p = s / n
    denom = 1.0 + z * z / n
    centre = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return (max(0.0, centre - half), min(1.0, centre + half))


def fmt_pct(p: float) -> str:
    return f"{100*p:5.1f}%"


def parse_run_id(run_id: str):
    p = run_id.split("-")
    return (p[0], p[1], p[2], f"{p[-2]}-{p[-1]}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--b0",
        default="experiments/failure-provenance/results-matrix/results-b0.csv",
    )
    ap.add_argument(
        "--operator",
        default="docs/research/failure-provenance/analysis-output/"
        "08-vocab-rescore/results-rescored.csv",
    )
    ap.add_argument(
        "--out-dir",
        default="docs/research/failure-provenance/analysis-output/10-b0-report",
    )
    args = ap.parse_args()

    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    b0_rows = list(csv.DictReader(pathlib.Path(args.b0).open()))
    op_rows = list(csv.DictReader(pathlib.Path(args.operator).open()))

    # Per-scenario B0 numbers.
    b0_by_scen: dict[str, list[int]] = defaultdict(list)
    b0_strict: dict[str, list[int]] = defaultdict(list)
    b0_cat: dict[str, list[int]] = defaultdict(list)
    for r in b0_rows:
        s = r["scenario_id"]
        b0_by_scen[s].append(int(r["top1_aligned"]))
        b0_strict[s].append(int(r["top1_correct"]))
        b0_cat[s].append(int(r["category_correct"]))

    # Operator pooled (C1-C5) per (scenario, engine).
    op_by: dict[tuple[str, str], list[int]] = defaultdict(list)
    for r in op_rows:
        scen, rep, conf, eng = parse_run_id(r["run_id"])
        op_by[(scen, eng)].append(int(r["top1_aligned"] == "1"))

    # Operator pooled best engine per scenario.
    best_by_scen: dict[str, tuple[str, int, int]] = {}
    for scen in SCENARIOS:
        best = None
        for eng in ENGINES:
            data = op_by.get((scen, eng), [])
            n = len(data)
            if n == 0:
                continue
            s = sum(data)
            if best is None or s / n > best[1] / best[2]:
                best = (eng, s, n)
        if best:
            best_by_scen[scen] = best

    # Markdown
    md = [
        "# B0 baseline vs operator engines — aligned top-1, per scenario",
        "",
        "B0 = vanilla LLM (`gpt-4o-mini-2024-07-18`, temperature 0) prompted "
        "with raw kubectl output (events, pods, deployments, services, "
        "endpoints, jobs, container logs) collected from each failing "
        "preview namespace. No operator artifacts (FailureReport, "
        "evidenceRefs, reconcile events) are exposed to the model. This is "
        "the floor: what an SRE would get by pasting kubectl into a chat "
        "LLM.",
        "",
        "The operator column is the pooled C1-C5 aligned top-1 across the "
        "best-performing engine per scenario (matcher: per-scenario alias "
        "table, identical to B0).",
        "",
        "| Scenario | n (B0) | B0 aligned top-1 | B0 category top-1 | "
        "Best operator engine | Operator aligned top-1 | gap |",
        "|---|---|---|---|---|---|---|",
    ]
    payload = []
    for scen in SCENARIOS:
        n = len(b0_by_scen[scen])
        if n == 0:
            continue
        b0_a = sum(b0_by_scen[scen])
        b0_c = sum(b0_cat[scen])
        op = best_by_scen.get(scen)
        if op:
            eng, op_s, op_n = op
            op_p = op_s / op_n
            gap = op_p - b0_a / n
            gap_s = f"+{100*gap:.0f} pp"
        else:
            eng = "—"
            op_p = float("nan")
            gap_s = "—"
        md.append(
            f"| {scen} | {n} | {fmt_pct(b0_a/n)} ({b0_a}/{n}) | "
            f"{fmt_pct(b0_c/n)} ({b0_c}/{n}) | "
            f"{eng} | {fmt_pct(op_p) if op_p == op_p else '—'} "
            f"({op_s}/{op_n}) | {gap_s} |"
        )
        payload.append({
            "scenario": scen,
            "n_b0": n,
            "b0_aligned": b0_a,
            "b0_category": b0_c,
            "best_operator_engine": eng,
            "operator_aligned": op_s if op else None,
            "operator_n": op_n if op else None,
        })

    # Pooled totals
    n_total = sum(len(v) for v in b0_by_scen.values())
    a_total = sum(sum(v) for v in b0_by_scen.values())
    c_total = sum(sum(v) for v in b0_cat.values())
    s_total = sum(sum(v) for v in b0_strict.values())
    md += [
        "",
        f"**Pooled B0 (n = {n_total}):**",
        f"- strict top-1:   {s_total}/{n_total} "
        f"({fmt_pct(s_total/n_total)}) — Wilson 95% CI "
        f"{[fmt_pct(x) for x in wilson_ci(s_total, n_total)]}",
        f"- aligned top-1:  {a_total}/{n_total} "
        f"({fmt_pct(a_total/n_total)}) — Wilson 95% CI "
        f"{[fmt_pct(x) for x in wilson_ci(a_total, n_total)]}",
        f"- category top-1: {c_total}/{n_total} "
        f"({fmt_pct(c_total/n_total)}) — Wilson 95% CI "
        f"{[fmt_pct(x) for x in wilson_ci(c_total, n_total)]}",
        "",
        "**Failure-mode observation.** Across the 99 B0 calls, the vanilla "
        "LLM systematically blames the *symptom-bearing* object — typically "
        "a test pod (`e2e-tests`, `microcks-import`, `ai-tests`) or the "
        "database pod (`postgres`) — instead of the *upstream component* at "
        "fault. F4 (broken backend route) is blamed on `postgres` 8/9 "
        "times; F8 (latency in backend) is blamed on `e2e-tests` 6/10 "
        "times; F9 (bad seed data) is blamed on `e2e-tests` 9/10 times. "
        "The operator's FailureReport closes this gap by linking each "
        "test-suite failure to its provenance — the changed file, the "
        "originating workload, the SQL or HTTP error in the upstream pod's "
        "log — which is the evidence the LLM needs to land on the right "
        "component.",
        "",
        f"_Cost: ~USD 0.10 for the full B0 baseline (99 calls). Model: "
        f"gpt-4o-mini-2024-07-18 via Azure OpenAI, temperature 0._",
    ]
    (out_dir / "b0-vs-operator.md").write_text("\n".join(md) + "\n")
    (out_dir / "b0-vs-operator.json").write_text(
        json.dumps({"per_scenario": payload, "pooled": {
            "n": n_total, "strict": s_total,
            "aligned": a_total, "category": c_total,
        }}, indent=2) + "\n"
    )
    print(f"wrote {out_dir / 'b0-vs-operator.md'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
