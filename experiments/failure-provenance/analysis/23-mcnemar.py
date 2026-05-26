#!/usr/bin/env python3
"""23-mcnemar.py — RQ4 McNemar test on grounded vs free-form per cell.

For each (scenario × LLM × {C4, C5}) cell, pair the grounded vs free-form
binary correctness on the same rep, build the 2x2 contingency table, and
run McNemar's exact test. Reports the odds ratio and exact p-value.
Pre-registered in analysis-plan.md §3 RQ4.

Source: per-rep diag files in results-matrix (S1) and results-multiapp.

Output:
  docs/research/failure-provenance/analysis-output/23-mcnemar/mcnemar.csv
  docs/research/failure-provenance/analysis-output/23-mcnemar/summary.md
"""
from __future__ import annotations
import csv
import json
import pathlib
import re
import sys
import datetime as dt
from collections import defaultdict

from statsmodels.stats.contingency_tables import mcnemar

ROOT = pathlib.Path(__file__).resolve().parents[1]
SCENARIOS = ROOT / "scenarios.yaml"
OUT = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/23-mcnemar"
OUT.mkdir(parents=True, exist_ok=True)


def load_ground_truth() -> dict:
    import yaml
    raw = yaml.safe_load(SCENARIOS.read_text())
    gt = {}
    for s in raw.get("scenarios", []):
        gt[s["id"]] = {
            "component": s["ground_truth"]["component"],
            "category": s["ground_truth"]["category"],
        }
    return gt


# Aliases mirror analysis/08-vocab-rescore.py for S1.
ALIASES = {
    "F1": [r"^migration-job$", r"^migration$", r"^db.?migration$", r"^postgres-?migrate$"],
    "F2": [r"^app-?deployment$", r"^app$", r"^backend$", r"^svc-backend$", r"^application$"],
    "F3": [r"^app-?deployment$", r"^app$", r"^image$", r"^image-?pull$", r"^deployment$"],
    "F4": [r"^backend$", r"^svc-backend$", r"^api$", r"^application$", r"^route$"],
    "F5": [r"^frontend$", r"^ui$", r"^catalogue$", r"^catalog$"],
    "F6": [r"^app-?deployment$", r"^app$", r"^backend$", r"^application$", r"^database connection$", r"^db readiness$"],
    "F7": [r"^service$", r"^svc-backend$", r"^backend$", r"^selector$", r"^routing$", r"^endpoint$", r"^networking$"],
    "F8": [r"^backend$", r"^svc-backend$", r"^api$", r"^application$", r"^latency$"],
    "F9": [r"^seed-?job$", r"^seed$", r"^ai-?seed$", r"^data$"],
    "F10": [r"^test-suite$", r"^test suite$", r"^tests$", r"^flaky$"],
}


def normalise(s: str) -> str:
    if not s:
        return ""
    s = s.lower().strip()
    s = re.sub(r"[^a-z0-9\- ]", "", s)
    s = re.sub(r"\s+", " ", s)
    return s


def aligned_match(scenario: str, comp: str) -> bool:
    target = normalise(comp)
    for pat in ALIASES.get(scenario, []):
        if re.match(pat, target):
            return True
    return False


DIAG_RE = re.compile(r"^diag-(C[1-5])-llm-(grounded|freeform)(?:-(llmb))?\.json$")


def main() -> int:
    # Collect per (scenario × LLM × C × mode) per rep: correct/incorrect
    cells = defaultdict(dict)  # (scenario, llm, C, mode) -> {rep: 0|1}
    # Sources of report-bearing dirs
    for root in [ROOT / "results-matrix", ROOT / "results-multiapp"]:
        if not root.is_dir():
            continue
        is_multiapp = root.name.endswith("multiapp")
        if is_multiapp:
            iter_dirs = list(root.glob("s*-*/F*"))
        else:
            iter_dirs = [p for p in root.glob("F*") if p.name != "F4-old-attempt7"]
        for fdir in iter_dirs:
            scenario = fdir.name
            for rdir in fdir.glob("r*"):
                rep = rdir.name
                for f in rdir.glob("diag-*.json"):
                    m = DIAG_RE.match(f.name)
                    if not m:
                        continue
                    C, mode, llmb = m.groups()
                    llm = "B" if llmb == "llmb" else "A"
                    try:
                        d = json.loads(f.read_text())
                        comp = d.get("diagnosis", {}).get("component", "")
                    except Exception:
                        comp = ""
                    correct = 1 if aligned_match(scenario, comp) else 0
                    key = (scenario, llm, C, mode)
                    cells[key][rep] = correct

    # For each (scenario, llm, C) build 2x2: grounded × freeform
    rows = []
    for (scenario, llm, C, mode), reps in list(cells.items()):
        pass  # collected
    pairs = defaultdict(lambda: {"both1": 0, "g1f0": 0, "g0f1": 0, "both0": 0, "n": 0})
    for (scenario, llm, C, mode), reps in cells.items():
        if mode != "grounded":
            continue
        other = cells.get((scenario, llm, C, "freeform"), {})
        for rep, g in reps.items():
            f = other.get(rep)
            if f is None:
                continue
            key = (scenario, llm, C)
            cell = pairs[key]
            cell["n"] += 1
            if g == 1 and f == 1: cell["both1"] += 1
            elif g == 1 and f == 0: cell["g1f0"] += 1
            elif g == 0 and f == 1: cell["g0f1"] += 1
            else: cell["both0"] += 1

    for (scenario, llm, C), cell in sorted(pairs.items()):
        n = cell["n"]
        if n < 5:
            rows.append({
                "scenario": scenario, "llm": llm, "configuration": C,
                "n": n, "both_correct": cell["both1"],
                "grounded_only": cell["g1f0"], "freeform_only": cell["g0f1"],
                "both_wrong": cell["both0"],
                "mcnemar_stat": "", "p_value": "", "skipped": "n<5"
            })
            continue
        table = [[cell["both1"], cell["g1f0"]],
                 [cell["g0f1"], cell["both0"]]]
        # exact McNemar (uses binomial), good for small discordant counts
        result = mcnemar(table, exact=True)
        rows.append({
            "scenario": scenario, "llm": llm, "configuration": C, "n": n,
            "both_correct": cell["both1"],
            "grounded_only": cell["g1f0"],
            "freeform_only": cell["g0f1"],
            "both_wrong": cell["both0"],
            "mcnemar_stat": f"{result.statistic:.3f}",
            "p_value": f"{result.pvalue:.6f}",
            "skipped": "",
        })

    csv_path = OUT / "mcnemar.csv"
    with csv_path.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()) if rows else
                            ["scenario","llm","configuration","n","both_correct","grounded_only","freeform_only","both_wrong","mcnemar_stat","p_value","skipped"])
        w.writeheader()
        for r in rows:
            w.writerow(r)

    summary = OUT / "summary.md"
    n_tests = sum(1 for r in rows if r.get("mcnemar_stat"))
    n_skip = sum(1 for r in rows if r.get("skipped"))
    with summary.open("w") as f:
        f.write("# RQ4 — McNemar grounded vs free-form (per cell)\n\n")
        f.write(f"Generated {dt.datetime.utcnow().isoformat()}Z. "
                f"{n_tests} tests run, {n_skip} cells skipped (n<5 paired reps).\n\n")
        f.write("Note: multi-app subjects (s2–s5) only run grounded mode, "
                "so McNemar is computed on S1 (s1-flask-catalog) where both "
                "modes exist. Multi-app cells are skipped.\n\n")
        f.write("| Scenario | LLM | C | n | g+f+ | g+f− | g−f+ | g−f− | stat | p |\n")
        f.write("|---|---|---|---|---|---|---|---|---|---|\n")
        for r in rows:
            if not r.get("mcnemar_stat"):
                continue
            f.write(f"| {r['scenario']} | {r['llm']} | {r['configuration']} | "
                    f"{r['n']} | {r['both_correct']} | {r['grounded_only']} | "
                    f"{r['freeform_only']} | {r['both_wrong']} | "
                    f"{r['mcnemar_stat']} | {r['p_value']} |\n")

    print(f"McNemar: {n_tests} tests, {n_skip} skipped. csv={csv_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
