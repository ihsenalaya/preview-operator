#!/usr/bin/env python3
"""09b-baseline-b0-multiapp.py — B0 baseline for multi-app captures.

Same B0 question as 09-baseline-b0.py for S1: "what does a vanilla LLM
guess if you just hand it the raw cluster output?". The multi-app matrix
did not separately capture kubectl artifacts during the run (the
orchestrator only saves the FailureReport), so for multi-app the
kubectl-equivalent bundle is reconstructed from the FailureReport's
evidence items, excluding the operator-specific PreviewCondition and
TestResult types — the LLM only sees what a Kubernetes SRE would have
from `kubectl get events / pods / logs`.

This is a documented deviation from the S1 B0 protocol: there, B0 read
separately-captured kubectl text files; here, it reads the
FailureReport's kubectl-equivalent evidence items. The deviation is
recorded in the §Threats-to-Validity table of EVALUATION-DRAFT.md.

Inputs:  experiments/failure-provenance/results-multiapp/{subject}/{F}/{r}/report.json
Outputs: experiments/failure-provenance/results-multiapp/{subject}/{F}/{r}/b0-diag.json
         experiments/failure-provenance/results-matrix/results-b0-multiapp.csv

Scoring uses the same vocabulary-aligned alias table as 17-multiapp-rescore.py
so B0 is matcher-comparable with the operator engines.
"""
from __future__ import annotations
import csv
import json
import os
import pathlib
import re
import sys
import time
import urllib.request
import urllib.error

ROOT = pathlib.Path(__file__).resolve().parents[1]
RESULTS = ROOT / "results-multiapp"
OUT_CSV = ROOT / "results-matrix" / "results-b0-multiapp.csv"

# Mirror of 17-multiapp-rescore.py's per-subject alias table.
SUBJECT_ALIASES: dict[tuple[str, str], list[str]] = {
    # (subject, role) → list of regex patterns that match a credited diagnosis
    ("s2-listmonk", "migration-job"): [
        r"^migration-?job$", r"^postgres-?migrate$", r"^migration$",
        r"^db.?migration$",
    ],
    ("s2-listmonk", "app"): [
        r"^listmonk$", r"^backend$", r"^svc-backend$", r"^app$",
        r"^application$", r"^app-?deployment$",
    ],
    ("s2-listmonk", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^routing$",
        r"^endpoint$", r"^networking$",
    ],
    ("s3-healthchecks", "migration-job"): [
        r"^migration-?job$", r"^postgres-?migrate$", r"^django.?migrate$",
        r"^migration$",
    ],
    ("s3-healthchecks", "app"): [
        r"^healthchecks$", r"^hc-?web$", r"^backend$", r"^svc-backend$",
        r"^app$", r"^application$",
    ],
    ("s3-healthchecks", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^routing$",
        r"^endpoint$",
    ],
    ("s4-umami", "migration-job"): [
        r"^migration-?job$", r"^prisma.?migrate$", r"^migration$",
        r"^postgres-?migrate$",
    ],
    ("s4-umami", "app"): [
        r"^umami$", r"^backend$", r"^svc-backend$", r"^app$",
        r"^application$",
    ],
    ("s4-umami", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^routing$",
    ],
    ("s5-petclinic", "migration-job"): [
        r"^migration-?job$", r"^flyway$", r"^migration$",
        r"^postgres-?migrate$",
    ],
    ("s5-petclinic", "app"): [
        r"^spring-?petclinic$", r"^petclinic$", r"^backend$",
        r"^svc-backend$", r"^app$", r"^application$",
    ],
    ("s5-petclinic", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^routing$",
    ],
}

GROUND_TRUTH = {
    "F1":  {"role": "migration-job", "category": "database"},
    "F2":  {"role": "app",           "category": "configuration"},
    "F3":  {"role": "app",           "category": "infrastructure"},
    "F6":  {"role": "app",           "category": "infrastructure"},
    "F7":  {"role": "service",       "category": "infrastructure"},
}

SYSTEM_PROMPT = (
    "You are a Kubernetes SRE diagnosing a failing pull-request preview "
    "environment. The user pastes raw cluster output (events, pods, "
    "container logs, job logs). Identify the single most likely root cause.\n\n"
    "Reply with ONLY a JSON object — no prose, no markdown fence — of the "
    "form:\n"
    "{\n"
    '  "component":   "<short identifier of the failing component>",\n'
    '  "category":    "<one of: database | configuration | infrastructure | '
    'application | observability | test-reliability>",\n'
    '  "probableCause":"<one sentence>",\n'
    '  "confidence":  "<low|medium|high>"\n'
    "}\n"
)


def normalise(s: str) -> str:
    if not s:
        return ""
    out = s.lower().strip()
    out = re.sub(r"[^a-z0-9\- ]", "", out)
    out = re.sub(r"\s+", " ", out)
    return out


def aligned_match(subject: str, scenario: str, diag_component: str) -> bool:
    role = GROUND_TRUTH.get(scenario, {}).get("role")
    if not role:
        return False
    target = normalise(diag_component)
    patterns = SUBJECT_ALIASES.get((subject, role), [])
    for pat in patterns:
        if re.match(pat, target):
            return True
    return False


def build_bundle_from_report(report: dict) -> str:
    """Reconstruct a kubectl-equivalent bundle from the FailureReport's
    evidence items. Excludes PreviewCondition (operator-specific) and
    TestResult (operator-derived) — only what an SRE would see from
    kubectl alone."""
    status = report.get("status", {})
    items = status.get("evidenceItems", [])

    events, pod_logs, job_logs = [], [], []
    for e in items:
        t = e.get("type")
        msg = (e.get("message") or "").strip()
        res = (e.get("resource") or "").strip()
        if t == "KubernetesEvent":
            events.append(f"- {res}: {msg}")
        elif t == "PodLog":
            pod_logs.append(f"### kubectl logs {res}\n{msg}")
        elif t == "JobLog":
            job_logs.append(f"### kubectl logs {res}\n{msg}")

    parts = []
    if events:
        parts.append("### kubectl get events --sort-by=.lastTimestamp\n"
                     + "\n".join(events))
    parts.extend(job_logs)
    parts.extend(pod_logs)
    return "\n\n".join(parts) if parts else "(no kubectl output captured)"


def call_llm(api_url: str, api_key: str, system: str, user: str,
             model: str = "gpt-4o-mini",
             max_retries: int = 5) -> tuple[str, int, int]:
    body = json.dumps({
        "model": model,
        "temperature": 0,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
    }).encode("utf-8")
    url = f"{api_url.rstrip('/')}/chat/completions?api-version=2024-08-01-preview"
    last_err: Exception | None = None
    backoff = 3
    for attempt in range(max_retries):
        req = urllib.request.Request(
            url, data=body,
            headers={"Content-Type": "application/json", "api-key": api_key},
            method="POST")
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                data = json.loads(resp.read())
                content = data["choices"][0]["message"]["content"]
                usage = data.get("usage", {})
                return (content, usage.get("prompt_tokens", 0),
                        usage.get("completion_tokens", 0))
        except urllib.error.HTTPError as e:
            if e.code == 429:
                time.sleep(backoff)
                backoff *= 2
                last_err = e
                continue
            raise
        except Exception as e:
            last_err = e
            time.sleep(backoff)
            backoff *= 2
    raise RuntimeError(f"LLM call failed after {max_retries} retries: {last_err}")


def parse_json_resp(text: str) -> dict:
    """Extract the JSON object, even if the LLM wrapped it in markdown."""
    t = text.strip()
    if t.startswith("```"):
        t = re.sub(r"^```(?:json)?\s*", "", t)
        t = re.sub(r"\s*```\s*$", "", t)
    try:
        return json.loads(t)
    except json.JSONDecodeError:
        m = re.search(r"\{[\s\S]*\}", t)
        if m:
            return json.loads(m.group(0))
        raise


def main() -> int:
    api_url = os.environ.get("AI_API_URL", "")
    api_key = (os.environ.get("OPENAI_API_KEY")
               or os.environ.get("AI_API_KEY", ""))
    if not api_url or not api_key:
        print("ERROR: set AI_API_URL + OPENAI_API_KEY (or AI_API_KEY)",
              file=sys.stderr)
        return 1

    model = os.environ.get("AI_MODEL", "gpt-4o-mini")
    rows: list[dict] = []
    n_calls = 0
    t_prompt = t_completion = 0

    for subject_dir in sorted(RESULTS.glob("s*-*")):
        subject = subject_dir.name
        for fdir in sorted(subject_dir.glob("F*")):
            scenario = fdir.name
            if scenario not in GROUND_TRUTH:
                continue
            for rdir in sorted(fdir.glob("r*")):
                report_path = rdir / "report.json"
                if not report_path.is_file():
                    continue
                out_path = rdir / "b0-diag.json"
                if out_path.is_file() and out_path.stat().st_size > 0:
                    # already done — load and add to rows
                    try:
                        cached = json.loads(out_path.read_text())
                        diag = cached.get("diagnosis", {})
                    except Exception:
                        diag = {}
                else:
                    report = json.loads(report_path.read_text())
                    bundle = build_bundle_from_report(report)
                    user_msg = (
                        "Below is everything I have from this preview "
                        "environment. Diagnose it.\n\n" + bundle
                    )
                    try:
                        content, pt, ct = call_llm(api_url, api_key,
                                                   SYSTEM_PROMPT, user_msg,
                                                   model)
                        n_calls += 1
                        t_prompt += pt
                        t_completion += ct
                        diag = parse_json_resp(content)
                    except Exception as e:
                        print(f"FAIL {subject}/{scenario}/{rdir.name}: {e}",
                              file=sys.stderr)
                        diag = {"component": "", "category": "",
                                "probableCause": f"error: {e}",
                                "confidence": "low"}
                    out_path.write_text(json.dumps({
                        "engine": "vanilla-llm-b0",
                        "model": model,
                        "diagnosis": diag,
                        "_calls": 1,
                    }, indent=2))

                aligned = aligned_match(subject, scenario,
                                        diag.get("component", ""))
                rep = rdir.name
                gt = GROUND_TRUTH[scenario]
                rows.append({
                    "subject": subject,
                    "scenario": scenario,
                    "rep": rep,
                    "gt_role": gt["role"],
                    "gt_category": gt["category"],
                    "b0_component": diag.get("component", ""),
                    "b0_category": diag.get("category", ""),
                    "b0_aligned": "1" if aligned else "0",
                })

    OUT_CSV.parent.mkdir(parents=True, exist_ok=True)
    with OUT_CSV.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        for r in rows:
            w.writerow(r)
    print(f"B0 multi-app: {len(rows)} rows, {n_calls} new LLM calls, "
          f"{t_prompt} + {t_completion} tokens")
    print(f"CSV: {OUT_CSV}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
