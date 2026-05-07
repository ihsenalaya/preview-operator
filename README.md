# cellenza-operator

> **Ephemeral preview environments for every pull request — with PostgreSQL, OpenTelemetry, AI-generated test data, and full GitHub integration — all from a single Kubernetes custom resource.**

```
kubectl apply -f pr-42.yaml   →   http://pr-42.preview.localtest.me   →   kubectl delete cellenza pr-42
```

---

## What is it?

**Cellenza** is a Kubernetes operator that turns a pull request number into a fully isolated preview environment in seconds. Each `Cellenza` resource provisions its own namespace, deployment, service, ingress, resource quota — and optionally a live PostgreSQL database, OpenTelemetry traces, a complete automated test suite, and AI-generated contextual seed data. Everything is cleaned up automatically when the TTL expires or the resource is deleted.

No shared staging environments. No manual setup. No cleanup scripts.

---

## Feature overview

| Feature | Description |
|---|---|
| Isolated namespaces | Each PR gets `preview-pr-<N>` — zero cross-PR pollution |
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
| Resource tiers | `small` / `medium` / `large` — quota extended automatically for DB and AI jobs |
| Copilot Extension | `@cellenza status pr-42`, `reset-db`, `enrich`, `extend`, `logs` from GitHub Copilot Chat |

---

## How it works

```
Developer opens PR
      │
      ▼
CI pipeline or kubectl apply
creates a Cellenza resource
      │
      ▼
 ┌─ Approval gate ───────────────────────────────────┐
 │  requiresApproval=true → stays Pending             │
 │  until approvedBy is set                           │
 └────────────────────────────────────────────────────┘
      │
      ▼
Operator provisions child resources
  • Namespace      preview-pr-42              (isolated per PR)
  • ResourceQuota  cellenza-quota             (CPU/RAM enforced per tier)
  • Secret         postgres-credentials       (generated once, never overwritten)
  • Deployment     postgres                   (optional: database.enabled=true)
  • Service        postgres                   (ClusterIP, DNS: postgres:5432)
  • Job            postgres-migrate           (optional, runs before app)
  • Job            postgres-seed              (optional, runs after migration)
  • Deployment     app                        (Recreate strategy, init container waits for Postgres)
  • Service        app                        (ClusterIP :80)
  • Ingress        pr-42.preview.localtest.me (via nginx)
      │
      ▼
GitHub notification (github.enabled=true)
  • Updates GitHub Deployment status → in_progress / success / failure
  • Posts PR comment with URL, DB state, traces, TTL, diagnostics
      │
      ▼
Automated test suite (testSuite.enabled=true)
  • Job smoke-tests      → probes /healthz + /api/products (operator-embedded script)
  • Job regression-tests → runs tests/regression.py from the app image
  • Job e2e-tests        → copies tests/e2e.py from app image; Playwright/Chromium executes
  • All three jobs run in parallel
  • Results posted as a dedicated PR comment with pass/fail table
      │
      ▼
AI enrichment (aiEnrichment.enabled=true)
  • Job ai-schema-dump  → pg_dump --schema-only → stored in ConfigMap
  • Operator calls AI API with PR diff + DB schema + app URL
  • AI generates seed.sql (realistic, schema-aware INSERTs) + test.py (targeted integration tests)
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

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --wait
```

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
  --version 0.12.8 \
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
     b. ResourceQuota  (auto-extended for DB, AI, and E2E jobs)
     c. DB reset handling (if database.resetRequested=true)
     d. Database provisioning (Secret → Service → Deployment → migrate → seed)
     e. DB checkpoints (save / restore)
     f. App Deployment (with init container, DB env vars, OTel annotations)
     g. App Service
     h. Ingress
     i. Wait for app availability
10. Mark phase → Running; notify GitHub
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
# Via kubectl (remove the status sub-object to re-trigger)
kubectl patch cz pr-42 --type=json \
  -p='[{"op":"remove","path":"/status/aiEnrichment"}]'

# Via Copilot Extension
@cellenza enrich pr-42
```

### Per-environment AI prompt customization

Store custom instructions for this environment in the cluster without touching the operator:

```bash
# Via Copilot Extension
@cellenza set-prompt pr-42 Generate at least 15 products across 5 categories. Only test /api/ endpoints.
@cellenza enrich pr-42

# Via kubectl
kubectl create configmap ai-prompt-pr-42 \
  --namespace=cellenza-operator-system \
  --from-literal=instructions="Generate at least 15 products across 5 categories." \
  --dry-run=client -o yaml | kubectl apply -f -
```

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
| `@cellenza reset-db pr-42` | Triggers DB reset: operator re-runs migration and seed on next reconcile |
| `@cellenza enrich pr-42` | Clears AI enrichment state and re-triggers generation |
| `@cellenza set-prompt pr-42 <instructions>` | Stores custom AI instructions for this environment |
| `@cellenza show-prompt pr-42` | Displays the current custom AI prompt |
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

**Step 3 — Deploy the extension:**

```bash
kubectl apply -f config/extension/rbac.yaml
kubectl apply -f config/extension/deployment.yaml
kubectl -n cellenza-operator-system rollout status deployment/cellenza-extension --timeout=60s
```

**Step 4 — Expose locally with ngrok:**

```bash
kubectl port-forward -n cellenza-operator-system svc/cellenza-extension 8090:8090 &
ngrok http 8090
# → paste the https:// URL as Webhook URL in the GitHub App settings
```

**Step 5 — Enable Copilot Chat:**

In the GitHub App settings → **Copilot** tab → set **App type** to `Agent`.

---

## Spec reference

| Field | Type | Default | Description |
|---|---|---|---|
| `branch` | string | **required** | Git branch name |
| `prNumber` | integer | **required** | Pull request number |
| `image` | string | **required** | Container image to deploy |
| `ttl` | string | `48h` | Time-to-live. Format: `24h`, `72h`, `30m` |
| `resourceTier` | `small` \| `medium` \| `large` | `medium` | CPU and memory quota tier |
| `replicas` | 1–5 | `1` | Number of app pod replicas |
| `requiresApproval` | bool | `false` | Block provisioning until `approvedBy` is set |
| `approvedBy` | string | — | Username of the approver |
| `database.enabled` | bool | `false` | Provision an ephemeral PostgreSQL instance |
| `database.version` | string | `"15"` | PostgreSQL major version |
| `database.databaseName` | string | `"appdb"` | Logical database name |
| `database.migration.enabled` | bool | `false` | Run a migration Job before the app |
| `database.migration.image` | string | `spec.image` | Override image for the migration Job |
| `database.migration.command` | string[] | — | Required when migration is enabled |
| `database.migration.args` | string[] | — | Optional migration arguments |
| `database.seed.enabled` | bool | `false` | Run a seed Job after migration |
| `database.seed.image` | string | `spec.image` | Override image for the seed Job |
| `database.seed.command` | string[] | — | Required when seed is enabled |
| `database.seed.args` | string[] | — | Optional seed arguments |
| `database.resetRequested` | bool | `false` | Trigger a full DB reset (auto-cleared) |
| `database.checkpointSave` | string | — | Name of snapshot to save (auto-cleared) |
| `database.checkpointRestore` | string | — | Name of snapshot to restore (auto-cleared) |
| `telemetry.enabled` | bool | `false` | Add OTel settings to the app Pod template |
| `telemetry.serviceName` | string | `cellenza-<name>` | Value for `OTEL_SERVICE_NAME` |
| `telemetry.autoInstrumentation.language` | enum | — | `python`, `java`, `nodejs`, `dotnet`, `go`, `sdk` |
| `telemetry.autoInstrumentation.instrumentationRef` | string | `"true"` | `Instrumentation` CR reference |
| `telemetry.autoInstrumentation.pythonPlatform` | `glibc` \| `musl` | — | Python platform override (Alpine) |
| `telemetry.autoInstrumentation.goTargetExecutable` | string | — | Required for Go auto-instrumentation |
| `github.enabled` | bool | `false` | Update GitHub Deployment and PR |
| `github.owner` | string | — | GitHub repository owner |
| `github.repo` | string | — | GitHub repository name |
| `github.deploymentId` | int64 | — | GitHub Deployment id |
| `github.environment` | string | `pr-<N>` | GitHub environment name |
| `github.commentOnReady` | bool | `false` | Post PR comment when preview reaches Running |
| `github.tokenSecretRef` | object | — | Secret reference for the GitHub token |
| `aiEnrichment.enabled` | bool | `false` | Generate seed SQL and tests via AI |
| `aiEnrichment.apiSecretRef` | object | — | Secret containing the AI API key |
| `aiEnrichment.githubTokenSecretRef` | object | — | Optional dedicated GitHub token for PR diff |
| `aiEnrichment.model` | string | `gpt-4o-mini` | AI model name |
| `aiEnrichment.seed.enabled` | bool | `true` | Run the `ai-seed` Job |
| `aiEnrichment.seed.image` | string | `postgres:15-alpine` | Override image for ai-seed |
| `aiEnrichment.tests.enabled` | bool | `true` | Run the `ai-tests` Job |
| `aiEnrichment.tests.image` | string | `python:3.12-slim` | Override image for ai-tests |
| `testSuite.enabled` | bool | `false` | Run the automated test suite |
| `testSuite.smoke.enabled` | bool | `true` | Run built-in smoke tests |
| `testSuite.regression.enabled` | bool | `true` | Run `tests/regression.py` |
| `testSuite.regression.command` | string[] | — | Override regression command |
| `testSuite.e2e.enabled` | bool | `true` | Run `tests/e2e.py` via Playwright |
| `testSuite.e2e.command` | string[] | — | Override E2E command |
| `testSuite.e2e.image` | string | `mcr.microsoft.com/playwright/python:v1.44.0-jammy` | Override Playwright image |

---

## Examples

### Minimal — app only

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

### Full-stack with PostgreSQL and approval gate

```yaml
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
    version: "16"
    databaseName: myapp
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
    seed:
      enabled: true
      command: ["python", "scripts/seed_preview.py"]
```

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
  apiURL: "https://models.inference.ai.azure.com"  # or https://api.openai.com/v1
  systemPrompt: ""              # override default; or use --set-file ai.systemPrompt=./prompt.txt

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
> kubectl apply -f https://raw.githubusercontent.com/ihsenalaya/cellenza-operator/v0.12.8/charts/cellenza-operator/crds/platform.company.io_cellenzas.yaml
> ```

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
git tag v0.12.8
git push origin v0.12.8
```

GitHub Actions automatically:
1. Builds and pushes all three images to GHCR with the version tag
2. Packages and publishes the Helm chart to GitHub Releases, GitHub Pages, and GHCR OCI

### CI pipelines

| Workflow | Trigger | What it does |
|---|---|---|
| `lint.yml` | push to any branch/tag | `golangci-lint` |
| `test.yml` | push to any branch/tag | Unit + envtest integration tests |
| `test-e2e.yml` | push to any branch/tag | Kind cluster + full E2E suite |
| `docker-release.yml` | push to `main` or `v*` tag | Builds and pushes `cellenza-operator` image |
| `extension-release.yml` | push to `main` or `v*` tag | Builds and pushes `cellenza-extension` image |
| `demo-app-release.yml` | push to `main` or `v*` tag | Builds and pushes `cellenza-demo-app` image |
| `helm-release.yml` | `v*` tag only | Packages and publishes Helm chart |

---

## License

Copyright 2026 ihsenalaya — Licensed under the [Apache License 2.0](LICENSE).
