#!/usr/bin/env python3
"""14c-b2-multiapp-rescore.py — re-score B2a (K8sGPT) + B2b (Kagent)
multi-app raw JSON outputs with a richer extraction logic, replacing
the on-the-fly parser in 14b-cluster-baselines-multiapp.py whose
output was matching 0/8 cells on the first subset.

What was wrong with the first parser
------------------------------------
- K8sGPT JSON has shape `{results: [{kind, name, details, error: [{Text}]}]}`.
  The original parser stuffed `kind + name + details` (~100 chars of
  free text) into the `component` column. The per-subject alias matcher
  expects a short token like `migration-job` / `backend` / `service`.
  Result: matcher missed every cell.
- Kagent JSON-RPC envelope has shape
  `{result: {artifacts: [{parts: [{kind: "text", text: "```json\n{component:...}\n```"}]}]}}`.
  The original parser only looked at the top-level text, returning
  the JSON-RPC `id` field. Result: matcher missed every cell.

Fixes
-----
- K8sGPT: scan `results[0].kind` + `results[0].name` + `results[0].details`
  AND `results[0].error[0].Text` for fingerprints of each role.
  Keyword-to-role map mirrors what k8sgpt actually says for our 10
  fault classes.
- Kagent: drill into `result.artifacts[].parts[]` for `kind: "text"`,
  strip ```json fences, parse the JSON object inside. Fall back to the
  text body for regex extraction if JSON parse fails.

Output:
  Overwrites experiments/failure-provenance/results-matrix/results-b2-multiapp.csv
  with the correctly-scored rows.
"""
from __future__ import annotations
import csv
import json
import pathlib
import re
import sys
from collections import defaultdict

ROOT = pathlib.Path(__file__).resolve().parents[1]
RESULTS = ROOT / "results-multiapp"
OUT_CSV = ROOT / "results-matrix" / "results-b2-multiapp.csv"

SUBJECT_IDX = {
    "s2-listmonk": 0, "s3-healthchecks": 1, "s4-umami": 2, "s5-petclinic": 3,
}

SCENARIO_ROLE = {
    "F1": "migration-job", "F2": "app", "F3": "app",
    "F4": "backend", "F5": "frontend", "F6": "app",
    "F7": "service", "F8": "backend", "F9": "seed-job",
    "F10": "test-suite",
}

SCENARIO_CATEGORY = {
    "F1": "database", "F2": "configuration", "F3": "infrastructure",
    "F4": "application", "F5": "application", "F6": "infrastructure",
    "F7": "infrastructure", "F8": "observability", "F9": "database",
    "F10": "test-reliability",
}

# Keyword-to-role fingerprint table — applied to K8sGPT's text output
# (kind + name + details + error[].Text concatenated). First match wins.
KEYWORD_ROLES = [
    (r"\b(migration|migrate|migrate-)\b", "migration-job"),
    (r"\b(seed|seeding|seed-job|catalogue|test data)\b", "seed-job"),
    (r"\b(image\s*pull|imagepullbackoff|errimagepull|not[\- ]?found.+image|pull.+image)\b", "app"),
    (r"\b(crashloopbackoff|configuration|env(ironment)?\s*var|keyerror|missing\s+config)\b", "app"),
    (r"\b(service\s*(does\s*not\s*exist|missing|broken|no\s*endpoint|selector))\b", "service"),
    (r"\b(endpoint|networking|routing|connection\s*refused|no\s*such\s*pod)\b", "service"),
    (r"\b(timeout|latency|slow|pg_sleep)\b", "backend"),
    (r"\b(test\s*(suite|reliability)|flaky|fp_f10|fp_f5_broken)\b", "test-suite"),
    (r"\b(frontend|html|javascript|next\.?js|admin\s*ui)\b", "frontend"),
    (r"\b(backend|api[\- ]?endpoint|backend\s*service|svc-backend)\b", "backend"),
    (r"\b(database\s*connection|db\s*readiness|postgres\s*unreachable)\b", "app"),
    (r"\b(deployment|deploy|pod\s*(not\s*ready|crash))\b", "app"),
]


def normalise(s: str) -> str:
    s = (s or "").lower().strip()
    s = re.sub(r"[^a-z0-9\- ]", "", s)
    return re.sub(r"\s+", " ", s)


def fingerprint_role(text: str) -> str:
    """Find the first matching role keyword in K8sGPT's free-form text."""
    if not text:
        return ""
    t = text.lower()
    for pat, role in KEYWORD_ROLES:
        if re.search(pat, t):
            return role
    return ""


def k8sgpt_extract(blob: dict, scenario: str) -> tuple[str, str]:
    """Return (component_role, category)."""
    results = blob.get("results") or blob.get("problems") or []
    if not results:
        return ("", "")
    # Concatenate the FIRST result's text fields for keyword search.
    p = results[0]
    parts = [
        str(p.get("kind", "")),
        str(p.get("name", "")),
        str(p.get("details", "")),
    ]
    for err in p.get("error", []) or []:
        if isinstance(err, dict):
            parts.append(str(err.get("Text", "")))
    text = " ".join(parts)
    role = fingerprint_role(text)
    return (role, SCENARIO_CATEGORY.get(scenario, "") if role else "")


def kagent_extract(blob: dict, scenario: str) -> tuple[str, str]:
    """Drill into Kagent A2A JSON-RPC envelope to find the agent's
    parsed JSON answer; fall back to regex on the raw text."""
    if not blob or "error" in blob:
        return ("", "")
    result = blob.get("result") or {}
    artifacts = result.get("artifacts") or []
    agent_text = ""
    for a in artifacts:
        for part in a.get("parts") or []:
            if isinstance(part, dict) and part.get("kind") == "text":
                agent_text += (part.get("text") or "") + "\n"
    if not agent_text:
        # Fall back to history's last assistant message
        history = result.get("history") or []
        for h in reversed(history):
            if h.get("role") in ("assistant", "agent"):
                for part in h.get("parts") or []:
                    if part.get("kind") == "text":
                        agent_text = part.get("text") or ""
                        break
                if agent_text:
                    break
    if not agent_text:
        return ("", "")
    # Strip markdown fences
    t = agent_text.strip()
    t = re.sub(r"```(?:json)?\s*", "", t)
    t = re.sub(r"\s*```\s*", "", t)
    # Try to find the JSON object
    m = re.search(r"\{[^{}]*\"component\"[^{}]*\}", t)
    if m:
        try:
            d = json.loads(m.group(0))
            comp = (d.get("component") or "").strip()
            cat = (d.get("category") or "").strip()
            # Map raw component string to a role via the keyword table
            role = fingerprint_role(comp) or comp
            return (role, cat)
        except Exception:
            pass
    # Last resort: keyword fingerprint on the entire agent text
    return (fingerprint_role(agent_text), "")


# Per-subject alias table (mirror of 17-multiapp-rescore).
SUBJECT_ALIASES_ROLE = {  # alias → role
    "migration-job": "migration-job",
    "postgres-migrate": "migration-job",
    "prisma-migrate": "migration-job",
    "flyway": "migration-job",
    "django-migrate": "migration-job",
    "app": "app",
    "application": "app",
    "app-deployment": "app",
    "deployment": "app",
    "listmonk": "app",
    "healthchecks": "app",
    "hc-web": "app",
    "umami": "app",
    "petclinic": "app",
    "spring-petclinic": "app",
    "backend": "backend",
    "svc-backend": "backend",
    "api": "backend",
    "frontend": "frontend",
    "ui": "frontend",
    "service": "service",
    "svc": "service",
    "seed-job": "seed-job",
    "seed": "seed-job",
    "test-suite": "test-suite",
    "tests": "test-suite",
    "flaky": "test-suite",
}


def aligned_for(subject: str, scenario: str, extracted_role: str) -> bool:
    """The extracted role is already canonical (from KEYWORD_ROLES);
    align if it matches the scenario's ground-truth role."""
    if not extracted_role:
        return False
    gt_role = SCENARIO_ROLE.get(scenario)
    if not gt_role:
        return False
    norm = SUBJECT_ALIASES_ROLE.get(extracted_role, extracted_role)
    return norm == gt_role


def main() -> int:
    rows = []
    for subject_dir in sorted(RESULTS.glob("s*-*")):
        subject = subject_dir.name
        if subject not in SUBJECT_IDX:
            continue
        for fdir in sorted(subject_dir.glob("F*")):
            scenario = fdir.name
            if scenario not in SCENARIO_ROLE:
                continue
            r1 = fdir / "r1"
            k8s_path = r1 / "k8sgpt.json"
            kag_path = r1 / "kagent.json"
            if not k8s_path.exists() and not kag_path.exists():
                continue
            row = {
                "subject": subject,
                "scenario": scenario,
                "gt_role": SCENARIO_ROLE[scenario],
                "gt_category": SCENARIO_CATEGORY.get(scenario, ""),
                "b2a_k8sgpt_role": "",
                "b2a_k8sgpt_aligned": "0",
                "b2b_kagent_role": "",
                "b2b_kagent_category": "",
                "b2b_kagent_aligned": "0",
            }
            if k8s_path.exists():
                try:
                    b = json.loads(k8s_path.read_text())
                    role, cat = k8sgpt_extract(b, scenario)
                    row["b2a_k8sgpt_role"] = role
                    row["b2a_k8sgpt_aligned"] = "1" if aligned_for(subject, scenario, role) else "0"
                except Exception as e:
                    row["b2a_k8sgpt_role"] = f"parse-error: {e}"
            if kag_path.exists():
                try:
                    b = json.loads(kag_path.read_text())
                    role, cat = kagent_extract(b, scenario)
                    row["b2b_kagent_role"] = role
                    row["b2b_kagent_category"] = cat
                    row["b2b_kagent_aligned"] = "1" if aligned_for(subject, scenario, role) else "0"
                except Exception as e:
                    row["b2b_kagent_role"] = f"parse-error: {e}"
            rows.append(row)

    if not rows:
        print("no rows to score", file=sys.stderr)
        return 1
    with OUT_CSV.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        for r in rows:
            w.writerow(r)
    n_align_a = sum(1 for r in rows if r["b2a_k8sgpt_aligned"] == "1")
    n_align_b = sum(1 for r in rows if r["b2b_kagent_aligned"] == "1")
    n = len(rows)
    print(f"Re-scored {n} cells")
    print(f"  B2a K8sGPT aligned: {n_align_a}/{n} ({100*n_align_a/n:.1f}%)")
    print(f"  B2b Kagent aligned: {n_align_b}/{n} ({100*n_align_b/n:.1f}%)")
    print(f"  CSV: {OUT_CSV}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
