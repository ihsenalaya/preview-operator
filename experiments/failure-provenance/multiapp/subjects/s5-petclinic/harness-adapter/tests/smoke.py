"""
Smoke tests for S5-Spring PetClinic REST.
API base: /api/  (no auth required by default).
Writes a smoke marker to the probe service.
"""
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:9966")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")

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


t("healthz",      lambda: (requests.get(BASE + "/healthz",   timeout=10).status_code == 200, "not 200"))
t("vets_list",    lambda: (isinstance(requests.get(BASE + "/api/vets",    timeout=10).json(), (list, dict)), "bad response"))
t("owners_list",  lambda: (isinstance(requests.get(BASE + "/api/owners",  timeout=10).json(), (list, dict)), "bad response"))
t("pets_list",    lambda: (isinstance(requests.get(BASE + "/api/pets",    timeout=10).json(), (list, dict)), "bad response"))
t("pettypes",     lambda: (isinstance(requests.get(BASE + "/api/pettypes", timeout=10).json(), (list, dict)), "bad response"))

# Write smoke marker
try:
    requests.post(PROBE + "/api/run-log", json={"suite": "smoke"}, timeout=5)
except Exception as e:
    print(f"FAIL smoke run_log_write: {e}")
    failed += 1

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
