"""Fault-injector dispatch for the multi-app failure-provenance matrix.

For subjects S2-S5 (pre-built upstream images) every in-scope fault is injected
at the **manifest level** — by mutating the Preview spec the operator applies —
or **post-deploy** — by patching a cluster object. No upstream source is built.
See docs/research/failure-provenance/multi-app-plan.md §3b.

`prepare(subject, fault, cfg)` returns a DeployPlan:
    subject      — the (possibly mutated) meta.yaml dict to deploy
    image        — the image ref for spec.image
    post_deploy  — callable(preview_name, runtime_namespace) run after apply,
                   or None

Fault model (S2-S5 in-scope subset F1,F2,F3,F6,F7):
    F1  invalid migration   — migration_command runs invalid SQL → Job fails
    F2  missing env var     — drop a required env var from the first service
    F3  invalid image tag   — spec.image set to a non-existent tag
    F6  DB readiness timeout — point the app/migration at an unreachable DB host
    F7  broken Service selector — post-deploy: repoint svc-<app> at no pods
"""
from __future__ import annotations
import copy
import dataclasses
import subprocess
import time
from typing import Callable, Optional


def _wait_for_service(ns: str, svc_name: str, timeout_s: int = 180) -> bool:
    """Block until `svc_name` exists in `ns`, or timeout. Returns True on
    success. Used by F7 to avoid racing the operator's Service creation."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        out = subprocess.run(
            ["kubectl", "-n", ns, "get", "service", svc_name],
            capture_output=True, text=True)
        if out.returncode == 0:
            return True
        time.sleep(3)
    return False


@dataclasses.dataclass
class DeployPlan:
    subject: dict
    image: str
    post_deploy: Optional[Callable[[str, str], None]] = None


def prepare(subject: dict, fault: str, cfg: dict) -> DeployPlan:
    sid = subject["id"]
    stock = cfg["subjects"]["images"][sid]
    s = copy.deepcopy(subject)

    if fault == "F3":
        # Invalid container image tag — operator never pulls it.
        return DeployPlan(subject=s, image=stock.rsplit(":", 1)[0] + ":fp-f3-nonexistent")

    if fault == "F1":
        # Invalid SQL migration — the migration Job runs a bad statement and
        # fails. Deterministic, stack-independent (psql is in every adapter).
        s["migration_command"] = [
            "sh", "-c",
            'psql "$DATABASE_URL" -v ON_ERROR_STOP=1 '
            '-c "CREATE INDX fp_f1_bad ON information_schema.tables (table_name)"',
        ]
        return DeployPlan(subject=s, image=stock)

    if fault == "F2":
        # Missing environment variable — drop a declared env var from the first
        # service so the app starts without required configuration.
        svcs = s.get("services", [])
        if svcs and svcs[0].get("env"):
            svcs[0]["env"] = svcs[0]["env"][1:]          # drop the first declared var
        else:
            # No declared env to drop — inject a broken DATABASE_URL override.
            svcs[0].setdefault("env", []).append(
                {"name": "DATABASE_URL", "value": "postgres://fp:fp@10.255.255.1:5432/fp"})
        return DeployPlan(subject=s, image=stock)

    if fault == "F6":
        # Database readiness timeout — repoint the app at an unroutable DB host
        # so readiness never succeeds.
        for svc in s.get("services", []):
            svc.setdefault("env", []).append(
                {"name": "DATABASE_URL", "value": "postgres://fp:fp@10.255.255.1:5432/fp"})
        return DeployPlan(subject=s, image=stock)

    if fault == "F7":
        # Multi-app F7 — Service routes to a port the application does NOT
        # bind to.
        #
        # The previous implementation patched Service.spec.selector
        # post-deploy. That race-conditioned against the operator's
        # reconciler: the operator reverts the selector within ~3s, so
        # whether the test Jobs saw a broken service depended on whether
        # they ran before or after the reconcile (verified live on
        # 2026-05-23 against pr-90709/pr-90710). The captures collected
        # this way are non-credible for a Q1 evaluation.
        #
        # Encoding the fault at the meta.yaml level — by setting
        # services[0].port to a port the application never listens on —
        # produces a deterministic, operator-respected failure: the
        # operator generates a Service with port 19999, the application
        # binds to its standard port (e.g. 9000 for listmonk, 8000 for
        # healthchecks, 3000 for umami, 8080 for petclinic), endpoints
        # exist (the selector still matches pods) but no process answers
        # on 19999 → connection refused → test suites fail → operator
        # captures FailureReport. Semantically still "service
        # mis-configured / unreachable" — the F7 infrastructure category.
        s = copy.deepcopy(subject)
        s["services"][0]["port"] = 19999
        return DeployPlan(subject=s, image=stock)

    raise ValueError(f"fault {fault} not in the S2-S5 manifest-injectable scope")
