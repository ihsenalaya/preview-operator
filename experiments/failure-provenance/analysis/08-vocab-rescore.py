#!/usr/bin/env python3
"""08-vocab-rescore.py — re-score top-1 with aligned component vocabulary.

The strict matcher in fp-score requires the diagnosis to emit *exactly* the
ground-truth component label from `scenarios.yaml` (e.g. `migration-job`,
`backend`, `frontend`, `service`, `seed-job`, `test-suite`). LLMs reliably
emit synonyms instead — `application`, `API`, `database migration`, etc. —
which the strict matcher returns 0 on.

This re-score reads every per-rep diagnosis JSON, applies the per-scenario
alias table below, and emits a CSV with a new `top1_aligned` column. The
original `top1_correct` is preserved so the paper can report both numbers
side by side ("strict matcher" vs "vocabulary-aligned matcher").

Alias table is auditable and explicitly per-scenario — no global "anything
goes" merging that would inflate accuracy.

Output: `docs/research/failure-provenance/analysis-output/08-vocab-rescore/`
- results-rescored.csv (all rows, augmented with top1_aligned column)
- diff-table.md (per-scenario × engine: strict top-1 vs aligned top-1)
"""
from __future__ import annotations

import argparse
import csv
import json
import math
import pathlib
import re
import sys


# Per-scenario alias table. Each scenario maps to a regex set that, if any
# matches the LOWER-CASED diagnosis component, is accepted as the
# ground-truth component. Keep aliases narrow and per-scenario, not global,
# so the rescoring stays defensible: "we credit the diagnosis when its
# component label is a documented synonym of the ground truth, scoped to
# the scenario whose semantics make that synonym applicable".
ALIASES: dict[str, dict[str, list[str]]] = {
    "F1": {
        "migration-job": [
            r"^migration-job$",
            r"^migration$",
            r"^database migration$",
            r"^db migration$",
            r"^postgres-migrate$",
            r"^alembic$",
            r"^database$",  # F1's only DB-side component
        ],
    },
    "F2": {
        "app-deployment": [
            r"^app-deployment$",
            r"^app$",
            r"^backend$",
            r"^svc-backend$",
            r"^application$",
            r"^deployment$",
        ],
    },
    "F3": {
        "app-deployment": [
            r"^app-deployment$",
            r"^app$",
            r"^backend$",
            r"^svc-backend$",
            r"^application$",
            r"^deployment$",
            r"^image$",
            r"^image-pull$",
        ],
    },
    "F4": {
        "backend": [
            r"^backend$",
            r"^svc-backend$",
            r"^api$",
            r"^api-endpoint$",
            r"^application$",
            r"^route$",
            r"^app\.py$",
        ],
    },
    "F5": {
        "frontend": [
            r"^frontend$",
            r"^svc-frontend$",
            r"^ui$",
            r"^catalogue$",
            r"^catalog$",
            r"^html$",
            r"^frontend\.py$",
        ],
    },
    "F6": {
        "app-deployment": [
            r"^app-deployment$",
            r"^app$",
            r"^backend$",
            r"^svc-backend$",
            r"^application$",
            r"^database connection$",
            r"^db readiness$",
        ],
    },
    "F7": {
        "service": [
            r"^service$",
            r"^svc-backend$",
            r"^backend$",  # selector points to backend service
            r"^selector$",
            r"^routing$",
            r"^endpoint$",
            r"^endpoints$",
            r"^networking$",
        ],
    },
    "F8": {
        "backend": [
            r"^backend$",
            r"^svc-backend$",
            r"^api$",
            r"^application$",
            r"^latency$",
            r"^observability$",
            r"^app\.py$",
        ],
    },
    "F9": {
        "seed-job": [
            r"^seed-job$",
            r"^seed$",
            r"^seeding$",
            r"^ai-seed$",
            r"^data$",
            r"^test data$",
            r"^regression$",  # F9 fails the regression suite
        ],
    },
    "F10": {
        "test-suite": [
            r"^test-suite$",
            r"^test suite$",
            r"^tests$",
            r"^test$",
            r"^test-reliability$",
            r"^flaky test$",
            r"^flaky$",
            r"^regression$",  # F10 also surfaces in regression
        ],
    },
}


def normalise(s: str) -> str:
    """Strip punctuation, collapse whitespace, lowercase."""
    if not s:
        return ""
    out = s.lower().strip()
    out = re.sub(r"[^a-z0-9\- ]", "", out)
    out = re.sub(r"\s+", " ", out)
    return out


def aligned_match(scenario: str, diag_component: str) -> bool:
    """True if `diag_component` matches the scenario's aligned vocabulary."""
    cfg = ALIASES.get(scenario, {})
    target = normalise(diag_component)
    if not target:
        return False
    for patterns in cfg.values():
        for pat in patterns:
            if re.match(pat, target):
                return True
    return False


def parse_run_id(run_id: str) -> tuple[str, str, str, str, str]:
    """`F4-r3-C5-llm-grounded` → (F4, r3, C5, llm, grounded)."""
    parts = run_id.split("-")
    if len(parts) < 5:
        return ("", "", "", "", "")
    return (parts[0], parts[1], parts[2], parts[-2], parts[-1])


def diag_path(matrix_dir: pathlib.Path, run_id: str) -> pathlib.Path:
    """Resolve the per-rep diag JSON for a CSV run_id."""
    scen, rep, conf, engine, mode = parse_run_id(run_id)
    return matrix_dir / scen / rep / f"diag-{conf}-{engine}-{mode}.json"


def wilson_ci(s: int, n: int, z: float = 1.96) -> tuple[float, float]:
    if n == 0:
        return (0.0, 0.0)
    p = s / n
    denom = 1.0 + z * z / n
    centre = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return (max(0.0, centre - half), min(1.0, centre + half))


def fmt_pct(p: float) -> str:
    return f"{100*p:5.1f}%"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--in",
        dest="in_path",
        default="results-matrix/results-augmented.csv",
    )
    ap.add_argument("--matrix-dir", default="results-matrix")
    ap.add_argument(
        "--out-dir",
        default="docs/research/failure-provenance/analysis-output/08-vocab-rescore",
    )
    args = ap.parse_args()

    in_p = pathlib.Path(args.in_path)
    matrix_dir = pathlib.Path(args.matrix_dir)
    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    rows = list(csv.DictReader(in_p.open()))
    print(f"loaded {len(rows)} rows")

    # Re-score.
    out_csv = out_dir / "results-rescored.csv"
    n_aligned_new = 0
    with out_csv.open("w", newline="") as fh:
        fields = list(rows[0].keys()) + ["top1_aligned", "diag_component"]
        w = csv.DictWriter(fh, fieldnames=fields)
        w.writeheader()
        for row in rows:
            scen = row["scenario_id"]
            run_id = row["run_id"]
            dp = diag_path(matrix_dir, run_id)
            comp = ""
            aligned = "0"
            if dp.exists():
                try:
                    j = json.loads(dp.read_text())
                    comp = j.get("diagnosis", {}).get("component", "")
                except Exception:  # noqa: BLE001
                    pass
            if comp and aligned_match(scen, comp):
                aligned = "1"
                if row.get("top1_correct") != "1":
                    n_aligned_new += 1
            row["top1_aligned"] = aligned
            row["diag_component"] = comp
            w.writerow(row)
    print(f"wrote {out_csv}")
    print(f"vocabulary alignment newly credited: {n_aligned_new} rows")

    # Diff table per (scenario × engine_mode).
    cells_strict: dict[tuple[str, str], int] = {}
    cells_aligned: dict[tuple[str, str], int] = {}
    cells_n: dict[tuple[str, str], int] = {}
    for row in csv.DictReader(out_csv.open()):
        scen = row["scenario_id"]
        engine = "-".join(row["run_id"].split("-")[-2:])
        key = (scen, engine)
        cells_n[key] = cells_n.get(key, 0) + 1
        if row.get("top1_correct") == "1":
            cells_strict[key] = cells_strict.get(key, 0) + 1
        if row.get("top1_aligned") == "1":
            cells_aligned[key] = cells_aligned.get(key, 0) + 1

    lines = [
        "# Vocabulary-aligned re-score — strict vs aligned top-1",
        "",
        "Per (scenario × engine) cell, 50 reps pooled across C1–C5 (LLM-A only).",
        "",
        "| Scenario | Engine | n | strict top-1 | aligned top-1 | gain |",
        "|---|---|---|---|---|---|",
    ]
    scenarios = sorted({k[0] for k in cells_n}, key=lambda s: int(s[1:]))
    for scen in scenarios:
        for engine in ("rule-grounded", "llm-grounded", "llm-freeform"):
            key = (scen, engine)
            n = cells_n.get(key, 0)
            if n == 0:
                continue
            s = cells_strict.get(key, 0)
            a = cells_aligned.get(key, 0)
            ps = s / n
            pa = a / n
            lines.append(
                f"| {scen} | {engine} | {n} | "
                f"{fmt_pct(ps)} ({s}/{n}) | "
                f"{fmt_pct(pa)} ({a}/{n}) | "
                f"+{a-s} |"
            )
    (out_dir / "diff-table.md").write_text("\n".join(lines) + "\n")
    print(f"wrote {out_dir / 'diff-table.md'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
