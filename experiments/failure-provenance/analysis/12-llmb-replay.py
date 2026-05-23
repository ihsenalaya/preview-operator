#!/usr/bin/env python3
"""12-llmb-replay.py — replay the LLM engine on the frozen Phase 5a
evidence bundles using LLM-B = `cohere-command-a` (Cohere family,
Azure AI Foundry, GlobalStandard, temperature 0).

Pre-registration substitution chain:
  1. The locked plan named LLM-B as `meta-llama/Llama-3.3-70B-Instruct-Turbo`
     via Together.ai. No Together.ai key was available.
  2. Llama-3.3-70B-Instruct was deployed on Azure AI Foundry instead
     (`fp-foundry-133641` AIServices account). 472 / 1000 cells completed
     before Microsoft's "us" callable-pool went out of capacity — every
     remaining call returned HTTP 404 `no_callers` for hours; capacity did
     not return within the experiment window.
  3. Llama-3.1-70B-Instruct: deprecated since 2025-06-30, capacity 1.
     Cohere-command-r-plus: also deprecated.
  4. Final: `cohere-command-a` (Cohere Command A, v1, GlobalStandard 20K
     TPM, lifecycle ~2099). Cohere is a third family distinct from both
     LLM-A (OpenAI gpt-4o-mini) and the F10 hallucination judge
     (Mistral-Large-3) — the cross-family sensitivity check the protocol
     requires is preserved.

All substitutions and outage timestamps are documented in
`LOCKED-PLAN.md §3` and `EVALUATION-DRAFT.md §7`. Llama remains future
work pending capacity restoration.

Reusing `fp-diagnose` keeps the prompt, JSON schema, and response parser
bit-for-bit identical to what the operator ran in production; only
`--model` and `--ai-base-url` change between LLM-A and LLM-B.

Inputs:
  results-matrix/{F}/r*/artifacts/failurereport.yaml   frozen bundles

Output:
  results-matrix/{F}/r*/diag-{C}-llm-{grounded,freeform}-llmb.json
  results-matrix/results-llmb.csv

Scoring is done at the end with the same alias matcher
(`08-vocab-rescore.py`); the comparison report is produced separately
(`13-llmb-report.py`).

Cost model: Azure Foundry Cohere Command A GlobalStandard — USD 2.50/1M
input, USD 10/1M output tokens. ~6-15 K input + ~50 output per call
≈ USD 0.025 per call; the full 1000-cell replay (10×10×5×2) projects to
~USD 25. The script prints the actual running cost as it goes.
"""
from __future__ import annotations

import argparse
import csv
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone

import yaml


SCENARIOS_DEFAULT = "F1,F2,F3,F4,F5,F6,F7,F8,F9,F10"
REPS_DEFAULT = "r1,r2,r3,r4,r5,r6,r7,r8,r9,r10"
LEVELS_DEFAULT = "C1,C2,C3,C4,C5"
MODES_DEFAULT = "grounded,freeform"

# Azure Foundry Cohere Command A GlobalStandard pricing, USD per 1M tokens.
PRICE_IN = 2.50 / 1_000_000
PRICE_OUT = 10.00 / 1_000_000


# Vocabulary aliases — mirrors 08-vocab-rescore.py.
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

GROUND_TRUTH = {
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


def normalise(s: str) -> str:
    s = (s or "").lower().strip()
    s = re.sub(r"[^a-z0-9\- ]", "", s)
    return re.sub(r"\s+", " ", s)


def aligned_match(scenario: str, comp: str) -> bool:
    target = normalise(comp)
    if not target:
        return False
    for pat in ALIASES.get(scenario, []):
        if re.match(pat, target):
            return True
    return False


def yaml_to_json(yaml_path: pathlib.Path) -> str:
    """Write a tempfile with the YAML report transcoded to JSON.

    Returns the temp file path; the caller deletes it. fp-diagnose only
    accepts JSON input, and the operator's runtime stores the report
    as a Kubernetes object that round-trips through both formats.
    """
    d = yaml.safe_load(yaml_path.read_text())
    fd, name = tempfile.mkstemp(suffix=".json", prefix="fr-")
    with os.fdopen(fd, "w") as fh:
        json.dump(d, fh)
    return name


def call_fp_diagnose(binary: str, report_json: str, *, engine: str, mode: str,
                     level: str, model: str, api_url: str, api_key: str,
                     timeout: int = 180) -> tuple[dict, str]:
    """Run fp-diagnose; return (parsed_result, stderr)."""
    cmd = [
        binary,
        "--report", report_json,
        "--engine", engine,
        "--mode", mode,
        "--level", level,
        "--model", model,
        "--ai-base-url", api_url,
        "--ai-api-key", api_key,
    ]
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    if p.returncode != 0:
        raise RuntimeError(f"fp-diagnose exited {p.returncode}: "
                           f"{p.stderr.strip()}")
    try:
        return json.loads(p.stdout), p.stderr
    except json.JSONDecodeError as e:
        raise RuntimeError(f"fp-diagnose stdout is not JSON: {e}\n"
                           f"stdout[:300]={p.stdout[:300]}")


def estimate_tokens(text: str) -> int:
    """Rough token estimate (4 chars ≈ 1 token) — used for cost logging
    when the API response carries no usage block."""
    return max(1, len(text) // 4)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--matrix-dir",
        default="experiments/failure-provenance/results-matrix")
    ap.add_argument("--binary",
        default="./results-matrix/.bin/fp-diagnose")
    ap.add_argument("--scenarios", default=SCENARIOS_DEFAULT)
    ap.add_argument("--reps",      default=REPS_DEFAULT)
    ap.add_argument("--levels",    default=LEVELS_DEFAULT)
    ap.add_argument("--modes",     default=MODES_DEFAULT)
    ap.add_argument("--model",     default="cohere-command-a")
    ap.add_argument("--deployment", default="cohere-command-a",
        help="Azure OpenAI deployment name (used to build the base URL)")
    ap.add_argument("--azure-host",
        default="https://fp-foundry-133641.cognitiveservices.azure.com")
    ap.add_argument("--out-csv",
        default="experiments/failure-provenance/results-matrix/"
                "results-llmb.csv")
    ap.add_argument("--force", action="store_true",
        help="re-run cells that already have a diag-*-llmb.json file")
    ap.add_argument("--budget", type=float, default=35.0,
        help="abort if running cost exceeds this many USD")
    ap.add_argument("--sleep", type=float, default=0.0,
        help="sleep N seconds between calls (rate-limit safety valve)")
    args = ap.parse_args()

    api_key = (os.environ.get("OPENAI_API_KEY")
               or os.environ.get("AI_API_KEY") or "")
    if not api_key:
        print("ERROR: set OPENAI_API_KEY", file=sys.stderr)
        return 2

    matrix_dir = pathlib.Path(args.matrix_dir)
    api_url = f"{args.azure_host.rstrip('/')}/openai/deployments/{args.deployment}"

    cells: list[dict] = []
    n_called = n_cached = n_skipped = n_failed = 0
    cost = 0.0

    for scen in args.scenarios.split(","):
        gt = GROUND_TRUTH[scen]
        for rep in args.reps.split(","):
            report_yaml = matrix_dir / scen / rep / "artifacts" / \
                "failurereport.yaml"
            if not report_yaml.exists():
                n_skipped += 1
                continue
            json_path = yaml_to_json(report_yaml)
            try:
                for level in args.levels.split(","):
                    for mode in args.modes.split(","):
                        out = matrix_dir / scen / rep / \
                            f"diag-{level}-llm-{mode}-llmb.json"
                        if out.exists() and not args.force:
                            n_cached += 1
                            data = json.loads(out.read_text())
                            comp = (data.get("diagnosis", {})
                                    .get("component", "") or "")
                            cat = (data.get("diagnosis", {})
                                   .get("category", "") or "")
                            in_tok = data.get("_prompt_tokens", 0)
                            out_tok = data.get("_completion_tokens", 0)
                        else:
                            t0 = time.time()
                            try:
                                result, stderr = call_fp_diagnose(
                                    args.binary, json_path,
                                    engine="llm", mode=mode, level=level,
                                    model=args.model, api_url=api_url,
                                    api_key=api_key)
                            except Exception as e:  # noqa: BLE001
                                n_failed += 1
                                print(f"[fail] {scen}/{rep}/{level}/{mode}: "
                                      f"{e}", file=sys.stderr)
                                if args.sleep:
                                    time.sleep(args.sleep)
                                continue
                            dt = time.time() - t0
                            comp = (result.get("diagnosis", {})
                                    .get("component", "") or "")
                            cat = (result.get("diagnosis", {})
                                   .get("category", "") or "")
                            # fp-diagnose does not surface OpenAI usage; we
                            # approximate from the raw I/O sizes — good enough
                            # to track running cost within ~10 %.
                            raw_in = (result.get("evidenceLevel", "")
                                      + str(result.get("diagnosis", {}))
                                      + result.get("rawOutput", ""))
                            in_tok = estimate_tokens(report_yaml.read_text())
                            out_tok = estimate_tokens(result.get("rawOutput", ""))
                            result["_prompt_tokens"] = in_tok
                            result["_completion_tokens"] = out_tok
                            result["_called_at_utc"] = datetime.now(
                                timezone.utc).isoformat(timespec="seconds")
                            result["_model"] = args.model
                            result["_seconds"] = round(dt, 2)
                            out.write_text(json.dumps(result, indent=2) + "\n")
                            n_called += 1
                            print(f"[done] {scen}/{rep}/{level}/{mode}: "
                                  f"{comp!r}  ({dt:.1f}s)")
                            if args.sleep:
                                time.sleep(args.sleep)

                        cost += in_tok * PRICE_IN + out_tok * PRICE_OUT
                        cells.append({
                            "run_id": f"{scen}-{rep}-{level}-llm-{mode}-llmb",
                            "scenario_id": scen,
                            "rep": rep,
                            "configuration": level,
                            "engine_mode": f"llm-{mode}",
                            "model": args.model,
                            "diag_component": comp,
                            "diag_category": cat,
                            "gt_component": gt["component"],
                            "gt_category": gt["category"],
                            "top1_correct":
                                "1" if normalise(comp) == normalise(
                                    gt["component"]) else "0",
                            "top1_aligned":
                                "1" if aligned_match(scen, comp) else "0",
                            "category_correct":
                                "1" if (cat or "").lower() ==
                                    gt["category"].lower() else "0",
                            "prompt_tokens": in_tok,
                            "completion_tokens": out_tok,
                        })

                        if cost > args.budget:
                            print(f"\n[budget] {cost:.2f} USD exceeds "
                                  f"--budget {args.budget}, stopping",
                                  file=sys.stderr)
                            os.unlink(json_path)
                            _write_csv(args.out_csv, cells)
                            return 3
            finally:
                os.unlink(json_path)

    _write_csv(args.out_csv, cells)
    print()
    print(f"=== LLM-B replay summary ===")
    print(f"calls made:      {n_called}")
    print(f"cached:          {n_cached}")
    print(f"failed:          {n_failed}")
    print(f"reps skipped:    {n_skipped} (no failurereport.yaml)")
    print(f"approx cost:     USD {cost:.2f}  (gpt-4o GlobalStandard)")
    print(f"CSV written:     {args.out_csv}")
    return 0


def _write_csv(path: str, rows: list[dict]) -> None:
    if not rows:
        return
    fields = list(rows[0].keys())
    with pathlib.Path(path).open("w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=fields)
        w.writeheader()
        w.writerows(rows)


if __name__ == "__main__":
    sys.exit(main())
