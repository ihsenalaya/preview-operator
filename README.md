# cellenza-operator

> **Ephemeral preview environments for every pull request — multi-service frontend + backend, PostgreSQL, operator-orchestrated test suite (smoke · regression · E2E), OpenTelemetry, AI-generated seed data, and full GitHub integration — all from a single Kubernetes custom resource.**

```
kubectl apply -f pr-42.yaml
  → http://pr-42.preview.localtest.me/      (frontend)
  → http://pr-42.preview.localtest.me/api   (backend)
  → operator runs smoke · regression · E2E  (testSuite)
  → kubectl delete cellenza pr-42           (full cleanup)
```

---

## What is it?

**Cellenza** is a Kubernetes operator that turns a pull request number into a fully isolated preview environment in seconds. A single `Cellenza` resource provisions:

- An **isolated namespace** with resource quotas (one per PR — zero cross-PR pollution)
- A **multi-service stack** via `spec.services[]` — frontend and backend each get their own Deployment, Service, and path-based ingress route
- An **ephemeral PostgreSQL** instance with cryptographically-generated credentials auto-injected into every service
- An **operator-orchestrated test suite** — smoke (built-in), regression (`tests/regression.py`), and E2E (Playwright/Chromium) all run in parallel after the environment is ready
- **OpenTelemetry auto-instrumentation** — zero code changes required
- **AI-generated seed data and integration tests** from the PR diff and live DB schema
- **GitHub Deployment status and PR comments** updated automatically by the operator

Everything is cleaned up automatically when the TTL expires or the resource is deleted.

No shared staging environments. No manual test steps. No cleanup scripts.

---

## Feature overview

| Feature | Description |
|---|---|
| Isolated namespaces | Each PR gets `preview-pr-<N>` — zero cross-PR pollution |
| **Multi-service** | Deploy a **frontend + backend** in one resource — each gets its own Deployment, Service, and ingress path |
| Ephemeral PostgreSQL | Optional sidecar DB with unique, cryptographically-generated credentials |
| DB migrations & seeds | One-shot Kubernetes Jobs run before the app, credentials auto-injected |
| DB checkpoints | Save and restore point-in-time DB snapshots via `pg_dump`/`psql` |
| OpenTelemetry | Zero-code auto-instrumentation for Python, Java, Node.js, .NET, Go |
| GitHub Deployment | CI sets the deployment ID; the operator publishes live Kubernetes state back to GitHub |
| Smart diagnostics | On failure: root cause, confidence level, significant logs, and kubectl debug commands — all in a PR comment |
| Automated test suite | Smoke (built-in) + Regression + E2E (Playwright/Chromium) — all three run in parallel |
| AI enrichment | PR diff + DB schema → AI generates contextual `seed.sql` and integration `test.py` |
| Approval gate | Block large or sensitive environments until a human sets `approvedBy` |
| TTL auto-expiry | Environments self-destruct after a configurable duration |
| Resource tiers | `small` / `medium` / `large` — quota extended automatically for DB, AI jobs, and extra services |
| Copilot Extension | `@cellenza status pr-42`, `reset-db`, `enrich`, `extend`, `logs` from GitHub Copilot Chat |

---

## How it works

```
Developer opens PR
      │
      ▼
CI workflow (GitHub Actions) or kubectl apply
creates a Cellenza resource
      │
      ▼
 ┌─ Approval gate ───────────────────────────────────┐
 │  requiresApproval=true → stays Pending             │
 │  until approvedBy is set                           │
 └────────────────────────────────────────────────────┘
      │
      ▼
Operator provisions child resources (namespace: preview-pr-42)
  ┌── Infrastructure ──────────────────────────────────────────────┐
  │  Namespace      preview-pr-42          isolated per PR          │
  │  ResourceQuota  cellenza-quota         CPU/RAM per tier          │
  └───────────────────────────────────────────────────────────────-┘
  ┌── Database (database.enabled=true) ────────────────────────────┐
  │  Secret         postgres-credentials   crypto-random, immutable │
  │  Deployment     postgres               pg:15-alpine, Recreate   │
  │  Service        postgres               ClusterIP :5432           │
  │  Job            postgres-migrate       runs before app           │
  │  Job            postgres-seed          runs after migration      │
  └────────────────────────────────────────────────────────────────┘
  ┌── Services ─────────────────────────────────────────────────────┐
  │  Multi-service mode  (spec.services[])                           │
  │    Deployment  svc-backend    port 8080, DB env vars injected    │
  │    Service     svc-backend    ClusterIP                          │
  │    Deployment  svc-frontend   port 3000, APP_MODE=frontend       │
  │    Service     svc-frontend   ClusterIP                          │
  │    Ingress     pr-42.preview.localtest.me                        │
  │                  /api  →  svc-backend:8080                       │
  │                  /     →  svc-frontend:3000                      │
  │                                                                   │
  │  Single-service mode  (spec.image)                               │
  │    Deployment  app            Recreate, init waits for Postgres  │
  │    Service     app            ClusterIP :80                       │
  │    Ingress     pr-42.preview.localtest.me  →  app:80             │
  └────────────────────────────────────────────────────────────────┘
      │
      ▼
GitHub notification (github.enabled=true)
  • Updates GitHub Deployment status → in_progress / success / failure
  • Posts PR comment with URL, DB state, traces, TTL, diagnostics
      │
      ▼
Operator-orchestrated test suite (testSuite.enabled=true)
  • Job smoke-tests      → probes /healthz + /api/products (operator-embedded)
  • Job regression-tests → runs tests/regression.py from the app image
  • Job e2e-tests        → copies tests/e2e.py; Playwright/Chromium executes
  • All three jobs run in parallel after environment reaches Running
  • Results posted as a dedicated PR comment with pass/fail table
      │
      ▼
AI enrichment (aiEnrichment.enabled=true)
  • Job ai-schema-dump  → pg_dump --schema-only → stored in ConfigMap
  • Operator calls AI API with PR diff + DB schema + app URL
  • AI generates seed.sql (realistic INSERTs) + test.py (targeted integration tests)
  • Job ai-seed         → psql -f /data/seed.sql
  • Job ai-tests        → pip install requests && python /data/test.py
  • Results published to status.aiEnrichment and PR comment
      │
      ▼
Environment runs until TTL expires or kubectl delete
  → finalizer ensures full cleanup including the namespace
```

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Kubernetes | 1.25+ | Kind, k3s, GKE, AKS, EKS, or any conformant cluster |
| Helm | 3.12+ | Required to install the operator |
| cert-manager | 1.13+ | Required for webhook TLS — skip with `--set webhook.enabled=false` |
| nginx ingress controller | any recent | Exposes preview URLs |
| Docker | any recent | Required for local Kind setup and image builds |
| kubectl | 1.25+ | |
| gh CLI | 2.x | Required to create GitHub Deployments and manage tokens |
| OpenTelemetry Operator | optional | Required only for `telemetry.autoInstrumentation` |
| GitHub token Secret | optional | Required only for `github.enabled=true` |
| GitHub App | optional | Required only for the Copilot Extension |
| ngrok | optional | Required to expose the extension locally for Copilot Chat |

---

## Installation

### 0. Create a local Kind cluster

Skip this step if you already have a Kubernetes cluster.

```bash
cat <<EOF | kind create cluster --name cellenza-test --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 80
        hostPort: 8080
        protocol: TCP
      - containerPort: 443
        hostPort: 8443
        protocol: TCP
EOF
```

Preview URLs will be reachable at `http://pr-42.preview.localtest.me:8080` — `localtest.me` resolves to `127.0.0.1`, no DNS setup needed.

### 1. Add all Helm repositories

```bash
helm repo add cellenza      https://ihsenalaya.github.io/cellenza-operator
helm repo add jetstack      https://charts.jetstack.io
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
helm repo update
```

### 2. Install cert-manager

```bash
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true \
  --wait
```

### 3. Install ingress-nginx

**For Kind clusters** (required — disables the admission webhook that causes x509 errors in Kind):

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.admissionWebhooks.enabled=false \
  --wait
```

**For production clusters** (keeps the admission webhook):

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --wait
```

> **Kind + admission webhook:** If you install without `--set controller.admissionWebhooks.enabled=false` and see `x509: certificate signed by unknown authority` errors, delete the webhook and reinstall:
> ```bash
> kubectl delete validatingwebhookconfiguration ingress-nginx-admission
> helm uninstall ingress-nginx -n ingress-nginx
> # Re-install with the flag above
> ```

For clusters without Kind port mappings:

```bash
kubectl port-forward -n ingress-nginx svc/ingress-nginx-controller 8080:80
```

### 4. Install OpenTelemetry Operator (optional)

Required only when using `telemetry.autoInstrumentation`.

```bash
helm install opentelemetry-operator open-telemetry/opentelemetry-operator \
  --namespace opentelemetry-operator-system \
  --create-namespace \
  --set "manager.collectorImage.repository=otel/opentelemetry-collector-contrib" \
  --set admissionWebhooks.certManager.enabled=true \
  --wait
```

### 5. Install Jaeger (optional)

Lightweight all-in-one Jaeger for local trace visualization.

```bash
helm install jaeger jaegertracing/jaeger \
  --namespace observability \
  --create-namespace \
  --set allInOne.enabled=true \
  --set provisionDataStore.cassandra=false \
  --set provisionDataStore.elasticsearch=false \
  --set storage.type=memory \
  --set agent.enabled=false \
  --set collector.enabled=false \
  --set query.enabled=false \
  --set allInOne.extraEnv[0].name=COLLECTOR_OTLP_ENABLED \
  --set allInOne.extraEnv[0].value="true" \
  --wait

kubectl apply -f https://raw.githubusercontent.com/ihsenalaya/cellenza-operator/main/demo-app/otel.yaml
kubectl port-forward -n observability svc/jaeger 16686:16686
# → http://localhost:16686
```

### 6. Install the operator

```bash
helm install cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system \
  --create-namespace \
  --wait
```

Verify:

```bash
kubectl get pods -n cellenza-operator-system
# cellenza-operator-647dc9db-xxxxx   1/1   Running   0   30s
```

### Install without webhooks (no cert-manager needed)

```bash
helm install cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system \
  --create-namespace \
  --set webhook.enabled=false
```

> Without webhooks, defaults are not applied at admission time and invalid specs are not rejected until reconciliation.

### Install via OCI (GHCR)

```bash
helm install cellenza-operator \
  oci://ghcr.io/ihsenalaya/charts/cellenza-operator \
  --version 0.13.1 \
  --namespace cellenza-operator-system \
  --create-namespace
```

---

## Usage

### Minimal example

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/my-feature
  prNumber: 42
  image: myapp:abc1234
```

```bash
kubectl apply -f pr-42.yaml
kubectl get cz pr-42 --watch
# NAME    PHASE          BRANCH              TIER     URL                              EXPIRES
# pr-42   Provisioning   feature/my-feature  medium   pr-42.preview.localtest.me       ...
# pr-42   Running        feature/my-feature  medium   pr-42.preview.localtest.me       2026-05-09T10:00:00Z
```

---

## Core reconciliation loop

The controller reconciles every `Cellenza` resource through a deterministic sequence:

```
1. Fetch the Cellenza object
2. Handle deletion (finalizer: clean up namespace + all child resources)
3. Add finalizer (first time only)
4. Detect spec changes → reset test/AI state for new generation
5. Check TTL expiry → auto-delete if expired
6. Check approval gate → stay Pending if requiresApproval=true and approvedBy is unset
7. Set ExpiresAt on first provision
8. Set phase → Provisioning
9. Reconcile child resources:
     a. Namespace
     b. ResourceQuota  (auto-extended for DB, AI, extra services, and E2E jobs)
     c. DB reset handling (if database.resetRequested=true)
     d. Database provisioning (Secret → Service → Deployment → migrate → seed)
     e. DB checkpoints (save / restore)
     f. Service deployments:
          • spec.services[]  → one Deployment + Service per entry (svc-<name>)
                               DB env vars injected into all; wait-for-postgres init container
          • spec.image       → single Deployment "app" + Service "app"
     g. Ingress:
          • multi-service → path-based routes (/api → svc-backend, / → svc-frontend)
          • single-service → pr-<N>.preview.localtest.me → app:80
     h. Wait for all deployments to reach minimum availability
10. Mark phase → Running; notify GitHub (deployment status → success, post PR comment)
11. Run test suite (smoke + regression + E2E in parallel)
12. Run AI enrichment (schema dump → generate → seed → tests)
13. Requeue before TTL expiry
```

Each step that fails transitions the environment to `Failed` and triggers automatic diagnostics collection.

---

## Ephemeral PostgreSQL

### Basic setup

```yaml
spec:
  database:
    enabled: true
    version: "15"
    databaseName: appdb
```

The operator:
1. Generates a unique username (`preview_42`) and a 64-character cryptographically-random hex password using `crypto/rand`
2. Stores them in `Secret` `postgres-credentials` in the PR namespace — **created once and never overwritten**
3. Starts a `postgres:15-alpine` deployment with `Recreate` strategy and `pg_isready` probes
4. Blocks the app pod with a `busybox` init container until PostgreSQL accepts TCP connections on port 5432
5. Injects all credentials into the app container as environment variables

**Injected environment variables:**

| Variable | Example value |
|---|---|
| `POSTGRES_USER` | `preview_42` |
| `POSTGRES_PASSWORD` | `a3f8c2...` (64-char hex) |
| `POSTGRES_DB` | `appdb` |
| `DATABASE_URL` | `postgresql://preview_42:a3f8c2...@postgres:5432/appdb?sslmode=disable` |

The demo image `ghcr.io/ihsenalaya/cellenza-demo-app:latest` logs database activity with a `[db]` prefix, for example:

```text
[db] Opening PostgreSQL connection database=appdb user=preview_42
[db] Initialized PostgreSQL schema table=messages
[db] Read messages from PostgreSQL rows=1
[db] Inserted message into PostgreSQL author=ihsen
```

Watch those logs with:

```bash
# Read credentials at any time
kubectl get secret postgres-credentials -n preview-pr-42 \
  -o jsonpath='{.data.DATABASE_URL}' | base64 -d
```

### Migrations and seeds

```yaml
spec:
  database:
    enabled: true
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
    seed:
      enabled: true
      command: ["python", "scripts/seed_preview.py"]
```

Migration and seed run as one-shot Kubernetes Jobs **before the app Deployment is reconciled**. Both jobs:

- Use `spec.image` by default (override with `image:` per task)
- Receive an init container (`busybox`) that waits for PostgreSQL to be ready
- Have all database environment variables automatically injected
- Block further provisioning until they succeed or fail

Example with a dedicated migration image:

```yaml
database:
  enabled: true
  databaseName: appdb
  migration:
    enabled: true
    image: ghcr.io/example/myapp-migrations:sha-abc123
    command: ["sh", "-c", "alembic upgrade head"]
  seed:
    enabled: true
    image: ghcr.io/example/myapp-migrations:sha-abc123
    command: ["sh", "-c", "python scripts/seed_preview.py"]
```

Check migration state:

```bash
kubectl get cellenza pr-42 \
  -o jsonpath='{.status.database.migration}{"\n"}{.status.database.seed}{"\n"}'
```

Inspect logs if a preview stays in `Provisioning`:

```bash
kubectl logs -n preview-pr-42 job/postgres-migrate
kubectl logs -n preview-pr-42 job/postgres-seed
```

### Database reset

Reset the database at any time (deletes migration and seed Jobs, clears DB status, re-runs both on next reconcile):

```bash
# Via kubectl
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"database":{"resetRequested":true}}}'

# Via Copilot Extension
@cellenza reset-db pr-42
```

The operator clears `resetRequested` automatically once the reset starts.

---

## Database checkpoints

Checkpoints allow you to save a point-in-time snapshot of the database state and restore it later. This is useful for reproducing test scenarios or resetting to a known good state during E2E testing.

### Save a checkpoint

```bash
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"database":{"checkpointSave":"before-order-flow"}}}'
```

The operator runs a `pg_dump --data-only` Job, reads the SQL from the pod logs, stores it in a ConfigMap `db-checkpoint-before-order-flow` in the preview namespace, then clears the field automatically.

### Restore a checkpoint

```bash
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"database":{"checkpointRestore":"before-order-flow"}}}'
```

The restore job TRUNCATEs all public tables (with `RESTART IDENTITY CASCADE`) then replays the saved SQL dump. The field is cleared automatically after completion.

### List available checkpoints

```bash
kubectl get cellenza pr-42 -o jsonpath='{.status.database.checkpoints}'
# ["before-order-flow","after-seed"]
```

### Checkpoint API for E2E tests

The E2E test Job receives a `CHECKPOINT_API` environment variable pointing to the Cellenza Extension API, enabling test scripts to trigger saves and restores programmatically:

```python
import os, requests
CHECKPOINT_API = os.environ.get("CHECKPOINT_API")

# Save state before a destructive test
requests.post(f"{CHECKPOINT_API}/checkpoint/save", json={"name": "before-delete"})

# Run the test...

# Restore to known state
requests.post(f"{CHECKPOINT_API}/checkpoint/restore", json={"name": "before-delete"})
```

> Checkpoint names must be lowercase alphanumeric with hyphens, max 48 characters.

---

## OpenTelemetry auto-instrumentation

When the OpenTelemetry Operator is installed, the controller can opt the application pod into zero-code auto-instrumentation via a single annotation — no changes to the application image required.

```yaml
spec:
  telemetry:
    enabled: true
    serviceName: cellenza-demo
    autoInstrumentation:
      language: python
      instrumentationRef: observability/python
```

The controller injects into the generated `Deployment`:

```yaml
spec:
  template:
    metadata:
      annotations:
        instrumentation.opentelemetry.io/inject-python: observability/python
    spec:
      containers:
      - name: app
        env:
        - name: OTEL_SERVICE_NAME
          value: cellenza-demo
        - name: OTEL_RESOURCE_ATTRIBUTES
          value: cellenza.name=demo,cellenza.pr_number=2,cellenza.branch=demo,k8s.namespace.name=preview-pr-2
```

**Supported languages:** `python`, `java`, `nodejs`, `dotnet`, `go`, `sdk`

**Python Alpine images:** set `pythonPlatform: musl` if using Alpine-based Python images.

**Go instrumentation:** set `goTargetExecutable: /app/myservice` — the path to the executable is required by the Go OTel Operator.

Access Jaeger traces:

```bash
kubectl port-forward -n observability svc/jaeger 16686:16686
# → http://localhost:16686  →  search service "cellenza-demo"
```

---

## GitHub integration

The operator bridges Kubernetes state to GitHub: it publishes deployment statuses and posts rich PR comments with diagnostics, test results, and AI enrichment summaries.

### Setup

**Step 1 — Create the GitHub Deployment from CI:**

```bash
DEPLOY_ID=$(gh api repos/OWNER/REPO/deployments \
  --method POST \
  --field ref="<commit-sha>" \
  --field environment="pr-42" \
  --field auto_merge=false \
  --jq '.id')
```

**Step 2 — Create the token Secret:**

```bash
kubectl create secret generic cellenza-github-token \
  --namespace=cellenza-operator-system \
  --from-literal=token="$GITHUB_PAT"
```

> Use a long-lived token. GitHub Actions `GITHUB_TOKEN` expires shortly after the workflow finishes while the preview and AI enrichment continue running.

**Step 3 — Reference from the Cellenza resource:**

```yaml
spec:
  github:
    enabled: true
    owner: acme
    repo: myapp
    deploymentId: 123456789
    environment: pr-42
    commentOnReady: true
    tokenSecretRef:
      name: cellenza-github-token
      namespace: cellenza-operator-system
      key: token
```

When the preview reaches `Running`, the controller:
- Sets the GitHub Deployment status to `success` with the environment URL
- Posts a PR comment with URL, DB state, migration/seed status, telemetry state, namespace, expiry, and AI health summary

When reconciliation fails, the controller posts a detailed failure comment — see [Diagnostics](#diagnostics).

---

## Diagnostics

On any failure, the controller automatically collects a structured diagnostic report and stores it in `status.diagnostics`. When GitHub integration is enabled, this is published as a PR comment.

The diagnostics include:

- **Reason** — machine-readable failure code (`DeploymentFailed`, `DatabaseMigrationFailed`, etc.)
- **Component** — the resource most likely responsible (`app`, `database`, `migration`, `seed`, `ingress`)
- **Root cause** — human-readable probable cause inferred from logs, events, and component state
- **Confidence** — `low`, `medium`, or `high`
- **Recommendations** — ordered list of developer-facing next actions
- **Significant logs** — high-signal lines grouped by component and container
- **Pod logs** — last 30 lines from the crashed application container
- **Last events** — recent Kubernetes warning events from the preview namespace
- **Debug commands** — ready-to-paste `kubectl` commands

**Example: ImagePullBackOff**

```markdown
## Cellenza Preview Failed

Environment: `pr-42` — Namespace: `preview-pr-42`

### Diagnosis
- Reason: `DeploymentFailed`
- Component: `app`
- Probable cause: **Container image cannot be pulled**
- Confidence: `high`

### Recommendations
1. Verify that image `ghcr.io/acme/myapp:does-not-exist` exists and is accessible from the cluster.
2. Check the image tag produced by CI for this pull request.
3. Confirm the namespace has the required imagePullSecrets if the registry is private.

### Recent Warning Events
- Pod/app-xyz: Error: ImagePullBackOff
- Pod/app-xyz: Failed to pull image "ghcr.io/acme/myapp:does-not-exist": not found

### Debug Commands
kubectl describe cellenza pr-42
kubectl get pods -n preview-pr-42
kubectl get events -n preview-pr-42 --sort-by=.lastTimestamp
```

**Example: database migration failure**

```markdown
### Diagnosis
- Reason: `DatabaseMigrationFailed`
- Component: `migration`
- Probable cause: **Database migration failed**
- Confidence: `high`

### Significant Logs
**migration** — `pod/postgres-migrate-abc container/migration`
ERROR relation messages already exists
migration failed at 003_create_messages.sql

### Recommendations
1. Check that the migration is idempotent — use CREATE TABLE IF NOT EXISTS or similar guards.
2. After fixing, reset the DB: kubectl patch cellenza pr-42 --type=merge -p '{"spec":{"database":{"resetRequested":true}}}'
```

---

## Automated test suite

The operator runs **smoke, regression, and E2E tests automatically** after the preview environment is ready — all three jobs run in parallel.

```yaml
spec:
  testSuite:
    enabled: true
    smoke: {}                    # built-in, no config needed
    regression:
      enabled: true
    e2e:
      enabled: true
```

### The three test jobs

**1. Smoke — operator-embedded, no files needed**

The smoke script is compiled into the operator binary. It launches a `python:3.12-slim` pod and runs:

```
GET /healthz       → expect 200
GET /api/products  → expect 200
```

**2. Regression — `tests/regression.py` from the app image**

Runs the application's own test file inside the app image against the live PostgreSQL instance. No extra image or configuration needed.

**3. E2E — `tests/e2e.py` via Playwright/Chromium**

Uses an init-container pattern to bridge the app image (which has the test code) with the Playwright runtime (which has Chromium):

```
Pod: e2e-tests
  ├── init container: copy-tests
  │     Image: <app image>
  │     Command: cp -R /app/tests/. /data/tests/
  │     Volume: emptyDir /data  (shared)
  │
  └── main container: e2e-tests
        Image: mcr.microsoft.com/playwright/python:v1.44.0-jammy
        Command: python /data/tests/e2e.py
        Env: APP_URL=http://app:80, PREVIEW_URL=<ingress URL>
             CHECKPOINT_API=http://cellenza-extension.../api/previews/pr-42
        Resources: 200m–1 CPU, 512Mi–1Gi RAM  (Chromium needs headroom)
        Volume: emptyDir /data  (shared)
```

The `CHECKPOINT_API` env var lets E2E tests programmatically save and restore DB snapshots during the test run.

### Required: test scripts in the app image

```dockerfile
COPY tests/ ./tests/
```

Output lines starting with `PASS` or `FAIL` are parsed by the operator for counters:

```python
# tests/regression.py — minimal example
import requests, sys, os
BASE = os.environ.get("APP_URL", "http://app:80")

tests = [("health", "/healthz", 200), ("products", "/api/products", 200)]
passed, failed = 0, 0
for name, path, code in tests:
    r = requests.get(BASE + path, timeout=10)
    ok = r.status_code == code
    print(f"{'PASS' if ok else 'FAIL'} regression {name}: {r.status_code}")
    if ok: passed += 1
    else: failed += 1
print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed else 0)
```

```python
# tests/e2e.py — minimal Playwright example
import os, sys
from playwright.sync_api import sync_playwright

BASE = os.environ.get("APP_URL", "http://app:80")
passed, failed = 0, 0

with sync_playwright() as p:
    browser = p.chromium.launch(args=["--no-sandbox", "--disable-dev-shm-usage"])
    page = browser.new_page()
    try:
        page.goto(BASE, wait_until="networkidle", timeout=30000)
        assert page.title() != ""
        print("PASS e2e homepage")
        passed += 1
    except Exception as e:
        print(f"FAIL e2e homepage: {e}")
        failed += 1
    finally:
        browser.close()

print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
```

### GitHub PR comment produced

```
## Cellenza Test Suite Results

**Overall: ✅ Succeeded**

| Suite      | Status        | Passed | Failed |
|------------|---------------|--------|--------|
| Smoke      | ✅ Succeeded  | 2      | 0      |
| Regression | ✅ Succeeded  | 9      | 0      |
| E2E        | ✅ Succeeded  | 6      | 0      |
```

### Check test status

```bash
kubectl get cz pr-42 -o jsonpath='{.status.tests}' | jq .
kubectl get cz pr-42 -o jsonpath='{range .status.tests.e2e.output[*]}{@}{"\n"}{end}'
```

### Comparison: Cellenza vs classic staging

| Capability | Classic staging | Cellenza preview |
|-----------|----------------|-----------------|
| Isolated per PR | ❌ shared state | ✅ dedicated namespace |
| Real database | ⚠️ often mocked | ✅ live PostgreSQL |
| Contextual seed data | ❌ generic fixtures | ✅ AI-generated from PR diff |
| Parallel PRs | ❌ flaky | ✅ no pollution between PRs |
| Results on PR | manual | ✅ automatic comment |
| E2E with real browser | rare | ✅ Playwright/Chromium |

---

## AI enrichment

After the preview reaches `Running`, the operator automatically generates context-aware seed data and integration tests by sending the PR diff and the live database schema to an AI provider.

### Setup

**Step 1 — Create the API key Secret:**

```bash
# OpenAI
kubectl create secret generic ai-api-key \
  --namespace=cellenza-operator-system \
  --from-literal=api-key=sk-...

# GitHub Models (free tier)
kubectl create secret generic ai-api-key \
  --namespace=cellenza-operator-system \
  --from-literal=api-key="$GITHUB_TOKEN"

helm upgrade cellenza-operator cellenza/cellenza-operator \
  --set ai.apiURL=https://models.inference.ai.azure.com
```

**Step 2 — Enable in the Cellenza resource:**

```yaml
spec:
  aiEnrichment:
    enabled: true
    apiSecretRef:
      name: ai-api-key
      key: api-key
    githubTokenSecretRef:          # optional: dedicated token for PR diff fetching
      name: cellenza-github-token
      namespace: cellenza-operator-system
      key: token
    model: gpt-4o-mini
    seed:
      enabled: true                # default: true
    tests:
      enabled: true                # default: true
```

### What happens internally

```
Preview Running
      │
      ▼
Job ai-schema-dump
  pg_dump --schema-only → pod stdout → operator reads logs → ConfigMap ai-enrichment (schema key)
  (waits for migration to complete so schema reflects post-migration state)
      │
      ▼
Operator calls AI API with:
  • PR diff          → fetched from GitHub API (application/vnd.github.diff)
  • DB schema        → from ConfigMap ai-enrichment
  • App URL          → http://app:80
  • Branch + PR №
  • System prompt    → from ConfigMap ai-prompt-template (Helm-managed, overrideable)
  • Extra instructions → from ConfigMap ai-prompt-<name> (per-environment, optional)
      │
      ▼
AI generates:
  • seed.sql  → realistic INSERT statements, schema-aware, referentially correct
  • test.py   → integration tests targeting modified code paths in the PR diff
      │
      ▼
Job ai-seed   → psql -h postgres -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /data/seed.sql
Job ai-tests  → pip install requests && python /data/test.py
  (APP_URL=http://app:80 injected into ai-tests)
      │
      ▼
Results → status.aiEnrichment.testResults[] and PR comment
```

### Monitor enrichment state

```bash
kubectl get cz pr-42 -o jsonpath='{.status.aiEnrichment}' | jq .
```

```json
{
  "phase": "Succeeded",
  "seedStatus": "Succeeded",
  "testsStatus": "Succeeded",
  "testResults": [
    "PASS: test_health",
    "PASS: test_create_product",
    "FAIL: test_order_stock — 409 expected"
  ],
  "completedAt": "2026-05-01T12:00:00Z"
}
```

### Re-trigger enrichment

```bash
# Via kubectl
kubectl patch cz pr-42 --type=json \
  -p='[{"op":"remove","path":"/status/aiEnrichment"}]'

# Via Copilot Extension
@cellenza retest-ai pr-42
```

### Per-environment AI prompt customization

Store custom instructions for this environment in the cluster without touching the operator:

```bash
# Via Copilot Extension
@cellenza set-prompt pr-42 Generate at least 15 products across 5 categories. Only test /api/ endpoints.
```

The extension creates a ConfigMap `ai-prompt-pr-42` in `cellenza-operator-system`. The operator reads it automatically on the next enrichment. Run `@cellenza enrich pr-42` afterward to regenerate with the new instructions.

To check current instructions:

```
@cellenza show-prompt pr-42
```

The prompt ConfigMap follows the Cellenza lifecycle — it is automatically deleted when the environment is removed.

To update via `kubectl` directly:

```bash
kubectl create configmap ai-prompt-pr-42 \
  --namespace=cellenza-operator-system \
  --from-literal=instructions="Generate at least 15 products across 5 categories." \
  --dry-run=client -o yaml | kubectl apply -f -
```

#### 7. Customize the default AI system prompt via Helm

The chart now ships a default system prompt file at `charts/cellenza-operator/files/ai-system-prompt.txt`
and renders it into a ConfigMap named `ai-prompt-template` in the operator namespace.

You can override that prompt globally without changing operator code:

```bash
helm upgrade --install cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system \
  --create-namespace \
  --set-file ai.systemPrompt=./my-ai-system-prompt.txt \
  --wait
```

Or store the prompt directly in your values file:

```yaml
ai:
  apiURL: "https://models.inference.ai.azure.com"
  systemPrompt: |
    You are a developer tool for preview environments.
    Only test JSON endpoints under /api/.
    Prefer 201 Created for successful resource-creation endpoints when the diff shows creation semantics.
```

Per-environment overrides created with `@cellenza set-prompt ...` are still supported. They are appended
as additional instructions on top of the default Helm-managed system prompt.

If the Helm ConfigMap is missing, the operator falls back to its built-in default prompt so manual or
older deployments keep working.

The prompt ConfigMap is automatically deleted when the environment is removed.

### Global AI prompt customization via Helm

```bash
helm upgrade --install cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system \
  --set-file ai.systemPrompt=./my-ai-system-prompt.txt
```

Per-environment overrides are appended on top of the global Helm-managed prompt. If the Helm ConfigMap is missing, the operator falls back to its built-in default prompt.

---

## Approval gate

Block provisioning of sensitive or large environments until a human explicitly approves them.

```yaml
spec:
  requiresApproval: true
  resourceTier: large     # large always forces requiresApproval=true (enforced by webhook)
```

The environment stays in `Pending` phase and the operator requeues every 30 seconds to check for approval. Once `approvedBy` is set, it transitions to `Provisioning`:

```bash
kubectl patch cellenza pr-99 --type=merge \
  -p '{"spec":{"approvedBy":"ihsenalaya"}}'
```

---

## Resource tiers and quota management

```yaml
spec:
  resourceTier: medium    # small | medium | large
```

| Tier | CPU request | CPU limit | Memory request | Memory limit |
|---|---|---|---|---|
| `small` | 100m | 250m | 128Mi | 256Mi |
| `medium` | 200m | 500m | 256Mi | 512Mi |
| `large` | 500m | 2000m | 512Mi | 2Gi |

The operator automatically extends the namespace `ResourceQuota` based on what is enabled:

| Add-on | Extra CPU limit | Extra RAM limit |
|---|---|---|
| `database.enabled=true` | +500m | +512Mi |
| `aiEnrichment.enabled=true` | +500m | +512Mi |
| `testSuite.enabled=true` (smoke + regression) | +1000m | +1Gi |
| `testSuite.enabled=true` (E2E / Playwright) | +1000m | +1Gi |

---

## Lifecycle

```
                 requiresApproval=true
                 and approvedBy not set
                        │
Created ──► Pending ────┘
                │
                │ (approved or requiresApproval=false)
                ▼
          Provisioning  (namespace, quota, deployment, service, ingress created)
                │
         ┌──────┴──────┐
         ▼             ▼
       Running        Failed  (image pull error, quota exceeded, migration failure, etc.)
         │
         │ TTL expired or kubectl delete
         ▼
      Terminating  (finalizer: all child resources deleted, namespace removed)
```

### Status fields

```bash
kubectl describe cellenza pr-42
```

| Field | Description |
|---|---|
| `status.phase` | Current lifecycle phase: `Pending`, `Provisioning`, `Running`, `Failed`, `Terminating` |
| `status.url` | Ingress URL of the preview environment |
| `status.expiresAt` | Timestamp of auto-deletion |
| `status.namespaceName` | Dedicated namespace (`preview-pr-<N>`) |
| `status.readyAt` | Timestamp of first `Running` transition |
| `status.databaseSecretName` | Name of the PostgreSQL credentials Secret |
| `status.database.ready` | PostgreSQL + migration + seed all complete |
| `status.database.migration` | `Skipped` / `Running` / `Succeeded` / `Failed` |
| `status.database.seed` | `Skipped` / `Running` / `Succeeded` / `Failed` |
| `status.database.checkpoints` | List of saved checkpoint names |
| `status.diagnostics.reason` | Machine-readable failure reason |
| `status.diagnostics.component` | Component most likely responsible |
| `status.diagnostics.rootCause` | Human-readable probable cause |
| `status.diagnostics.confidence` | `low`, `medium`, or `high` |
| `status.diagnostics.recommendations` | Developer-facing suggested actions |
| `status.diagnostics.significantLogs` | High-signal log lines grouped by component |
| `status.diagnostics.podLogs` | Last 30 lines from the crashed app container |
| `status.diagnostics.lastEvents` | Recent warning events from the preview namespace |
| `status.diagnostics.debugCommands` | Ready-to-paste `kubectl` commands |
| `status.github.deploymentState` | Last GitHub Deployment state emitted |
| `status.github.commentId` | PR comment id created by the controller |
| `status.github.lastError` | Latest non-blocking GitHub notification error |
| `status.aiEnrichment.phase` | `Pending` / `Generating` / `Running` / `Succeeded` / `Failed` |
| `status.aiEnrichment.seedStatus` | AI seed job state |
| `status.aiEnrichment.testsStatus` | AI tests job state |
| `status.aiEnrichment.testResults` | `PASS: …` / `FAIL: …` lines from AI tests output |
| `status.aiEnrichment.summary` | Human-readable enrichment summary |
| `status.tests.phase` | Overall test suite phase |
| `status.tests.smoke` | Smoke test result (phase, passed, failed, output) |
| `status.tests.regression` | Regression test result |
| `status.tests.e2e` | E2E test result |
| `status.conditions` | Standard conditions: `Ready`, `Approved`, `Expired`, `DatabaseReady`, `MigrationReady`, `SeedReady`, `AIEnrichmentReady`, `TestSuiteReady` |

---

## GitHub Copilot Extension

The **Cellenza Extension** is a companion server that surfaces preview environment management directly inside GitHub Copilot Chat — no `kubectl` access needed for developers.

```
Developer → @cellenza status pr-42
                │
                ▼
         GitHub Copilot Chat
                │  POST webhook (OpenAI SSE streaming)
                ▼
     cellenza-extension server  (port 8090)
                │  controller-runtime + kubernetes client
                ▼
        Kubernetes API Server
                │
                ▼
         Cellenza CR → response streamed back to Copilot
```

### Available commands

All responses are in French. Arguments accept `pr-42`, `42`, or `#42`.

| Command | Description |
|---|---|
| `@cellenza list` | Lists all active environments with phase, branch, and TTL remaining |
| `@cellenza status pr-42` | Phase, URL, branch, tier, replicas, uptime, TTL, DB state, GitHub state, crash diagnostics |
| `@cellenza logs pr-42` | Last 40 lines from the app pod; falls back to `status.diagnostics.podLogs` when pod is gone |
| `@cellenza extend pr-42 [24h]` | Extends TTL by the given duration (default `24h`) — immediate effect |
| `@cellenza wake pr-42` | Sets `spec.replicas` to `1` to restart a scaled-down environment |
| `@cellenza reset-db pr-42` | Sets `spec.database.resetRequested: true` — operator deletes migration/seed jobs and re-runs them on next reconcile |
| `@cellenza enrich pr-42` | Resets AI enrichment state, deletes generated artifacts, and asks the operator to regenerate seed + tests |
| `@cellenza set-prompt pr-42 <instructions>` | Stores custom AI instructions for this environment in the cluster (no operator redeploy needed) |
| `@cellenza show-prompt pr-42` | Displays the current custom prompt for this environment |
| `@cellenza help` | Shows the command list |

### RBAC granted to the extension

| Resource | Verbs |
|---|---|
| `cellenzas` | `get`, `list`, `watch`, `patch`, `update` |
| `cellenzas/status` | `get`, `patch`, `update` |
| `pods` | `get`, `list`, `watch` |
| `pods/log` | `get` |
| `configmaps` (namespace `cellenza-operator-system`) | `get`, `create`, `patch`, `update`, `delete` |

### Setup

**Step 1 — Create the GitHub App**

Go to **Settings → Developer settings → GitHub Apps → New GitHub App** and fill in:

| Field | Value |
|---|---|
| GitHub App name | `cellenza-extension` |
| Webhook secret | `openssl rand -hex 32` |
| Permissions → Repository → Pull requests | Read-only |
| Permissions → Account → Copilot Chat | Read-only |
| Where can this be installed? | Only on this account |

**Step 2 — Create the webhook secret in the cluster:**

```bash
kubectl create secret generic cellenza-extension-secret \
  --namespace=cellenza-operator-system \
  --from-literal=webhook-secret="$WEBHOOK_SECRET"
```

> The webhook secret is `optional` in the deployment — the extension starts without it, but GitHub webhook validation will be skipped. Set it for production use.

#### 3. Verify the extension image is available

The extension image is built automatically by CI on every push to `main` or on version tags:

```text
ghcr.io/ihsenalaya/cellenza-extension:latest
ghcr.io/ihsenalaya/cellenza-extension:<version>
```

To build it locally:

```bash
docker build -f Dockerfile.extension -t cellenza-extension:local .
kind load docker-image cellenza-extension:local --name cellenza-test
```

Then update the image in `config/extension/deployment.yaml` before applying.

#### 4. Deploy the extension

```bash
kubectl apply -f config/extension/rbac.yaml
kubectl apply -f config/extension/deployment.yaml
kubectl -n cellenza-operator-system rollout status deployment/cellenza-extension --timeout=60s
```

**Step 4 — Expose locally with ngrok:**

```bash
kubectl port-forward -n cellenza-operator-system svc/cellenza-extension 8090:8090 &
ngrok http 8090
# → Forwarding: https://abc123.ngrok-free.app → http://localhost:8090
```

Copy the `https://` ngrok URL and paste it as **Webhook URL** in your GitHub App settings (append `/` — the extension serves at the root path).

#### 6. Enable Copilot Chat for the App

In your GitHub App settings → **Copilot** tab:
- Set **App type** to `Agent`
- Set **Inference description** to something like `Manage Cellenza preview environments`
- Save

Now open GitHub Copilot Chat in any repository where the App is installed and type `@cellenza help` to verify the connection.

### Trigger a database reset from Copilot Chat

```
@cellenza reset-db pr-42
```

The extension patches `spec.database.resetRequested: true`. The controller detects this on the next reconcile loop, deletes both migration and seed jobs, clears `status.database`, and re-runs the full database setup sequence. The flag is cleared automatically once the reset starts.

### Relaunch AI enrichment from Copilot Chat

```
@cellenza enrich pr-42
```

The extension clears `status.aiEnrichment`, deletes `ai-enrichment`, `ai-seed`, `ai-tests`, and `ai-schema-dump` in the preview namespace, then lets the operator regenerate seed and tests on the next reconcile.

### Customize AI instructions without redeploying

```
@cellenza set-prompt pr-42 Only test /api/ endpoints. Generate 20 products with realistic prices.
@cellenza enrich pr-42
```

`set-prompt` creates a ConfigMap `ai-prompt-pr-42` in `cellenza-operator-system`. The operator reads it automatically when generating seed and tests. No operator redeploy needed — the instructions take effect on the next `enrich` call. The ConfigMap is deleted automatically when the environment is removed.

```
@cellenza show-prompt pr-42
```

Returns the current custom instructions for that environment.

### Read pod logs from a failed environment

```
@cellenza logs pr-42
```

If the pod is running, the extension streams the last 40 live lines. If the environment is in `Failed` phase, it falls back to `status.diagnostics.podLogs` (last 30 lines captured by the controller at failure time).

### Full example — diagnosing a crash from Copilot Chat

A developer notices their preview is stuck. They open Copilot Chat and type:

```
@cellenza status pr-42
```

The extension responds in French with the current phase, DB status, and diagnostics:

```
## ❌ pr-42 — Failed

**Branch:** `feature/my-feature`
**Tier:** `medium`
**Replicas:** 1
**TTL:** expire dans 23h45m

**Erreur:** Deployment app is unavailable: Deployment does not have minimum availability.

**Derniers logs:**
```
Error: ImagePullBackOff
Failed to pull image "ghcr.io/acme/myapp:does-not-exist": not found
```

---
`@cellenza logs pr-42` · `@cellenza extend pr-42` · `@cellenza reset-db pr-42` · `@cellenza enrich pr-42` · `@cellenza set-prompt pr-42 <instructions>`
```

They can request the raw pod logs:

```
@cellenza logs pr-42
```

```
**Logs — pr-42** (namespace: `preview-pr-42`)

[db] Opening PostgreSQL connection database=appdb user=preview_42
Error: connection refused
```

Or trigger a database reset after fixing a migration:

```
@cellenza reset-db pr-42
```

```
**Reset DB lancé** pour `pr-42`

L'opérateur va:
1. Supprimer les jobs migration et seed
2. Recréer la base de données
3. Rejouer les migrations
4. Rejouer le seed

Suivi: `@cellenza status pr-42`
```

The controller patches `spec.database.resetRequested: true`, deletes the failed Jobs, and re-runs migration and seed on the next reconcile cycle.

> **Note:** The extension responds in French. The `kubectl` debug commands shown in PR comments are generated by the **operator** (in `status.diagnostics.debugCommands`), not by the extension.

### Testing crash scenarios locally

To validate the diagnostics and GitHub comment flow against a real PR:

```bash
# 1. Create a GitHub Deployment for the target PR
DEPLOY_ID=$(gh api repos/OWNER/REPO/deployments \
  --method POST \
  --field ref="<branch-or-sha>" \
  --field environment="pr-<N>-crash-test" \
  --field auto_merge=false \
  --jq '.id')

# 2. Create a token Secret
kubectl create secret generic github-token-crash-test \
  --namespace=cellenza-operator-system \
  --from-literal=token="$GITHUB_TOKEN"

# 3. Apply a Cellenza with a non-existent image to trigger ImagePullBackOff
kubectl apply -f - <<EOF
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-77
spec:
  branch: feature/new-auth
  prNumber: 77
  image: myapp:abc1234
  resourceTier: medium
  ttl: 48h
  requiresApproval: true
  database:
    enabled: true
    owner: OWNER
    repo: REPO
    deploymentId: $DEPLOY_ID
    environment: pr-<N>-crash-test
    commentOnReady: true
    tokenSecretRef:
      name: github-token-crash-test
      namespace: cellenza-operator-system
      key: token
EOF

# 4. Watch the phase move to Failed and the comment appear on the PR
kubectl get cellenza pr-<N>-crash --watch

# 5. Clean up
kubectl delete cellenza pr-<N>-crash
```

Within ~30 seconds the controller detects `ErrImagePull`, collects diagnostics, posts a `failure` GitHub Deployment status, and comments on the PR with the root cause and recommendations.

---

## Demo app

The repository ships a ready-to-use Flask demo app (`demo-app/`) that showcases the full operator feature set: PostgreSQL integration, OTel auto-instrumentation, and live environment variables.

### What it does

- Connects to the PostgreSQL instance provisioned by the operator using the injected `DATABASE_URL` env var
- Creates a `messages` table on startup (`CREATE TABLE IF NOT EXISTS`)
- Exposes a simple UI where you can post and read messages
- Displays all operator-injected env vars (`POSTGRES_USER`, `POSTGRES_DB`, `PREVIEW_BRANCH`, `PREVIEW_PR`, `ENVIRONMENT`)
- Exposes `/healthz` for the readiness probe

> **More advanced demo:** [ihsenalaya/idp-testing](https://github.com/ihsenalaya/idp-testing) is a full product catalogue app (categories, products, reviews, orders) that showcases AI enrichment — the AI generates real product names, prices, discounts, and star ratings from the PR diff and DB schema.

### Image

Built automatically on every push to `main` or version tag:

```text
ghcr.io/ihsenalaya/cellenza-demo-app:latest
```

### Deploy with the demo Cellenza manifest

```bash
kubectl patch cellenza pr-77 --type=merge \
  -p '{"spec":{"approvedBy":"ihsenalaya"}}'
```

### Full-stack with OpenTelemetry

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: demo
spec:
  branch: demo
  prNumber: 2
  image: ghcr.io/ihsenalaya/cellenza-demo-app:0.5.1
  resourceTier: small
  ttl: 72h
  database:
    enabled: true
    version: "15"
    databaseName: appdb
  telemetry:
    enabled: true
    serviceName: cellenza-demo
    autoInstrumentation:
      language: python
      instrumentationRef: observability/python
```

### Full-stack with GitHub integration, tests, and AI enrichment

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/product-catalogue
  prNumber: 42
  image: ghcr.io/acme/myapp:sha-abc123
  resourceTier: medium
  ttl: 48h
  database:
    enabled: true
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
  github:
    enabled: true
    owner: acme
    repo: myapp
    deploymentId: 123456789
    commentOnReady: true
    tokenSecretRef:
      name: cellenza-github-token
      namespace: cellenza-operator-system
      key: token
  testSuite:
    enabled: true
    regression:
      enabled: true
    e2e:
      enabled: true
  aiEnrichment:
    enabled: true
    apiSecretRef:
      name: ai-api-key
      key: api-key
    model: gpt-4o-mini
```

### Frontend + Backend + Database

Use `spec.services` to deploy multiple containers in one environment. Each service gets its own Deployment, ClusterIP Service, and ingress path. `spec.image` is ignored when `services` is set.

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/product-catalogue
  prNumber: 42
  image: "" # ignored when spec.services is set
  resourceTier: medium
  ttl: 48h
  database:
    enabled: true
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
  services:
    - name: backend
      image: ghcr.io/acme/myapp-api:sha-abc123
      port: 8080
      pathPrefix: /api
      env:
        - name: LOG_LEVEL
          value: debug
    - name: frontend
      image: ghcr.io/acme/myapp-ui:sha-abc123
      port: 3000
      pathPrefix: /
      env:
        - name: VITE_API_URL
          value: http://pr-42.preview.localtest.me/api
```

**What the operator creates:**

| Resource | Name | Details |
|---|---|---|
| `Deployment` | `svc-backend` | Runs the API container on port 8080 |
| `Service` | `svc-backend` | ClusterIP, selects `app: svc-backend` |
| `Deployment` | `svc-frontend` | Runs the UI container on port 3000 |
| `Service` | `svc-frontend` | ClusterIP, selects `app: svc-frontend` |
| `Ingress` | `app` | Routes `/api` → `svc-backend:8080`, `/` → `svc-frontend:3000` |

The ingress routes by path prefix — longer prefixes match first, so `/api/products` hits the backend before the frontend catches `/`.

**Database credentials** are injected into every service as env vars (`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `DATABASE_URL`). Each service also gets a `wait-for-postgres` init container so neither the frontend nor the backend starts before PostgreSQL is accepting connections.

**All add-ons continue to work in multi-service mode:**

| Add-on | Behavior |
|---|---|
| PostgreSQL | Sidecar created as usual; credentials auto-injected to all services |
| AI enrichment | `APP_URL` points to the first service in the list |
| Smoke tests | `APP_URL` env var injected — script reads it automatically |
| Regression/E2E tests | Use the first service's image and URL |
| Telemetry | Pod annotations applied to all service deployments |
| Resource quota | Tier headroom added for each additional service |

**`ServiceSpec` fields:**

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | required | Unique name — Deployment and Service are called `svc-<name>` |
| `image` | string | required | Container image to deploy |
| `port` | int32 | `80` | Container port |
| `pathPrefix` | string | — | URL path routed to this service (e.g. `/api`, `/`). Omit to deploy without ingress exposure |
| `replicas` | int32 | `spec.replicas` | Pod replicas for this specific service |
| `env` | EnvVar[] | — | Additional environment variables injected into this service's container |

### Complete example — all features

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  # ── Identity ────────────────────────────────────────────────────────────────
  branch: feature/my-feature
  prNumber: 42
  image: unused               # required by webhook; ignored when spec.services[] is set
  ttl: 48h
  resourceTier: medium        # small | medium | large
  replicas: 1

  # ── Approval gate (optional) ─────────────────────────────────────────────────
  requiresApproval: false
  # approvedBy: platform-team   # unblocks provisioning when requiresApproval: true

  # ── Multi-service — frontend + backend ──────────────────────────────────────
  services:
    - name: backend
      image: ghcr.io/acme/myapp:sha-abc123
      port: 8080
      pathPrefix: /api          # ingress: /api/* → svc-backend:8080
    - name: frontend
      image: ghcr.io/acme/myapp:sha-abc123
      port: 3000
      pathPrefix: /             # ingress: /* → svc-frontend:3000
      env:
        - name: APP_MODE
          value: frontend
        - name: PREVIEW_PR
          value: "42"
        - name: PREVIEW_BRANCH
          value: feature/my-feature

  # ── Ephemeral PostgreSQL ─────────────────────────────────────────────────────
  database:
    enabled: true
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
    seed:
      enabled: true
      command: ["python", "scripts/seed_preview.py"]

  # ── OpenTelemetry auto-instrumentation ──────────────────────────────────────
  telemetry:
    enabled: true
    serviceName: myapp-pr-42
    autoInstrumentation:
      language: python          # python | java | nodejs | dotnet | go
      instrumentationRef: observability/python

  # ── Operator-orchestrated test suite ────────────────────────────────────────
  testSuite:
    enabled: true
    smoke: {}                   # built-in: GET /healthz + GET /api/products
    regression:
      enabled: true             # runs tests/regression.py from the app image
    e2e:
      enabled: true             # runs tests/e2e.py via Playwright/Chromium

  # ── AI enrichment ────────────────────────────────────────────────────────────
  aiEnrichment:
    enabled: true
    apiSecretRef:
      name: ai-api-key          # kubectl create secret generic ai-api-key --from-literal=api-key=...
      key: api-key
    model: gpt-4o-mini          # gpt-4o-mini (fast) | gpt-4o (better quality)
    seed:
      enabled: true             # runs ai-seed Job: psql -f seed.sql
    tests:
      enabled: true             # runs ai-tests Job: python test.py

  # ── GitHub Deployment + PR comments ─────────────────────────────────────────
  github:
    enabled: true
    owner: acme
    repo: myapp
    deploymentId: 123456789     # returned by github.rest.repos.createDeployment()
    environment: pr-42
    commentOnReady: true
    tokenSecretRef:
      name: cellenza-github-token
      namespace: cellenza-operator-system
      key: token
```

**What the operator creates from this CR:**

```
Namespace        preview-pr-42
ResourceQuota    cellenza-quota         (medium + DB + AI + E2E headroom)
Secret           postgres-credentials   (crypto-random, immutable)
Deployment       postgres               pg:15-alpine
Service          postgres               ClusterIP :5432
Job              postgres-migrate       alembic upgrade head
Job              postgres-seed          seed_preview.py
Deployment       svc-backend            api image, port 8080, /healthz probe, DB env vars
Service          svc-backend            ClusterIP :8080
Deployment       svc-frontend           ui image, port 3000, APP_MODE=frontend
Service          svc-frontend           ClusterIP :3000
Ingress          app                    /api → svc-backend | / → svc-frontend
Job              smoke-tests            built-in script (python:3.12-slim)
Job              regression-tests       tests/regression.py
Job              e2e-tests              tests/e2e.py + Playwright init-container
Job              ai-schema-dump         pg_dump --schema-only → ConfigMap
ConfigMap        ai-enrichment          seed.sql + test.py (AI-generated)
Job              ai-seed                psql -f /data/seed.sql
Job              ai-tests               python /data/test.py
```

### Load testing with multiple replicas

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-55-loadtest
spec:
  branch: perf/connection-pool
  prNumber: 55
  image: myapp:perf-build
  resourceTier: medium
  replicas: 3
  ttl: 24h
```

---

## Useful commands

```bash
# List all preview environments
kubectl get cellenza
kubectl get cz  # shortname

# Watch a specific environment
kubectl get cz pr-42 --watch

# Approve an environment
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"approvedBy":"your-username"}}'

# Delete an environment (triggers immediate cleanup)
kubectl delete cellenza pr-42

# Reset the database (re-runs migration + seed)
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"database":{"resetRequested":true}}}'

# Save a DB checkpoint
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"database":{"checkpointSave":"before-my-test"}}}'

# Restore a DB checkpoint
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"database":{"checkpointRestore":"before-my-test"}}}'

# Read DB credentials
kubectl get secret postgres-credentials -n preview-pr-42 \
  -o jsonpath='{.data.DATABASE_URL}' | base64 -d

# Read pod logs captured at failure time
kubectl get cz pr-42 -o jsonpath='{.status.diagnostics.podLogs}' | jq .

# Check AI enrichment state
kubectl get cz pr-42 -o jsonpath='{.status.aiEnrichment}' | jq .

# Check test suite results
kubectl get cz pr-42 -o jsonpath='{.status.tests}' | jq .

# Watch controller logs
kubectl logs -n cellenza-operator-system deployment/cellenza-operator -f
```

---

## Helm values reference

```yaml
replicaCount: 1

image:
  repository: ghcr.io/ihsenalaya/cellenza-operator
  pullPolicy: IfNotPresent
  tag: ""                       # defaults to Chart.appVersion

ai:
  apiURL: "https://models.inference.ai.azure.com"  # GitHub Models (free tier); use https://api.openai.com/v1 for OpenAI

imagePullSecrets: []
# - name: ghcr-pull-secret

nameOverride: ""
fullnameOverride: ""

serviceAccount:
  name: ""

leaderElect: true               # set false for single-node dev clusters

resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

webhook:
  enabled: true                 # requires cert-manager; set false to skip TLS setup
  port: 9443

certManager:
  enabled: true
  issuerName: ""                # auto-creates a self-signed issuer when empty

metrics:
  port: 8443

nodeSelector: {}
tolerations: []
affinity: {}
```

---

## Demo app

The repository ships a ready-to-use Flask demo app (`demo-app/`) that showcases the full feature set.

```bash
kubectl apply -f demo-app/cellenza-demo.yaml
kubectl get cellenza demo --watch

kubectl port-forward -n ingress-nginx svc/ingress-nginx-controller 8080:80
# → http://pr-1.preview.localtest.me:8080

kubectl logs -n preview-pr-1 deployment/app -c app -f
# [db] Opening PostgreSQL connection database=appdb user=preview_1
# [db] Initialized PostgreSQL schema table=messages
# [db] Inserted message into PostgreSQL author=ihsen
```

> **More advanced demo:** [ihsenalaya/idp-testing](https://github.com/ihsenalaya/idp-testing) is a full product catalogue app (categories, products, reviews, orders) that showcases AI enrichment — the AI generates real product names, prices, discounts, and star ratings from the PR diff and DB schema.

---

## Architecture

```
cellenza-operator/
├── api/v1alpha1/
│   └── cellenza_types.go          CRD schema: CellenzaSpec, CellenzaStatus, all nested types
├── cmd/
│   ├── main.go                    Operator entry point
│   └── extension/main.go          Copilot Extension server entry point
├── internal/
│   ├── controller/
│   │   ├── cellenza_controller.go  Main reconcile loop: namespace, quota, DB, deployment, ingress
│   │   ├── ai_enrichment.go        AI schema dump, generation, seed job, test job
│   │   ├── checkpoint.go           DB checkpoint save/restore via pg_dump / psql
│   │   ├── diagnostics.go          Failure root-cause analysis and log collection
│   │   ├── github.go               GitHub Deployment status + PR comment publishing
│   │   └── tests.go                Smoke, regression, and E2E job orchestration
│   ├── extension/
│   │   ├── server.go               HTTP server, GitHub webhook validation, SSE streaming
│   │   ├── commands.go             @cellenza command implementations
│   │   └── checkpoint_api.go       REST API for E2E test checkpoint integration
│   └── webhook/v1alpha1/
│       └── cellenza_webhook.go     Defaulter + Validator admission webhooks
├── charts/
│   └── cellenza-operator/          Helm chart for distribution
│       └── files/ai-system-prompt.txt  Default AI system prompt
└── .github/workflows/              CI: lint, test, e2e, docker release, helm release
```

**Child resources managed per Cellenza:**

| Resource | Name | Notes |
|---|---|---|
| `Namespace` | `preview-pr-<N>` | Isolated per PR, labelled by branch and PR number |
| `ResourceQuota` | `cellenza-quota` | Enforces tier limits; auto-extended for DB, AI, E2E |
| `Secret` | `postgres-credentials` | Crypto-random credentials — created once, never overwritten |
| `Deployment` | `postgres` | `Recreate` strategy, `pg_isready` probes |
| `Service` | `postgres` | ClusterIP :5432, DNS `postgres` within the namespace |
| `Job` | `postgres-migrate` | Optional; injects DB env vars; init container waits for Postgres |
| `Job` | `postgres-seed` | Optional; same as migration |
| `Job` | `checkpoint-save-<name>` | `pg_dump --data-only` → ConfigMap |
| `Job` | `checkpoint-restore-<name>` | TRUNCATE + `psql -f /data/dump.sql` |
| `Deployment` | `app` | `Recreate` strategy; `busybox` init container; OTel annotations |
| `Service` | `app` | ClusterIP :80 |
| `Ingress` | `app` | `pr-<N>.preview.localtest.me` via nginx |
| `Job` | `ai-schema-dump` | `pg_dump --schema-only` |
| `ConfigMap` | `ai-enrichment` | `seed.sql` + `test.py` generated by AI |
| `Job` | `ai-seed` | `psql -f /data/seed.sql` |
| `Job` | `ai-tests` | `python /data/test.py` |
| `Job` | `smoke-tests` | Operator-embedded script (`python:3.12-slim`) |
| `Job` | `regression-tests` | `tests/regression.py` from the app image |
| `Job` | `e2e-tests` | `tests/e2e.py` via Playwright init-container pattern |
| `ConfigMap` | `cellenza-test-suite` | Holds the embedded `smoke.py` script |
| `ConfigMap` | `ai-prompt-<name>` (in operator ns) | Per-environment AI instructions; deleted with the Cellenza |

A **finalizer** (`platform.company.io/finalizer`) ensures all child resources and the preview namespace are cleaned up even on force-delete.

---

## Upgrading

```bash
helm repo update
helm upgrade cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system
```

> CRDs are not automatically upgraded by Helm. Apply the updated CRD manually first if the new version changes the schema:
> ```bash
> helm show crds oci://ghcr.io/ihsenalaya/charts/cellenza-operator --version 0.13.1 \
>   | tail -n +3 | kubectl apply -f -
> ```
> The `tail -n +3` strips the two-line OCI pull header that Helm prepends before the YAML.

## Uninstalling

```bash
helm uninstall cellenza-operator -n cellenza-operator-system
```

> Existing `Cellenza` resources and the CRD are intentionally preserved. Delete them manually if needed:
> ```bash
> kubectl delete crd cellenzas.platform.company.io
> ```

---

## Development

### Prerequisites

- Go 1.25+
- Docker
- `kubectl` + cluster (Kind recommended)
- `make`

### Run locally

```bash
make install    # install CRDs into the cluster
make run        # run controller locally with your kubeconfig
```

### Run tests

```bash
make test        # unit + envtest integration tests
make test-e2e    # E2E against a real Kind cluster
```

### Build images

```bash
# Operator
make docker-build docker-push IMG=ghcr.io/ihsenalaya/cellenza-operator:dev

# Copilot Extension
docker build -f Dockerfile.extension -t ghcr.io/ihsenalaya/cellenza-extension:dev .
docker push ghcr.io/ihsenalaya/cellenza-extension:dev

# Demo app
docker build -t ghcr.io/ihsenalaya/cellenza-demo-app:dev demo-app
docker push ghcr.io/ihsenalaya/cellenza-demo-app:dev
```

### Release

```bash
git tag v0.13.1
git push origin v0.13.1
```

GitHub Actions automatically:
1. Builds and pushes all three images to GHCR with the version tag
2. Packages and publishes the Helm chart to GitHub Releases, GitHub Pages, and GHCR OCI

### CI pipelines

| Workflow | Trigger | What it does |
|---|---|---|
| `lint.yml` | push to any branch/tag | Runs `golangci-lint` |
| `test.yml` | push to any branch/tag | Runs unit + envtest integration tests |
| `test-e2e.yml` | push to any branch/tag | Spins up Kind, installs operator, runs e2e suite |
| `docker-release.yml` | push to `main` or `v*` tag | Builds and pushes `cellenza-operator` image to GHCR |
| `extension-release.yml` | push to `main` or `v*` tag | Builds and pushes `cellenza-extension` image to GHCR |
| `demo-app-release.yml` | push to `main` or `v*` tag | Builds and pushes `cellenza-demo-app` image to GHCR |
| `helm-release.yml` | `v*` tag only | Packages and publishes Helm chart to GitHub Releases + GitHub Pages + GHCR OCI |

### Release a new version

```bash
git tag v0.13.1
git push origin v0.13.1
```

GitHub Actions will automatically:
1. Build and push `ghcr.io/ihsenalaya/cellenza-operator:<version>`
2. Build and push `ghcr.io/ihsenalaya/cellenza-extension:<version>`
3. Build and push `ghcr.io/ihsenalaya/cellenza-demo-app:<version>` (if `demo-app/` changed)
4. Package and publish the Helm chart to GitHub Releases and GitHub Pages
5. Push the chart to `oci://ghcr.io/ihsenalaya/charts/cellenza-operator`

---

## License

Copyright 2026 ihsenalaya — Licensed under the [Apache License 2.0](LICENSE).
