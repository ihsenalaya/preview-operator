"""
Smoke tests for S2-Listmonk.
Uses HTTP Basic auth (admin:harness123) for all API calls.
Writes a smoke marker to the probe service for the run_log_clean isolation probe.
"""
import base64
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:9000")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")
AUTH = ("admin", "harness123")

passed = failed = 0


def t(name, fn):
    global passed, failed
    try:
        ok, reason = fn()
        if ok:
            print(f"PASS smoke {name}")
            passed += 1
        else:
            print(f"FAIL smoke {name}: {reason}")
            failed += 1
    except Exception as e:
        print(f"FAIL smoke {name}: {e}")
        failed += 1


t("healthz",       lambda: (requests.get(BASE + "/healthz",      timeout=5).status_code == 200, "not 200"))
t("lists_get",     lambda: (requests.get(BASE + "/api/lists",    timeout=5, auth=AUTH).status_code == 200, "not 200"))
t("subscribers",   lambda: (requests.get(BASE + "/api/subscribers", timeout=5, auth=AUTH).status_code == 200, "not 200"))
t("campaigns",     lambda: (requests.get(BASE + "/api/campaigns",   timeout=5, auth=AUTH).status_code == 200, "not 200"))
t("templates",     lambda: (requests.get(BASE + "/api/templates",   timeout=5, auth=AUTH).status_code == 200, "not 200"))

# Write smoke marker — regression checks this was cleared by restore-regression
try:
    requests.post(PROBE + "/api/run-log", json={"suite": "smoke"}, timeout=5)
except Exception as e:
    print(f"FAIL smoke run_log_write: {e}")
    failed += 1

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
