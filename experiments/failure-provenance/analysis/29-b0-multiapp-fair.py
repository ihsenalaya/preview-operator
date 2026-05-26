#!/usr/bin/env python3
"""
29-b0-multiapp-fair.py — Fair multi-app B0 baseline (vanilla LLM on raw kubectl).

Reviewer critique #2: the current multi-app B0 reported at 43.0 % aligned
top-1 in §5.8.3b of the article is an UPPER BOUND because it was fed the
operator's `evidenceItems` formatted as kubectl-equivalent text, *not* raw
kubectl output. The article correctly flags this, but a true apples-to-apples
multi-app B0 is needed for the reviewer.

This script does the fair comparison:
  For each (subject ∈ S2-S5, scenario ∈ F1-F10, rep ∈ 1..3):
    1. Read pre-captured raw kubectl artefacts from
       results-multiapp/{subject}/{scenario}/{rep}/artifacts/{events,pods,
       deployments,services,endpoints,jobs}.txt + logs/*.log
       (Same files 09-baseline-b0.py uses for S1, just under results-multiapp.)
    2. Concatenate into a kubectl-like blob.
    3. Send to LLM-A (gpt-4o-mini, temp 0) with the same prompt as 09-baseline-b0.
    4. Score against per-scenario ground truth + per-subject alias table from
       17-multiapp-rescore.py (component & category aligned).

Output: results-matrix/results-b0-multiapp-fair.csv
        docs/research/failure-provenance/analysis-output/29-b0-multiapp-fair/

Cost: ~3 reps * 4 subjects * scenarios applicable = ~50-120 LLM calls
      at gpt-4o-mini pricing ≈ $0.05-$0.10 total.

Prerequisites (run on VM):
  - AZURE_OPENAI_API_KEY, AZURE_OPENAI_ENDPOINT in env
  - results-multiapp/*/F*/r*/artifacts/ must contain kubectl text dumps
    (the multi-app orchestrator captures these alongside the FailureReport)
  - openai Python package (`pip install openai`)
"""
from __future__ import annotations

import argparse
import csv
import json
import os
import sys
import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
RESULTS_MULTIAPP = REPO / "experiments/failure-provenance/results-multiapp"
SCENARIOS_YAML = REPO / "experiments/failure-provenance/scenarios.yaml"
OUT_BASE = REPO / "experiments/failure-provenance/results-matrix"
ANALYSIS_OUT = REPO / "docs/research/failure-provenance/analysis-output/29-b0-multiapp-fair"
ANALYSIS_OUT.mkdir(parents=True, exist_ok=True)

# Mirror the S1 B0 prompt exactly (from 09-baseline-b0.py).
SYSTEM_PROMPT = """You are an SRE diagnosing a failed Kubernetes preview environment.
You will be given the raw output of `kubectl get` and `kubectl logs` commands.
Identify the single most likely root-cause component (a Kubernetes resource name
or workload) and the failure category. Return strict JSON with three fields:
component, category, cause. Categories: database, configuration, infrastructure,
application, observability, test-reliability."""

USER_TEMPLATE = """Failed preview namespace evidence:

EVENTS:
{events}

PODS:
{pods}

DEPLOYMENTS:
{deployments}

SERVICES:
{services}

JOBS:
{jobs}

LOGS (last 200 lines per container):
{logs}

Respond with strict JSON: {{"component": "...", "category": "...", "cause": "..."}}
"""


def call_llm(prompt: str, system: str = SYSTEM_PROMPT, max_tokens: int = 256) -> dict:
    """Call Azure OpenAI gpt-4o-mini, temp 0, return parsed JSON (or {'error':...})."""
    try:
        from openai import AzureOpenAI
    except ImportError:
        return {"error": "openai package not installed (pip install openai)"}
    client = AzureOpenAI(
        api_key=os.environ["AZURE_OPENAI_API_KEY"],
        api_version="2024-02-15-preview",
        azure_endpoint=os.environ["AZURE_OPENAI_ENDPOINT"],
    )
    resp = client.chat.completions.create(
        model="gpt-4o-mini",
        temperature=0,
        max_tokens=max_tokens,
        response_format={"type": "json_object"},
        messages=[{"role": "system", "content": system},
                  {"role": "user", "content": prompt}],
    )
    txt = resp.choices[0].message.content
    try:
        return json.loads(txt)
    except json.JSONDecodeError:
        return {"error": "non-JSON", "raw": txt}


def read_kubectl_bundle(artifacts: Path) -> str | None:
    """Reproduce 09-baseline-b0.py build_kubectl_bundle for a multi-app run."""
    def safe(fp: Path, tail: int = 4000) -> str:
        if not fp.exists():
            return "(not captured)"
        t = fp.read_text(errors="ignore")
        return t[-tail:] if len(t) > tail else t

    fields = {
        "events": safe(artifacts / "events.txt"),
        "pods": safe(artifacts / "pods.txt"),
        "deployments": safe(artifacts / "deployments.txt"),
        "services": safe(artifacts / "services.txt"),
        "jobs": safe(artifacts / "jobs.txt"),
    }
    logs_dir = artifacts / "logs"
    log_chunks = []
    if logs_dir.exists():
        for log in sorted(logs_dir.glob("*.log"))[:8]:  # cap to 8 logs
            t = log.read_text(errors="ignore")
            t = t.splitlines()[-200:]
            log_chunks.append(f"--- {log.name} ---\n" + "\n".join(t))
    fields["logs"] = "\n\n".join(log_chunks) if log_chunks else "(no logs captured)"

    # Hard truncate to ~14k chars (well under gpt-4o-mini's 128k context)
    bundle = USER_TEMPLATE.format(**fields)
    if len(bundle) > 14000:
        bundle = bundle[:14000] + "\n\n[TRUNCATED]"
    return bundle


def load_ground_truth():
    try:
        import yaml
    except ImportError:
        print("install PyYAML", file=sys.stderr); sys.exit(2)
    with SCENARIOS_YAML.open() as f:
        spec = yaml.safe_load(f)
    return {sc["id"]: sc.get("ground_truth", {}) for sc in spec.get("scenarios", [])}


# Per-subject alias map mirroring 17-multiapp-rescore.py — extend as needed.
ALIAS = {
    "s2-listmonk":     {"backend": {"listmonk", "listmonk-app", "listmonk backend"},
                        "frontend": {"listmonk-frontend"}},
    "s3-healthchecks": {"backend": {"hc-web", "healthchecks", "django"},
                        "frontend": {"hc-web-frontend"}},
    "s4-umami":        {"backend": {"umami", "umami-app", "next.js"},
                        "frontend": {"umami-frontend"}},
    "s5-petclinic":    {"backend": {"spring-petclinic", "spring-boot", "petclinic"},
                        "frontend": {"petclinic-frontend"}},
}
COMMON_ALIAS = {
    "migration-job":  {"postgres-migrate", "db-migration", "migration", "migration job",
                        "database migration", "database migration script"},
    "app-deployment": {"application", "app", "deployment", "main pod"},
    "service":        {"svc", "kubernetes service", "service object"},
    "seed-job":       {"db-seed", "seed", "data seeder"},
    "test-suite":     {"e2e", "e2e-tests", "playwright", "regression", "test"},
}


def aligned_match(diag: str, gt: str, subject: str) -> bool:
    if not diag or not gt:
        return False
    d, g = diag.strip().lower(), gt.strip().lower()
    if d == g:
        return True
    syn = set(s.lower() for s in COMMON_ALIAS.get(gt, set()))
    syn |= set(s.lower() for s in ALIAS.get(subject, {}).get(gt, set()))
    return d in syn


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--subjects", default="s2-listmonk,s3-healthchecks,s4-umami,s5-petclinic")
    ap.add_argument("--max-reps", type=int, default=3,
                    help="reps per (subject, scenario) for cost control (default: 3)")
    ap.add_argument("--scenarios", default="F1,F2,F3,F4,F5,F6,F7,F8,F9,F10",
                    help="comma-separated scenario IDs to score")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    gt_map = load_ground_truth()
    subjects = args.subjects.split(",")
    scenarios = args.scenarios.split(",")

    work = []
    for subj in subjects:
        for sc in scenarios:
            for rep_dir in sorted((RESULTS_MULTIAPP / subj / sc).glob("r*"))[: args.max_reps]:
                art = rep_dir / "artifacts"
                if not art.exists():
                    continue
                work.append((subj, sc, rep_dir.name, art))

    print(f"plan: {len(work)} LLM calls "
          f"({args.max_reps} reps × {len(subjects)} subjects × {len(scenarios)} scenarios; "
          f"actual reps depend on what was captured)")

    if args.dry_run:
        for w in work[:5]:
            print(" ", w[:3])
        print(f"  ...+{max(0,len(work)-5)} more")
        return 0

    if "AZURE_OPENAI_API_KEY" not in os.environ:
        print("ERR: AZURE_OPENAI_API_KEY not set", file=sys.stderr); return 2

    rows = []
    for i, (subj, sc, rep, art) in enumerate(work, 1):
        print(f"[{i}/{len(work)}] {subj} {sc} {rep}")
        bundle = read_kubectl_bundle(art)
        if bundle is None:
            continue
        diag = call_llm(bundle)
        gt = gt_map.get(sc, {})
        row = {
            "subject": subj, "scenario": sc, "rep": rep,
            "gt_component": gt.get("component", ""),
            "gt_category": gt.get("category", ""),
            "diag_component": diag.get("component", "") if "error" not in diag else "",
            "diag_category": diag.get("category", "") if "error" not in diag else "",
            "diag_cause": (diag.get("cause", "") if "error" not in diag else diag.get("error",""))[:200],
            "top1_strict": int(diag.get("component","").strip().lower() == gt.get("component","").strip().lower()),
            "top1_aligned": int(aligned_match(diag.get("component",""), gt.get("component",""), subj)),
            "category_correct": int(diag.get("category","").strip().lower() == gt.get("category","").strip().lower()),
            "error": diag.get("error", ""),
        }
        rows.append(row)
        # also persist per-rep diag for audit
        (art.parent / "b0-fair-diag.json").write_text(json.dumps(diag, indent=2))

    csv_path = OUT_BASE / "results-b0-multiapp-fair.csv"
    with csv_path.open("w", newline="") as f:
        if rows:
            w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
            w.writeheader()
            w.writerows(rows)
    print(f"wrote {csv_path}")

    # markdown summary
    n = len(rows)
    aligned = sum(r["top1_aligned"] for r in rows)
    strict = sum(r["top1_strict"] for r in rows)
    cat = sum(r["category_correct"] for r in rows)
    md = [
        "# Fair multi-app B0 baseline (vanilla LLM on raw `kubectl`)",
        "",
        f"Generated by `29-b0-multiapp-fair.py` on {n} reps.",
        "",
        "## Pooled top-1 (S2-S5, raw kubectl input)",
        "",
        "| Matcher | Hits | Aligned top-1 |",
        "|---|---|---|",
        f"| strict | {strict}/{n} | {strict/n:.3f} |" if n else "| strict | — | — |",
        f"| aligned | {aligned}/{n} | {aligned/n:.3f} |" if n else "| aligned | — | — |",
        f"| category-only | {cat}/{n} | {cat/n:.3f} |" if n else "| category | — | — |",
        "",
        "## Comparison to upper-bound multi-app B0",
        "",
        "The article currently reports a 43 % multi-app B0 aligned top-1, flagged as ",
        "UPPER BOUND because that pass fed B0 the operator's `evidenceItems` formatted ",
        "as kubectl-like text. The fair B0 (this script) consumes the raw kubectl ",
        "artefacts from `results-multiapp/*/F*/r*/artifacts/` instead. The expectation is ",
        "a substantially lower number, closer to the S1 B0 (4 % aligned).",
        "",
        "Source: `results-multiapp/*/F*/r*/artifacts/{events,pods,jobs,...}.txt`.",
        "Script: `experiments/failure-provenance/analysis/29-b0-multiapp-fair.py`.",
        "",
    ]
    (ANALYSIS_OUT / "summary.md").write_text("\n".join(md))
    print("wrote:", ANALYSIS_OUT / "summary.md")
    return 0


if __name__ == "__main__":
    sys.exit(main())
