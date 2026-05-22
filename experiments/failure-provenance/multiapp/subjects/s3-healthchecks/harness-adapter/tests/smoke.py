"""
Smoke tests for S3-Healthchecks.
Uses Authorization: ApiKey <key> for REST API v3.
Writes a smoke marker to the probe service for the run_log_clean isolation probe.
"""
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:8000")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")
API_KEY = os.environ.get("HC_API_KEY", "harness-api-key-aaaaaaaaaaaaaaaa")
HDRS = {"X-Api-Key": API_KEY}

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


t("healthz",      lambda: (requests.get(BASE + "/healthz",        timeout=5).status_code == 200, "not 200"))
t("checks_list",  lambda: (requests.get(BASE + "/api/v3/checks/",  timeout=5, headers=HDRS).status_code == 200, "not 200"))
t("channels",     lambda: (requests.get(BASE + "/api/v3/channels/", timeout=5, headers=HDRS).status_code == 200, "not 200"))

# Write smoke marker
try:
    requests.post(PROBE + "/api/run-log", json={"suite": "smoke"}, timeout=5)
except Exception as e:
    print(f"FAIL smoke run_log_write: {e}")
    failed += 1

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
