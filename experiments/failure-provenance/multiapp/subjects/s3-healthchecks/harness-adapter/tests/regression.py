"""
Regression tests for S3-Healthchecks.
Includes run_log_clean isolation probe and check_count_matches_seed.
"""
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:8000")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")
API_KEY = os.environ.get("HC_API_KEY", "harness-api-key-aaaaaaaaaaaaaaaa")
HDRS = {"X-Api-Key": API_KEY}
SEED_COUNT = 2

passed = failed = 0


def t(name, fn):
    global passed, failed
    try:
        ok, reason = fn()
        if ok:
            print(f"PASS regression {name}")
            passed += 1
        else:
            print(f"FAIL regression {name}: {reason}")
            failed += 1
    except Exception as e:
        print(f"FAIL regression {name}: {e}")
        failed += 1


# ── isolation probe ──────────────────────────────────────────────────────────
log = requests.get(PROBE + "/api/run-log", timeout=5).json()
smoke_count = log.get("smoke", 0)
t("run_log_clean", lambda: (
    smoke_count == 0,
    f"expected 0 smoke markers, got {smoke_count} (isolation drift)"
))

# ── functional tests ─────────────────────────────────────────────────────────
t("healthz",      lambda: (requests.get(BASE + "/healthz", timeout=5).status_code == 200, "not 200"))
t("checks_list",  lambda: (requests.get(BASE + "/api/v3/checks/",   timeout=5, headers=HDRS).status_code == 200, "not 200"))
t("channels",     lambda: (requests.get(BASE + "/api/v3/channels/", timeout=5, headers=HDRS).status_code == 200, "not 200"))

# Checks CRUD
r = requests.post(BASE + "/api/v3/checks/",
                  json={"name": "exp-check", "tags": "experiment", "timeout": 3600, "grace": 60},
                  timeout=5, headers=HDRS)
t("check_create", lambda: (r.status_code == 201, f"status {r.status_code}"))

check_ping_url = r.json().get("ping_url") if r.status_code == 201 else None
check_uuid = check_ping_url.split("/")[-1] if check_ping_url else None

if check_uuid:
    t("check_fetch",  lambda: (
        requests.get(BASE + f"/api/v3/checks/{check_uuid}", timeout=5, headers=HDRS).status_code == 200,
        "not 200"
    ))
    t("check_ping",   lambda: (
        requests.get(BASE + f"/ping/{check_uuid}", timeout=5).status_code == 200,
        "ping not 200"
    ))
    t("check_delete", lambda: (
        requests.delete(BASE + f"/api/v3/checks/{check_uuid}", timeout=5, headers=HDRS).status_code == 200,
        "delete failed"
    ))
else:
    for n in ("check_fetch", "check_ping", "check_delete"):
        print(f"FAIL regression {n}: no check created"); failed += 3

# Seed count probe
checks = requests.get(BASE + "/api/v3/checks/", timeout=5, headers=HDRS).json()
check_count = len(checks.get("checks", []))
t("check_count_matches_seed", lambda: (
    check_count == SEED_COUNT,
    f"expected {SEED_COUNT} checks, got {check_count}"
))

# Write regression marker
requests.post(PROBE + "/api/run-log", json={"suite": "regression"}, timeout=5)

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
