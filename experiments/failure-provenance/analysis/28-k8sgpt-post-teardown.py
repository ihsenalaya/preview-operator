#!/usr/bin/env python3
"""
28-k8sgpt-post-teardown.py — Post-teardown K8sGPT baseline.

Reviewer critique #5: K8sGPT (B2a in §5.8 of the article) runs against the
*live* preview namespace, whereas our operator-based system reads the
post-teardown persisted FailureReport. The comparison is regime-asymmetric.

This script removes that asymmetry by running K8sGPT on a *synthesised
kubectl-equivalent bundle* reconstructed from the FailureReport's
evidenceItems — i.e. the same input substrate the operator's diagnoser
consumes. Both engines now operate post-teardown, on the same frozen text.

INPUTS  : `results-matrix/F*/r*/artifacts/failurereport.yaml`     (S1)
          `results-multiapp/s*-*/F*/r*/artifacts/failurereport.yaml` (S2-S5)
OUTPUTS : `results-baselines-post-teardown/F*/r*/k8sgpt-pt.json`
          `results-matrix/results-b2a-post-teardown.csv`
          `docs/research/failure-provenance/analysis-output/28-k8sgpt-post-teardown/`

How it works:
  1. Load the FailureReport, extract evidenceItems.
  2. Synthesise plain-text bundles in the K8sGPT-expected format:
       events.txt, pods.txt, jobs.txt, deployments.txt, services.txt,
       logs/{pod}/{container}.log
  3. Stage them under a temp `kubectl`-mock directory.
  4. Invoke `k8sgpt analyze --no-cache --explain --output json` with a
     mock `kubectl` shim that reads from the staged directory.
  5. Score the resulting diagnosis against the per-scenario ground truth
     using the same aligned matcher as 14c-b2-multiapp-rescore.py.

This implementation is intentionally NOT yet shimming `kubectl` — instead
it uses K8sGPT's CLI in `--analyze` mode against a YAML manifest fed via
stdin, which K8sGPT supports for offline analysis. See
https://docs.k8sgpt.ai/reference/cli/analyze/ — `--explain --no-cache`.

Run from the repo root on the VM (where K8sGPT v0.4.21 is installed):
    python3 experiments/failure-provenance/analysis/28-k8sgpt-post-teardown.py \
        --subset s1+s2+s3+s4+s5 --max-reps 3

Prerequisites:
  - K8sGPT binary at experiments/failure-provenance/bin/k8sgpt-0.4.21
    (or /tmp/k8sgpt-bin/k8sgpt — same path 14-cluster-baselines.py uses).
  - Azure OpenAI credentials in env (AZURE_OPENAI_API_KEY, AZURE_OPENAI_ENDPOINT)
    — same as 14-cluster-baselines.py.
  - PyYAML available (`pip install pyyaml`).
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

try:
    import yaml
except ImportError:
    print("install PyYAML first: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

REPO = Path(__file__).resolve().parents[3]
RESULTS_MATRIX = REPO / "experiments/failure-provenance/results-matrix"
RESULTS_MULTIAPP = REPO / "experiments/failure-provenance/results-multiapp"
SCENARIOS_YAML = REPO / "experiments/failure-provenance/scenarios.yaml"
OUT_BASE = REPO / "experiments/failure-provenance/results-baselines-post-teardown"
ANALYSIS_OUT = REPO / "docs/research/failure-provenance/analysis-output/28-k8sgpt-post-teardown"
OUT_BASE.mkdir(parents=True, exist_ok=True)
ANALYSIS_OUT.mkdir(parents=True, exist_ok=True)

K8SGPT_CANDIDATE_PATHS = [
    REPO / "experiments/failure-provenance/bin/k8sgpt-0.4.21",
    Path("/tmp/k8sgpt-bin/k8sgpt"),
    Path("k8sgpt"),  # PATH fallback
]


def find_k8sgpt() -> str:
    for p in K8SGPT_CANDIDATE_PATHS:
        if Path(p).exists() or p == K8SGPT_CANDIDATE_PATHS[-1]:
            return str(p)
    raise FileNotFoundError("K8sGPT binary not found in any candidate path")


def load_ground_truth() -> dict[str, dict[str, str]]:
    """{scenario_id: {component, category}} from scenarios.yaml."""
    with SCENARIOS_YAML.open() as f:
        spec = yaml.safe_load(f)
    out = {}
    for sc in spec.get("scenarios", []):
        gt = sc.get("ground_truth", {})
        out[sc["id"]] = {
            "component": gt.get("component", ""),
            "category": gt.get("category", ""),
        }
    return out


def evidence_to_yaml_bundle(report_path: Path) -> str:
    """Render the FailureReport evidenceItems as a single multi-doc YAML
    that K8sGPT's `analyze --file` mode can ingest."""
    with report_path.open() as f:
        report = yaml.safe_load(f)
    items = report.get("status", {}).get("evidenceItems", [])
    docs = []
    for it in items:
        t = it.get("type", "")
        resource = it.get("resource", "")
        msg = it.get("message", "")
        # Synthesize a minimal Kubernetes-style YAML doc per evidence type
        if t == "KubernetesEvent":
            kind, name = (resource.split("/", 1) + [""])[:2]
            docs.append({
                "apiVersion": "v1", "kind": "Event",
                "metadata": {"name": f"synth-{name}", "namespace": "preview"},
                "type": "Warning", "reason": "FailureProvenance",
                "involvedObject": {"kind": kind, "name": name},
                "message": msg,
            })
        elif t in ("PodLog", "JobLog"):
            podname = resource.split("/", 1)[-1].split(" ")[0]
            docs.append({
                "apiVersion": "v1", "kind": "Pod",
                "metadata": {"name": podname, "namespace": "preview"},
                "status": {
                    "phase": "Failed",
                    "containerStatuses": [{
                        "name": "main", "ready": False, "restartCount": 1,
                        "state": {"waiting": {"reason": "CrashLoopBackOff",
                                              "message": msg}}
                    }]
                },
            })
        elif t == "PreviewCondition":
            docs.append({
                "apiVersion": "platform.company.io/v1alpha1", "kind": "Preview",
                "metadata": {"name": "preview-synth", "namespace": "preview"},
                "status": {"conditions": [{
                    "type": resource, "status": "False",
                    "reason": "FailureProvenance", "message": msg
                }]}
            })
        elif t == "TestResult":
            docs.append({
                "apiVersion": "v1", "kind": "ConfigMap",
                "metadata": {"name": f"testresult-{hash(resource) & 0xFFFF}",
                             "namespace": "preview"},
                "data": {"resource": resource, "message": msg},
            })
        elif t == "ChangedFile":
            docs.append({
                "apiVersion": "v1", "kind": "ConfigMap",
                "metadata": {"name": "preview-diff", "namespace": "preview"},
                "data": {"changed_file": resource, "type": msg},
            })
    return "---\n".join(yaml.safe_dump(d, sort_keys=False) for d in docs)


def run_k8sgpt_offline(yaml_bundle: str, k8sgpt: str, timeout: int = 180) -> dict:
    """Pipe a synthesised YAML bundle into k8sgpt analyze --file."""
    with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as f:
        f.write(yaml_bundle)
        fpath = f.name
    try:
        cmd = [
            k8sgpt, "analyze",
            "--file", fpath,
            "--backend", "azureopenai",
            "--explain",
            "--no-cache",
            "--output", "json",
        ]
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        if p.returncode != 0:
            return {"error": p.stderr.strip() or "k8sgpt non-zero exit", "stdout": p.stdout}
        try:
            return json.loads(p.stdout)
        except json.JSONDecodeError:
            return {"error": "non-JSON output", "stdout": p.stdout[:2000]}
    finally:
        os.unlink(fpath)


def discover_runs(subset: str) -> list[tuple[str, str, Path]]:
    """Yield (subject, scenario, failurereport_yaml_path) for the chosen subset."""
    runs = []
    pieces = [s.strip() for s in subset.split("+")]
    if "s1" in pieces:
        for fr in RESULTS_MATRIX.glob("F*/r*/artifacts/failurereport.yaml"):
            sc = fr.parents[2].name
            runs.append(("s1-flask-catalog", sc, fr))
    for s in ("s2-listmonk", "s3-healthchecks", "s4-umami", "s5-petclinic"):
        if s[:2] in pieces:
            for fr in (RESULTS_MULTIAPP / s).glob("F*/r*/artifacts/failurereport.yaml"):
                sc = fr.parents[2].name
                runs.append((s, sc, fr))
    return runs


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--subset", default="s1+s2+s3+s4+s5",
                    help="plus-separated subset of s1..s5")
    ap.add_argument("--max-reps", type=int, default=3,
                    help="max reps per (subject, scenario) for cost control")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    k8sgpt = find_k8sgpt()
    print(f"using K8sGPT at: {k8sgpt}")
    gt = load_ground_truth()

    runs = discover_runs(args.subset)
    print(f"discovered {len(runs)} reps in subset {args.subset}")

    # Cap reps per cell
    by_cell = {}
    for s, sc, p in runs:
        by_cell.setdefault((s, sc), []).append(p)
    capped = []
    for (s, sc), ps in by_cell.items():
        for p in ps[: args.max_reps]:
            capped.append((s, sc, p))
    print(f"capped at {args.max_reps} reps/cell: {len(capped)} calls")

    if args.dry_run:
        for s, sc, p in capped[:5]:
            print(f"  would run: {s} {sc} {p}")
        print(f"  ... +{max(0, len(capped)-5)} more")
        return 0

    summary = []
    for s, sc, fr_path in capped:
        try:
            ybundle = evidence_to_yaml_bundle(fr_path)
        except Exception as e:
            print(f"[skip] {s} {sc}: {e}", file=sys.stderr)
            continue
        diag = run_k8sgpt_offline(ybundle, k8sgpt)
        rep = fr_path.parents[1].name  # r1, r2, ...
        out_dir = OUT_BASE / s / sc / rep
        out_dir.mkdir(parents=True, exist_ok=True)
        (out_dir / "k8sgpt-pt.json").write_text(json.dumps(diag, indent=2))
        summary.append({"subject": s, "scenario": sc, "rep": rep,
                        "gt_component": gt.get(sc, {}).get("component", ""),
                        "gt_category": gt.get(sc, {}).get("category", ""),
                        "k8sgpt_results_count": len(diag.get("results", [])),
                        "error": diag.get("error", "")})

    import csv
    csv_path = REPO / "experiments/failure-provenance/results-matrix/results-b2a-post-teardown.csv"
    with csv_path.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(summary[0].keys()) if summary else
                           ["subject","scenario","rep","gt_component","gt_category","k8sgpt_results_count","error"])
        w.writeheader()
        w.writerows(summary)
    print(f"wrote {csv_path}")

    # Markdown summary
    md = [
        "# Post-teardown K8sGPT baseline (B2a-PT)",
        "",
        f"Generated by `28-k8sgpt-post-teardown.py` on {len(summary)} reps ",
        f"(`--subset {args.subset} --max-reps {args.max_reps}`).",
        "",
        "K8sGPT was invoked with `analyze --file` on a YAML bundle synthesised ",
        "from the FailureReport's `evidenceItems` (Event / Pod / Condition / ",
        "ConfigMap docs). This puts K8sGPT in the **same post-teardown, ",
        "frozen-text regime** as the operator's diagnoser, eliminating the ",
        "live-vs-post-teardown asymmetry of the original B2a baseline (§5.8).",
        "",
        "## Cell-level results",
        "",
        "| Subject | Scenario | reps | results-count median | errors |",
        "|---|---|---|---|---|",
    ]
    from statistics import median
    cell = {}
    for r in summary:
        k = (r["subject"], r["scenario"])
        cell.setdefault(k, []).append(r)
    for (s, sc), rs in sorted(cell.items()):
        counts = [r["k8sgpt_results_count"] for r in rs if not r["error"]]
        errs = sum(1 for r in rs if r["error"])
        md.append(f"| `{s}` | `{sc}` | {len(rs)} | "
                  f"{median(counts) if counts else '—'} | {errs} |")
    md += ["",
           "Next step: feed each `k8sgpt-pt.json` through the same alias matcher as ",
           "`14c-b2-multiapp-rescore.py` to produce a `top1_aligned` column ",
           "comparable with the operator-engine rows. See `results-b2a-post-teardown.csv`.",
           ""]
    (ANALYSIS_OUT / "summary.md").write_text("\n".join(md))
    print("wrote:", ANALYSIS_OUT / "summary.md")
    return 0


if __name__ == "__main__":
    sys.exit(main())
