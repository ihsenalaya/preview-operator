"""
Regression tests for S2-Listmonk.
Includes the run_log_clean isolation probe (RQ1 primary metric).
"""
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:9000")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")
AUTH = ("admin", "harness123")
# Post-install baseline: `listmonk --install --yes` creates 2 default lists
# (Default list, Opt-in list) before our harness seed inserts 3 more. The
# true baseline after migration is therefore 5 entities, not 3.
# See ANALYSIS_S2.md §3.a for the empirical reproduction.
SEED_COUNT = 5

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


# ── isolation probe ─────────────────────────────────────────────────────────
# smoke.py wrote suite='smoke' before this suite.
# With isolation ON  → restore-regression truncated run_log → smoke_count == 0.
# With isolation OFF → run_log accumulates               → smoke_count == 1.
log = requests.get(PROBE + "/api/run-log", timeout=5).json()
smoke_count = log.get("smoke", 0)
t("run_log_clean", lambda: (
    smoke_count == 0,
    f"expected 0 smoke markers, got {smoke_count} (isolation drift)"
))

# ── functional tests ─────────────────────────────────────────────────────────
t("healthz",        lambda: (requests.get(BASE + "/healthz", timeout=5).status_code == 200, "not 200"))

# Lists CRUD
r_lists = requests.get(BASE + "/api/lists", timeout=5, auth=AUTH).json()
t("lists_returns_data", lambda: (isinstance(r_lists.get("data", {}).get("results"), list), "bad response shape"))

r_create = requests.post(BASE + "/api/lists",
                         json={"name": "exp-list", "type": "public", "optin": "single",
                               "tags": [], "description": ""},
                         timeout=5, auth=AUTH)
t("list_create",    lambda: (r_create.status_code == 200, f"status {r_create.status_code}"))

created_id = r_create.json().get("data", {}).get("id") if r_create.status_code == 200 else None
if created_id:
    t("list_get",   lambda: (requests.get(BASE + f"/api/lists/{created_id}", timeout=5, auth=AUTH).status_code == 200, "not 200"))
    t("list_delete", lambda: (requests.delete(BASE + f"/api/lists/{created_id}", timeout=5, auth=AUTH).status_code == 200, "delete failed"))
else:
    for n in ("list_get", "list_delete"):
        print(f"FAIL regression {n}: no list created"); failed += 2

# Subscriber CRUD
r_sub = requests.post(BASE + "/api/subscribers",
                      json={"email": "exp@harness.local", "name": "Exp Subscriber",
                            "status": "enabled", "lists": [], "attribs": {}},
                      timeout=5, auth=AUTH)
t("subscriber_create", lambda: (r_sub.status_code == 200, f"status {r_sub.status_code}"))

# Seed count probe: after restore regression restored the DB, list count == SEED_COUNT
# (the exp-list created above was just deleted; this verifies the baseline)
r_all = requests.get(BASE + "/api/lists?page=1&per_page=100", timeout=5, auth=AUTH).json()
list_count = r_all.get("data", {}).get("total", -1)
t("list_count_matches_seed", lambda: (
    list_count == SEED_COUNT,
    f"expected {SEED_COUNT} lists (seed), got {list_count}"
))

t("subscribers_list", lambda: (requests.get(BASE + "/api/subscribers", timeout=5, auth=AUTH).status_code == 200, "not 200"))
t("campaigns_list",   lambda: (requests.get(BASE + "/api/campaigns",   timeout=5, auth=AUTH).status_code == 200, "not 200"))
t("templates_list",   lambda: (requests.get(BASE + "/api/templates",   timeout=5, auth=AUTH).status_code == 200, "not 200"))

# Write regression marker — e2e checks this was cleared by restore-e2e
requests.post(PROBE + "/api/run-log", json={"suite": "regression"}, timeout=5)

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
