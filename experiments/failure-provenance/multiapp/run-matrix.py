#!/usr/bin/env python3
"""run-matrix.py — multi-app failure-provenance experiment orchestrator.

Drives the failure-provenance matrix across the five subjects defined in
config.yaml. For each (subject, fault, repetition) it injects the fault,
builds (or reuses) the faulted image, deploys a Preview via the operator,
waits for the FailureReport, scores it, and appends a results-CSV row.

Design notes
------------
* meta.yaml-driven   — Preview CRs come from harness/preview_factory.py, so the
                       same orchestrator handles Flask / Go / Django / Next.js /
                       Spring subjects without per-stack manifest code.
* build-once         — the faulted image for (subject, fault) is identical across
                       repetitions; it is built once and cached.
* concurrent         — a worker pool runs up to --concurrency previews at once
                       (default 3; bounded by the 2-node cluster).
* fail-loud, --dry-run default — nothing touches the cluster unless --execute.

Fault injectors live in injectors_multiapp/ and are dispatched by
(subject_id, fault). app-agnostic faults (F3/F6/F7) need no per-subject code.

Status: orchestrator core. Per-subject injectors are filled in and validated
one subject at a time — see docs/research/failure-provenance/multi-app-plan.md.
"""
from __future__ import annotations
import argparse
import concurrent.futures
import csv
import os
import pathlib
import subprocess
import sys
import threading
import time

import yaml

ROOT = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
from harness import config as hconfig          # noqa: E402  vendored
from harness import preview_factory as pf      # noqa: E402  vendored

CR_NAMESPACE = "default"
REPORT_TIMEOUT_S = 1200
EXPERIMENT_LABEL = "failure-provenance.experiment/owned=true"


# --------------------------------------------------------------------------
# build-once image cache
# --------------------------------------------------------------------------
class ImageCache:
    """Builds the faulted image for (subject, fault) once and reuses it.

    The fault injection is deterministic, so all repetitions of one
    (subject, fault) share an image — 5x7 builds instead of 5x7x10.
    """

    def __init__(self, registry: str, execute: bool):
        self._registry = registry
        self._execute = execute
        self._lock = threading.Lock()
        self._building: dict[str, threading.Event] = {}
        self._images: dict[str, str] = {}

    def get(self, subject_id: str, fault: str, build_fn) -> str:
        key = f"{subject_id}/{fault}"
        with self._lock:
            if key in self._images:
                return self._images[key]
            ev = self._building.get(key)
            if ev is None:
                ev = threading.Event()
                self._building[key] = ev
                owner = True
            else:
                owner = False
        if not owner:
            ev.wait()
            return self._images[key]
        image = build_fn()           # may take minutes — run outside the lock
        with self._lock:
            self._images[key] = image
        ev.set()
        return image


# --------------------------------------------------------------------------
# one (subject, fault, rep) unit
# --------------------------------------------------------------------------
# Stable subject indices so two processes launched with different
# --subjects lists do NOT collide on PR numbers.
SUBJECT_IDX = {
    "s2-listmonk": 0,
    "s3-healthchecks": 1,
    "s4-umami": 2,
    "s5-petclinic": 3,
}


def run_unit(subject: dict, subject_idx: int, fault: str, rep: int, cfg: dict,
             cache: ImageCache, out_dir: pathlib.Path, execute: bool) -> dict:
    """Execute one cluster run and return a results row dict."""
    sid = subject["id"]
    # Deterministic, collision-free PR number based on the *stable* subject
    # index (not the position in --subjects), so parallel processes that
    # cover disjoint subjects share no PR numbers.
    stable_idx = SUBJECT_IDX.get(sid, subject_idx)
    pr_number = 90000 + stable_idx * 1000 + _fault_idx(fault) * 100 + rep
    name = f"pr-{pr_number}"
    run_dir = out_dir / sid / fault / f"r{rep}"
    row = {"subject": sid, "fault": fault, "rep": rep, "preview": name,
           "status": "pending"}

    # Idempotent restart: skip if this rep already captured a report.
    report_path = run_dir / "report.json"
    if report_path.exists() and report_path.stat().st_size > 0:
        row["status"] = "skip-existing"
        return row

    run_dir.mkdir(parents=True, exist_ok=True)

    if not execute:
        row["status"] = "dry-run"
        return row

    # 1. inject the fault — yields the (possibly mutated) deploy plan
    from injectors_multiapp import dispatch as inject_dispatch    # noqa
    plan = inject_dispatch.prepare(subject, fault, cfg)
    _ = cache  # build-once cache reserved for code-fault image builds (S1)

    # 2. deploy the Preview via the meta.yaml-driven factory
    pf.create(name=name, cr_namespace=CR_NAMESPACE, pr_number=pr_number,
              subject=plan.subject, subject_image=plan.image,
              probe_image=cfg["subjects"]["probe_image"])
    _kubectl("label", "preview", name, EXPERIMENT_LABEL, "--overwrite")

    # 3. post-deploy fault step (e.g. F7 patches the Service selector)
    if plan.post_deploy is not None:
        ns = _wait_for_namespace(pr_number)
        if ns:
            plan.post_deploy(name, ns)

    # 4. wait for the FailureReport (operator captures it on failure)
    if not _wait_for_report(f"{name}-failure"):
        row["status"] = "no-report"
        pf.delete(name, pf.runtime_namespace(pr_number), wait=False)
        return row

    with (run_dir / "report.json").open("w") as fh:
        _kubectl("get", "failurereport", f"{name}-failure", "-o", "json", stdout=fh)
    row["status"] = "captured"

    # 5. teardown
    pf.delete(name, pf.runtime_namespace(pr_number), wait=False)
    return row


def _fault_idx(fault: str) -> int:
    return int(fault[1:])


def _wait_for_namespace(pr_number: int, timeout_s: int = 300) -> str:
    """Return the preview's runtime namespace once it exists, else ''."""
    ns = pf.runtime_namespace(pr_number)
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        out = subprocess.run(["kubectl", "get", "namespace", ns],
                             capture_output=True, text=True)
        if out.returncode == 0:
            return ns
        time.sleep(5)
    return ""


def _wait_for_report(report_name: str) -> bool:
    deadline = time.monotonic() + REPORT_TIMEOUT_S
    while time.monotonic() < deadline:
        out = subprocess.run(
            ["kubectl", "get", "failurereport", report_name,
             "-o", "jsonpath={.status.phase}"],
            capture_output=True, text=True)
        if out.returncode == 0 and out.stdout.strip() == "Captured":
            return True
        time.sleep(15)
    return False


def _kubectl(*args, stdout=None):
    return subprocess.run(["kubectl", *args], check=False,
                          stdout=stdout, text=True)


# --------------------------------------------------------------------------
# main
# --------------------------------------------------------------------------
def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--execute", action="store_true",
                    help="run for real (default: dry-run plan only)")
    ap.add_argument("--concurrency", type=int, default=3)
    ap.add_argument("--reps", type=int, default=10)
    ap.add_argument("--subjects", default="",
                    help="comma-separated subject ids; default: all enabled")
    ap.add_argument("--output-dir", default=str(ROOT / "results-multiapp"))
    args = ap.parse_args()

    cfg = yaml.safe_load((ROOT / "config.yaml").read_text())
    enabled = args.subjects.split(",") if args.subjects \
        else cfg["subjects"]["enabled"]
    subjects = [hconfig.load_subject(sid) for sid in enabled]
    out_dir = pathlib.Path(args.output_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    units = [(s, si, f, r)
             for si, s in enumerate(subjects)
             for f in cfg["faults"].get(s["id"], [])
             for r in range(1, args.reps + 1)]
    print(f"subjects   : {[s['id'] for s in subjects]}")
    print(f"cluster runs: {len(units)}  (concurrency {args.concurrency})")
    if not args.execute:
        print("dry-run: pass --execute to run against the cluster")
        return 0

    cache = ImageCache(cfg["registry"], args.execute)
    rows: list[dict] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        futs = {pool.submit(run_unit, s, si, f, r, cfg, cache, out_dir, True): (s["id"], f, r)
                for (s, si, f, r) in units}
        for fut in concurrent.futures.as_completed(futs):
            sid, f, r = futs[fut]
            try:
                rows.append(fut.result())
                print(f"done {sid} {f} r{r}: {rows[-1]['status']}")
            except Exception as exc:                       # noqa: BLE001
                print(f"FAIL {sid} {f} r{r}: {exc}")
                rows.append({"subject": sid, "fault": f, "rep": r,
                             "status": f"error: {exc}"})

    csv_path = out_dir / "runs.csv"
    with csv_path.open("w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=["subject", "fault", "rep",
                                           "preview", "status"])
        w.writeheader()
        for row in rows:
            w.writerow({k: row.get(k, "") for k in w.fieldnames})
    print(f"runs summary: {csv_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
