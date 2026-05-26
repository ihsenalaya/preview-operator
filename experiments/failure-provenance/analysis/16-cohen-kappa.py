#!/usr/bin/env python3
"""16-cohen-kappa.py — generate the Cohen κ inter-annotator subsample
and compute κ (a) using a third LLM (Mistral-Large-3) as proxy
annotator now, and (b) leaving slots for a human annotator to fill in
later.

The methodology requires κ for diagnosis correctness on a 20 %
shuffled subsample, drawn from the 1500-cell matrix without
replacement and *blinded* (scenario id stripped from the prompt the
annotator sees) so the annotator must judge the diagnosis on its own
evidential merits rather than against the ground truth.

Two outputs:
  1. `subsample-blinded.csv` — 300 rows (random 20 % of 1500) with
     unique `case_id`, the masked evidence summary, the diagnosis text,
     and an empty `correct_human` column. Reproducible via the seeded
     RNG.
  2. `proxy-annotation.csv` — same rows but with the proxy annotator's
     judgement filled in, plus a computed Cohen κ between the proxy
     and the operator's aligned-matcher (the article's reference label).

The proxy annotator is **Mistral-Large-3** (Mistral AI family, third
LLM, distinct from LLM-A=OpenAI and LLM-B=Meta). It is *not* a
substitute for a human annotator — it is a sensitivity check that the
ground-truth label is not an artefact of the operator's matcher. The
human κ remains the canonical figure and is the focus of `task #28`.

Usage:
  # generate the blinded subsample (no LLM calls)
  python3 16-cohen-kappa.py --emit-subsample

  # run the Mistral proxy annotator
  python3 16-cohen-kappa.py --proxy

  # compute κ between two labelled columns
  python3 16-cohen-kappa.py --kappa --col-a correct_proxy \\
      --col-b correct_aligned
"""
from __future__ import annotations

import argparse
import csv
import json
import math
import os
import pathlib
import random
import re
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone

import yaml


SAMPLE_FRACTION = 0.20
RNG_SEED = 20260523
OUT_DIR = pathlib.Path(
    "docs/research/failure-provenance/analysis-output/16-cohen-kappa")


PROXY_SYSTEM_PROMPT = (
    "You are an SRE rating whether a Kubernetes preview-environment "
    "diagnosis is correct. You see a masked evidence summary (with the "
    "scenario id and ground-truth labels stripped) and a single proposed "
    "diagnosis. Reply with ONLY a JSON object of the form:\n\n"
    "{\n"
    '  "correct": true | false,\n'
    '  "reason": "<one short sentence>"\n'
    "}\n\n"
    "A diagnosis is correct when the named component is the actual "
    "failing component the evidence supports (allowing reasonable "
    "synonyms — e.g. `postgres-migrate` and `migration-job` both refer "
    "to the same thing). It is incorrect when it blames the wrong "
    "component (e.g. the test runner instead of the upstream service)."
)


def mask_evidence(items: list[dict]) -> str:
    """Build a redacted text summary the annotator can read without
    leaking the scenario id."""
    lines = []
    for it in items[:40]:  # cap context length
        msg = (it.get("message") or "").replace("\n", " ")
        if len(msg) > 220:
            msg = msg[:220] + "…"
        # Strip the ground-truth-bearing namespace prefix
        resource = str(it.get("resource", "")).replace(
            "preview-pr-", "preview-pr-XXXX/")
        lines.append(
            f"- type={it.get('type', '?')} resource={resource} msg={msg!r}"
        )
    return "\n".join(lines)


def parse_run_id(run_id: str):
    p = run_id.split("-")
    return (p[0], p[1], p[2], f"{p[-2]}-{p[-1]}")


def load_rescored_rows(path: pathlib.Path) -> list[dict]:
    return list(csv.DictReader(path.open()))


def load_bundle(scen: str, rep: str, matrix_dir: pathlib.Path) -> list[dict]:
    p = matrix_dir / scen / rep / "artifacts" / "failurereport.yaml"
    try:
        d = yaml.safe_load(p.read_text()) or {}
    except FileNotFoundError:
        return []
    return d.get("status", {}).get("evidenceItems", [])


def emit_subsample(args) -> int:
    rows = load_rescored_rows(pathlib.Path(args.rescored_csv))
    rng = random.Random(RNG_SEED)
    k = int(len(rows) * SAMPLE_FRACTION)
    sample = rng.sample(rows, k)
    print(f"loaded {len(rows)} cells; sampling {k} ({SAMPLE_FRACTION:.0%})")

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    out = OUT_DIR / "subsample-blinded.csv"
    matrix_dir = pathlib.Path(args.matrix_dir)
    fields = [
        "case_id", "run_id_hidden", "masked_evidence_text",
        "proposed_component", "proposed_category", "proposed_cause",
        "operator_label_aligned",  # the matcher's call (ground-truth proxy)
        "correct_human",            # to be filled in
    ]
    with out.open("w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=fields)
        w.writeheader()
        for i, r in enumerate(sample):
            scen, rep, conf, eng = parse_run_id(r["run_id"])
            bundle = load_bundle(scen, rep, matrix_dir)
            # Filter to the recorded evidence level for fidelity
            if conf == "C1":
                bundle = [it for it in bundle
                          if it.get("type") in ("PodLog", "JobLog")]
            elif conf == "C2":
                bundle = [it for it in bundle if it.get("type") in
                          ("PodLog", "JobLog", "KubernetesEvent")]
            elif conf == "C3":
                bundle = [it for it in bundle if it.get("type") in
                          ("PodLog", "JobLog", "KubernetesEvent",
                           "TestResult")]
            masked = mask_evidence(bundle)
            w.writerow({
                "case_id": f"K{i+1:04d}",
                "run_id_hidden": r["run_id"],  # kept for joining, blinded
                "masked_evidence_text": masked,
                "proposed_component": r.get("diag_component", ""),
                "proposed_category": "",  # not in the rescored CSV
                "proposed_cause": "",     # not in the rescored CSV
                "operator_label_aligned": r.get("top1_aligned", "0"),
                "correct_human": "",
            })
    print(f"wrote {out}  ({k} rows)")
    return 0


def call_mistral(api_url: str, api_key: str, deployment: str,
                 system: str, user: str, max_retries: int = 6) -> dict:
    body = json.dumps({
        "messages": [
            {"role": "system", "content": system},
            {"role": "user",   "content": user},
        ],
        "temperature": 0,
        "max_tokens": 120,
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
            return {
                "content": data["choices"][0]["message"]["content"],
                "usage": data.get("usage", {}),
            }
        except urllib.error.HTTPError as e:
            last_err = e
            if e.code in (429, 500, 502, 503, 504):
                time.sleep(min(60, 2 ** attempt))
                continue
            raise
        except (urllib.error.URLError, TimeoutError) as e:
            last_err = e
            time.sleep(min(60, 2 ** attempt))
    raise RuntimeError(f"mistral call failed: {last_err}")


def parse_proxy(content: str) -> dict:
    raw = content.strip()
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
    return {"_parse_error": True, "_raw": content}


def proxy_annotate(args) -> int:
    api_key = (os.environ.get("OPENAI_API_KEY")
               or os.environ.get("AI_API_KEY") or "")
    if not api_key:
        print("ERROR: set OPENAI_API_KEY (Foundry)", file=sys.stderr)
        return 2

    inp = OUT_DIR / "subsample-blinded.csv"
    if not inp.exists():
        print(f"missing {inp} — run --emit-subsample first", file=sys.stderr)
        return 2
    rows = list(csv.DictReader(inp.open()))

    out_path = OUT_DIR / "proxy-annotation.csv"
    # Resume support: if some rows already annotated, keep them.
    done: dict[str, dict] = {}
    if out_path.exists() and not args.force:
        for r in csv.DictReader(out_path.open()):
            done[r["case_id"]] = r

    fields = list(rows[0].keys()) + [
        "correct_proxy", "proxy_reason", "proxy_raw"]
    n_called = 0
    n_cached = 0
    cost = 0.0
    out_rows: list[dict] = []

    for r in rows:
        case_id = r["case_id"]
        if case_id in done:
            n_cached += 1
            out_rows.append(done[case_id])
            continue
        user = (
            "Evidence (redacted):\n"
            f"{r['masked_evidence_text']}\n\n"
            "Proposed diagnosis:\n"
            f"  component:     {r['proposed_component']!r}\n\n"
            "Reply with the JSON verdict described in the system prompt."
        )
        try:
            res = call_mistral(args.azure_host, api_key,
                               args.deployment,
                               PROXY_SYSTEM_PROMPT, user)
        except Exception as e:  # noqa: BLE001
            print(f"[error] {case_id}: {e}", file=sys.stderr)
            continue
        v = parse_proxy(res["content"])
        r_out = dict(r)
        r_out["correct_proxy"] = (
            "1" if v.get("correct") is True else
            "0" if v.get("correct") is False else "")
        r_out["proxy_reason"] = (v.get("reason") or "")[:200]
        r_out["proxy_raw"] = res["content"][:400]
        out_rows.append(r_out)
        n_called += 1
        usage = res.get("usage", {})
        cost += usage.get("prompt_tokens", 0) * (2.0 / 1_000_000)
        cost += usage.get("completion_tokens", 0) * (6.0 / 1_000_000)
        if n_called % 25 == 0:
            print(f"  {n_called}/{len(rows) - n_cached} (cost ~"
                  f"USD {cost:.3f})", file=sys.stderr)

    with out_path.open("w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=fields)
        w.writeheader()
        w.writerows(out_rows)
    print(f"wrote {out_path}: {n_called} new + {n_cached} cached "
          f"(approx cost USD {cost:.2f})")
    return 0


def cohen_kappa(labels_a: list[int], labels_b: list[int]) -> float:
    """Cohen's κ for two binary annotators on aligned positions."""
    if len(labels_a) != len(labels_b) or not labels_a:
        return float("nan")
    n = len(labels_a)
    agree = sum(1 for x, y in zip(labels_a, labels_b) if x == y)
    p_o = agree / n
    p_a1 = sum(labels_a) / n
    p_b1 = sum(labels_b) / n
    p_e = p_a1 * p_b1 + (1 - p_a1) * (1 - p_b1)
    if abs(1 - p_e) < 1e-12:
        return float("nan")
    return (p_o - p_e) / (1 - p_e)


def kappa_report(args) -> int:
    p = OUT_DIR / "proxy-annotation.csv"
    if not p.exists():
        print(f"missing {p}", file=sys.stderr)
        return 2
    rows = list(csv.DictReader(p.open()))
    A = [int(r[args.col_a]) for r in rows
         if r.get(args.col_a) in ("0", "1")
         and r.get(args.col_b) in ("0", "1")]
    B = [int(r[args.col_b]) for r in rows
         if r.get(args.col_a) in ("0", "1")
         and r.get(args.col_b) in ("0", "1")]
    k = cohen_kappa(A, B)
    print(f"Cohen κ ({args.col_a} vs {args.col_b}, n = {len(A)}): "
          f"{k:.3f}")
    # Magnitude interpretation (Landis & Koch 1977)
    mag = ("poor" if k < 0 else "slight" if k < 0.2 else
           "fair" if k < 0.4 else "moderate" if k < 0.6 else
           "substantial" if k < 0.8 else "almost perfect")
    print(f"  Landis & Koch 1977 magnitude: {mag}")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--matrix-dir",
        default="experiments/failure-provenance/results-matrix")
    ap.add_argument("--rescored-csv",
        default="docs/research/failure-provenance/analysis-output/"
                "08-vocab-rescore/results-rescored.csv")
    ap.add_argument("--azure-host",
        default="https://fp-foundry-133641.cognitiveservices.azure.com")
    ap.add_argument("--deployment", default="mistral-large-3")
    ap.add_argument("--emit-subsample", action="store_true")
    ap.add_argument("--proxy", action="store_true")
    ap.add_argument("--kappa", action="store_true")
    ap.add_argument("--col-a", default="correct_proxy")
    ap.add_argument("--col-b", default="operator_label_aligned")
    ap.add_argument("--force", action="store_true")
    args = ap.parse_args()
    if args.emit_subsample:
        return emit_subsample(args)
    if args.proxy:
        return proxy_annotate(args)
    if args.kappa:
        return kappa_report(args)
    ap.print_help()
    return 0


if __name__ == "__main__":
    sys.exit(main())
