#!/usr/bin/env python3
"""15-f10-hallucination-judge.py — judge F10 (flaky-test negative control)
diagnoses for hallucination.

The article's RQ4 asks: when there is no actual fault — F10's failing test
is the canonical negative control, by construction — do the diagnostic
engines confabulate a cause? The strict answer requires an *independent*
arbiter: a model from a different LLM family than both LLM-A
(gpt-4o-mini, OpenAI) and LLM-B (Llama-3.3-70B, Meta).

`LOCKED-PLAN.md` named the judge as Claude Sonnet 4.5 (Anthropic). Claude
is not available on the user's Azure subscription (Anthropic isn't in
the supported model-format list — only AI21, Cohere, Meta, Microsoft,
Mistral AI, OpenAI, xAI). Substitution: **Mistral-Large-3** (Mistral AI),
deployed on the same `fp-foundry-133641` Azure AI Foundry resource.
This is a third LLM family (Mistral, distinct from both OpenAI and Meta),
preserving the cross-family independence the protocol requires. The
substitution is documented in `LOCKED-PLAN.md §3` and
`EVALUATION-DRAFT.md §5`.

For each rep × evidence-level × engine-mode of scenario F10:
  1. Read the per-rep evidence bundle (`failurereport.yaml`) filtered to
     the level (C1..C5 via the same allowlists as the operator).
  2. Read the diagnosis (`diag-{C}-{engine}-{mode}.json`).
  3. Ask Mistral: given this evidence and this diagnosis, is the
     diagnosis hallucinated (asserts a fault that the evidence does not
     support)?
  4. Capture the verdict, the rationale, and the cited evidence items.

Output:
  experiments/failure-provenance/results-matrix/F10/r*/judge-{C}-{engine}-{mode}.json
  experiments/failure-provenance/results-matrix/results-f10-judge.csv
  docs/research/failure-provenance/analysis-output/15-f10-judge/
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

import yaml


# Evidence-type allowlists per level — same as 11-rule-rescore.py.
LEVEL_TYPES: dict[str, set[str] | None] = {
    "C1": {"PodLog", "JobLog"},
    "C2": {"PodLog", "JobLog", "KubernetesEvent"},
    "C3": {"PodLog", "JobLog", "KubernetesEvent", "TestResult"},
    "C4": None,
    "C5": None,
}


JUDGE_SYSTEM_PROMPT = (
    "You are an independent arbiter assessing whether a Kubernetes "
    "preview-environment diagnosis is *hallucinated*. A diagnosis is "
    "hallucinated when its claim is not supported by the evidence "
    "provided: it names a cause (broken backend, missing config, image "
    "pull failure, …) that the evidence shows did not actually happen.\n\n"
    "F10 is a flaky-test scenario: the only thing that failed is a test "
    "that intentionally fails randomly (about 1 run in N). Any concrete "
    "infrastructure / application / configuration cause is therefore "
    "*hallucinated by construction* — the right answer is \"flaky\" / "
    "\"test-reliability\" / \"no underlying fault\".\n\n"
    "You return ONLY a JSON object (no markdown fences, no prose) of the "
    "form:\n"
    "{\n"
    '  "hallucinated": true | false,\n'
    '  "verdict": "<one short sentence>",\n'
    '  "evidence_supports_diagnosis": true | false\n'
    "}"
)


def yaml_load(p: pathlib.Path) -> dict:
    try:
        return yaml.safe_load(p.read_text()) or {}
    except FileNotFoundError:
        return {}


def filter_to_level(items: list[dict], level: str) -> list[dict]:
    allow = LEVEL_TYPES.get(level)
    if allow is None:
        return items
    return [it for it in items if it.get("type") in allow]


def build_user_prompt(bundle: list[dict], diagnosis: dict, scen: str,
                      level: str) -> str:
    # Compact the evidence for the LLM — keep IDs, types, resource,
    # 200-char message snippets.
    lines = [
        f"Scenario: {scen} (F10 — flaky-test negative control).",
        f"Evidence level: {level} "
        f"({'logs only' if level == 'C1' else 'logs + events' if level == 'C2' else 'logs + events + tests' if level == 'C3' else 'full bundle'}).",
        "",
        "Evidence items:",
    ]
    for it in bundle:
        msg = (it.get("message") or "").replace("\n", " ")
        if len(msg) > 200:
            msg = msg[:200] + "…"
        lines.append(
            f"  - id={it.get('id', '?')}  type={it.get('type', '?')}  "
            f"resource={it.get('resource', '?')}  msg={msg!r}"
        )
    lines += [
        "",
        "Diagnosis under review:",
        f"  component:     {diagnosis.get('component', '?')!r}",
        f"  category:      {diagnosis.get('category', '?')!r}",
        f"  probableCause: {diagnosis.get('probableCause', '?')!r}",
        f"  confidence:    {diagnosis.get('confidence', '?')!r}",
        f"  evidenceRefs:  {diagnosis.get('evidenceRefs', [])}",
        "",
        "Reply with the JSON verdict described in the system prompt.",
    ]
    return "\n".join(lines)


def call_judge(api_url: str, api_key: str, model: str, deployment: str,
               system: str, user: str, max_retries: int = 6) -> dict:
    body = json.dumps({
        "messages": [
            {"role": "system", "content": system},
            {"role": "user",   "content": user},
        ],
        "temperature": 0,
        "max_tokens": 200,
    }).encode()
    url = (f"{api_url.rstrip('/')}/openai/deployments/{deployment}"
           f"/chat/completions?api-version=2024-08-01-preview")
    last_err: Exception | None = None
    for attempt in range(max_retries):
        req = urllib.request.Request(
            url, data=body,
            headers={"Content-Type": "application/json",
                     "api-key": api_key}, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=120) as resp:
                data = json.loads(resp.read().decode())
            content = data["choices"][0]["message"]["content"]
            usage = data.get("usage", {})
            return {
                "raw": content,
                "model": data.get("model", model),
                "prompt_tokens": usage.get("prompt_tokens", 0),
                "completion_tokens": usage.get("completion_tokens", 0),
            }
        except urllib.error.HTTPError as e:
            last_err = e
            if e.code in (429, 500, 502, 503, 504):
                delay = min(60, 2 ** attempt)
                print(f"  HTTP {e.code} — retry in {delay}s",
                      file=sys.stderr)
                time.sleep(delay)
                continue
            raise
        except (urllib.error.URLError, TimeoutError) as e:
            last_err = e
            time.sleep(min(60, 2 ** attempt))
    raise RuntimeError(f"judge call failed: {last_err}")


def parse_verdict(text: str) -> dict:
    raw = text.strip()
    raw = re.sub(r"^```(?:json)?", "", raw).strip()
    raw = re.sub(r"```$", "", raw).strip()
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        m = re.search(r"\{[\s\S]*\}", raw)
        if m:
            try:
                return json.loads(m.group(0))
            except json.JSONDecodeError:
                pass
    return {"_parse_error": True, "_raw": text}


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--matrix-dir",
        default="experiments/failure-provenance/results-matrix")
    ap.add_argument("--out-csv",
        default="experiments/failure-provenance/results-matrix/"
                "results-f10-judge.csv")
    ap.add_argument("--reps", default="r1,r2,r3,r4,r5,r6,r7,r8,r9,r10")
    ap.add_argument("--levels", default="C1,C2,C3,C4,C5")
    ap.add_argument("--modes",
        default="rule-grounded,llm-grounded,llm-freeform")
    ap.add_argument("--model", default="Mistral-Large-3")
    ap.add_argument("--deployment", default="mistral-large-3")
    ap.add_argument("--azure-host",
        default="https://fp-foundry-133641.cognitiveservices.azure.com")
    ap.add_argument("--force", action="store_true")
    ap.add_argument("--include-llmb", action="store_true",
        help="also judge the LLM-B (Llama) replay diagnoses on F10")
    args = ap.parse_args()

    api_key = (os.environ.get("OPENAI_API_KEY")
               or os.environ.get("AI_API_KEY") or "")
    if not api_key:
        print("ERROR: set OPENAI_API_KEY (Foundry key)", file=sys.stderr)
        return 2

    matrix_dir = pathlib.Path(args.matrix_dir)
    rows: list[dict] = []
    n_called = 0

    for rep in args.reps.split(","):
        bundle_path = matrix_dir / "F10" / rep / "artifacts" \
            / "failurereport.yaml"
        full_items = yaml_load(bundle_path).get("status", {}).get(
            "evidenceItems", [])
        if not full_items:
            continue
        for level in args.levels.split(","):
            level_items = filter_to_level(full_items, level)
            for mode in args.modes.split(","):
                # LLM-A diagnoses
                diag_paths = [
                    ("LLM-A", matrix_dir / "F10" / rep
                     / f"diag-{level}-{mode}.json"),
                ]
                if args.include_llmb and mode != "rule-grounded":
                    diag_paths.append((
                        "LLM-B", matrix_dir / "F10" / rep
                        / f"diag-{level}-{mode}-llmb.json"))
                for llm_label, dp in diag_paths:
                    if not dp.exists():
                        continue
                    try:
                        diag_blob = json.loads(dp.read_text())
                    except json.JSONDecodeError:
                        continue
                    diag = diag_blob.get("diagnosis", {})
                    out = matrix_dir / "F10" / rep / \
                        f"judge-{level}-{mode}-{llm_label.lower()}.json"
                    if out.exists() and not args.force:
                        record = json.loads(out.read_text())
                    else:
                        user = build_user_prompt(level_items, diag, "F10",
                                                 level)
                        try:
                            r = call_judge(
                                args.azure_host, api_key,
                                args.model, args.deployment,
                                JUDGE_SYSTEM_PROMPT, user,
                            )
                        except Exception as e:  # noqa: BLE001
                            print(f"[error] {rep}/{level}/{mode}/{llm_label}: "
                                  f"{e}", file=sys.stderr)
                            continue
                        verdict = parse_verdict(r["raw"])
                        record = {
                            "scenario": "F10",
                            "rep": rep,
                            "level": level,
                            "mode": mode,
                            "judged_llm": llm_label,
                            "judge_model": r["model"],
                            "called_at_utc": datetime.now(
                                timezone.utc).isoformat(timespec="seconds"),
                            "raw": r["raw"],
                            "verdict": verdict,
                            "diagnosis_under_review": diag,
                            "prompt_tokens": r["prompt_tokens"],
                            "completion_tokens": r["completion_tokens"],
                        }
                        out.write_text(json.dumps(record, indent=2) + "\n")
                        n_called += 1
                        print(f"[done] F10/{rep}/{level}/{mode}/{llm_label}: "
                              f"hallucinated={verdict.get('hallucinated', '?')}")
                    v = record.get("verdict", {})
                    rows.append({
                        "scenario_id": "F10",
                        "rep": rep,
                        "configuration": level,
                        "engine_mode": mode,
                        "judged_llm": llm_label,
                        "hallucinated": (
                            "1" if v.get("hallucinated") is True else
                            "0" if v.get("hallucinated") is False else "?"),
                        "verdict_text": v.get("verdict", "")[:200],
                        "diag_component":
                            record["diagnosis_under_review"].get(
                                "component", ""),
                        "diag_category":
                            record["diagnosis_under_review"].get(
                                "category", ""),
                    })

    if rows:
        fields = list(rows[0].keys())
        pathlib.Path(args.out_csv).parent.mkdir(parents=True, exist_ok=True)
        with pathlib.Path(args.out_csv).open("w", newline="") as fh:
            w = csv.DictWriter(fh, fieldnames=fields)
            w.writeheader()
            w.writerows(rows)
        print(f"\nwrote {args.out_csv}")

        # Per-engine hallucination rate
        from collections import defaultdict
        by_eng: dict[str, list[int]] = defaultdict(list)
        for r in rows:
            if r["hallucinated"] in ("0", "1"):
                by_eng[(r["engine_mode"], r["judged_llm"])].append(
                    int(r["hallucinated"]))
        print("\nHallucination rate (F10 negative control) by engine × LLM:")
        for k, v in sorted(by_eng.items()):
            if v:
                print(f"  {k[0]} × {k[1]}: {sum(v)}/{len(v)} = "
                      f"{100*sum(v)/len(v):.1f}%")
    print(f"\nnew judge calls: {n_called}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
