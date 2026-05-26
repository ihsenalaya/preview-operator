"""
Regression tests for S5-Spring PetClinic REST.
Includes run_log_clean isolation probe and pet_count_matches_seed.
"""
import os
import sys
import requests

BASE = os.environ.get("APP_URL", "http://svc-backend:9966")
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")
SEED_COUNT = 13  # default Flyway R__Insert_default_data.sql pets

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
t("healthz",      lambda: (requests.get(BASE + "/healthz",    timeout=10).status_code == 200, "not 200"))
t("vets_list",    lambda: (isinstance(requests.get(BASE + "/api/vets",   timeout=10).json(), (list, dict)), "bad vets"))
t("owners_list",  lambda: (isinstance(requests.get(BASE + "/api/owners", timeout=10).json(), (list, dict)), "bad owners"))

# Owner CRUD
r_owner = requests.post(BASE + "/api/owners",
                        json={"firstName": "Exp", "lastName": "Owner",
                              "address": "123 Harness St", "city": "Testville",
                              "telephone": "5550000000"},
                        timeout=10)
t("owner_create", lambda: (r_owner.status_code == 201, f"status {r_owner.status_code}"))

owner_id = r_owner.json().get("id") if r_owner.status_code == 201 else None
if owner_id:
    t("owner_fetch",  lambda: (
        requests.get(BASE + f"/api/owners/{owner_id}", timeout=10).status_code == 200, "not 200"
    ))
    # Spring PetClinic REST returns 200 or 204 depending on minor version.
    # lastName must match @Pattern("[a-zA-Z]*") — no dashes/digits allowed
    # (the dash in "Owner-Updated" was the cause of fix5's 0/18 idempotence).
    r_upd = requests.put(BASE + f"/api/owners/{owner_id}",
                 json={"id": owner_id, "firstName": "Exp", "lastName": "OwnerUpdated",
                       "address": "123 Harness St", "city": "Testville", "telephone": "5550000001"},
                 timeout=10)
    t("owner_update", lambda: (
        r_upd.status_code in (200, 204),
        f"status {r_upd.status_code}"
    ))
    t("owner_delete", lambda: (
        requests.delete(BASE + f"/api/owners/{owner_id}", timeout=10).status_code in (200, 204),
        "delete failed"
    ))
else:
    for n in ("owner_fetch", "owner_update", "owner_delete"):
        print(f"FAIL regression {n}: no owner created"); failed += 3

# Pet count probe
pets = requests.get(BASE + "/api/pets", timeout=10).json()
pet_list = pets if isinstance(pets, list) else pets.get("items", pets.get("content", []))
pet_count = len(pet_list)
t("pet_count_matches_seed", lambda: (
    pet_count == SEED_COUNT,
    f"expected {SEED_COUNT} pets, got {pet_count}"
))

t("pettypes",     lambda: (isinstance(requests.get(BASE + "/api/pettypes", timeout=10).json(), (list, dict)), "bad response"))
t("specialties",  lambda: (isinstance(requests.get(BASE + "/api/specialties", timeout=10).json(), (list, dict)), "bad response"))

# Write regression marker
requests.post(PROBE + "/api/run-log", json={"suite": "regression"}, timeout=5)

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
