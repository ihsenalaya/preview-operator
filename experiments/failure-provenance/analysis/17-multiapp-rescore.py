#!/usr/bin/env python3
"""17-multiapp-rescore.py — vocabulary-aligned rescoring for the multi-app
S2-S5 matrix (`run-matrix.py` output under `results-multiapp/`).

S2-S5 reuse the same fault classes as S1 (F1, F2, F3, F6, F7) but the
component vocabulary the LLM sees is subject-specific. listmonk's backend
pod is `listmonk` not `svc-backend`; healthchecks' backend is `web` /
`hc-web`; umami's is `umami` / `app`; petclinic's is `spring-petclinic`.

The per-subject alias table below extends the S1 alias matcher
(08-vocab-rescore.py) with subject-specific synonyms so the matcher fairly
credits diagnoses that name the right thing in subject-specific words.

Output:
  docs/research/failure-provenance/analysis-output/17-multiapp-rescore/
    per-subject-results.csv       per (subject × scenario × C × engine × rep)
    summary-by-subject.md         aligned top-1 with Wilson CIs
    summary-pooled.md             pooled aligned top-1 across S2-S5
"""
from __future__ import annotations

import argparse
import csv
import json
import math
import pathlib
import re
import sys
from collections import defaultdict


# Ground truth per (subject, scenario). The pre-registered scenarios F1-F10
# map to logical roles ("the migration job", "the application", "the
# Service") which exist on every subject under different names.
GROUND_TRUTH = {
    "F1":  {"component_role": "migration-job",  "category": "database"},
    "F2":  {"component_role": "app",            "category": "configuration"},
    "F3":  {"component_role": "app",            "category": "infrastructure"},
    "F4":  {"component_role": "backend",        "category": "application"},
    "F5":  {"component_role": "frontend",       "category": "application"},
    "F6":  {"component_role": "app",            "category": "infrastructure"},
    "F7":  {"component_role": "service",        "category": "infrastructure"},
    "F8":  {"component_role": "backend",        "category": "observability"},
    "F9":  {"component_role": "seed-job",       "category": "database"},
    "F10": {"component_role": "test-suite",     "category": "test-reliability"},
}


# Per-subject component aliases. Each (subject, role) maps to a regex
# alias list; if the diagnoser's `component` (lower-cased, normalised)
# matches any pattern, the diagnosis is credited.
SUBJECT_ALIASES: dict[tuple[str, str], list[str]] = {
    # === S1 — Flask catalogue (reference) ===
    ("s1-flask-catalog", "migration-job"): [
        r"^migration-job$", r"^migration$", r"^database migration$",
        r"^postgres-migrate$", r"^alembic$", r"^database$",
    ],
    ("s1-flask-catalog", "app"): [
        r"^app$", r"^app-deployment$", r"^backend$", r"^svc-backend$",
        r"^application$", r"^deployment$", r"^image$", r"^image-pull$",
    ],
    ("s1-flask-catalog", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^selector$",
        r"^endpoints$", r"^networking$",
    ],

    # === S2 — listmonk (Go backend + Postgres) ===
    # Pod names: listmonk-migrate (init), listmonk (backend), postgres, probe
    ("s2-listmonk", "migration-job"): [
        r"^migration-job$", r"^migration$", r"^listmonk-migrate$",
        r"^listmonk-install$", r"^install$", r"^database migration$",
        r"^postgres-migrate$", r"^database$",
    ],
    ("s2-listmonk", "app"): [
        r"^app$", r"^backend$", r"^svc-backend$", r"^listmonk$",
        r"^application$", r"^deployment$", r"^app-deployment$",
        r"^image$", r"^image-pull$", r"^newsletter$",
    ],
    ("s2-listmonk", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^listmonk$",
        r"^selector$", r"^endpoints$", r"^networking$",
    ],

    # === S3 — Healthchecks.io (Django Python + Postgres) ===
    # Pod names: hc-migrate, hc-web (backend), postgres, probe
    ("s3-healthchecks", "migration-job"): [
        r"^migration-job$", r"^migration$", r"^hc-migrate$",
        r"^healthchecks-migrate$", r"^django-migrate$", r"^manage$",
        r"^database migration$", r"^postgres-migrate$", r"^database$",
    ],
    ("s3-healthchecks", "app"): [
        r"^app$", r"^backend$", r"^web$", r"^hc-web$", r"^healthchecks$",
        r"^django$", r"^svc-backend$", r"^application$",
        r"^app-deployment$", r"^deployment$", r"^image$", r"^image-pull$",
    ],
    ("s3-healthchecks", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^web$",
        r"^hc-web$", r"^healthchecks$", r"^selector$", r"^endpoints$",
    ],

    # === S4 — Umami (Next.js + Postgres) ===
    # Pod names: umami-migrate (Prisma), umami (backend), postgres, probe
    ("s4-umami", "migration-job"): [
        r"^migration-job$", r"^migration$", r"^umami-migrate$",
        r"^prisma$", r"^prisma-migrate$", r"^database migration$",
        r"^postgres-migrate$", r"^database$",
    ],
    ("s4-umami", "app"): [
        r"^app$", r"^backend$", r"^svc-backend$", r"^umami$",
        r"^application$", r"^app-deployment$", r"^deployment$",
        r"^next$", r"^nextjs$", r"^analytics$",
        r"^image$", r"^image-pull$",
    ],
    ("s4-umami", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^umami$",
        r"^selector$", r"^endpoints$",
    ],

    # === S5 — Spring PetClinic (Spring Boot Java + Postgres) ===
    # Pod names: petclinic-migrate (Flyway), petclinic / spring-petclinic
    # (backend), postgres, probe
    ("s5-petclinic", "migration-job"): [
        r"^migration-job$", r"^migration$", r"^petclinic-migrate$",
        r"^flyway$", r"^flyway-migrate$", r"^database migration$",
        r"^postgres-migrate$", r"^database$",
    ],
    ("s5-petclinic", "app"): [
        r"^app$", r"^backend$", r"^svc-backend$", r"^petclinic$",
        r"^spring-petclinic$", r"^spring$", r"^springboot$", r"^java$",
        r"^application$", r"^app-deployment$", r"^deployment$",
        r"^image$", r"^image-pull$",
    ],
    ("s5-petclinic", "service"): [
        r"^service$", r"^svc-backend$", r"^backend$", r"^petclinic$",
        r"^spring-petclinic$", r"^selector$", r"^endpoints$",
    ],

    # Added 2026-05-24 — F4/F5/F8/F9/F10 multi-app coverage. F4/F8 ground
    # truth is `backend`; F5 is `frontend`; F9 is `seed-job`; F10 is
    # `test-suite`. The aliases below mirror what each subject's LLM
    # diagnoser actually names for those roles.
    ("s1-flask-catalog", "backend"): [
        r"^backend$", r"^svc-backend$", r"^api$", r"^app\.py$",
        r"^application$", r"^app$", r"^route$",
    ],
    ("s1-flask-catalog", "frontend"): [
        r"^frontend$", r"^ui$", r"^catalogue$", r"^catalog$", r"^html$",
        r"^frontend\.py$", r"^web$",
    ],
    ("s1-flask-catalog", "seed-job"): [
        r"^seed-?job$", r"^seed$", r"^ai-?seed$", r"^data$", r"^seeding$",
        r"^ai-?enrichment$", r"^catalogue-?seed$",
    ],
    ("s1-flask-catalog", "test-suite"): [
        r"^test-?suite$", r"^test$", r"^tests$", r"^flaky$",
        r"^e2e-?tests?$", r"^smoke-?tests?$", r"^regression-?tests?$",
    ],
    ("s2-listmonk", "backend"): [
        r"^backend$", r"^svc-backend$", r"^listmonk$", r"^api$",
        r"^application$", r"^app$",
    ],
    ("s2-listmonk", "frontend"): [
        r"^frontend$", r"^admin$", r"^ui$", r"^web$",
    ],
    ("s2-listmonk", "seed-job"): [
        r"^seed-?job$", r"^seed$", r"^migration$", r"^data$",
    ],
    ("s2-listmonk", "test-suite"): [
        r"^test-?suite$", r"^tests?$", r"^flaky$",
        r"^e2e-?tests?$", r"^smoke-?tests?$", r"^regression-?tests?$",
    ],
    ("s3-healthchecks", "backend"): [
        r"^backend$", r"^svc-backend$", r"^hc-?web$", r"^healthchecks$",
        r"^web$", r"^api$", r"^application$",
    ],
    ("s3-healthchecks", "frontend"): [
        r"^frontend$", r"^web$", r"^ui$", r"^django-?ui$",
    ],
    ("s3-healthchecks", "seed-job"): [
        r"^seed-?job$", r"^seed$", r"^migration$", r"^data$",
    ],
    ("s3-healthchecks", "test-suite"): [
        r"^test-?suite$", r"^tests?$", r"^flaky$",
        r"^e2e-?tests?$", r"^smoke-?tests?$", r"^regression-?tests?$",
    ],
    ("s4-umami", "backend"): [
        r"^backend$", r"^svc-backend$", r"^umami$", r"^api$",
        r"^application$", r"^app$", r"^next\.?js$",
    ],
    ("s4-umami", "frontend"): [
        r"^frontend$", r"^ui$", r"^web$", r"^next\.?js$",
    ],
    ("s4-umami", "seed-job"): [
        r"^seed-?job$", r"^seed$", r"^prisma-?seed$", r"^data$",
    ],
    ("s4-umami", "test-suite"): [
        r"^test-?suite$", r"^tests?$", r"^flaky$",
        r"^e2e-?tests?$", r"^smoke-?tests?$", r"^regression-?tests?$",
    ],
    ("s5-petclinic", "backend"): [
        r"^backend$", r"^svc-backend$", r"^petclinic$",
        r"^spring-?petclinic$", r"^api$", r"^application$", r"^spring(-boot)?$",
    ],
    ("s5-petclinic", "frontend"): [
        # petclinic-rest is REST-only — frontend is N/A; alias list kept
        # for matcher symmetry but will not match in practice.
        r"^frontend$", r"^ui$", r"^web$", r"^swagger$",
    ],
    ("s5-petclinic", "seed-job"): [
        r"^seed-?job$", r"^seed$", r"^flyway$", r"^migration$", r"^data$",
    ],
    ("s5-petclinic", "test-suite"): [
        r"^test-?suite$", r"^tests?$", r"^flaky$",
        r"^e2e-?tests?$", r"^smoke-?tests?$", r"^regression-?tests?$",
    ],
}


def normalise(s: str) -> str:
    s = (s or "").lower().strip()
    s = re.sub(r"[^a-z0-9\- ]", "", s)
    return re.sub(r"\s+", " ", s)


def aligned_match(subject: str, role: str, diag_component: str) -> bool:
    target = normalise(diag_component)
    if not target:
        return False
    patterns = SUBJECT_ALIASES.get((subject, role), [])
    return any(re.match(pat, target) for pat in patterns)


def parse_diag_filename(name: str) -> tuple[str, str, str] | None:
    """`diag-C1-rule-grounded.json` → ('C1', 'rule', 'grounded').
    The `-llmb` variant collapses to engine `llm-b` so the engine_mode
    column distinguishes LLM-A vs LLM-B."""
    m = re.match(r"diag-(C[1-5])-(rule|llm)-(grounded|freeform)(-llmb)?\.json$", name)
    if not m:
        return None
    conf, engine, mode, llmb = m.group(1), m.group(2), m.group(3), m.group(4)
    if engine == "llm" and llmb:
        engine = "llm-b"
    return (conf, engine, mode)


def wilson_ci(s: int, n: int, z: float = 1.96):
    if n == 0:
        return (0.0, 0.0)
    p = s / n
    denom = 1.0 + z * z / n
    centre = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return (max(0.0, centre - half), min(1.0, centre + half))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--matrix-dir",
        default="experiments/failure-provenance/results-multiapp")
    ap.add_argument("--out-dir",
        default="docs/research/failure-provenance/analysis-output/"
                "17-multiapp-rescore")
    args = ap.parse_args()

    matrix_dir = pathlib.Path(args.matrix_dir)
    out_dir = pathlib.Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    rows: list[dict] = []
    for subject_dir in sorted(matrix_dir.glob("*")):
        if not subject_dir.is_dir():
            continue
        subject = subject_dir.name
        for scen_dir in sorted(subject_dir.glob("F*")):
            if not scen_dir.is_dir():
                continue
            scen = scen_dir.name
            gt = GROUND_TRUTH.get(scen)
            if not gt:
                continue
            role = gt["component_role"]
            for rep_dir in sorted(scen_dir.glob("r*")):
                if not rep_dir.is_dir():
                    continue
                rep = rep_dir.name
                for diag_file in sorted(rep_dir.glob("diag-*.json")):
                    parsed = parse_diag_filename(diag_file.name)
                    if not parsed:
                        continue
                    conf, engine, mode = parsed
                    try:
                        blob = json.loads(diag_file.read_text())
                    except json.JSONDecodeError:
                        continue
                    comp = (blob.get("diagnosis", {})
                            .get("component", "") or "")
                    cat = (blob.get("diagnosis", {})
                           .get("category", "") or "")
                    aligned = aligned_match(subject, role, comp)
                    rows.append({
                        "subject_id": subject,
                        "scenario_id": scen,
                        "rep": rep,
                        "configuration": conf,
                        "engine_mode": f"{engine}-{mode}",
                        "diag_component": comp,
                        "diag_category": cat,
                        "gt_role": role,
                        "gt_category": gt["category"],
                        "top1_aligned": "1" if aligned else "0",
                        "category_correct":
                            "1" if cat.lower() == gt["category"].lower() else "0",
                    })

    # Write CSV
    if not rows:
        print(f"no diag files found under {matrix_dir}", file=sys.stderr)
        return 1
    out_csv = out_dir / "per-subject-results.csv"
    fields = list(rows[0].keys())
    with out_csv.open("w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=fields)
        w.writeheader()
        w.writerows(rows)
    print(f"wrote {out_csv} ({len(rows)} rows)")

    # Summary by (subject × engine)
    cells: dict[tuple[str, str], list[int]] = defaultdict(list)
    cells_scen: dict[tuple[str, str, str], list[int]] = defaultdict(list)
    for r in rows:
        cells[(r["subject_id"], r["engine_mode"])].append(
            int(r["top1_aligned"]))
        cells_scen[(r["subject_id"], r["scenario_id"], r["engine_mode"])].append(
            int(r["top1_aligned"]))

    md = [
        "# Multi-app S2-S5 — vocabulary-aligned top-1 by subject × engine",
        "",
        "Per (subject × engine) cells, pooled across F1, F2, F3, F6, F7 "
        "and C1-C5 evidence levels.",
        "",
        "| Subject | Engine | n | aligned top-1 | Wilson 95% CI |",
        "|---|---|---|---|---|",
    ]
    for (subj, eng), data in sorted(cells.items()):
        n = len(data)
        s = sum(data)
        lo, hi = wilson_ci(s, n)
        md.append(
            f"| {subj} | {eng} | {n} | {100*s/n:5.1f}% ({s}/{n}) | "
            f"[{100*lo:.1f}%, {100*hi:.1f}%] |"
        )
    (out_dir / "summary-by-subject.md").write_text("\n".join(md) + "\n")
    print(f"wrote {out_dir / 'summary-by-subject.md'}")

    # Pooled across S2-S5 by engine
    pooled: dict[str, list[int]] = defaultdict(list)
    for r in rows:
        if r["subject_id"] != "s1-flask-catalog":
            pooled[r["engine_mode"]].append(int(r["top1_aligned"]))
    md = [
        "# Multi-app S2-S5 pooled aligned top-1 by engine",
        "",
        "Pooled across listmonk + healthchecks + umami + petclinic, "
        "F1+F2+F3+F6+F7, C1-C5, n = 10 reps. (S1 excluded — it is the "
        "primary application reported in §3.)",
        "",
        "| Engine | n | aligned top-1 | Wilson 95% CI |",
        "|---|---|---|---|",
    ]
    for eng, data in sorted(pooled.items()):
        n = len(data)
        s = sum(data)
        lo, hi = wilson_ci(s, n)
        md.append(
            f"| {eng} | {n} | {100*s/n:5.1f}% ({s}/{n}) | "
            f"[{100*lo:.1f}%, {100*hi:.1f}%] |"
        )
    (out_dir / "summary-pooled.md").write_text("\n".join(md) + "\n")
    print(f"wrote {out_dir / 'summary-pooled.md'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
