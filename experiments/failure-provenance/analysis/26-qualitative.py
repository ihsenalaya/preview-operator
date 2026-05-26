#!/usr/bin/env python3
"""26-qualitative.py — qualitative analysis of representative failure cases.

For each scenario (F1–F10 on S1, F1/F2/F3/F6/F7 on multi-app subjects),
pick one representative rep and produce a narrative case-study card
with:
  - failure signature (operator's bundle summary)
  - evidence items captured
  - rule-engine and LLM diagnoses at C5
  - what the diagnoses got right / wrong vs ground truth
  - one-paragraph editorial commentary

Output:
  docs/research/failure-provenance/analysis-output/26-qualitative/
    s1-flask-catalog-F1.md
    s1-flask-catalog-F2.md
    ...
    s5-petclinic-F7.md
    summary.md
"""
from __future__ import annotations
import json
import pathlib
import sys
import datetime as dt

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/26-qualitative"
OUT.mkdir(parents=True, exist_ok=True)
SCENARIOS = ROOT / "scenarios.yaml"


def load_scenarios() -> dict:
    import yaml
    raw = yaml.safe_load(SCENARIOS.read_text())
    return {s["id"]: s for s in raw.get("scenarios", [])}


def walk_subjects():
    s1 = ROOT / "results-matrix"
    if s1.is_dir():
        yield ("s1-flask-catalog", s1, "F*")
    ma = ROOT / "results-multiapp"
    if ma.is_dir():
        for sdir in sorted(ma.glob("s*-*")):
            yield (sdir.name, sdir, "F*")


def main() -> int:
    scenarios = load_scenarios()
    cards = []
    for subject, sroot, glob in walk_subjects():
        for fdir in sorted(sroot.glob(glob)):
            if not fdir.is_dir() or fdir.name == "F4-old-attempt7":
                continue
            scenario = fdir.name
            if scenario not in scenarios:
                continue
            # Pick r1 as representative (deterministic)
            r1 = fdir / "r1"
            report_path = r1 / "report.json"
            if not report_path.is_file():
                continue
            try:
                d = json.loads(report_path.read_text())
            except Exception:
                continue
            status = d.get("status", {}) or {}
            evidence = status.get("evidenceItems", []) or []
            gt = scenarios[scenario].get("ground_truth", {})

            # Pull C5 LLM diagnoses
            diags = {}
            for f in r1.glob("diag-C5-llm-grounded*.json"):
                try:
                    j = json.loads(f.read_text())
                    label = "LLM-B" if "llmb" in f.name else "LLM-A"
                    diags[label] = j.get("diagnosis", {})
                except Exception:
                    pass

            card_path = OUT / f"{subject}-{scenario}.md"
            with card_path.open("w") as out:
                out.write(f"# Case study — {subject} / {scenario}\n\n")
                out.write(f"**Family:** {scenarios[scenario].get('family', '—')}  \n")
                out.write(f"**Ground truth:** component=`{gt.get('component','')}` "
                          f"category=`{gt.get('category','')}`  \n")
                out.write(f"**Bundle size:** {status.get('bundleSizeBytes', 0)} bytes — "
                          f"**Evidence level:** {status.get('evidenceLevel', '—')}  \n")
                out.write(f"**Detected at:** `{status.get('failureDetectedAt', '—')}`  \n\n")

                out.write("## Evidence items captured\n\n")
                out.write("| Type | Resource | Message (truncated) |\n|---|---|---|\n")
                for e in evidence[:8]:
                    m = (e.get("message") or "").replace("\n", " ")
                    if len(m) > 80:
                        m = m[:80] + "…"
                    out.write(f"| {e.get('type','')} | "
                              f"{(e.get('resource') or '')[:48]} | {m} |\n")
                if len(evidence) > 8:
                    out.write(f"| … | … | (`{len(evidence) - 8}` more items not shown) |\n")
                out.write("\n")

                out.write("## C5 LLM diagnoses\n\n")
                for label in ("LLM-A", "LLM-B"):
                    diag = diags.get(label, {})
                    out.write(f"### {label}\n\n")
                    out.write(f"- component: `{diag.get('component', '—')}`  \n")
                    out.write(f"- category:  `{diag.get('category', '—')}`  \n")
                    cause = diag.get("cause") or diag.get("probableCause") or "—"
                    if isinstance(cause, dict):
                        cause = json.dumps(cause)
                    if len(str(cause)) > 280:
                        cause = str(cause)[:280] + "…"
                    out.write(f"- cause:     {cause}\n\n")

                out.write("## Commentary\n\n")
                # Programmatic commentary
                a_comp = (diags.get("LLM-A", {}).get("component") or "").lower()
                b_comp = (diags.get("LLM-B", {}).get("component") or "").lower()
                gt_comp = (gt.get("component") or "").lower()
                a_hit = gt_comp in a_comp or a_comp in gt_comp if (a_comp and gt_comp) else False
                b_hit = gt_comp in b_comp or b_comp in gt_comp if (b_comp and gt_comp) else False
                if a_hit and b_hit:
                    note = "Both LLMs hit the ground-truth component."
                elif a_hit and not b_hit:
                    note = "Only LLM-A matched ground truth. LLM-B named an upstream symptom."
                elif b_hit and not a_hit:
                    note = "Only LLM-B matched ground truth. LLM-A named an upstream symptom."
                else:
                    note = ("Neither LLM matched ground truth strictly. "
                            "Either the diagnosis names a symptom (test pod, downstream "
                            "service) or uses a different vocabulary than the matcher "
                            "expects.")
                out.write(f"{note}\n\n")
                out.write(f"_Rep `r1` is used as representative; the other 9 reps for "
                          f"this scenario are tabulated in the per-cell tables of "
                          f"`results-augmented.csv` and `results-multiapp/runs.csv`._\n")
            cards.append((subject, scenario, card_path))

    # Index
    idx = OUT / "summary.md"
    with idx.open("w") as f:
        f.write("# Qualitative analysis — case studies\n\n")
        f.write(f"Generated {dt.datetime.utcnow().isoformat()}Z. "
                f"{len(cards)} cards.\n\n")
        for subject, scenario, p in cards:
            f.write(f"- [{subject} / {scenario}](./{p.name})\n")
    print(f"Qualitative: {len(cards)} cards in {OUT}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
