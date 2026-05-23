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

    if fault == "F4":
        # F4 — broken backend route. The application starts cleanly, but one of
        # its critical resources is missing at query time: the post-migration
        # step renames the subject's primary table to a name the app does not
        # know. Subsequent API calls that read that table return 5xx.
        # Semantically still "application: the application's behavior is
        # broken" — the path from request to a populated resource is severed.
        critical_table = {
            "s2-listmonk":     "lists",
            "s3-healthchecks": "api_check",
            "s4-umami":        "website",
            "s5-petclinic":    "pets",
        }.get(sid)
        if critical_table is None:
            raise ValueError(f"F4 multi-app: unknown critical table for {sid}")
        original = s.get("migration_command") or []
        # Append a "rename critical table" psql command after the original
        # migration runs (so seeding still works, then the table is severed).
        rename = (
            f'apt-get install -y postgresql-client 2>/dev/null; '
            f'psql "$DATABASE_URL" -c "ALTER TABLE {critical_table} '
            f'RENAME TO {critical_table}_fp_f4_broken"'
        )
        if original and isinstance(original, list) and len(original) >= 3:
            # original is typically [sh, -c, <command>]; suffix our command
            new_cmd = original[2] + f' && {rename}'
            s["migration_command"] = [original[0], original[1], new_cmd]
        return DeployPlan(subject=s, image=stock)

    if fault == "F5":
        # F5 — frontend bug. Re-scoped 2026-05-23 19:15 UTC after the first
        # Phase A run on s2-listmonk showed every F5 rep timing out as a
        # no-report: the harness-adapter smoke suites for backend-only
        # subjects (s2-listmonk, s3-healthchecks, s5-petclinic) call
        # /api/* endpoints exclusively and never exercise the frontend
        # asset path. Setting a frontend-URL env var therefore does not
        # produce a smoke failure → no FailureReport → no-report. This is
        # a methodological mismatch between fault scope and test scope,
        # NOT a missing measurement. F5 is N/A for these subjects;
        # s4-umami is the one exception because Next.js consumes
        # NEXT_PUBLIC_API_URL at startup. Documented in
        # threats-to-validity.md §7.6.
        if sid in ("s2-listmonk", "s3-healthchecks", "s5-petclinic"):
            raise ValueError(
                f"F5 N/A for {sid}: smoke suite targets backend APIs only; "
                "frontend not exercised by the harness-adapter tests")
        # s4-umami: the env-var hack actually breaks Next.js startup.
        svcs = s.get("services", [])
        name = "NEXT_PUBLIC_API_URL"
        val = "http://fp-f5-broken-frontend.invalid:0"
        svcs[0].setdefault("env", []).append({"name": name, "value": val})
        return DeployPlan(subject=s, image=stock)

    if fault == "F8":
        # F8 — backend latency. Inject a Postgres BEFORE-INSERT trigger that
        # calls pg_sleep(2) on the critical table; every write path that
        # touches that table is now slow by 2 s. Smoke / regression suites
        # exercise these writes, so the latency surfaces as test timeouts
        # OR observably slower responses. Semantically still "observability:
        # the system is unhealthy in a way that is only visible in timing".
        critical_table = {
            "s2-listmonk":     "lists",
            "s3-healthchecks": "api_check",
            "s4-umami":        "website",
            "s5-petclinic":    "pets",
        }.get(sid)
        if critical_table is None:
            raise ValueError(f"F8 multi-app: unknown critical table for {sid}")
        sql = (
            "CREATE OR REPLACE FUNCTION fp_f8_delay() RETURNS trigger AS "
            "\\$\\$ BEGIN PERFORM pg_sleep(2); RETURN NEW; END; \\$\\$ "
            "LANGUAGE plpgsql; "
            f"CREATE TRIGGER fp_f8_latency BEFORE INSERT OR UPDATE ON "
            f"{critical_table} FOR EACH ROW EXECUTE FUNCTION fp_f8_delay();"
        )
        original = s.get("migration_command") or []
        inject = (
            f'apt-get install -y postgresql-client 2>/dev/null; '
            f'psql "$DATABASE_URL" -c "{sql}"'
        )
        if original and isinstance(original, list) and len(original) >= 3:
            new_cmd = original[2] + f' && {inject}'
            s["migration_command"] = [original[0], original[1], new_cmd]
        return DeployPlan(subject=s, image=stock)

    if fault == "F9":
        # F9 — bad seed data. The migration runs successfully but the seed
        # step inserts a row that violates the application's logical
        # constraints. For subjects with a 'lists'-like primary table, we
        # insert a row with a deliberately bad NOT-NULL or foreign-key
        # value. Diagnosis must point at the seed step, not at the
        # backend or migration.
        critical_table = {
            "s2-listmonk":     "lists",
            "s3-healthchecks": "api_check",
            "s4-umami":        "website",
            "s5-petclinic":    "pets",
        }.get(sid)
        if critical_table is None:
            raise ValueError(f"F9 multi-app: unknown critical table for {sid}")
        # An UPDATE that NULLs a known-required column triggers the app's
        # subsequent read to crash on the NULL.
        bad_sql_per_subject = {
            "s2-listmonk":     "UPDATE lists SET name = NULL WHERE id = (SELECT id FROM lists LIMIT 1)",
            "s3-healthchecks": "UPDATE api_check SET name = NULL WHERE id = (SELECT id FROM api_check LIMIT 1)",
            "s4-umami":        "UPDATE website SET name = NULL WHERE website_id = (SELECT website_id FROM website LIMIT 1)",
            "s5-petclinic":    "UPDATE pets SET name = NULL WHERE id = (SELECT id FROM pets LIMIT 1)",
        }
        original = s.get("migration_command") or []
        bad = bad_sql_per_subject[sid]
        inject = (
            f'apt-get install -y postgresql-client 2>/dev/null; '
            f'psql "$DATABASE_URL" -c "{bad}"'
        )
        if original and isinstance(original, list) and len(original) >= 3:
            new_cmd = original[2] + f' && {inject}'
            s["migration_command"] = [original[0], original[1], new_cmd]
        return DeployPlan(subject=s, image=stock)

    if fault == "F10":
        # F10 — flaky test. Sets the FP_F10_FLAKY=1 env var on services so
        # the test wrapper (when rebuilt with the flaky-mode support — see
        # harness-adapter/wrapper.py) injects a deliberate non-deterministic
        # failure on one test. By construction F10 has no real root cause:
        # the diagnosis evaluation here is the F10 hallucination control.
        svcs = s.get("services", [])
        for svc in svcs:
            svc.setdefault("env", []).append({"name": "FP_F10_FLAKY", "value": "1"})
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
