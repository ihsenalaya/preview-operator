#!/usr/bin/env python3
"""00-augment-csv.py — add the 7 columns the execution-plan §5 requires.

Reads results-matrix/results.csv (the live scoring CSV, 1500 data rows) and
emits results-matrix/results-augmented.csv with the following columns added,
sourced from the locked Phase-5a metadata:

| Column                  | Source                                        |
|-------------------------|-----------------------------------------------|
| concurrency_label       | derived: bulk Phase-5a rows = `isolated`      |
|                         | (run-kind-experiments.sh runs scenarios       |
|                         | sequentially, one preview at a time)          |
| llm_model_version       | derived from run_id suffix: `rule-grounded`   |
|                         | → empty, `llm-grounded` / `llm-freeform`      |
|                         | → `gpt-4o-mini-2024-07-18`                    |
| llm_provider            | empty for rule, `api.openai.com` for llm-*    |
|                         | (Azure deployment route, see                  |
|                         | PHASE5A-FREEZE.md)                            |
| llm_temperature         | empty for rule, `0` for llm-*                 |
| operator_image_digest   | constant, the digest captured at freeze       |
| frozen_commit_sha       | constant, the commit at the freeze tag        |
| called_at_utc           | re-used from failure_detected_at column       |
|                         | (no per-call timestamp captured during the    |
|                         | live run; documented gap)                     |

This script is idempotent. It does NOT modify the existing CSV — it writes a
new file next to it.
"""
from __future__ import annotations

import argparse
import csv
import pathlib
import sys


# Phase-5a freeze constants (locked in PHASE5A-FREEZE.md).
OPERATOR_IMAGE_DIGEST = (
    "testagentdevops.azurecr.io/preview-operator"
    "@sha256:88cd767f41c07f0eb65d85ea0d95f543926d07839f373b099dbb1c1d591a3a30"
)
FROZEN_COMMIT_SHA = "2e81a648c3244c276dc3d58821a01e61d536901e"
LLM_A_MODEL = "gpt-4o-mini-2024-07-18"
LLM_A_PROVIDER = "api.openai.com"  # Azure-routed; logged here as the
# semantic source (the model identifier is OpenAI's, the routing is via Azure
# OpenAI endpoint — recorded under llm_provider for audit).

NEW_COLUMNS = [
    "concurrency_label",
    "llm_model_version",
    "llm_provider",
    "llm_temperature",
    "operator_image_digest",
    "frozen_commit_sha",
    "called_at_utc",
]


def engine_mode_from_run_id(run_id: str) -> tuple[str, str]:
    """Parse `F1-r3-C4-llm-grounded` → ('llm', 'grounded')."""
    parts = run_id.split("-")
    if len(parts) < 2:
        return ("", "")
    mode = parts[-1]
    engine = parts[-2]
    return (engine, mode)


def llm_fields_for(engine: str) -> tuple[str, str, str]:
    """Return (model, provider, temperature) for a row's engine."""
    if engine == "llm":
        return (LLM_A_MODEL, LLM_A_PROVIDER, "0")
    # rule-grounded rows have no LLM
    return ("", "", "")


def augment(in_path: pathlib.Path, out_path: pathlib.Path) -> int:
    with in_path.open() as fh_in, out_path.open("w", newline="") as fh_out:
        reader = csv.DictReader(fh_in)
        if not reader.fieldnames:
            print(f"empty CSV: {in_path}", file=sys.stderr)
            return 1
        out_fields = list(reader.fieldnames) + NEW_COLUMNS
        writer = csv.DictWriter(fh_out, fieldnames=out_fields)
        writer.writeheader()
        n = 0
        for row in reader:
            engine, _mode = engine_mode_from_run_id(row.get("run_id", ""))
            model, provider, temperature = llm_fields_for(engine)
            row["concurrency_label"] = "isolated"
            row["llm_model_version"] = model
            row["llm_provider"] = provider
            row["llm_temperature"] = temperature
            row["operator_image_digest"] = OPERATOR_IMAGE_DIGEST
            row["frozen_commit_sha"] = FROZEN_COMMIT_SHA
            row["called_at_utc"] = row.get("failure_detected_at", "")
            writer.writerow(row)
            n += 1
    print(f"augmented {n} rows -> {out_path}")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in",
        dest="in_path",
        default="results-matrix/results.csv",
        help="input CSV (live scoring output)",
    )
    ap.add_argument(
        "--out",
        dest="out_path",
        default="results-matrix/results-augmented.csv",
        help="output CSV with the 7 added columns",
    )
    args = ap.parse_args()
    in_p = pathlib.Path(args.in_path)
    out_p = pathlib.Path(args.out_path)
    if not in_p.exists():
        print(f"input not found: {in_p}", file=sys.stderr)
        return 2
    return augment(in_p, out_p)


if __name__ == "__main__":
    sys.exit(main())
