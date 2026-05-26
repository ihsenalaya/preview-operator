#!/usr/bin/env python3
"""15a-claude-annotate.py — AI-assisted first pass on the κ subsample.

Writes `correct_human` in subsample-blinded.csv and a side audit file
`claude-annotation-audit.csv` with the per-row reasoning.

The human reviewer (the project lead) MUST go through the audit file
and override decisions they disagree with — without that, the column
is not a human annotation and cannot be used as the canonical κ.

Decision rules
--------------
Ground truth per scenario (scenarios.yaml):
  F1  migration-job / database
  F2  app-deployment / configuration
  F3  app-deployment / infrastructure
  F4  backend / application
  F5  frontend / application
  F6  app-deployment / infrastructure
  F7  service / infrastructure
  F8  backend / observability
  F9  seed-job / database
  F10 test-suite / test-reliability   (baseline — any confident
                                       component cause is a hallucination
                                       by construction; correct = 0)

Per-row decision for `correct = 1`:
  - proposed_component matches ground_truth component (after alias)
  - AND scenario != F10
A few aliases are deliberately tight: e.g. "application" does NOT
alias to "backend" or "service" because the paper's whole point is to
distinguish symptom from root cause.
"""
from __future__ import annotations
import csv
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
KAPPA_DIR = ROOT.parent.parent / "docs/research/failure-provenance/analysis-output/16-cohen-kappa"
BLINDED = KAPPA_DIR / "subsample-blinded.csv"
AUDIT = KAPPA_DIR / "claude-annotation-audit.csv"

# Tight alias sets — symptom-vs-cause vocabulary the paper argues about.
# Per scenario, the alias set is what counts as the true root cause.
ALIAS = {
    "F1": {"migration-job", "postgres-migrate", "migration", "migrate",
           "db-migration", "database-migration"},
    "F2": {"app-deployment", "app", "application", "idp-preview",
           "backend", "svc-backend"},  # missing-env crashes the app pod
    "F3": {"app-deployment", "app", "application", "deployment",
           "image", "container"},      # ImagePullBackOff is a deployment issue
    "F4": {"backend", "svc-backend", "backend-service", "api"},
    "F5": {"frontend", "ui", "web", "client"},
    "F6": {"app-deployment", "app", "application", "postgres",
           "database", "db"},          # readiness race between db and app
    "F7": {"service", "svc", "svc-backend", "networking", "network",
           "ingress", "routing"},
    "F8": {"backend", "svc-backend", "api"},   # latency is in the backend
    "F9": {"seed-job", "seed", "catalogue-seed", "ai-enrichment",
           "enrichment", "data", "test-data"},
    "F10": set(),                      # any confident proposal is wrong
}

# "Symptom" components — proposals that point at where the failure SHOWS UP
# rather than where it COMES FROM. For most scenarios except the one whose
# ground truth IS that component, naming a symptom is incorrect by the
# failure-provenance criterion.
SYMPTOM_COMPONENTS = {
    "e2e-tests", "test-suite", "regression-tests", "smoke-tests",
    "contract-tests", "tests",
}


def scenario_of(run_id_hidden: str) -> str:
    m = re.match(r"(F\d+)-", run_id_hidden or "")
    return m.group(1) if m else ""


def normalize(s: str) -> str:
    s = (s or "").strip().lower()
    s = re.sub(r"[_/]", "-", s)
    s = re.sub(r"\s+", "-", s)
    return s


def decide(row: dict) -> tuple[str, str]:
    """Return (correct_human, reason)."""
    scen = scenario_of(row["run_id_hidden"])
    proposed = normalize(row["proposed_component"])
    op = row["operator_label_aligned"]

    if scen == "F10":
        if proposed and proposed != "test-suite":
            return "0", (f"F10 is the baseline / flaky-test control; any "
                         f"confident component cause ('{proposed}') is "
                         f"a hallucination by construction.")
        if proposed == "test-suite":
            return "1", "F10 ground truth is the test suite itself."
        return "0", "F10: no component named, no hallucination but no cause either."

    if not proposed:
        return "0", "Empty proposed_component cannot match the ground truth."

    aliases = ALIAS.get(scen, set())
    if proposed in aliases:
        return "1", (f"{scen}: proposed '{proposed}' is in the ground-truth "
                     f"alias set {sorted(aliases)}.")

    # symptom-vs-cause: name an upstream effect at the test layer
    if proposed in SYMPTOM_COMPONENTS:
        return "0", (f"{scen}: proposed '{proposed}' is a symptom layer "
                     f"(where the failure shows up), not the root cause. "
                     f"Ground truth aliases: {sorted(aliases)}.")

    return "0", (f"{scen}: proposed '{proposed}' is outside the ground-truth "
                 f"alias set {sorted(aliases)}. Operator-aligned label = {op}.")


def main() -> int:
    with BLINDED.open(newline="") as f:
        reader = csv.DictReader(f)
        rows = list(reader)
        fieldnames = reader.fieldnames or []

    audit_rows = []
    for r in rows:
        decision, reason = decide(r)
        r["correct_human"] = decision
        audit_rows.append({
            "case_id": r["case_id"],
            "run_id_hidden": r["run_id_hidden"],
            "scenario": scenario_of(r["run_id_hidden"]),
            "proposed_component": r["proposed_component"],
            "proposed_category": r["proposed_category"],
            "operator_label_aligned": r["operator_label_aligned"],
            "claude_decision": decision,
            "claude_reasoning": reason,
            "agrees_with_operator":
                "yes" if decision == r["operator_label_aligned"] else "no",
        })

    with BLINDED.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=fieldnames)
        w.writeheader()
        for r in rows:
            w.writerow(r)

    with AUDIT.open("w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(audit_rows[0].keys()))
        w.writeheader()
        for r in audit_rows:
            w.writerow(r)

    n = len(rows)
    n1 = sum(1 for r in rows if r["correct_human"] == "1")
    disagree_op = sum(1 for r in audit_rows if r["agrees_with_operator"] == "no")
    print(f"annotated: {n} rows ({n1} = correct, {n-n1} = incorrect)")
    print(f"disagrees with operator-aligned on: {disagree_op} rows "
          f"({disagree_op/n*100:.1f}%)")
    print(f"blinded csv (correct_human filled): {BLINDED}")
    print(f"audit file (per-row reasoning):     {AUDIT}")
    print()
    print("NEXT — the human reviewer MUST go through the audit, override "
          "decisions they disagree with, and produce a final canonical κ. "
          "Without that step the column is AI-generated, not human.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
