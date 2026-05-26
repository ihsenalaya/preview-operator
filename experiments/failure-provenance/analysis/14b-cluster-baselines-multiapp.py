#!/usr/bin/env python3
"""14b-cluster-baselines-multiapp.py — B2a (K8sGPT) + B2b (Kagent) on
multi-app subjects (s2-listmonk, s3-healthchecks, s4-umami, s5-petclinic).

Mirrors `14-cluster-baselines.py` (which runs on S1 previews
pr-9300..9309) but iterates the multi-app PR space: PR =
90000 + subject_idx × 1000 + fault_idx × 100 + rep. We use rep=1 only,
4 subjects × 10 faults = 40 cells.

Flow per cell:
  1. Verify the operator has a FailureReport for the PR (pr-<N>-failure
     should exist — it does, the matrix has captured all of them).
     If the preview's namespace was torn down (it is, per run-matrix.py
     default), re-deploy the preview via the meta.yaml-driven factory
     with the same fault injection so K8sGPT and Kagent see the same
     failure state.
  2. Wait for the namespace to be Ready (pods coming up) and for the
     failure conditions to manifest.
  3. Run K8sGPT v0.4.21 analyze --backend azureopenai -o json against
     the namespace.
  4. Run Kagent k8s-agent via A2A JSON-RPC on http://localhost:18080/
     (port-forwarded from kagent-system/k8s-agent).
  5. Score the K8sGPT and Kagent answers with the same per-subject
     vocabulary-aligned alias matcher used in 17-multiapp-rescore.

Outputs:
  experiments/failure-provenance/results-multiapp/{subject}/{F}/r1/k8sgpt.json
  experiments/failure-provenance/results-multiapp/{subject}/{F}/r1/kagent.json
  experiments/failure-provenance/results-matrix/results-b2-multiapp.csv

Pragmatic scope:
  - 1 rep per (subject × fault) = 40 cells total (matches the S1 B2
    protocol which uses 1 rep per F).
  - K8sGPT needs Azure OpenAI key (preview-openai-idp).
  - Kagent runs locally; requires
    `kubectl -n kagent-system port-forward svc/k8s-agent 18080:8080`.
"""
from __future__ import annotations

import argparse
import csv
import json
import os
import pathlib
import re
import shlex
import subprocess
import sys
import time
import urllib.error
import urllib.request
import datetime as dt

ROOT = pathlib.Path(__file__).resolve().parents[1]
RESULTS = ROOT / "results-multiapp"
OUT_CSV = ROOT / "results-matrix" / "results-b2-multiapp.csv"

# Stable subject index (mirror of run-matrix.py SUBJECT_IDX).
SUBJECT_IDX = {
    "s2-listmonk": 0,
    "s3-healthchecks": 1,
    "s4-umami": 2,
    "s5-petclinic": 3,
}

# Per-subject ground-truth role per scenario (mirror of
# 17-multiapp-rescore.py GROUND_TRUTH + SUBJECT_ALIASES).
SCENARIO_ROLE = {
    "F1":  "migration-job",
    "F2":  "app",
    "F3":  "app",
    "F4":  "backend",
    "F5":  "frontend",
    "F6":  "app",
    "F7":  "service",
    "F8":  "backend",
    "F9":  "seed-job",
    "F10": "test-suite",
}

SCENARIO_CATEGORY = {
    "F1":  "database",
    "F2":  "configuration",
    "F3":  "infrastructure",
    "F4":  "application",
    "F5":  "application",
    "F6":  "infrastructure",
    "F7":  "infrastructure",
    "F8":  "observability",
    "F9":  "database",
    "F10": "test-reliability",
}

# Reuse the per-subject alias table from 17-multiapp-rescore. Inline
# here so this script is standalone.
SUBJECT_ALIASES: dict[tuple[str, str], list[str]] = {}
for _subject in SUBJECT_IDX:
    SUBJECT_ALIASES[(_subject, "migration-job")] = [
        r"^migration-?job$", r"^postgres-?migrate$", r"^migration$",
        r"^db.?migration$", r"^prisma.?migrate$", r"^flyway$",
        r"^django.?migrate$",
    ]
    SUBJECT_ALIASES[(_subject, "app")] = [
        r"^app$", r"^application$", r"^app-?deployment$", r"^backend$",
        r"^svc-backend$", r"^listmonk$", r"^healthchecks$", r"^hc-?web$",
        r"^umami$", r"^spring-?petclinic$", r"^petclinic$",
    ]
    SUBJECT_ALIASES[(_subject, "backend")] = [
        r"^backend$", r"^svc-backend$", r"^api$", r"^application$",
        r"^route$", r"^listmonk$", r"^umami$", r"^petclinic$",
        r"^spring-?petclinic$", r"^hc-?web$", r"^healthchecks$",
    ]
    SUBJECT_ALIASES[(_subject, "frontend")] = [
        r"^frontend$", r"^ui$", r"^web$", r"^admin$", r"^static$",
    ]
    SUBJECT_ALIASES[(_subject, "service")] = [
        r"^service$", r"^svc$", r"^svc-backend$", r"^backend$",
        r"^networking$", r"^routing$",
    ]
    SUBJECT_ALIASES[(_subject, "seed-job")] = [
        r"^seed-?job$", r"^seed$", r"^catalogue-?seed$",
        r"^ai-?enrichment$", r"^data$", r"^test-?data$",
    ]
    SUBJECT_ALIASES[(_subject, "test-suite")] = [
        r"^test-?suite$", r"^test$", r"^tests$", r"^flaky$",
        r"^e2e-?tests?$", r"^smoke-?tests?$", r"^regression-?tests?$",
    ]


def normalise(s: str) -> str:
    s = (s or "").lower().strip()
    s = re.sub(r"[^a-z0-9\- ]", "", s)
    return re.sub(r"\s+", " ", s)


def aligned_match(subject: str, scenario: str, comp: str) -> bool:
    role = SCENARIO_ROLE.get(scenario)
    if not role:
        return False
    target = normalise(comp)
    if not target:
        return False
    for pat in SUBJECT_ALIASES.get((subject, role), []):
        if re.match(pat, target):
            return True
    return False


# K8sGPT runner — same shape as 14-cluster-baselines.py.
def run_k8sgpt(binary: str, namespace: str, *, timeout: int = 180) -> dict:
    cmd = [
        binary, "analyze", "--explain", "--backend", "azureopenai",
        "--namespace", namespace, "--output", "json", "--no-cache",
    ]
    proc = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    out = (proc.stdout or "").strip()
    err = (proc.stderr or "").strip()
    try:
        return json.loads(out) if out.startswith("{") else {
            "raw_stdout": out, "raw_stderr": err, "rc": proc.returncode}
    except Exception:
        return {"raw_stdout": out, "raw_stderr": err, "rc": proc.returncode}


def k8sgpt_to_component(blob: dict, subject: str, scenario: str) -> tuple[str, str]:
    """Extract (component, category) from k8sgpt JSON.

    K8sGPT v0.4.21 returns problems as a list with 'name', 'kind',
    'details' fields. We pick the first problem's `kind` + `name` and
    map to our role vocabulary."""
    problems = blob.get("results") or blob.get("problems") or []
    if not problems and "kind" in blob:
        problems = [blob]
    if not problems:
        return ("", "")
    p = problems[0]
    name = (p.get("name") or "").lower()
    kind = (p.get("kind") or "").lower()
    details = (p.get("details") or "").lower() if isinstance(p.get("details"), str) else ""
    # Heuristic: if mention of migration → migration-job role; pod →
    # app; service → service; etc.
    text = f"{kind} {name} {details}"
    return (text[:80], "")  # raw text returned as component for alias matching


def call_kagent(url: str, namespace: str, *, timeout: int = 240) -> dict:
    prompt = (
        f"Investigate Kubernetes namespace {namespace}. Identify the single "
        f"most likely root cause of the failure. Reply with ONLY a JSON "
        f"object: {{\"component\":\"<short id>\", \"category\":\"<one of "
        f"database|configuration|infrastructure|application|observability|"
        f"test-reliability>\"}}"
    )
    msg_id = f"probe-{namespace}-{int(time.time())}"
    body = json.dumps({
        "jsonrpc": "2.0",
        "id": msg_id,
        "method": "message/send",
        "params": {
            "message": {
                "kind": "message",
                "role": "user",
                "messageId": msg_id,
                "parts": [{"kind": "text", "text": prompt}],
            },
        },
    }).encode()
    req = urllib.request.Request(
        url.rstrip("/") + "/",
        data=body, method="POST",
        headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read())
    except Exception as e:
        return {"error": str(e)}


def kagent_to_component(blob: dict) -> tuple[str, str]:
    if not blob or "error" in blob:
        return ("", "")
    # Kagent's response shape varies; try to extract JSON from the text.
    text = ""
    if isinstance(blob.get("result"), dict):
        m = blob["result"].get("message")
        if isinstance(m, dict):
            text = m.get("content", "") or ""
        elif isinstance(m, str):
            text = m
    if not text:
        text = json.dumps(blob)
    m = re.search(r"\{[^{}]*\"component\"[^{}]*\}", text)
    if m:
        try:
            d = json.loads(m.group(0))
            return (d.get("component", ""), d.get("category", ""))
        except Exception:
            pass
    return (text[:80], "")


# K8sGPT and Kagent need a LIVE namespace. The matrix tore down each
# preview after capture, so we re-deploy a single rep (r=1) per
# (subject, scenario) here, keep it alive, probe it, then tear down.

def _kubectl(*args, capture: bool = False, check: bool = False):
    p = subprocess.run(["kubectl", *args], check=check,
                       capture_output=capture, text=True)
    return p


def deploy_preview(subject_id: str, scenario: str, cfg: dict) -> tuple[str, str]:
    """Re-deploy a (subject, scenario, r=1) preview via the multi-app
    factory + injector. Return (preview_name, namespace)."""
    sys.path.insert(0, str(ROOT / "multiapp"))
    from harness import config as hconfig
    from harness import preview_factory as pf
    from injectors_multiapp import dispatch as inject_dispatch

    subject = hconfig.load_subject(subject_id)
    plan = inject_dispatch.prepare(subject, scenario, cfg)

    rep = 1
    pr_number = 90000 + SUBJECT_IDX[subject_id] * 1000 + int(scenario[1:]) * 100 + rep
    name = f"pr-{pr_number}"
    pf.create(name=name, cr_namespace="default", pr_number=pr_number,
              subject=plan.subject, subject_image=plan.image,
              probe_image=cfg["subjects"]["probe_image"])
    _kubectl("label", "preview", name,
             "failure-provenance.experiment/owned=true", "--overwrite")
    ns = pf.runtime_namespace(pr_number)
    if plan.post_deploy is not None:
        # Wait briefly for namespace then run post-deploy
        for _ in range(60):
            r = _kubectl("get", "ns", ns, capture=True)
            if r.returncode == 0:
                break
            time.sleep(2)
        plan.post_deploy(name, ns)
    return name, ns


def wait_for_failure(ns: str, timeout: int = 300) -> bool:
    """Wait for the preview to settle into a probe-able state — either
    Failed phase or any non-Provisioning state for at least 30 s."""
    name = ns.replace("preview-", "")
    deadline = time.monotonic() + timeout
    stable_since = None
    while time.monotonic() < deadline:
        r = _kubectl("get", "preview", name, "-n", "default",
                     "-o", "jsonpath={.status.phase}", capture=True)
        phase = (r.stdout or "").strip()
        if phase in ("Failed", "Running"):
            if stable_since is None:
                stable_since = time.monotonic()
            elif time.monotonic() - stable_since >= 30:
                return True
        else:
            stable_since = None
        time.sleep(5)
    return False


def teardown(name: str, ns: str):
    _kubectl("delete", "preview", name, "-n", "default", "--wait=false")


def main() -> int:
    import yaml
    ap = argparse.ArgumentParser()
    ap.add_argument("--k8sgpt-binary",
                    default=str(ROOT / "results-matrix" / ".bin" / "fp-diagnose"),
                    help="Path to k8sgpt binary")
    ap.add_argument("--kagent-url", default="http://localhost:18080/")
    ap.add_argument("--skip-kagent", action="store_true")
    ap.add_argument("--skip-k8sgpt", action="store_true")
    ap.add_argument("--subjects", default="s2-listmonk,s3-healthchecks,s4-umami,s5-petclinic")
    ap.add_argument("--scenarios", default="F1,F2,F3,F4,F5,F6,F7,F8,F9,F10")
    ap.add_argument("--no-deploy", action="store_true",
                    help="Skip preview re-deploy, use existing live namespaces")
    args = ap.parse_args()

    # k8sgpt path
    k8sgpt_bin = ROOT / "bin" / "k8sgpt-0.4.21"
    if not k8sgpt_bin.exists():
        # Try the default
        k8sgpt_bin = pathlib.Path(args.k8sgpt_binary)
    if not k8sgpt_bin.exists() and not args.skip_k8sgpt:
        print(f"ERROR: k8sgpt binary not found at {k8sgpt_bin}", file=sys.stderr)
        return 1

    cfg = yaml.safe_load((ROOT / "multiapp" / "config.yaml").read_text())

    subjects = [s.strip() for s in args.subjects.split(",") if s.strip()]
    scenarios = [s.strip() for s in args.scenarios.split(",") if s.strip()]

    rows = []
    for subject in subjects:
        for scen in scenarios:
            print(f"\n=== {subject} / {scen} ===")
            row = {
                "subject": subject, "scenario": scen,
                "gt_role": SCENARIO_ROLE.get(scen, ""),
                "gt_category": SCENARIO_CATEGORY.get(scen, ""),
                "b2a_k8sgpt_component": "", "b2a_k8sgpt_aligned": "0",
                "b2b_kagent_component": "", "b2b_kagent_category": "",
                "b2b_kagent_aligned": "0",
            }
            try:
                # Deploy or detect existing
                if args.no_deploy:
                    rep = 1
                    pr = 90000 + SUBJECT_IDX[subject] * 1000 + int(scen[1:]) * 100 + rep
                    name = f"pr-{pr}"
                    ns = f"preview-{name}"
                else:
                    try:
                        name, ns = deploy_preview(subject, scen, cfg)
                    except ValueError as exc:
                        # F5 N/A for petclinic etc. — old config; with the fix
                        # this should not happen anymore.
                        print(f"  N/A: {exc}")
                        row["b2a_k8sgpt_component"] = "N/A"
                        rows.append(row)
                        continue
                    print(f"  deployed {name} -> {ns}")
                    if not wait_for_failure(ns):
                        print("  WARN: preview did not stabilise in 5 min; probing anyway")

                rep = 1
                rep_dir = RESULTS / subject / scen / f"r{rep}"
                rep_dir.mkdir(parents=True, exist_ok=True)

                # K8sGPT
                if not args.skip_k8sgpt:
                    blob = run_k8sgpt(str(k8sgpt_bin), ns)
                    (rep_dir / "k8sgpt.json").write_text(json.dumps(blob, indent=2))
                    comp, _cat = k8sgpt_to_component(blob, subject, scen)
                    row["b2a_k8sgpt_component"] = comp
                    row["b2a_k8sgpt_aligned"] = "1" if aligned_match(subject, scen, comp) else "0"
                    print(f"  [k8sgpt] component={comp[:60]!r} aligned={row['b2a_k8sgpt_aligned']}")

                # Kagent
                if not args.skip_kagent:
                    res = call_kagent(args.kagent_url, ns)
                    (rep_dir / "kagent.json").write_text(json.dumps(res, indent=2))
                    comp, cat = kagent_to_component(res)
                    row["b2b_kagent_component"] = comp
                    row["b2b_kagent_category"] = cat
                    row["b2b_kagent_aligned"] = "1" if aligned_match(subject, scen, comp) else "0"
                    print(f"  [kagent] component={comp[:60]!r} aligned={row['b2b_kagent_aligned']}")

                if not args.no_deploy:
                    teardown(name, ns)
                    print(f"  teardown {name}")
            except Exception as exc:
                print(f"  ERROR: {exc}", file=sys.stderr)
                row["b2a_k8sgpt_component"] = f"error: {exc}"

            rows.append(row)

    # Write CSV
    OUT_CSV.parent.mkdir(parents=True, exist_ok=True)
    with OUT_CSV.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        for r in rows:
            w.writerow(r)
    n_align_a = sum(1 for r in rows if r["b2a_k8sgpt_aligned"] == "1")
    n_align_b = sum(1 for r in rows if r["b2b_kagent_aligned"] == "1")
    n = len(rows)
    print(f"\nDONE: {n} cells written to {OUT_CSV}")
    print(f"  B2a K8sGPT aligned: {n_align_a}/{n}")
    print(f"  B2b Kagent aligned: {n_align_b}/{n}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
