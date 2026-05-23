#!/usr/bin/env python3
"""19-evidence-precision.py — RQ2 Evidence Precision (metric M6).

Precision = (# captured evidence items whose type is in expected_evidence)
            / (# captured evidence items)

Equivalently: of the items the operator persisted, what fraction are on
the expected_evidence whitelist for the scenario? High precision means the
operator is not flooding the bundle with off-topic items.

Recall is already populated (metric M7) — this script closes the
precision/recall pair for RQ2.

Source of truth for expected_evidence:
  experiments/failure-provenance/scenarios.yaml  (per scenario)

For multi-app subjects (s2-s5) the same expected_evidence whitelist
applies: the fault classes are identical (F1, F2, F3, F6, F7) and the
evidence types are subject-independent.

Outputs:
  docs/research/failure-provenance/analysis-output/19-evidence-precision/precision.csv
  docs/research/failure-provenance/analysis-output/19-evidence-precision/summary.md
"""
from __future__ import annotations
import csv
import json
import pathlib
import re
import sys
import datetime as dt
from collections import defaultdict, Counter

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[1]
SCENARIOS = ROOT / "scenarios.yaml"
OUT = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/19-evidence-precision"
OUT.mkdir(parents=True, exist_ok=True)


def load_expected_evidence() -> dict[str, set[str]]:
    raw = yaml.safe_load(SCENARIOS.read_text())
    out: dict[str, set[str]] = {}
    for s in raw.get("scenarios", []):
        out[s["id"]] = set(s.get("expected_evidence", []) or [])
    return out


def walk_reports():
    """Yield (subject, scenario, rep, report_path)."""
    s1 = ROOT / "results-matrix"
    if s1.is_dir():
        for fdir in sorted(s1.glob("F*")):
            if not fdir.is_dir() or fdir.name in {"F4-old-attempt7"}:
                continue
            for rdir in sorted(fdir.glob("r*")):
                rp = rdir / "report.json"
                if rp.is_file():
                    yield ("s1-flask-catalog", fdir.name, rdir.name, rp)
    ma = ROOT / "results-multiapp"
    if ma.is_dir():
        for sdir in sorted(ma.glob("s*-*")):
            for fdir in sorted(sdir.glob("F*")):
                for rdir in sorted(fdir.glob("r*")):
                    rp = rdir / "report.json"
                    if rp.is_file():
                        yield (sdir.name, fdir.name, rdir.name, rp)


def main() -> int:
    expected = load_expected_evidence()
    rows = []
    for subject, scenario, rep, rpath in walk_reports():
        try:
            d = json.loads(rpath.read_text())
        except Exception:
            continue
        items = d.get("status", {}).get("evidenceItems", []) or []
        expected_set = expected.get(scenario, set())
        n_total = len(items)
        n_on_whitelist = sum(1 for it in items if it.get("type") in expected_set)
        precision = (n_on_whitelist / n_total) if n_total else None
        rows.append({
            "subject": subject,
            "scenario": scenario,
            "rep": rep,
            "n_items_total": n_total,
            "n_items_on_whitelist": n_on_whitelist,
            "evidence_precision": (f"{precision:.4f}" if precision is not None else ""),
            "expected_types": "|".join(sorted(expected_set)),
        })

    csv_path = OUT / "precision.csv"
    with csv_path.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        for r in rows:
            w.writerow(r)

    # Per (subject × scenario) summary
    bins = defaultdict(list)
    for r in rows:
        try:
            p = float(r["evidence_precision"])
        except Exception:
            continue
        bins[(r["subject"], r["scenario"])].append(p)

    summary = OUT / "summary.md"
    with summary.open("w") as out:
        out.write("# RQ2 — Evidence Precision (M6)\n\n")
        out.write(f"Generated {dt.datetime.utcnow().isoformat()}Z. "
                  f"{len(rows)} reports scored.\n\n")
        out.write("Precision = items-on-whitelist / items-captured. "
                  "Whitelist = `expected_evidence` from scenarios.yaml.\n\n")
        out.write("| Subject | Scenario | n | mean | min | max |\n")
        out.write("|---|---|---|---|---|---|\n")
        for k in sorted(bins.keys()):
            vals = bins[k]
            out.write(f"| {k[0]} | {k[1]} | {len(vals)} | "
                      f"{sum(vals)/len(vals):.3f} | "
                      f"{min(vals):.3f} | {max(vals):.3f} |\n")

    print(f"Evidence precision: {len(rows)} rows -> {csv_path}")
    print(f"Summary: {summary}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
