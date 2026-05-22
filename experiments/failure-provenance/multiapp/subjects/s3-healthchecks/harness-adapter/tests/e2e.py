"""
E2E tests for S3-Healthchecks.
Includes both isolation probes: run_log_clean + entity_count_matches_seed.
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
            print(f"PASS e2e {name}")
            passed += 1
        else:
            print(f"FAIL e2e {name}: {reason}")
            failed += 1
    except Exception as e:
        print(f"FAIL e2e {name}: {e}")
        failed += 1


# ── isolation probe 1: run_log_clean ────────────────────────────────────────
log = requests.get(PROBE + "/api/run-log", timeout=5).json()
regression_count = log.get("regression", 0)
t("run_log_clean", lambda: (
    regression_count == 0,
    f"expected 0 regression markers after restore-e2e, got {regression_count}"
))

# ── isolation probe 2: entity_count_matches_seed ────────────────────────────
checks = requests.get(BASE + "/api/v3/checks/", timeout=5, headers=HDRS).json()
check_count = len(checks.get("checks", []))
t("entity_count_matches_seed", lambda: (
    check_count == SEED_COUNT,
    f"expected {SEED_COUNT} checks after restore, got {check_count}"
))

# ── end-to-end flow: check lifecycle ────────────────────────────────────────
t("healthz", lambda: (requests.get(BASE + "/healthz", timeout=5).status_code == 200, "not 200"))

r = requests.post(BASE + "/api/v3/checks/",
                  json={"name": "e2e-check", "tags": "e2e", "timeout": 1800, "grace": 60},
                  timeout=5, headers=HDRS)
t("e2e_create_check", lambda: (r.status_code == 201, f"status {r.status_code}"))

ping_url = r.json().get("ping_url") if r.status_code == 201 else None
uuid = ping_url.split("/")[-1] if ping_url else None

if uuid:
    t("e2e_ping",   lambda: (requests.get(BASE + f"/ping/{uuid}", timeout=5).status_code == 200, "ping failed"))
    t("e2e_ping_fail", lambda: (requests.get(BASE + f"/ping/{uuid}/fail", timeout=5).status_code == 200, "fail-ping failed"))
    flips = requests.get(BASE + f"/api/v3/checks/{uuid}/flips/", timeout=5, headers=HDRS)
    t("e2e_flips",  lambda: (flips.status_code == 200, f"status {flips.status_code}"))
    t("e2e_delete", lambda: (requests.delete(BASE + f"/api/v3/checks/{uuid}", timeout=5, headers=HDRS).status_code == 200, "delete failed"))
else:
    for n in ("e2e_ping", "e2e_ping_fail", "e2e_flips", "e2e_delete"):
        print(f"FAIL e2e {n}: no check created"); failed += 4

# Write e2e marker
requests.post(PROBE + "/api/run-log", json={"suite": "e2e"}, timeout=5)

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
