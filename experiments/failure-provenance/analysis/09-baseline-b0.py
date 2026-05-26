#!/usr/bin/env python3
"""09-baseline-b0.py — B0 practitioner baseline: vanilla LLM on raw kubectl.

The B0 baseline answers the question "what would an SRE get if they just
pasted their kubectl output into a chat LLM?". It is the floor we expect the
operator + FailureReport + provenance stack to clear, and the article's
RQ-baselines panel claims operator-side evidence is what makes the LLM
useful — that claim requires a measured B0 floor, not a hand-waved one.

Inputs (per scenario × rep, no operator artifacts):
  results-matrix/{F}/{r}/artifacts/
    events.txt         (kubectl get events --sort-by=.lastTimestamp)
    pods.txt           (kubectl get pods -o wide)
    deployments.txt    (kubectl get deploy)
    services.txt       (kubectl get svc)
    jobs.txt           (kubectl get jobs)
    endpoints.txt      (kubectl get endpoints)
    logs/*.log         (kubectl logs per pod — last 200 lines)

Excluded on purpose — would leak operator-side information:
    failurereport.yaml, reconcileevents.yaml, idp-preview/ source tree.

Output: one JSON per rep at  results-matrix/{F}/{r}/b0-diag.json
        plus a flat CSV   results-matrix/results-b0.csv
        scored with the same per-scenario alias table that 08-vocab-rescore
        uses, so B0 vs operator engines are matcher-comparable.

Run cost: 100 calls × ~5K-15K input tokens × gpt-4o-mini ≈ USD 0.30 total.
Honours AI_API_URL + OPENAI_API_KEY (Azure OpenAI-compatible). Reproducible
by re-running this script unchanged — temperature is fixed at 0.
"""
from __future__ import annotations

import argparse
import csv
import json
import os
import pathlib
import re
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone

# --- vocabulary alignment (mirror of 08-vocab-rescore.py) ---------------------
# Kept inline so this script is standalone; the canonical table lives in
# 08-vocab-rescore.py — keep both in sync.
ALIASES: dict[str, dict[str, list[str]]] = {
    "F1": {"migration-job": [
        r"^migration-job$", r"^migration$", r"^database migration$",
        r"^db migration$", r"^postgres-migrate$", r"^alembic$", r"^database$",
    ]},
    "F2": {"app-deployment": [
        r"^app-deployment$", r"^app$", r"^backend$", r"^svc-backend$",
        r"^application$", r"^deployment$",
    ]},
    "F3": {"app-deployment": [
        r"^app-deployment$", r"^app$", r"^backend$", r"^svc-backend$",
        r"^application$", r"^deployment$", r"^image$", r"^image-pull$",
    ]},
    "F4": {"backend": [
        r"^backend$", r"^svc-backend$", r"^api$", r"^api-endpoint$",
        r"^application$", r"^route$", r"^app\.py$",
    ]},
    "F5": {"frontend": [
        r"^frontend$", r"^svc-frontend$", r"^ui$", r"^catalogue$",
        r"^catalog$", r"^html$", r"^frontend\.py$",
    ]},
    "F6": {"app-deployment": [
        r"^app-deployment$", r"^app$", r"^backend$", r"^svc-backend$",
        r"^application$", r"^database connection$", r"^db readiness$",
    ]},
    "F7": {"service": [
        r"^service$", r"^svc-backend$", r"^backend$", r"^selector$",
        r"^routing$", r"^endpoint$", r"^endpoints$", r"^networking$",
    ]},
    "F8": {"backend": [
        r"^backend$", r"^svc-backend$", r"^api$", r"^application$",
        r"^latency$", r"^observability$", r"^app\.py$",
    ]},
    "F9": {"seed-job": [
        r"^seed-job$", r"^seed$", r"^seeding$", r"^ai-seed$", r"^data$",
        r"^test data$", r"^regression$",
    ]},
    "F10": {"test-suite": [
        r"^test-suite$", r"^test suite$", r"^tests$", r"^test$",
        r"^test-reliability$", r"^flaky test$", r"^flaky$", r"^regression$",
    ]},
}

GROUND_TRUTH: dict[str, dict[str, str]] = {
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

CATEGORIES = sorted({g["category"] for g in GROUND_TRUTH.values()})

# Per-log-file tail length — enough for the failure signal, short enough to
# keep the prompt under ~20K tokens.
LOG_TAIL_LINES = 200
# Per-section character cap, defence-in-depth against pathological logs.
SECTION_CHAR_CAP = 25_000


def normalise(s: str) -> str:
    if not s:
        return ""
    out = s.lower().strip()
    out = re.sub(r"[^a-z0-9\- ]", "", out)
    out = re.sub(r"\s+", " ", out)
    return out


def aligned_match(scenario: str, diag_component: str) -> bool:
    cfg = ALIASES.get(scenario, {})
    target = normalise(diag_component)
    if not target:
        return False
    for patterns in cfg.values():
        for pat in patterns:
            if re.match(pat, target):
                return True
    return False


def read_text(p: pathlib.Path, cap: int = SECTION_CHAR_CAP) -> str:
    try:
        data = p.read_text(errors="replace")
    except FileNotFoundError:
        return ""
    if len(data) > cap:
        data = data[:cap] + f"\n... [truncated, {len(data) - cap} chars omitted]"
    return data


def tail(p: pathlib.Path, n: int = LOG_TAIL_LINES, cap: int = SECTION_CHAR_CAP) -> str:
    try:
        lines = p.read_text(errors="replace").splitlines()
    except FileNotFoundError:
        return ""
    tailed = "\n".join(lines[-n:])
    if len(tailed) > cap:
        tailed = tailed[-cap:]
        tailed = "... [truncated]\n" + tailed
    return tailed


def build_kubectl_bundle(artifacts_dir: pathlib.Path) -> str:
    """Concatenate the kubectl outputs an SRE would routinely capture."""
    parts: list[str] = []
    for name in ("events.txt", "pods.txt", "deployments.txt", "services.txt",
                 "endpoints.txt", "jobs.txt"):
        body = read_text(artifacts_dir / name)
        if body.strip():
            parts.append(f"### kubectl {name.removesuffix('.txt')}\n{body}")

    logs_dir = artifacts_dir / "logs"
    if logs_dir.is_dir():
        # Skip *.previous.log to avoid double-counting; SREs read the current
        # log first, the previous log is only consulted if needed.
        log_files = sorted(p for p in logs_dir.glob("*.log")
                           if not p.name.endswith(".previous.log"))
        for lp in log_files:
            body = tail(lp)
            if body.strip():
                parts.append(f"### kubectl logs {lp.name}\n{body}")

    return "\n\n".join(parts) if parts else "(no kubectl output captured)"


SYSTEM_PROMPT = (
    "You are a Kubernetes SRE diagnosing a failing pull-request preview "
    "environment. The user pastes raw kubectl output (events, pods, "
    "deployments, services, endpoints, jobs, container logs) collected from "
    "the preview namespace. Identify the single most likely root cause.\n\n"
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


USER_PROMPT_HEADER = (
    "Below is everything I have from this preview environment. Diagnose it.\n\n"
)


def call_llm(api_url: str, api_key: str, model: str,
             system: str, user: str, max_retries: int = 8) -> tuple[str, int, int]:
    """Single Azure OpenAI chat-completions call at temperature 0.

    Returns (content, prompt_tokens, completion_tokens). Raises on hard
    failure after retries.
    """
    body = json.dumps({
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
        "temperature": 0,
        "response_format": {"type": "json_object"},
        "max_tokens": 400,
    }).encode()
    url = f"{api_url.rstrip('/')}/chat/completions?api-version=2024-08-01-preview"
    last_err: Exception | None = None
    for attempt in range(max_retries):
        req = urllib.request.Request(
            url,
            data=body,
            headers={
                "Content-Type": "application/json",
                "api-key": api_key,
                "Authorization": f"Bearer {api_key}",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                data = json.loads(resp.read().decode())
                content = data["choices"][0]["message"]["content"]
                usage = data.get("usage", {})
                return (
                    content,
                    usage.get("prompt_tokens", 0),
                    usage.get("completion_tokens", 0),
                )
        except urllib.error.HTTPError as e:
            last_err = e
            if e.code in (429, 500, 502, 503, 504):
                # Honour Retry-After when Azure sends one; otherwise exp
                # backoff capped at 60s. 429s during the matrix run typically
                # last 30-60s — capping the delay keeps the run bounded.
                ra = e.headers.get("Retry-After") if e.headers else None
                delay = min(60, int(ra)) if ra and ra.isdigit() else min(
                    60, 2 ** attempt)
                print(f"  HTTP {e.code} — retry in {delay}s "
                      f"(attempt {attempt+1}/{max_retries})", file=sys.stderr)
                time.sleep(delay)
                continue
            raise
        except (urllib.error.URLError, TimeoutError) as e:
            last_err = e
            delay = 2 ** attempt
            print(f"  {type(e).__name__} — retry in {delay}s", file=sys.stderr)
            time.sleep(delay)
    raise RuntimeError(f"LLM call failed after {max_retries} attempts: {last_err}")


def parse_diag(content: str) -> dict:
    """Robust JSON extraction — the model is told to emit pure JSON but
    occasionally wraps it in a ```json fence; strip that defensively."""
    raw = content.strip()
    if raw.startswith("```"):
        raw = re.sub(r"^```(?:json)?", "", raw).strip()
        raw = re.sub(r"```$", "", raw).strip()
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        # Last-ditch: find the first {...} block
        m = re.search(r"\{.*\}", raw, re.DOTALL)
        if m:
            try:
                return json.loads(m.group(0))
            except json.JSONDecodeError:
                pass
        return {"_parse_error": True, "raw": content}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--matrix-dir",
                    default="experiments/failure-provenance/results-matrix")
    ap.add_argument("--out-csv",
                    default="experiments/failure-provenance/results-matrix/"
                            "results-b0.csv")
    ap.add_argument("--scenarios", default="F1,F2,F3,F4,F5,F6,F7,F8,F9,F10")
    ap.add_argument("--reps", default="r1,r2,r3,r4,r5,r6,r7,r8,r9,r10")
    ap.add_argument("--model", default=os.environ.get("OPENAI_MODEL",
                    "gpt-4o-mini-2024-07-18"))
    ap.add_argument("--force", action="store_true",
                    help="re-run even if b0-diag.json already exists")
    args = ap.parse_args()

    api_url = os.environ.get("AI_API_URL", "")
    api_key = os.environ.get("OPENAI_API_KEY", "") or os.environ.get(
        "AI_API_KEY", "")
    if not api_url or not api_key:
        print("ERROR: set AI_API_URL and OPENAI_API_KEY", file=sys.stderr)
        return 2

    matrix_dir = pathlib.Path(args.matrix_dir)
    out_csv = pathlib.Path(args.out_csv)
    out_csv.parent.mkdir(parents=True, exist_ok=True)

    scenarios = args.scenarios.split(",")
    reps = args.reps.split(",")

    rows: list[dict] = []
    n_skipped = 0
    n_called = 0
    n_correct = 0
    n_aligned = 0
    n_cat = 0
    total_in = 0
    total_out = 0

    for scen in scenarios:
        gt = GROUND_TRUTH[scen]
        for rep in reps:
            run_id = f"{scen}-{rep}-B0"
            artifacts = matrix_dir / scen / rep / "artifacts"
            out_json = matrix_dir / scen / rep / "b0-diag.json"

            if not (artifacts / "events.txt").exists():
                print(f"[skip] {run_id}: no events.txt in {artifacts}")
                n_skipped += 1
                continue

            if out_json.exists() and not args.force:
                try:
                    record = json.loads(out_json.read_text())
                    diag = record.get("diagnosis", {})
                    bundle_chars = record.get("bundle_chars", 0)
                    p_tok = record.get("prompt_tokens", 0)
                    c_tok = record.get("completion_tokens", 0)
                    print(f"[cached] {run_id}: {diag.get('component', '?')}")
                except Exception:  # noqa: BLE001
                    out_json.unlink(missing_ok=True)
                    continue
            else:
                bundle = build_kubectl_bundle(artifacts)
                user_msg = USER_PROMPT_HEADER + bundle
                bundle_chars = len(bundle)
                try:
                    content, p_tok, c_tok = call_llm(
                        api_url, api_key, args.model,
                        SYSTEM_PROMPT, user_msg,
                    )
                except Exception as e:  # noqa: BLE001
                    print(f"[error] {run_id}: {e}", file=sys.stderr)
                    n_skipped += 1
                    continue
                diag = parse_diag(content)
                record = {
                    "run_id": run_id,
                    "scenario": scen,
                    "rep": rep,
                    "engine": "B0",
                    "model": args.model,
                    "called_at_utc": datetime.now(timezone.utc).isoformat(
                        timespec="seconds"),
                    "bundle_chars": bundle_chars,
                    "prompt_tokens": p_tok,
                    "completion_tokens": c_tok,
                    "system_prompt": SYSTEM_PROMPT,
                    "diagnosis": diag,
                    "raw_response": content,
                }
                out_json.write_text(json.dumps(record, indent=2) + "\n")
                n_called += 1
                print(f"[done] {run_id}: {diag.get('component', '?')}  "
                      f"(tok={p_tok}+{c_tok})")

            comp = (diag.get("component") or "").strip()
            cat = (diag.get("category") or "").strip().lower()
            strict_comp = "1" if normalise(comp) == normalise(gt["component"]) else "0"
            aligned = "1" if aligned_match(scen, comp) else "0"
            cat_ok = "1" if cat == gt["category"].lower() else "0"
            if strict_comp == "1":
                n_correct += 1
            if aligned == "1":
                n_aligned += 1
            if cat_ok == "1":
                n_cat += 1
            total_in += p_tok
            total_out += c_tok

            rows.append({
                "run_id": run_id,
                "scenario_id": scen,
                "rep": rep,
                "configuration": "B0",
                "engine": "B0",
                "engine_mode": "B0",
                "model": args.model,
                "diag_component": comp,
                "diag_category": cat,
                "gt_component": gt["component"],
                "gt_category": gt["category"],
                "top1_correct": strict_comp,
                "top1_aligned": aligned,
                "category_correct": cat_ok,
                "bundle_chars": bundle_chars,
                "prompt_tokens": p_tok,
                "completion_tokens": c_tok,
            })

    fields = ["run_id", "scenario_id", "rep", "configuration", "engine",
              "engine_mode", "model", "diag_component", "diag_category",
              "gt_component", "gt_category", "top1_correct", "top1_aligned",
              "category_correct", "bundle_chars", "prompt_tokens",
              "completion_tokens"]
    with out_csv.open("w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=fields)
        w.writeheader()
        w.writerows(rows)

    n_total = len(rows)
    print()
    print(f"=== B0 baseline summary ===")
    print(f"calls made:      {n_called}  (cached: {n_total - n_called - n_skipped})")
    print(f"skipped:         {n_skipped}")
    print(f"scored:          n = {n_total}")
    if n_total:
        print(f"strict top-1:    {n_correct}/{n_total} "
              f"({100*n_correct/n_total:.1f}%)")
        print(f"aligned top-1:   {n_aligned}/{n_total} "
              f"({100*n_aligned/n_total:.1f}%)")
        print(f"category top-1:  {n_cat}/{n_total} "
              f"({100*n_cat/n_total:.1f}%)")
    print(f"tokens used:     {total_in} in + {total_out} out")
    cost = total_in / 1e6 * 0.15 + total_out / 1e6 * 0.60
    print(f"approx cost:     USD {cost:.4f}  (gpt-4o-mini Azure pricing)")
    print(f"CSV written:     {out_csv}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
