# Subject Contract

Every experimental subject placed under `subjects/` must comply with this contract.
The harness reads `meta.yaml` from the subject directory; the operator runs the
workload without modification.

---

## 1. Directory layout

```
subjects/<id>/
  meta.yaml                   ← required: subject metadata (this contract)
  harness-adapter/            ← required for external subjects (S2–S5)
    Dockerfile                ← builds the adapter image
    requirements.txt          ← Python deps for test scripts
    wrapper.py                ← starts upstream app + /healthz proxy
    tests/
      smoke.py                ← 5 smoke tests
      regression.py           ← ≥10 regression tests incl. run_log_clean probe
      e2e.py                  ← ≥8 end-to-end tests incl. isolation probes
```

For the reference subject (S1) the test scripts live directly in `testapp/tests/`
and the adapter is the testapp image itself.

---

## 2. meta.yaml schema

```yaml
id:          string    # unique slug, e.g. "s2-listmonk"
name:        string    # human-readable, e.g. "Listmonk Newsletter Manager"
description: string    # one-sentence purpose
origin:
  repo:      string    # GitHub owner/repo or "internal"
  version:   string    # exact version tag or commit
  license:   string    # SPDX identifier, e.g. "AGPL-3.0"
language:    string    # primary implementation language
framework:   string    # web framework
database:    string    # always "postgresql" for this study

seed_entity: string    # name of primary entity (e.g. "products", "lists")
seed_count:  int       # expected count after migration+seed (isolation probe)

migration_command:     # YAML sequence — passed verbatim to spec.database.migration.command
  - sh
  - -c
  - "..."

services:              # forwarded to spec.services[] in the Preview CR
  - name:        string
    image_key:   string   # "app" | "probe" | "custom"
    image:       string   # only when image_key == "custom"
    port:        int
    path_prefix: string
    env:                  # optional static env vars (list of {name, value})
      - name:  string
        value: string
```

`image_key` values:
- `"app"` → resolved to `cfg["subjects"]["images"][id]` at runtime (the harness-adapter image)
- `"probe"` → resolved to `cfg["subjects"]["probe_image"]` (shared probe sidecar)
- `"custom"` → uses the literal `image` field

---

## 3. Service naming and /healthz

The operator names each service's K8s Deployment and ClusterIP as `svc-<name>`.
It probes readiness via `GET /healthz` on the service port.

**Every service MUST expose `GET /healthz` returning HTTP 200.**

For upstream applications that do not expose `/healthz`, the harness-adapter
`wrapper.py` acts as a thin HTTP proxy on the declared port and responds 200 to
`/healthz` while forwarding all other traffic to the upstream app on a sidecar port.

The probe service (image_key: "probe") is implemented in `subjects/probe/` and
already satisfies this requirement.

---

## 4. Test scripts interface

Tests live at `/app/tests/{smoke,regression,e2e}.py` inside the adapter image.

| Suite | Job container | Entry point |
|-------|--------------|-------------|
| smoke | adapter image (via `spec.testSuite.smoke.image`) | `python /app/tests/smoke.py` |
| regression | adapter image | `python /app/tests/regression.py` |
| e2e | Playwright image (tests copied via init-container) | `python /data/tests/e2e.py` |

The operator injects the following environment variables into every test job:

| Variable | Value |
|----------|-------|
| `APP_URL` | `http://svc-<services[0].name>:<port>` |
| `FRONTEND_URL` | URL of service with `path_prefix: "/"`, or `APP_URL` |
| `DATABASE_URL` | PostgreSQL DSN from `postgres-credentials` secret |

The operator also injects `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`
from the `postgres-credentials` secret into regression jobs.

### Output format

Each test prints one line per assertion:

```
PASS <suite> <test_name>
FAIL <suite> <test_name>: <reason>
```

And a summary line at the end:

```
Results: N passed, M failed
```

The operator counts lines beginning with `PASS` / `FAIL` to populate
`status.tests.{suite}.passed` / `status.tests.{suite}.failed`.
The job exit code must be `0` (all pass) or `1` (any failure).

---

## 5. Isolation probes

Every subject MUST implement these two probes.

### 5.1 run_log_clean (regression + e2e)

The `smoke` suite writes a `"smoke"` marker row to the run-log service:

```python
PROBE = os.environ.get("PROBE_URL", "http://svc-probe:9090")
requests.post(PROBE + "/api/run-log", json={"suite": "smoke"}, timeout=5)
```

The `regression` suite verifies the marker was cleared by the `restore-regression`
checkpoint step:

```python
log = requests.get(PROBE + "/api/run-log", timeout=5).json()
smoke_count = log.get("smoke", 0)
t("run_log_clean", lambda: (smoke_count == 0,
    f"expected 0 smoke markers, got {smoke_count} (isolation drift)"))
```

With `isolation=ON` the checkpoint restores the DB, truncating `run_log` → PASS.
With `isolation=OFF` the marker persists → FAIL.

For S1 (embedded run-log): the probe URL is `APP_URL` (the app itself handles
`/api/run-log`). For S2–S5: the probe URL is `http://svc-probe:9090`.

### 5.2 entity_count_matches_seed (e2e)

The `e2e` suite verifies that the primary entity count equals `meta.yaml:seed_count`
after the `restore-e2e` checkpoint step:

```python
count = <subject-specific count query>
t("entity_count_matches_seed", lambda: (
    count == SEED_COUNT,
    f"expected {SEED_COUNT} {SEED_ENTITY}, got {count} (restore may have failed)"
))
```

This confirms that the checkpoint restoration reset the DB to the exact post-seed
state, removing entities created by the regression suite.

---

## 6. Probe service (shared)

`subjects/probe/` provides a shared Flask microservice that manages the `run_log`
table for subjects S2–S5. It must be included as a service in every adapted subject's
`meta.yaml` (image_key: `"probe"`, port: `9090`, path_prefix: `"/probe"`).

The probe reads `DATABASE_URL` from environment (injected by the operator from
`postgres-credentials`). It creates the `run_log` table on startup if absent.

Endpoints:
- `GET /healthz` → `200 ok`
- `GET /api/run-log` → `{"<suite>": <count>, ...}`
- `POST /api/run-log` → insert `{suite}` row, return `201`

---

## 7. Build and registration

```bash
# Build and push adapter image for subject Sn
cd subjects/sN-<name>/harness-adapter/
docker build -t ghcr.io/<owner>/sN-<name>-adapter:<version> .
docker push ghcr.io/<owner>/sN-<name>-adapter:<version>

# Build and push shared probe
cd subjects/probe/
docker build -t ghcr.io/<owner>/harness-probe:latest .
docker push ghcr.io/<owner>/harness-probe:latest

# Register in config.yaml
subjects:
  probe_image: ghcr.io/<owner>/harness-probe:latest
  enabled: [s1-flask-catalog, s2-listmonk, ...]
  images:
    s2-listmonk: ghcr.io/<owner>/s2-listmonk-adapter:<version>
```
