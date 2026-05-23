#!/usr/bin/env python3
"""14-cluster-baselines.py — drive B2a (K8sGPT v0.4.21) and B2b (Kagent
generic k8s-agent) baselines against live failing preview namespaces.

Assumes:
  - The baseline orchestrator (`run-kind-experiments.sh --keep --reps 1
    --scenario all`) has deployed previews `preview-pr-9300..9309` and
    each has a `FailureReport`. The `--keep` flag prevents teardown so
    K8sGPT and Kagent can probe the live cluster.
  - K8sGPT v0.4.21 is at `experiments/failure-provenance/bin/k8sgpt-0.4.21`
    or `/tmp/k8sgpt-bin/k8sgpt`; it has been authenticated against
    `preview-openai-idp` (the same Azure OpenAI deployment used by the
    operator and by B0).
  - `kubectl -n kagent-system port-forward svc/k8s-agent 18080:8080` is
    running, OR this script port-forwards itself.

For each scenario:
  1. Resolve the namespace from `preview-pr-9300+i`.
  2. Run K8sGPT analyze with --explain --backend azureopenai -o json.
     Parse the JSON, pick the top-confidence "problem", map to a
     component/category prediction.
  3. Send a Kagent A2A `message/send` with a fixed SRE diagnostic prompt
     that names the namespace. Parse the agent's reply for a
     component/category JSON.

Output:
  experiments/failure-provenance/results-baselines/F*/r1/k8sgpt.json
  experiments/failure-provenance/results-baselines/F*/r1/kagent.json
  experiments/failure-provenance/results-matrix/results-b2.csv

Both are scored with the same vocabulary-aligned matcher as B0, the
operator engines, and the LLM-B replay — so the comparison is
matcher-fair.
"""
from __future__ import annotations

import argparse
import csv
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone


SCENARIO_GT = {
    "F1":  {"component": "migration-job",  "category": "database"},
    "F2":  {"component": "app-deployment", "category": "configuration"},
    "F3":  {"component": "app-deployment", "category": "infrastructure"},
    "F4":  {"component": "backend",        "category": "application"},
    "F5":  {"component": "frontend",       "category": "application"},
    "F6":  {"component": "app-deployment", "category": "infrastructure"},
    "F7":  {"component": "service",        "category": "infrastructure"},
    "F8":  {"component": "backend",        "category": "observability"},
    "F9":  {"component": "seed-job",       "category": "database"},
    "F10": {"component": "test-suite",     "category": "test-reliability"},
}

# Per-scenario alias matcher (mirror of 08-vocab-rescore.py).
ALIASES: dict[str, list[str]] = {
    "F1":  [r"^migration-job$", r"^migration$", r"^database migration$",
            r"^db migration$", r"^postgres-migrate$", r"^alembic$",
            r"^database$"],
    "F2":  [r"^app-deployment$", r"^app$", r"^backend$", r"^svc-backend$",
            r"^application$", r"^deployment$"],
    "F3":  [r"^app-deployment$", r"^app$", r"^backend$", r"^svc-backend$",
            r"^application$", r"^deployment$", r"^image$", r"^image-pull$"],
    "F4":  [r"^backend$", r"^svc-backend$", r"^api$", r"^api-endpoint$",
            r"^application$", r"^route$", r"^app\.py$"],
    "F5":  [r"^frontend$", r"^svc-frontend$", r"^ui$", r"^catalogue$",
            r"^catalog$", r"^html$", r"^frontend\.py$"],
    "F6":  [r"^app-deployment$", r"^app$", r"^backend$", r"^svc-backend$",
            r"^application$", r"^database connection$", r"^db readiness$"],
    "F7":  [r"^service$", r"^svc-backend$", r"^backend$", r"^selector$",
            r"^routing$", r"^endpoint$", r"^endpoints$", r"^networking$"],
    "F8":  [r"^backend$", r"^svc-backend$", r"^api$", r"^application$",
            r"^latency$", r"^observability$", r"^app\.py$"],
    "F9":  [r"^seed-job$", r"^seed$", r"^seeding$", r"^ai-seed$", r"^data$",
            r"^test data$", r"^regression$"],
    "F10": [r"^test-suite$", r"^test suite$", r"^tests$", r"^test$",
            r"^test-reliability$", r"^flaky test$", r"^flaky$",
            r"^regression$"],
}


def normalise(s: str) -> str:
    s = (s or "").lower().strip()
    s = re.sub(r"[^a-z0-9\- ]", "", s)
    return re.sub(r"\s+", " ", s)


def aligned_match(scenario: str, comp: str) -> bool:
    target = normalise(comp)
    if not target:
        return False
    return any(re.match(pat, target) for pat in ALIASES.get(scenario, []))


# ---------- K8sGPT ---------------------------------------------------------

K8SGPT_OUT_TO_COMPONENT_HINTS = [
    # (regex, normalised_component) — ordered most-specific-first. The
    # image-pull and selector matches must come before the generic
    # "migration" / "backend" hints because the latter are substrings of
    # pod names that K8sGPT often surfaces even when the root cause is
    # elsewhere (e.g. F3's image-pull failure happens on a migration pod).
    (r"image[ -]?pull|errimagepull|manifest unknown", "app-deployment"),
    (r"no endpoints|endpoints not available|selector mismatch", "service"),
    (r"crashloopbackoff|missing.{0,30}env|keyerror|environment variable",
     "app-deployment"),
    (r"contract.{0,20}fail|backend.{0,20}endpoint",     "backend"),
    (r"frontend|catalogue|playwright|ui",                "frontend"),
    (r"slow|latency|timeout|deadline exceeded",         "backend"),
    (r"\bseed-?job\b|ai-seed",                           "seed-job"),
    (r"\bmigration\b.*(sql|syntax|error)|\balembic\b.*error|postgres-migrate.*error|postgres-migrate.*fail",
     "migration-job"),
    (r"\bmigration\b|\balembic\b|postgres-migrate",     "migration-job"),
]


def run_k8sgpt(binary: str, namespace: str, *, timeout: int = 180) -> dict:
    """Run K8sGPT analyze and return the parsed JSON (problems array)."""
    cmd = [
        binary, "analyze",
        "--namespace", namespace,
        "--backend", "azureopenai",
        "--explain",
        "--no-cache",
        "--output", "json",
    ]
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    raw = p.stdout
    if not raw:
        return {"_error": p.stderr.strip() or "empty stdout",
                "_returncode": p.returncode}
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        # k8sgpt sometimes wraps JSON in noise — extract first {...}
        m = re.search(r"\{[\s\S]*\}", raw)
        if m:
            try:
                return json.loads(m.group(0))
            except json.JSONDecodeError:
                pass
        return {"_error": "k8sgpt stdout is not JSON",
                "_raw": raw[:500]}


def k8sgpt_to_component(blob: dict, scenario: str) -> tuple[str, str]:
    """Map a K8sGPT analyze blob to a (component, category) tuple,
    using a deterministic best-match rule. Handles both schema shapes
    (`problems` and `results` keys) and the fact that K8sGPT v0.4.21's
    `error` field is a list of structured items, not a string."""
    # K8sGPT v0.4.21 puts the count in `problems` (int) and the array in
    # `results`. Older docs called the array `problems`. Prefer whichever
    # is a list.
    results = blob.get("results")
    problems_raw = blob.get("problems")
    if isinstance(results, list):
        problems = results
    elif isinstance(problems_raw, list):
        problems = problems_raw
    else:
        problems = []
    if not problems:
        if blob.get("_error"):
            return "k8sgpt-error", "unknown"
        return "no-issue-detected", "unknown"

    def stringify(v):
        if v is None:
            return ""
        if isinstance(v, list):
            return " ".join(
                stringify(x.get("Text") if isinstance(x, dict) else x)
                for x in v)
        if isinstance(v, dict):
            return " ".join(str(x) for x in v.values())
        return str(v)

    # Crucially: route on the *structured* finding (kind/name/error[]),
    # NOT on the LLM-generated `details` field. The remediation advice
    # K8sGPT's LLM produces almost always mentions "environment variable"
    # or "configuration", which would over-fire the F2 hint and mis-
    # classify scenarios. The structured fields are the actual cluster
    # observation; the LLM details are the suggested fix.
    body = " ".join(
        stringify(p.get("error")) + " "
        + stringify(p.get("kind")) + " "
        + stringify(p.get("name"))
        for p in problems
    ).lower()
    for pat, comp in K8SGPT_OUT_TO_COMPONENT_HINTS:
        if re.search(pat, body):
            return comp, ""
    first = problems[0]
    return (
        f"{first.get('kind', '?')}/{first.get('name', '?')}",
        "",
    )


# ---------- Kagent k8s-agent (A2A JSON-RPC) -------------------------------

KAGENT_USER_PROMPT_TEMPLATE = (
    "You are diagnosing a failing Kubernetes preview environment in the "
    "namespace `{namespace}`. Use kubectl tools to inspect the namespace, "
    "find the single most likely root cause, and reply with ONLY a JSON "
    "object of the form:\n\n"
    "{{\n"
    '  "component": "<short identifier of the failing component>",\n'
    '  "category":  "<database | configuration | infrastructure | '
    'application | observability | test-reliability>",\n'
    '  "probableCause": "<one sentence>"\n'
    "}}\n\n"
    "Use the namespace `{namespace}` and inspect its events, pods, "
    "deployments, services, jobs, endpoints and the most recent container "
    "logs. Do not return any prose outside the JSON object."
)


def call_kagent(url: str, namespace: str, *, timeout: int = 240) -> dict:
    """POST one A2A message/send to Kagent k8s-agent. Return parsed dict
    with `_diagnosis` (the parsed JSON inside the agent's text part) and
    `_raw_text` (the full text reply)."""
    msg = KAGENT_USER_PROMPT_TEMPLATE.format(namespace=namespace)
    body = json.dumps({
        "jsonrpc": "2.0",
        "id": f"probe-{namespace}-{int(time.time())}",
        "method": "message/send",
        "params": {
            "message": {
                "kind": "message",
                "role": "user",
                "messageId": f"probe-{namespace}-{int(time.time())}",
                "parts": [{"kind": "text", "text": msg}],
            },
        },
    }).encode()
    req = urllib.request.Request(
        url, data=body,
        headers={"Content-Type": "application/json"}, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            payload = json.loads(resp.read().decode())
    except (urllib.error.URLError, TimeoutError) as e:
        return {"_error": str(e)}
    result = payload.get("result", {})
    artifacts = result.get("artifacts", [])
    raw = ""
    if artifacts:
        for part in artifacts[0].get("parts", []):
            if part.get("kind") == "text":
                raw = part.get("text", "")
                break
    # Best-effort JSON extraction
    diag = None
    m = re.search(r"\{[\s\S]*\}", raw)
    if m:
        try:
            diag = json.loads(m.group(0))
        except json.JSONDecodeError:
            diag = {"_parse_error": True}
    return {"_diagnosis": diag, "_raw_text": raw, "_full": payload}


# ---------- driver --------------------------------------------------------


def find_namespace_for_scenario(scen: str, pr_base: int) -> str | None:
    """Map an F-scenario to its live preview namespace via the `branch`
    label set by the orchestrator (`fp-f1`, `fp-f2`, …). The `pr_base`
    argument is kept for backward compatibility but unused now."""
    label = f"fp-{scen.lower()}"
    p = subprocess.run(
        ["kubectl", "get", "namespace",
         "-l", f"branch={label}",
         "-o", "jsonpath={.items[*].metadata.name}"],
        capture_output=True, text=True)
    names = (p.stdout or "").split()
    return names[0] if names else None


def wait_for_failure_report(ns: str, *, timeout: int = 1200) -> bool:
    """Wait until a FailureReport exists in the cluster for this preview."""
    deadline = time.time() + timeout
    pr = ns.replace("preview-pr-", "")
    while time.time() < deadline:
        p = subprocess.run(
            ["kubectl", "get", "failurereport",
             f"pr-{pr}-failure", "-o", "name"],
            capture_output=True, text=True)
        if p.returncode == 0 and p.stdout.strip():
            return True
        time.sleep(15)
    return False


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--scenarios", default="F1,F2,F3,F4,F5,F6,F7,F8,F9,F10")
    ap.add_argument("--pr-base", type=int, default=9300)
    ap.add_argument("--out-dir",
        default="experiments/failure-provenance/results-baselines")
    ap.add_argument("--out-csv",
        default="experiments/failure-provenance/results-matrix/"
                "results-b2.csv")
    ap.add_argument("--k8sgpt-binary",
        default="experiments/failure-provenance/bin/k8sgpt-0.4.21")
    ap.add_argument("--kagent-url", default="http://localhost:18080/",
        help="A2A JSON-RPC endpoint. Use kubectl port-forward.")
    ap.add_argument("--skip-k8sgpt", action="store_true")
    ap.add_argument("--skip-kagent", action="store_true")
    ap.add_argument("--wait-timeout", type=int, default=1200,
        help="seconds to wait per scenario for FailureReport.")
    args = ap.parse_args()

    if not args.skip_k8sgpt and not pathlib.Path(args.k8sgpt_binary).exists():
        # Fall back to /tmp/k8sgpt-bin/k8sgpt
        fallback = pathlib.Path("/tmp/k8sgpt-bin/k8sgpt")
        if fallback.exists():
            args.k8sgpt_binary = str(fallback)
        else:
            print(f"K8sGPT binary not found: {args.k8sgpt_binary}",
                  file=sys.stderr)
            return 2

    rows: list[dict] = []
    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    for scen in args.scenarios.split(","):
        gt = SCENARIO_GT[scen]
        ns = find_namespace_for_scenario(scen, args.pr_base)
        if ns is None:
            print(f"[skip] {scen}: no namespace found "
                  f"(preview-pr-{args.pr_base + int(scen[1:]) - 1})")
            continue
        print(f"\n=== {scen} → {ns} ===")

        # Wait for the FailureReport so the operator has finished its
        # capture before we run the baseline probes. This makes the
        # comparison apples-to-apples: same evidence, three probes.
        if not wait_for_failure_report(ns, timeout=args.wait_timeout):
            print(f"[skip] {scen}: no FailureReport within "
                  f"{args.wait_timeout}s")
            continue

        scen_dir = out_dir / scen / "r1"
        scen_dir.mkdir(parents=True, exist_ok=True)
        row = {
            "run_id": f"{scen}-r1-B2",
            "scenario_id": scen,
            "namespace": ns,
            "gt_component": gt["component"],
            "gt_category":  gt["category"],
            "probed_at_utc": datetime.now(timezone.utc).isoformat(
                timespec="seconds"),
        }

        # ----- B2a K8sGPT -----
        if not args.skip_k8sgpt:
            t0 = time.time()
            print(f"  [k8sgpt] analyze --namespace {ns}")
            blob = run_k8sgpt(args.k8sgpt_binary, ns)
            (scen_dir / "k8sgpt.json").write_text(
                json.dumps(blob, indent=2) + "\n")
            comp, _ = k8sgpt_to_component(blob, scen)
            row["b2a_k8sgpt_component"] = comp
            row["b2a_k8sgpt_aligned"] = "1" if aligned_match(scen, comp) else "0"
            row["b2a_k8sgpt_seconds"] = f"{time.time()-t0:.1f}"
            print(f"    → component={comp!r}  aligned="
                  f"{row['b2a_k8sgpt_aligned']}")

        # ----- B2b Kagent k8s-agent -----
        if not args.skip_kagent:
            t0 = time.time()
            print(f"  [kagent] message/send for {ns}")
            res = call_kagent(args.kagent_url, ns)
            (scen_dir / "kagent.json").write_text(
                json.dumps(res, indent=2) + "\n")
            diag = res.get("_diagnosis") or {}
            comp = (diag.get("component") if isinstance(diag, dict) else "") or ""
            cat = (diag.get("category")  if isinstance(diag, dict) else "") or ""
            row["b2b_kagent_component"] = comp
            row["b2b_kagent_category"]  = cat
            row["b2b_kagent_aligned"]   = "1" if aligned_match(scen, comp) else "0"
            row["b2b_kagent_seconds"]   = f"{time.time()-t0:.1f}"
            print(f"    → component={comp!r}  category={cat!r}  "
                  f"aligned={row['b2b_kagent_aligned']}")

        rows.append(row)

    # Write CSV
    if rows:
        fields = sorted({k for r in rows for k in r.keys()})
        with pathlib.Path(args.out_csv).open("w", newline="") as fh:
            w = csv.DictWriter(fh, fieldnames=fields)
            w.writeheader()
            w.writerows(rows)
        print(f"\nwrote {args.out_csv}")
        # Per-baseline aligned tally
        for col in ("b2a_k8sgpt_aligned", "b2b_kagent_aligned"):
            if any(col in r for r in rows):
                s = sum(int(r.get(col, "0") or "0") for r in rows)
                n = sum(1 for r in rows if col in r)
                print(f"  {col}: {s}/{n} = {100*s/n:.1f}%")
    return 0


if __name__ == "__main__":
    sys.exit(main())
