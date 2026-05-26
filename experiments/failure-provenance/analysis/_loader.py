"""_loader.py — unified long-format loader joining results-augmented.csv (S1
rule + LLM-A) with results-llmb.csv (S1 LLM-B), and (when present) the
multi-app diag files. Produces a clean pandas DataFrame with one row per
(subject × scenario × rep × C × engine_mode × llm) cell.

Columns:
  subject, scenario, rep, configuration (C1..C5), engine (rule|llm),
  mode (grounded|freeform), llm (A|B|rule), correct (0|1), aligned (0|1),
  category_correct (0|1, where available)
"""
from __future__ import annotations
import json
import pathlib
import re
from typing import Iterable

import pandas as pd

ROOT = pathlib.Path(__file__).resolve().parents[1]


def _parse_run_id(run_id: str) -> tuple[str, str, str, str, str]:
    """run_id like 'F1-r1-C1-llm-grounded-llmb' → (scenario, rep, C, engine_mode, llm_tag)."""
    m = re.match(r"(F\d+)-(r\d+)-(C\d)-(rule|llm)-(grounded|freeform)(?:-(llmb))?$", run_id or "")
    if not m:
        return ("", "", "", "", "")
    scenario, rep, C, engine, mode = m.groups()[:5]
    llmb = m.group(6) or ""
    llm = "B" if llmb == "llmb" else ("A" if engine == "llm" else "rule")
    em = f"{engine}-{mode}"
    return scenario, rep, C, em, llm


def load_unified() -> pd.DataFrame:
    rows = []
    # 1) S1 augmented (1500 rows: rule + LLM-A grounded + LLM-A freeform across C1-C5)
    aug = ROOT / "results-matrix" / "results-augmented.csv"
    if aug.is_file():
        df = pd.read_csv(aug)
        for _, r in df.iterrows():
            scenario, rep, C, em, llm = _parse_run_id(str(r.get("run_id", "")))
            engine, _, mode = em.partition("-")
            rows.append({
                "subject": "s1-flask-catalog",
                "scenario": scenario or str(r.get("scenario_id", "")),
                "rep": rep,
                "configuration": C or str(r.get("configuration", "")),
                "engine": engine,
                "mode": mode,
                "llm": llm,
                "correct": int(r.get("top1_correct", 0) or 0),
                "aligned": int(r.get("top1_correct", 0) or 0),
                "evidence_recall": float(r.get("evidence_recall", 0) or 0),
                "bundle_size_bytes": int(r.get("bundle_size_bytes", 0) or 0),
                "hallucination_rate": float(r.get("hallucination_rate", 0) or 0),
            })
    # 2) S1 LLM-B replay (1000 rows)
    llmb = ROOT / "results-matrix" / "results-llmb.csv"
    if llmb.is_file():
        df = pd.read_csv(llmb)
        for _, r in df.iterrows():
            scenario, rep, C, em, llm = _parse_run_id(str(r.get("run_id", "")))
            engine, _, mode = em.partition("-")
            rows.append({
                "subject": "s1-flask-catalog",
                "scenario": scenario or str(r.get("scenario_id", "")),
                "rep": rep,
                "configuration": C or str(r.get("configuration", "")),
                "engine": engine,
                "mode": mode,
                "llm": llm,
                "correct": int(r.get("top1_correct", 0) or 0),
                "aligned": int(r.get("top1_aligned", 0) or 0),
                "evidence_recall": 0.0,
                "bundle_size_bytes": 0,
                "hallucination_rate": 0.0,
            })
    # 3) Multi-app rescore (2000 rows: LLM-A + LLM-B on S2-S5)
    ma = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/17-multiapp-rescore/per-subject-results.csv"
    if ma.is_file():
        df = pd.read_csv(ma)
        for _, r in df.iterrows():
            em = str(r.get("engine_mode", ""))
            # engine_mode formats: llm-grounded, llm-b-grounded, rule-grounded
            if em.startswith("llm-b-"):
                engine = "llm"; mode = em[len("llm-b-"):]; llm = "B"
            elif em.startswith("llm-"):
                engine = "llm"; mode = em[len("llm-"):]; llm = "A"
            elif em.startswith("rule-"):
                engine = "rule"; mode = em[len("rule-"):]; llm = "rule"
            else:
                continue
            rows.append({
                "subject": str(r.get("subject_id", "")),
                "scenario": str(r.get("scenario_id", "")),
                "rep": str(r.get("rep", "")),
                "configuration": str(r.get("configuration", "")),
                "engine": engine,
                "mode": mode,
                "llm": llm,
                "correct": int(r.get("top1_aligned", 0) or 0),
                "aligned": int(r.get("top1_aligned", 0) or 0),
                "evidence_recall": 0.0,
                "bundle_size_bytes": 0,
                "hallucination_rate": 0.0,
            })
    return pd.DataFrame(rows)
