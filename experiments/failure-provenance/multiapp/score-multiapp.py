#!/usr/bin/env python3
"""score-multiapp.py — offline scorer for the multi-app failure-provenance matrix.

run-matrix.py captures one report.json per (subject, fault, rep). This script
walks those reports and, for each, runs the **same** fp-diagnose -> fp-score
pipeline the S1 run-kind harness uses: 5 evidence levels x 3 engine-modes ->
one results-CSV row each. Output: results-multiapp.csv, schema-identical to the
S1 results-matrix/results.csv so the two are directly comparable.

Capture (cluster-bound) and scoring (offline) are kept separate on purpose:
this script is re-runnable any time — e.g. to re-score with a fixed diagnoser
or an aligned component vocabulary — without touching the cluster.
"""
from __future__ import annotations
import argparse
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent
REPO_ROOT = ROOT.parents[2]                 # .../preview-operator
LEVELS = ["C1", "C2", "C3", "C4", "C5"]
ENGINE_MODES = [("rule", "grounded"), ("llm", "grounded"), ("llm", "freeform")]


def build_tools(bindir: pathlib.Path) -> tuple[pathlib.Path, pathlib.Path]:
    """Build fp-diagnose and fp-score, return their paths."""
    bindir.mkdir(parents=True, exist_ok=True)
    for cmd in ("fp-diagnose", "fp-score"):
        subprocess.run(["go", "build", "-o", str(bindir / cmd), f"./cmd/{cmd}"],
                       cwd=REPO_ROOT, check=True)
    return bindir / "fp-diagnose", bindir / "fp-score"


def csv_header() -> str:
    """Reuse the S1 results-matrix header so both CSVs share one schema."""
    s1 = ROOT.parent / "results-matrix" / "results.csv"
    if s1.exists():
        return s1.read_text().splitlines()[0] + "\n"
    return ("scenario_id,run_id,cluster_type,configuration,failure_detected_at,"
            "diagnosis_available_at,mttd_seconds,top1_correct,top3_correct,"
            "evidence_precision,evidence_recall,hallucination_rate,"
            "recommendation_score,bundle_size_bytes,cpu_overhead,memory_overhead,"
            "storage_overhead,namespace_deleted,evidence_survived,notes\n")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--results-dir", default=str(ROOT / "results-multiapp"))
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    rdir = pathlib.Path(args.results_dir)
    out = pathlib.Path(args.out) if args.out else rdir / "results-multiapp.csv"
    fp_diagnose, fp_score = build_tools(ROOT / "bin")

    reports = sorted(rdir.glob("*/*/r*/report.json"))
    print(f"found {len(reports)} report.json file(s) under {rdir}")
    if not reports:
        print("nothing to score — run run-matrix.py --execute first")
        return 0

    rows = 0
    with out.open("w") as fh:
        fh.write(csv_header())
        for rpt in reports:
            rep = rpt.parent.name                       # r<rep>
            fault = rpt.parent.parent.name              # F<n>
            subject = rpt.parent.parent.parent.name     # s<n>-<name>
            for level in LEVELS:
                for engine, mode in ENGINE_MODES:
                    run_id = f"{subject}-{fault}-{rep}-{level}-{engine}-{mode}"
                    diag = rpt.parent / f"diag-{level}-{engine}-{mode}.json"
                    subprocess.run(
                        [str(fp_diagnose), "--report", str(rpt), "--level", level,
                         "--engine", engine, "--mode", mode, "--out", str(diag)],
                        check=False)
                    res = subprocess.run(
                        [str(fp_score), "--report", str(rpt), "--result", str(diag),
                         "--scenario", fault, "--run-id", run_id],
                        capture_output=True, text=True)
                    if res.stdout.strip():
                        fh.write(res.stdout if res.stdout.endswith("\n")
                                 else res.stdout + "\n")
                        rows += 1
                    else:
                        print(f"  WARN no score row for {run_id}: {res.stderr.strip()[:80]}")
    print(f"wrote {rows} rows -> {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
