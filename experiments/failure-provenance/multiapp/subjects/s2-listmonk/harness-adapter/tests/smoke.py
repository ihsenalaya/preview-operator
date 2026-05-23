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



# F5 frontend health-check: request the served HTML root and confirm the
# wrapper.py is NOT in its FP_F5_BROKEN_FRONTEND mode (status 500 + magic
# marker). Under fault injection this test fails, producing the smoke
# failure the operator's failure path needs to capture an F5 report.
def _frontend_check():
    try:
        r = requests.get(BASE + "/", timeout=8, headers={"Accept": "text/html"})
        if r.status_code != 200:
            return (False, f"status={r.status_code}")
        if "FP_F5_BROKEN_FRONTEND" in r.text:
            return (False, "broken-frontend marker present")
        return (True, "ok")
    except Exception as e:
        return (False, str(e))

t("frontend_root", _frontend_check)

# F10 flaky-test injector — when FP_F10_FLAKY=1 the smoke suite fails
# with probability 0.5, mimicking a test that is flaky for no reason
# (the RQ4 hallucination control: there is no real root cause). The
# operator's failure-detection path treats a failed smoke suite as a
# real failure and captures a FailureReport for the rep.
import random as _fp_random
if os.environ.get("FP_F10_FLAKY") == "1" and _fp_random.random() < 0.5:
    failed += 1
    print("FAIL smoke fp_f10_flaky_injection: deliberate flake (no real cause)")

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
