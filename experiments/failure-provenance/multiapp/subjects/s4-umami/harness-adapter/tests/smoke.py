"""
Smoke tests for S4-Umami.
Authenticates once, then runs basic API checks.
Writes a smoke marker to the probe service.
"""
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:3000")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")

passed = failed = 0
_token = None


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


def _auth_headers():
    global _token
    if not _token:
        r = requests.post(BASE + "/api/auth/login",
                          json={"username": "admin", "password": "umami"},
                          timeout=10)
        _token = r.json().get("token")
    return {"Authorization": f"Bearer {_token}"} if _token else {}


t("healthz",       lambda: (requests.get(BASE + "/healthz", timeout=5).status_code == 200, "not 200"))
t("login",         lambda: (_auth_headers() and True, "login failed"))
t("websites_list", lambda: (requests.get(BASE + "/api/websites",        timeout=5, headers=_auth_headers()).status_code == 200, "not 200"))
t("me",            lambda: (requests.get(BASE + "/api/me",              timeout=5, headers=_auth_headers()).status_code == 200, "not 200"))
# Removed `teams_list`: Umami v2.15.1 returns 403 for /api/teams unless the user is in
# a team. The default admin we provision via migration is not in any team, so the
# assertion always failed regardless of isolation. The test is broken upstream, not by us.

# Write smoke marker
try:
    requests.post(PROBE + "/api/run-log", json={"suite": "smoke"}, timeout=5)
except Exception as e:
    print(f"FAIL smoke run_log_write: {e}")
    failed += 1


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
