"""
E2E tests for S2-Listmonk.
Runs in the Playwright container (tests copied via init-container).
Uses requests only — no browser needed for a JSON API subject.
Includes both isolation probes: run_log_clean + entity_count_matches_seed.
"""
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:9000")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")
AUTH = ("admin", "harness123")
# Post-install baseline: 2 default lists from `listmonk --install --yes` +
# 3 from harness seed = 5. See ANALYSIS_S2.md §3.a.
SEED_COUNT = 5

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
r_lists = requests.get(BASE + "/api/lists?page=1&per_page=100", timeout=5, auth=AUTH).json()
list_count = r_lists.get("data", {}).get("total", -1)
t("entity_count_matches_seed", lambda: (
    list_count == SEED_COUNT,
    f"expected {SEED_COUNT} lists after restore, got {list_count} (restore may have failed)"
))

# ── end-to-end API flow ──────────────────────────────────────────────────────
t("healthz", lambda: (requests.get(BASE + "/healthz", timeout=5).status_code == 200, "not 200"))

# Create list → create subscriber → add subscriber to list → verify
r_list = requests.post(BASE + "/api/lists",
                       json={"name": "e2e-flow-list", "type": "public",
                             "optin": "single", "tags": []},
                       timeout=5, auth=AUTH)
t("e2e_create_list", lambda: (r_list.status_code == 200, f"status {r_list.status_code}"))

list_id = r_list.json().get("data", {}).get("id") if r_list.status_code == 200 else None

r_sub = requests.post(BASE + "/api/subscribers",
                      json={"email": "e2e@harness.local", "name": "E2E Sub",
                            "status": "enabled",
                            "lists": [list_id] if list_id else [],
                            "attribs": {}},
                      timeout=5, auth=AUTH)
t("e2e_create_subscriber", lambda: (r_sub.status_code == 200, f"status {r_sub.status_code}"))

sub_id = r_sub.json().get("data", {}).get("id") if r_sub.status_code == 200 else None
if sub_id:
    t("e2e_subscriber_fetch", lambda: (
        requests.get(BASE + f"/api/subscribers/{sub_id}", timeout=5, auth=AUTH).status_code == 200,
        "not 200"
    ))
else:
    print("FAIL e2e e2e_subscriber_fetch: no subscriber created"); failed += 1

t("e2e_campaigns_available", lambda: (requests.get(BASE + "/api/campaigns", timeout=5, auth=AUTH).status_code == 200, "not 200"))
t("e2e_templates_available", lambda: (requests.get(BASE + "/api/templates", timeout=5, auth=AUTH).status_code == 200, "not 200"))

# Write e2e marker to run-log
requests.post(PROBE + "/api/run-log", json={"suite": "e2e"}, timeout=5)

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
