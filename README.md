# cellenza-operator

A Kubernetes operator that provisions **ephemeral preview environments** for pull requests. Each `Cellenza` resource creates a dedicated namespace with its own deployment, service, ingress, resource quota — and optionally a **PostgreSQL database with auto-generated credentials**, **OpenTelemetry auto-instrumentation**, and **GitHub Deployment/PR status updates** — and tears it all down automatically when the TTL expires.

## How it works

```
Developer opens PR
      │
      ▼
kubectl apply / CI pipeline
creates a Cellenza resource
      │
      ▼
Operator creates:
  • Namespace      preview-pr-42
  • ResourceQuota  (based on resourceTier, +headroom for DB and AI jobs)
  • Secret         postgres-credentials  (unique credentials, generated once)
  • Deployment     postgres              (optional, when database.enabled=true)
  • Service        postgres
  • Deployment     app  (Recreate strategy, waits for postgres via init container)
  • Service        app
  • Ingress        →  pr-42.preview.localtest.me
  • OTEL annotations/env vars (optional, when telemetry.enabled=true)
      │
      ▼
Operator updates GitHub Deployment / PR comment
when github.enabled=true
      │
      ▼
Test Suite (when testSuite.enabled=true):
  • Job smoke-tests      → tests /health + /api/products (operator-built-in)
  • Job regression-tests → runs /app/tests/regression.py from the app image
  • Job e2e-tests        → runs /app/tests/e2e.py from the app image
  • All three jobs run in parallel
  • Results posted as a dedicated PR comment with pass/fail table
      │
      ▼
AI enrichment (when aiEnrichment.enabled=true):
  • Fetches PR diff + dumps DB schema
  • Calls AI API → generates seed.sql + test.py
  • Job ai-seed  → psql seed.sql against the preview DB
  • Job ai-tests → pip install requests && python test.py
  • Results posted to PR comment and visible via @cellenza status
      │
      ▼
Environment runs until TTL expires
or until the Cellenza is deleted
```

An **approval gate** is available for sensitive environments: set `requiresApproval: true` and the operator will block provisioning until a human sets `approvedBy`.

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Kubernetes | 1.25+ | Kind, k3s, GKE, AKS, EKS, or any conformant cluster |
| Helm | 3.12+ | Required to install the operator |
| cert-manager | 1.13+ | Required for webhook TLS — can be skipped with `--set webhook.enabled=false` |
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
kind create cluster --name cellenza-test
```

For preview environments to be reachable from your browser via ingress, map the ingress ports when creating the cluster:

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

Preview URLs will then be reachable at `http://pr-42.preview.localtest.me:8080` from your machine (no DNS needed — `localtest.me` resolves to `127.0.0.1`).

### 1. Add all Helm repositories

Run this once to register all required repositories:

```bash
helm repo add cellenza      https://ihsenalaya.github.io/cellenza-operator
helm repo add jetstack      https://charts.jetstack.io
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
helm repo update
```

### 2. Install cert-manager

Required for webhook TLS. Skip if already installed.

```bash
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true \
  --wait
```

### 3. Install ingress-nginx

Preview environments are exposed through Kubernetes `Ingress` resources.

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --wait
```

For Kind clusters with the port mapping from step 0, previews will be available at `http://pr-42.preview.localtest.me:8080` immediately after this step. For clusters without port mapping, use a port-forward:

```bash
kubectl port-forward -n ingress-nginx svc/ingress-nginx-controller 8080:80
```

### 4. Install OpenTelemetry Operator (optional)

Required only when using `telemetry.autoInstrumentation`. cert-manager must be installed first.

```bash
helm install opentelemetry-operator open-telemetry/opentelemetry-operator \
  --namespace opentelemetry-operator-system \
  --create-namespace \
  --set "manager.collectorImage.repository=otel/opentelemetry-collector-contrib" \
  --set admissionWebhooks.certManager.enabled=true \
  --wait
```

### 5. Install Jaeger (optional)

A lightweight all-in-one Jaeger for local trace visualization. Skip if you already have a tracing backend.

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
```

Then deploy the OTel Collector and the `Instrumentation` CR:

```bash
kubectl apply -f https://raw.githubusercontent.com/ihsenalaya/cellenza-operator/main/demo-app/otel.yaml
```

Access the Jaeger UI:

```bash
kubectl port-forward -n observability svc/jaeger 16686:16686
# open http://localhost:16686
```

### 6. Install the operator

```bash
helm install cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system \
  --create-namespace \
  --wait
```

The chart installs the CRD, RBAC, webhooks, and the controller in one shot. Verify:

```bash
kubectl get pods -n cellenza-operator-system
# NAME                                  READY   STATUS    RESTARTS   AGE
# cellenza-operator-647dc9db-xxxxx      1/1     Running   0          30s
```

### (Optional) GHCR pull secret for private registries

If the operator image or your app images are hosted in a private GHCR registry, create an image pull secret:

```bash
kubectl create secret docker-registry ghcr-pull-secret \
  --namespace=cellenza-operator-system \
  --docker-server=ghcr.io \
  --docker-username=<github-username> \
  --docker-password=<github-token>
```

Then reference it in the Helm install:

```bash
helm install cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system \
  --create-namespace \
  --set imagePullSecrets[0].name=ghcr-pull-secret
```

### Install without webhooks (no cert-manager needed)

```bash
helm install cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system \
  --create-namespace \
  --set webhook.enabled=false
```

> Without webhooks, defaults are not automatically applied and invalid specs are not rejected at admission time. Validation happens at reconciliation instead.

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

### With PostgreSQL

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/my-feature
  prNumber: 42
  image: myapp:abc1234
  database:
    enabled: true
    version: "15"       # PostgreSQL major version
    databaseName: appdb # logical database name
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
    seed:
      enabled: true
      command: ["python", "seed.py"]
```

The operator will:
1. Generate a unique username (`preview_42`) and a cryptographically random password
2. Store them in a Secret `postgres-credentials` in the PR namespace
3. Start a `postgres:15-alpine` deployment
4. Run optional migration and seed Jobs with the database credentials injected
5. Block the app deployment until those Jobs succeed
6. Block the app pod with an init container (`busybox`) until PostgreSQL is ready
7. Inject the credentials into the app container as environment variables

**Credentials are generated once and never overwritten**, even if the Cellenza is updated.

The app receives these environment variables automatically:

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
kubectl logs -n preview-pr-42 deployment/app -c app -f
```

### Running database migrations and seed data

Migration and seed tasks are optional one-shot Kubernetes Jobs. They run after PostgreSQL is created and before the app Deployment is reconciled. By default, each task uses `spec.image`, so the commands must exist inside your application image. You can override that with `image` per task.

The operator injects the same database environment variables into each task:

- `DATABASE_URL`
- `POSTGRES_DB`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`

Example with an app image that contains Alembic and a seed script:

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/database-change
  prNumber: 42
  image: ghcr.io/example/myapp:sha-abc123
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

Both commands should be idempotent: migrations should tolerate already-applied schema changes, and seed scripts should use upserts or `ON CONFLICT DO NOTHING` for demo rows.

Check progress from the Cellenza status:

```bash
kubectl get cellenza pr-42 \
  -o jsonpath='{.status.database.migration}{"\n"}{.status.database.seed}{"\n"}'
```

Inspect task logs if a preview stays in `Provisioning` or moves to `Failed`:

```bash
kubectl logs -n preview-pr-42 job/postgres-migrate
kubectl logs -n preview-pr-42 job/postgres-seed
kubectl describe cellenza pr-42
```

### With OpenTelemetry auto-instrumentation

When the OpenTelemetry Operator and Jaeger are installed (steps 4–5), the controller can opt the application Pod into zero-code auto-instrumentation via a single annotation.

The `demo-app/otel.yaml` file shipped in this repo creates two resources in the `observability` namespace:

- an `OpenTelemetryCollector` that receives OTLP from instrumented pods and forwards traces to Jaeger
- an `Instrumentation` CR for Python that the OTel Operator injects into annotated pods

Reference the `Instrumentation` from your `Cellenza` resource using `namespace/name`:

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

The controller injects these settings into the generated app `Deployment` automatically:

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

Generate traffic and open Jaeger to see the traces:

```bash
curl http://pr-2.preview.localtest.me:8080
kubectl port-forward -n observability svc/jaeger 16686:16686
# open http://localhost:16686 → search for service "cellenza-demo"
```

For the Flask demo app, Python auto-instrumentation produces HTTP spans for `GET /` and database spans for the PostgreSQL queries.

### With Automated Test Suite

The operator runs **smoke, regression, and E2E tests automatically** after the preview environment is ready — all three jobs run in parallel. Results are posted as a dedicated PR comment.

#### What the operator adds to testing

The operator does more than "run a CI script". Its role is to turn a pull request into a **real, isolated test environment**, then orchestrate tests inside that environment:

- It provisions a dedicated preview namespace for the PR, with its own app deployment, service, ingress, and optional PostgreSQL database.
- It waits until the preview is actually usable before launching tests, instead of running them against mocks or half-ready infrastructure.
- It runs smoke, regression, and E2E jobs against the real deployed app and the real database state of that PR.
- It keeps PRs isolated from each other, so test data and failures from PR-41 cannot pollute PR-42.
- It collects test outputs into structured status fields on the `Cellenza` resource and posts a summarized result back to GitHub.
- It can combine test execution with AI-generated seed data and AI-generated test jobs when `aiEnrichment.enabled=true`.

#### Why this matters vs a classic test environment

| Capability | Classic staging | Cellenza preview |
|-----------|----------------|-----------------|
| Isolated per PR | ❌ shared state | ✅ dedicated namespace |
| Real database | ⚠️ often mocked | ✅ live PostgreSQL |
| Contextual seed data | ❌ generic fixtures | ✅ AI-generated from PR diff |
| Parallel PRs | ❌ flaky | ✅ no pollution between PRs |
| Results on PR | manual | ✅ automatic comment |

#### The three test jobs

**1. Smoke tests** — built into the operator, no file required

The smoke script is embedded in the operator binary. It starts a `python:3.12-slim` pod and runs:

```
GET /healthz  → expect 200
GET /api/products  → expect 200
```

These two checks verify that the deployment itself succeeded: the container started, the network is reachable, and the API responds. If smoke fails, regression and E2E are still launched in parallel.

**2. Regression tests** — `tests/regression.py` from the app image

The regression job runs the app's own test file inside the app image (no extra image needed). It validates every existing HTTP endpoint against the live PostgreSQL instance — catching integration regressions that would be invisible in a mocked unit-test environment.

All output lines starting with `PASS`/`FAIL` are parsed by the operator to build the pass/fail counters.

**3. E2E tests** — `tests/e2e.py` from the app image, run inside Playwright

E2E tests use real headless Chromium (via Playwright) to simulate actual user interactions — clicking, scrolling, filling forms — against the deployed preview URL.

Because the Playwright image (`mcr.microsoft.com/playwright/python:v1.44.0-jammy`) does not contain the app code, the operator uses an **init-container pattern**:

```
Pod: e2e-tests
  ├── init container: copy-tests
  │     Image: <app image>  (same as spec.image)
  │     Command: cp /app/tests/e2e.py /data/e2e.py
  │     Volume: emptyDir /data  (shared)
  │
  └── main container: e2e-tests
        Image: mcr.microsoft.com/playwright/python:v1.44.0-jammy
        Command: python /data/e2e.py
        Env: APP_URL=http://app:80, PREVIEW_URL=<ingress URL>
        Volume: emptyDir /data  (shared)
        Resources: 200m–1 CPU, 512Mi–1Gi RAM  (Chromium needs headroom)
```

The init container copies `e2e.py` from the app image into a shared `emptyDir`. Playwright then executes it with Chromium. This keeps the test code versioned alongside the application while using the official Playwright runtime.

#### Required: test scripts in the app image

Add `tests/regression.py` and `tests/e2e.py` to your app repo and include them in the Docker image:

```dockerfile
COPY tests/ ./tests/
```

Output lines must start with `PASS` or `FAIL` to be parsed by the operator:

```python
# tests/regression.py — minimal example
import requests, sys, os
BASE = os.environ.get("APP_URL", "http://app:80")

tests = [
    ("health", "GET", "/healthz", 200, None),
    ("products", "GET", "/api/products", 200, lambda r: isinstance(r.json(), list)),
]
passed, failed = 0, 0
for name, method, path, code, check in tests:
    r = requests.request(method, BASE + path, timeout=10)
    ok = r.status_code == code and (check(r) if check and r.status_code == code else True)
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

def run(name, fn):
    global passed, failed
    with sync_playwright() as p:
        browser = p.chromium.launch(args=["--no-sandbox", "--disable-dev-shm-usage"])
        page = browser.new_page()
        try:
            fn(page)
            print(f"PASS e2e {name}")
            passed += 1
        except Exception as e:
            print(f"FAIL e2e {name}: {e}")
            failed += 1
        finally:
            browser.close()

def test_homepage(page):
    page.goto(BASE, wait_until="networkidle", timeout=30000)
    assert page.title() != "", "page has no title"

run("homepage", test_homepage)
print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed > 0 else 0)
```

#### Cellenza spec

```yaml
spec:
  testSuite:
    enabled: true
    smoke: {}                    # built-in, no config needed
    regression:
      enabled: true              # default command: python /app/tests/regression.py
    e2e:
      enabled: true              # default command: python /app/tests/e2e.py
      # image: mcr.microsoft.com/playwright/python:v1.44.0-jammy  # default
```

#### GitHub PR comment produced

```
## Cellenza Test Suite Results

**Overall: ✅ Succeeded**

| Suite      | Status        | Passed | Failed |
|------------|---------------|--------|--------|
| Smoke      | ✅ Succeeded  | 2      | 0      |
| Regression | ✅ Succeeded  | 9      | 0      |
| E2E        | ✅ Succeeded  | 6      | 0      |
```

#### What is executed and what is published

When the test suite is enabled, the operator executes three concrete workloads against the preview:

- **Smoke**: an operator-managed script that probes `/healthz` and `/api/products`
- **Regression**: `tests/regression.py` copied from the application image
- **E2E**: `tests/e2e.py` copied from the application image and executed in Playwright

After execution, the operator publishes results in two forms:

- **CR status**: per-suite phases, pass/fail counters, and parsed output under `status.tests.*`
- **GitHub comment**: a dedicated PR comment summarizing the three suites in a pass/fail table

So the application repository owns the regression/E2E test code, while the operator owns orchestration, execution timing, result collection, and publication.

#### Check status via CLI

```bash
kubectl get cz pr-42 -o jsonpath='{.status.tests}' | jq .
kubectl get cz pr-42 -o jsonpath='{range .status.tests.e2e.output[*]}{@}{"\n"}{end}'
```

---

### With GitHub Deployment automation

CI (or a human) creates the GitHub Deployment and a token Secret, then the controller publishes the observed Kubernetes state back to GitHub.

#### 1. Create the GitHub Deployment

```bash
gh api repos/OWNER/REPO/deployments \
  --method POST \
  --field ref="<commit-sha-or-branch>" \
  --field environment="pr-42" \
  --field description="Preview environment for PR 42" \
  --field auto_merge=false \
  --jq '{id, environment}'
# → {"id": 4536974429, "environment": "pr-42"}
```

Use the returned `id` as `spec.github.deploymentId` in the Cellenza resource.

#### 2. Create the token Secret

```bash
kubectl create secret generic cellenza-github-token \
  --namespace=cellenza-operator-system \
  --from-literal=token="$GITHUB_PAT"
```

> Use a long-lived token here. Do not use the GitHub Actions workflow `GITHUB_TOKEN` or other short-lived installation tokens, because the controller may need to update GitHub and fetch PR diffs long after the workflow has finished.
>
> If you rotate the token, replace the Secret in place:
> ```bash
> kubectl create secret generic cellenza-github-token \
>   --namespace=cellenza-operator-system \
>   --from-literal=token="$NEW_TOKEN" \
>   --dry-run=client -o yaml | kubectl apply -f -
> ```
> The controller retries GitHub calls on the next reconcile loop automatically.

#### 3. Reference the Secret from the Cellenza resource

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-42
spec:
  branch: feature/my-feature
  prNumber: 42
  image: ghcr.io/acme/myapp:abc1234
  resourceTier: medium
  ttl: 48h
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

Use a long-lived GitHub credential for `github.tokenSecretRef`. Do not point the controller at the workflow `GITHUB_TOKEN`, because GitHub Actions job tokens expire shortly after the workflow completes while the preview and AI enrichment continue running.

When the preview reaches `Running`, the controller sends a GitHub Deployment `success` status with `status.url` as the environment URL and creates one PR comment when `commentOnReady` is true. That comment includes preview evidence such as app readiness, PostgreSQL readiness, migration status, seed status, telemetry state, namespace, URL, expiry, and an AI-assisted health summary.

When reconciliation fails, the controller collects automatic diagnostics from the preview namespace and comments on the PR with:

- the failing component (`app`, `database`, `migration`, `seed`, `ingress`, etc.)
- the operator reason and error message
- the probable root cause and confidence level
- significant log lines selected from app, PostgreSQL, migration, and seed components
- recommended next actions for the developer
- recent Kubernetes warning events
- useful `kubectl` debug commands

Example failure comment — image pull error:

~~~markdown
## Cellenza Preview Failed

Environment: `pr-42`
Namespace: `preview-pr-42`

### Diagnosis

- Reason: `DeploymentFailed`
- Component: `app`
- Message: Deployment app is unavailable: Deployment does not have minimum availability.
- Probable cause: **Container image cannot be pulled**
- Confidence: `high`

### Recommendations

1. Verify that image `ghcr.io/acme/myapp:does-not-exist` exists and is accessible from the cluster.
2. Check the image tag produced by CI for this pull request.
3. Confirm the namespace has the required imagePullSecrets if the registry is private.

### Recent Warning Events

- Pod/app-xyz: Error: ImagePullBackOff
- Pod/app-xyz: Failed to pull image "ghcr.io/acme/myapp:does-not-exist": not found
- Pod/app-xyz: Error: ErrImagePull

### Debug Commands

```bash
kubectl describe cellenza pr-42
kubectl get pods -n preview-pr-42
kubectl get events -n preview-pr-42 --sort-by=.lastTimestamp
kubectl describe deployment app -n preview-pr-42
```
~~~

Example failure comment — database migration error:

~~~markdown
## Cellenza Preview Failed

Environment: `pr-42`
Namespace: `preview-pr-42`

### Diagnosis

- Reason: `DatabaseMigrationFailed`
- Component: `migration`
- Message: Job postgres-migrate failed: BackoffLimitExceeded
- Probable cause: **Database migration failed**
- Confidence: `high`

### Significant Logs

**migration** — `pod/postgres-migrate-abc container/migration`

```text
ERROR relation messages already exists
migration failed at 003_create_messages.sql
```

### Recommendations

1. Check that the migration is idempotent and can run on a fresh preview database.
2. Look for duplicate table/index creation or schema ordering issues in the highlighted logs.
3. After fixing the migration, request a DB reset with `kubectl patch cellenza pr-42 --type=merge -p '{"spec":{"database":{"resetRequested":true}}}'`.

### Recent Warning Events

- Pod/postgres-migrate-abc: BackoffLimitExceeded

### Debug Commands

```bash
kubectl describe cellenza pr-42
kubectl get pods -n preview-pr-42
kubectl logs -n preview-pr-42 job/postgres-migrate
```
~~~

Example failure comment — application crash (CrashLoopBackOff):

~~~markdown
## Cellenza Preview Failed

Environment: `pr-42`
Namespace: `preview-pr-42`

### Diagnosis

- Reason: `DeploymentFailed`
- Component: `app`
- Message: Deployment app is unavailable: Deployment does not have minimum availability.
- Probable cause: **Application pod is unhealthy**
- Confidence: `medium`

### Recommendations

1. Inspect the highlighted app logs and the deployment description.
2. Check readiness/liveness probe paths, startup time, and required environment variables.
3. Rebuild the application image if the failure started after a code change.

### Recent Warning Events

- Pod/app-xyz: Back-off restarting failed container app in pod app-xyz_preview-pr-42(...)

### Debug Commands

```bash
kubectl describe cellenza pr-42
kubectl get pods -n preview-pr-42
kubectl get events -n preview-pr-42 --sort-by=.lastTimestamp
kubectl describe deployment app -n preview-pr-42
```
~~~

> Application crashes (`CrashLoopBackOff`) are diagnosed with `medium` confidence because the root cause can vary: a missing environment variable, a failed startup script, a misconfigured readiness probe, or a code-level panic. The recommendations always guide the developer to inspect pod logs and the deployment description first.

When the resource is deleted, the finalizer sends an `inactive` status before cleanup completes.

Read the credentials at any time:

```bash
kubectl get secret postgres-credentials -n preview-pr-42 \
  -o jsonpath='{.data.DATABASE_URL}' | base64 -d
```

### With AI Enrichment

After the preview reaches `Running`, the operator automatically generates context-aware seed data and integration tests by sending the PR diff and the live database schema to an AI API.

#### 1. Create the API key Secret

```bash
# OpenAI
kubectl create secret generic ai-api-key \
  --namespace=cellenza-operator-system \
  --from-literal=api-key=sk-...

# GitHub Models (free tier)
kubectl create secret generic ai-api-key \
  --namespace=cellenza-operator-system \
  --from-literal=api-key="$GITHUB_TOKEN"
# Also override the API URL when using GitHub Models:
helm upgrade cellenza-operator ... --set ai.apiURL=https://models.inference.ai.azure.com
```

#### 2. Enable in the Cellenza resource

```yaml
spec:
  aiEnrichment:
    enabled: true
    apiSecretRef:
      name: ai-api-key
      key: api-key
    githubTokenSecretRef:
      name: cellenza-github-token
      namespace: cellenza-operator-system
      key: token
    model: gpt-4o-mini   # default, can be gpt-4o, etc.
    seed:
      enabled: true      # default: true when omitted
    tests:
      enabled: true      # default: true when omitted
```

`aiEnrichment.githubTokenSecretRef` is optional. When set, AI enrichment uses that GitHub token specifically to fetch the PR diff. When omitted, it falls back to `github.tokenSecretRef` for backward compatibility.

#### 3. What happens

```
Preview Running
      │
      ▼
ai-schema-dump Job  →  pg_dump --schema-only  →  ConfigMap ai-enrichment (schema)
      │
      ▼
Operator calls AI API with:
  • PR diff (GitHub API)          → what changed in this PR
  • DB schema (from ConfigMap)    → table structure
  • App URL (http://app:80)       → where to send HTTP requests
      │
      ▼
AI generates:
  • seed.sql  → INSERT statements with realistic, schema-aware data
  • test.py   → integration tests targeting the modified code paths
      │
      ▼
ai-seed Job   →  psql -f /data/seed.sql
ai-tests Job  →  pip install requests && python /data/test.py
      │
      ▼
Results in status.aiEnrichment.testResults[] and PR comment
```

#### 4. Monitor enrichment state

```bash
kubectl get cz pr-42 -o jsonpath='{.status.aiEnrichment}' | jq .
```

```json
{
  "phase": "Succeeded",
  "seedStatus": "Succeeded",
  "testsStatus": "Succeeded",
  "testResults": ["PASS: test_health", "PASS: test_create_product", "FAIL: test_order_stock — 409 expected"],
  "completedAt": "2026-05-01T12:00:00Z"
}
```

#### 5. Re-trigger enrichment

```bash
# Via kubectl
kubectl patch cz pr-42 --type=merge \
  -p='{"spec":{"aiEnrichment":{"rerunRequested":true}}}'

# Via Copilot Extension
@cellenza retest-ai pr-42
```

#### 6. Customize AI instructions per environment

You can store custom prompt instructions directly in the cluster without touching the operator code:

```
@cellenza set-prompt pr-42 Generate at least 15 products across 5 categories. Only test /api/ endpoints.
```

The extension creates a ConfigMap `ai-prompt-pr-42` in `cellenza-operator-system`. The operator reads it automatically on the next enrichment. Run `@cellenza retest-ai pr-42` afterward to regenerate with the new instructions.

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

Apply it:

```bash
kubectl apply -f pr-42.yaml
kubectl get cellenza pr-42 --watch
```

```
NAME    PHASE          BRANCH              TIER     URL                              EXPIRES
pr-42   Provisioning   feature/my-feature  medium   pr-42.preview.localtest.me       ...
pr-42   Running        feature/my-feature  medium   pr-42.preview.localtest.me       2026-04-24T10:00:00Z
```

---

## Spec reference

| Field | Type | Default | Description |
|---|---|---|---|
| `branch` | string | **required** | Git branch name |
| `prNumber` | integer | **required** | Pull request number |
| `image` | string | **required** | Container image to deploy (`repo:tag`) |
| `ttl` | string | `48h` | Time-to-live before auto-deletion. Format: `24h`, `72h`, `30m` |
| `resourceTier` | `small` \| `medium` \| `large` | `medium` | Controls CPU and memory quotas |
| `replicas` | 1–5 | `1` | Number of pod replicas |
| `requiresApproval` | bool | `false` | Block provisioning until `approvedBy` is set |
| `approvedBy` | string | — | Username of the approver (required when `requiresApproval: true`) |
| `database.enabled` | bool | `false` | Provision an ephemeral PostgreSQL instance |
| `database.version` | string | `"15"` | PostgreSQL major version |
| `database.databaseName` | string | `"appdb"` | Logical database name created inside PostgreSQL |
| `database.migration.enabled` | bool | `false` | Run a migration Job before the app is deployed |
| `database.migration.image` | string | `spec.image` | Optional image for the migration Job |
| `database.migration.command` | string array | — | Required when migration is enabled |
| `database.migration.args` | string array | — | Optional migration command arguments |
| `database.seed.enabled` | bool | `false` | Run a seed Job after migration and before the app is deployed |
| `database.seed.image` | string | `spec.image` | Optional image for the seed Job |
| `database.seed.command` | string array | — | Required when seed is enabled |
| `database.seed.args` | string array | — | Optional seed command arguments |
| `database.resetRequested` | bool | `false` | Trigger a full DB reset: deletes migration/seed jobs, clears DB status, re-runs both jobs on next reconcile. Cleared automatically by the controller. |
| `telemetry.enabled` | bool | `false` | Add OpenTelemetry settings to the app Pod template |
| `telemetry.serviceName` | string | `cellenza-<name>` | Value for `OTEL_SERVICE_NAME` |
| `telemetry.autoInstrumentation.language` | `python` \| `java` \| `nodejs` \| `dotnet` \| `go` \| `sdk` | — | Auto-instrumentation annotation language |
| `telemetry.autoInstrumentation.instrumentationRef` | string | `"true"` | `Instrumentation` reference: `true`, `name`, or `namespace/name` |
| `telemetry.autoInstrumentation.pythonPlatform` | `glibc` \| `musl` | — | Python auto-instrumentation platform override |
| `telemetry.autoInstrumentation.goTargetExecutable` | string | — | Required executable path for Go auto-instrumentation |
| `github.enabled` | bool | `false` | Let the controller update a GitHub Deployment and optionally comment on the PR |
| `github.owner` | string | — | GitHub repository owner or organization |
| `github.repo` | string | — | GitHub repository name |
| `github.deploymentId` | int | — | GitHub Deployment id created by CI |
| `github.environment` | string | `pr-<number>` | GitHub environment name |
| `github.commentOnReady` | bool | `false` | Create one PR comment when the preview reaches `Running` |
| `github.tokenSecretRef.name` | string | — | Secret containing a GitHub token |
| `github.tokenSecretRef.namespace` | string | `cellenza-operator-system` | Secret namespace |
| `github.tokenSecretRef.key` | string | `token` | Secret data key |
| `aiEnrichment.enabled` | bool | `false` | Generate seed SQL and integration tests via AI after the preview reaches Running |
| `aiEnrichment.apiSecretRef.name` | string | — | Secret containing the AI API key (`api-key` key by default) |
| `aiEnrichment.apiSecretRef.namespace` | string | `cellenza-operator-system` | Namespace of the API key secret |
| `aiEnrichment.apiSecretRef.key` | string | `api-key` | Secret data key |
| `aiEnrichment.githubTokenSecretRef.name` | string | — | Optional dedicated GitHub token Secret for fetching the PR diff |
| `aiEnrichment.githubTokenSecretRef.namespace` | string | `cellenza-operator-system` | Namespace of the AI GitHub token Secret |
| `aiEnrichment.githubTokenSecretRef.key` | string | `token` | Secret data key for the AI GitHub token |
| `aiEnrichment.model` | string | `gpt-4o-mini` | AI model name (e.g. `gpt-4o`, `gpt-4o-mini`) |
| `aiEnrichment.seed.enabled` | bool | `true` | Run the `ai-seed` Job (psql the generated seed.sql) |
| `aiEnrichment.seed.image` | string | `postgres:15-alpine` | Image override for the seed Job |
| `aiEnrichment.tests.enabled` | bool | `true` | Run the `ai-tests` Job (python the generated test.py) |
| `aiEnrichment.tests.image` | string | `python:3.12-slim` | Image override for the tests Job |

### Resource tiers

When `database.enabled: true`, the operator automatically adds PostgreSQL headroom (+500m CPU / +512Mi RAM) to the namespace quota. When `aiEnrichment.enabled: true`, an additional +500m CPU / +512Mi RAM is reserved for AI jobs (`ai-seed`, `ai-tests`).

| Tier | CPU request | CPU limit | Memory request | Memory limit |
|---|---|---|---|---|
| `small` | 100m | 250m | 128Mi | 256Mi |
| `medium` | 200m | 500m | 256Mi | 512Mi |
| `large` | 500m | 2000m | 512Mi | 2Gi |

> `large` always forces `requiresApproval: true` (enforced by the webhook).

---

## Examples

### Custom TTL and tier

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: pr-99
spec:
  branch: feature/heavy-load-test
  prNumber: 99
  image: myapp:latest
  ttl: 72h
  resourceTier: large
  requiresApproval: true
  replicas: 2
```

Approve the environment after review:

```bash
kubectl patch cellenza pr-99 --type=merge \
  -p '{"spec":{"approvedBy":"ihsenalaya"}}'
```

The operator will move from `Pending` → `Provisioning` → `Running` once approved.

### Multiple replicas for load testing

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

### Full-stack environment (app + PostgreSQL + approval gate)

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
```

### Full-stack environment with OpenTelemetry traces

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

---

## Lifecycle

```
                     requiresApproval=true
                     and approvedBy not set
                            │
Created ──► Pending ────────┘
                │
                │ (approved or requiresApproval=false)
                ▼
          Provisioning  (namespace, quota, deployment, service, ingress created)
                │
         ┌──────┴──────┐
         ▼             ▼
       Running        Failed  (image pull error, quota exceeded, etc.)
         │
         │ TTL expired or kubectl delete
         ▼
      Terminating  (all child resources deleted, namespace removed)
```

### Status fields

```bash
kubectl describe cellenza pr-42
```

| Field | Description |
|---|---|
| `status.phase` | Current lifecycle phase |
| `status.url` | Ingress URL of the preview environment |
| `status.expiresAt` | Timestamp when the environment will be auto-deleted |
| `status.namespaceName` | Dedicated namespace created by the operator |
| `status.databaseSecretName` | Name of the Secret holding PostgreSQL credentials (when database is enabled) |
| `status.database.ready` | Whether PostgreSQL plus configured migration/seed jobs are complete |
| `status.database.migration` | Migration job state: `Skipped`, `Running`, `Succeeded`, or `Failed` |
| `status.database.seed` | Seed job state: `Skipped`, `Running`, `Succeeded`, or `Failed` |
| `status.readyAt` | Timestamp of the first transition to `Running` |
| `status.diagnostics.reason` | Latest operator diagnostic reason when the preview fails |
| `status.diagnostics.component` | Component most likely responsible for the failure |
| `status.diagnostics.message` | Human-readable diagnostic summary |
| `status.diagnostics.rootCause` | Probable root cause inferred from logs, events, and component state |
| `status.diagnostics.confidence` | Diagnostic confidence: `low`, `medium`, or `high` |
| `status.diagnostics.recommendations` | Developer-facing suggested next actions |
| `status.diagnostics.significantLogs` | Selected high-signal log lines grouped by component/source |
| `status.diagnostics.podLogs` | Last 30 lines from the crashed app container |
| `status.diagnostics.lastEvents` | Recent warning events from the preview namespace |
| `status.diagnostics.debugCommands` | Useful `kubectl` commands to troubleshoot the preview |
| `status.github.deploymentState` | Last GitHub Deployment state emitted by the controller |
| `status.github.lastEnvironmentUrl` | Last URL sent to GitHub |
| `status.github.commentId` | PR comment id created by the controller |
| `status.github.lastError` | Latest non-blocking GitHub notification error |
| `status.aiEnrichment.phase` | AI enrichment lifecycle phase: `Pending`, `Generating`, `Running`, `Succeeded`, `Failed`, `Skipped` |
| `status.aiEnrichment.seedStatus` | State of the `ai-seed` Job: `Pending`, `Running`, `Succeeded`, `Failed`, `Skipped` |
| `status.aiEnrichment.testsStatus` | State of the `ai-tests` Job: `Pending`, `Running`, `Succeeded`, `Failed`, `Skipped` |
| `status.aiEnrichment.testResults` | Array of test result lines extracted from `ai-tests` output (`PASS: …` / `FAIL: …`) |
| `status.aiEnrichment.summary` | Human-readable summary of what the AI generated |
| `status.aiEnrichment.error` | Error message if the enrichment failed |
| `status.aiEnrichment.completedAt` | Timestamp of enrichment completion |
| `status.conditions` | Kubernetes-standard conditions: `Ready`, `Approved`, `Expired`, `DatabaseReady`, `MigrationReady`, `SeedReady`, `AIEnrichmentReady` |

---

## GitHub Copilot Extension

The **Cellenza Extension** is a companion server that exposes preview environment management directly inside GitHub Copilot Chat — no `kubectl` access needed for developers.

### How it works

```
Developer → @cellenza status pr-42
                │
                ▼
         GitHub Copilot Chat
                │  (POST webhook, OpenAI SSE format)
                ▼
     cellenza-extension server  (port 8090)
                │  (controller-runtime + kubernetes client)
                ▼
        Kubernetes API Server
                │
                ▼
         Cellenza CR → response streamed back to Copilot
```

The extension server is a separate binary (`cmd/extension/`) deployed in `cellenza-operator-system`. It speaks the GitHub Copilot Extension protocol (OpenAI-compatible SSE streaming) and reads/patches `Cellenza` resources and pod logs via a dedicated ClusterRole.

**RBAC granted to the extension:**

| Resource | Verbs |
|---|---|
| `cellenzas` | `get`, `list`, `watch`, `patch`, `update` |
| `cellenzas/status` | `get`, `patch`, `update` |
| `pods` | `get`, `list`, `watch` |
| `pods/log` | `get` |
| `configmaps` (namespace `cellenza-operator-system`) | `get`, `create`, `patch`, `update`, `delete` |

### Available commands

All responses are in French. Arguments accept `pr-42`, `42`, or `#42`.

| Command | Description |
|---|---|
| `@cellenza list` | Lists all active environments with phase, branch, and TTL remaining |
| `@cellenza status pr-42` | Phase, URL, branch, tier, replicas, running time, TTL, DB state, GitHub Deployment state, and crash diagnostics if `Failed` |
| `@cellenza logs pr-42` | Last 40 lines from the app pod; falls back to `status.diagnostics.podLogs` when the pod is gone |
| `@cellenza extend pr-42 [24h]` | Extends TTL by the given duration (default `24h`) — patches `spec.ttl` and `status.expiresAt` immediately |
| `@cellenza wake pr-42` | Sets `spec.replicas` to `1` to restart a scaled-down environment |
| `@cellenza reset-db pr-42` | Sets `spec.database.resetRequested: true` — operator deletes migration/seed jobs and re-runs them on next reconcile |
| `@cellenza retest-ai pr-42` | Sets `spec.aiEnrichment.rerunRequested: true` — operator replays DB setup when enabled, skips the standard test suite for this cycle, and regenerates AI seed/tests |
| `@cellenza enrich pr-42` | Backward-compatible alias for `@cellenza retest-ai pr-42` |
| `@cellenza set-prompt pr-42 <instructions>` | Stores custom AI instructions for this environment in the cluster (no operator redeploy needed) |
| `@cellenza show-prompt pr-42` | Displays the current custom prompt for this environment |
| `@cellenza help` | Shows the command list |

### Setup from scratch

#### 1. Create the GitHub App

Go to **github.com → Settings → Developer settings → GitHub Apps → New GitHub App** and fill in:

| Field | Value |
|---|---|
| GitHub App name | `cellenza-extension` (or any name) |
| Homepage URL | your repo URL |
| Webhook URL | leave empty for now — you'll fill it after deploy |
| Webhook secret | generate a random string: `openssl rand -hex 32` |
| Permissions → Repository → Pull requests | Read-only |
| Permissions → Account → Copilot Chat | Read-only |
| Subscribe to events | *(none required)* |
| Where can this GitHub App be installed? | Only on this account |

After creating the App:
- Copy the **Webhook Secret** you generated
- Install the App on your account (Settings → Install App)

#### 2. Create the webhook secret in the cluster

```bash
WEBHOOK_SECRET="<the-secret-you-generated-above>"

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

Verify it is running:

```bash
kubectl get pods -n cellenza-operator-system -l app=cellenza-extension
# NAME                                  READY   STATUS    RESTARTS   AGE
# cellenza-extension-fbd8948cc-xxxxx    1/1     Running   0          1m
```

#### 5. Expose the extension (local Kind with ngrok)

```bash
# Forward the extension service port
kubectl port-forward -n cellenza-operator-system svc/cellenza-extension 8090:8090 &

# Start ngrok
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
@cellenza retest-ai pr-42
```

The extension now patches `spec.aiEnrichment.rerunRequested: true`. The operator owns the rerun from there:
- it deletes AI-generated artifacts and AI jobs
- it replays DB migration/seed when `database.enabled=true`
- it skips the standard smoke/regression/E2E suite for that cycle
- it regenerates `seed.sql` and `test.py`, then re-runs `ai-seed` and `ai-tests`

`@cellenza enrich pr-42` remains available as an alias for backward compatibility.

### Customize AI instructions without redeploying

```
@cellenza set-prompt pr-42 Only test /api/ endpoints. Generate 20 products with realistic prices.
@cellenza retest-ai pr-42
```

`set-prompt` creates a ConfigMap `ai-prompt-pr-42` in `cellenza-operator-system`. The operator reads it automatically when generating seed and tests. No operator redeploy needed — the instructions take effect on the next `retest-ai` call. The ConfigMap is deleted automatically when the environment is removed.

This override is environment-specific. For a cluster-wide default prompt managed by Helm, use
`ai.systemPrompt` or `--set-file ai.systemPrompt=...` on the operator chart.

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
`@cellenza logs pr-42` · `@cellenza extend pr-42` · `@cellenza reset-db pr-42` · `@cellenza retest-ai pr-42` · `@cellenza set-prompt pr-42 <instructions>`
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
  name: pr-<N>-crash
spec:
  branch: <branch>
  prNumber: <N>
  image: ghcr.io/acme/myapp:does-not-exist
  resourceTier: small
  ttl: 1h
  github:
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
kubectl apply -f demo-app/cellenza-demo.yaml
kubectl get cellenza demo --watch
```

This applies:

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Cellenza
metadata:
  name: demo
spec:
  branch: demo
  prNumber: 1
  image: ghcr.io/ihsenalaya/cellenza-demo-app:0.5.1
  resourceTier: medium
  ttl: 72h
  database:
    enabled: true
    version: "15"
    databaseName: appdb
  telemetry:
    enabled: true
    serviceName: cellenza-demo-app
    autoInstrumentation:
      language: python
      instrumentationRef: observability/python
```

Access the app:

```bash
kubectl port-forward -n ingress-nginx svc/ingress-nginx-controller 8080:80
# open http://pr-1.preview.localtest.me:8080
```

Watch PostgreSQL activity in the logs:

```bash
kubectl logs -n preview-pr-1 deployment/app -c app -f
# [db] Opening PostgreSQL connection database=appdb user=preview_1
# [db] Initialized PostgreSQL schema table=messages
# [db] Read messages from PostgreSQL rows=1
# [db] Inserted message into PostgreSQL author=ihsen
```

### Build locally

```bash
docker build -t cellenza-demo-app:local demo-app
kind load docker-image cellenza-demo-app:local --name cellenza-test
kubectl patch cellenza demo --type merge \
  -p '{"spec":{"image":"cellenza-demo-app:local"}}'
```

---

## Useful commands

```bash
# List all preview environments
kubectl get cellenza

# Watch a specific environment come up
kubectl get cellenza pr-42 --watch

# Approve an environment
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"approvedBy":"your-username"}}'

# Delete an environment (triggers immediate cleanup)
kubectl delete cellenza pr-42

# Trigger a database reset (deletes migration/seed jobs and re-runs them)
kubectl patch cellenza pr-42 --type=merge \
  -p '{"spec":{"database":{"resetRequested":true}}}'

# Check controller logs
kubectl logs -n cellenza-operator-system \
  deployment/cellenza-operator -f

# Read pod logs captured at failure time
kubectl get cellenza pr-42 -o jsonpath='{.status.diagnostics.podLogs}' | jq .
```

---

## Helm values reference

```yaml
# charts/cellenza-operator/values.yaml

replicaCount: 1

image:
  repository: ghcr.io/ihsenalaya/cellenza-operator
  pullPolicy: IfNotPresent
  tag: ""             # defaults to Chart.appVersion

ai:
  apiURL: "https://models.inference.ai.azure.com"  # GitHub Models (free tier); use https://api.openai.com/v1 for OpenAI
  systemPrompt: ""                                 # optional global override; defaults to charts/cellenza-operator/files/ai-system-prompt.txt

# Image pull secrets for private GHCR registries
imagePullSecrets: []
# - name: ghcr-pull-secret

# Override the chart or release name
nameOverride: ""
fullnameOverride: ""

# Service account name (leave empty to use the chart default)
serviceAccount:
  name: ""

leaderElect: true     # set false for single-node dev clusters

resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

webhook:
  enabled: true       # requires cert-manager; set false to skip webhook TLS setup
  port: 9443

certManager:
  enabled: true
  issuerName: ""      # leave empty to auto-create a self-signed issuer
                      # or set to an existing cert-manager Issuer name

metrics:
  port: 8443

nodeSelector: {}
tolerations: []
affinity: {}
```

---

## Upgrading

```bash
helm repo update
helm upgrade cellenza-operator cellenza/cellenza-operator \
  --namespace cellenza-operator-system
```

> CRDs are not automatically upgraded by Helm (by design). If a new version changes the CRD schema, apply the updated CRD manually first:
> ```bash
> kubectl apply -f https://raw.githubusercontent.com/ihsenalaya/cellenza-operator/v0.12.8/charts/cellenza-operator/crds/platform.company.io_cellenzas.yaml
> ```

## Uninstalling

```bash
helm uninstall cellenza-operator -n cellenza-operator-system
```

> The CRD and existing `Cellenza` resources are intentionally left in place by Helm to protect your data. Delete them manually if needed:
> ```bash
> kubectl delete crd cellenzas.platform.company.io
> ```

---

## Development

### Prerequisites

- Go 1.25+
- Docker
- `kubectl` with access to a cluster (Kind recommended for local dev)
- `make`
- `kind` (for local testing)
- `gh` CLI (for GitHub integration testing)

### Run locally against a cluster

```bash
# Install CRDs into the cluster
make install

# Run the controller locally (uses your current kubeconfig)
make run
```

### Run tests

```bash
# Unit and integration tests (uses envtest — downloads API server binaries automatically)
make test

# E2E tests against a real Kind cluster
make test-e2e
```

The integration tests use [envtest](https://book.kubebuilder.io/reference/envtest) from controller-runtime, which spins up a real Kubernetes API server in-process — no cluster needed.

### Build and push the operator image

```bash
make docker-build docker-push IMG=ghcr.io/ihsenalaya/cellenza-operator:dev
```

### Build and push the extension image

The extension has its own Dockerfile (`Dockerfile.extension`):

```bash
docker build -f Dockerfile.extension -t ghcr.io/ihsenalaya/cellenza-extension:dev .
docker push ghcr.io/ihsenalaya/cellenza-extension:dev
```

To test locally with Kind:

```bash
docker build -f Dockerfile.extension -t cellenza-extension:local .
kind load docker-image cellenza-extension:local --name cellenza-test
# Update image in config/extension/deployment.yaml, then:
kubectl apply -f config/extension/deployment.yaml
```

### Build and push the demo app image

```bash
docker build -t ghcr.io/ihsenalaya/cellenza-demo-app:dev demo-app
docker push ghcr.io/ihsenalaya/cellenza-demo-app:dev
```

To test locally with Kind:

```bash
docker build -t cellenza-demo-app:local demo-app
kind load docker-image cellenza-demo-app:local --name cellenza-test
kubectl patch cellenza demo --type merge \
  -p '{"spec":{"image":"cellenza-demo-app:local"}}'
```

### CI pipelines

| Workflow | Trigger | What it does |
|---|---|---|
| `lint.yml` | push to any branch/tag | Runs `golangci-lint` |
| `test.yml` | push to any branch/tag | Runs unit + envtest integration tests |
| `test-e2e.yml` | push to any branch/tag | Spins up Kind, installs operator, runs e2e suite |
| `docker-release.yml` | `v*` tag only | Builds and pushes `cellenza-operator` image to GHCR |
| `extension-release.yml` | `v*` tag only | Builds and pushes `cellenza-extension` image to GHCR |
| `demo-app-release.yml` | `v*` tag only | Builds and pushes `cellenza-demo-app` image to GHCR |
| `helm-release.yml` | `v*` tag only | Packages and publishes Helm chart to GitHub Releases + GitHub Pages + GHCR OCI |

### Release a new version

```bash
git tag v0.12.8
git push origin v0.12.8
```

GitHub Actions will automatically:
1. Build and push `ghcr.io/ihsenalaya/cellenza-operator:<version>`
2. Build and push `ghcr.io/ihsenalaya/cellenza-extension:<version>`
3. Build and push `ghcr.io/ihsenalaya/cellenza-demo-app:<version>` (if `demo-app/` changed)
4. Package and publish the Helm chart to GitHub Releases and GitHub Pages
5. Push the chart to `oci://ghcr.io/ihsenalaya/charts/cellenza-operator`

---

## Architecture

```
cellenza-operator/
├── api/v1alpha1/          # CRD types (CellenzaSpec, CellenzaStatus)
├── cmd/
│   ├── main.go            # Operator entry point
│   └── extension/main.go  # Copilot Extension server entry point
├── internal/
│   ├── controller/        # Reconciliation loop + diagnostics
│   ├── extension/         # Copilot Extension HTTP server + commands
│   └── webhook/v1alpha1/  # Defaulter + Validator admission webhooks
├── config/
│   └── extension/         # RBAC + Deployment manifests for the extension
├── charts/
│   └── cellenza-operator/ # Helm chart for distribution
└── .github/workflows/     # CI: docker build, helm release
```

The controller watches `Cellenza` resources cluster-wide and reconciles the following child resources in the PR-specific namespace:

- `Namespace` — isolated per PR (`preview-pr-<number>`)
- `ResourceQuota` — enforces the `resourceTier` limits (extended automatically when PostgreSQL is enabled)
- `Secret` `postgres-credentials` — unique credentials generated with `crypto/rand`, **created once and never overwritten**
- `Deployment` `postgres` — PostgreSQL sidecar (only when `database.enabled: true`)
- `Service` `postgres` — ClusterIP on port 5432, DNS name `postgres` within the namespace
- `Job` `postgres-migrate` / `postgres-seed` — optional one-shot database tasks before app rollout
- `Deployment` `app` — runs the specified image; includes a `busybox` init container that blocks startup until PostgreSQL is ready and optional OpenTelemetry auto-instrumentation annotations
- `Service` `app` — ClusterIP service for the app
- `Ingress` — exposes the environment at `pr-<number>.preview.localtest.me`
- `ConfigMap` `ai-prompt-template` (in `cellenza-operator-system`) — default AI system prompt managed by the Helm chart
- `Job` `ai-schema-dump` — dumps the DB schema via `pg_dump --schema-only` and stores it in a ConfigMap
- `ConfigMap` `ai-enrichment` — holds `seed.sql` (AI-generated INSERT statements) and `test.py` (AI-generated integration tests)
- `Job` `ai-seed` — runs `psql -f /data/seed.sql` against the preview PostgreSQL
- `Job` `ai-tests` — runs `pip install requests && python /data/test.py` with `APP_URL=http://app:80`
- `ConfigMap` `ai-prompt-<name>` (in `cellenza-operator-system`) — optional per-environment AI instructions stored via `@cellenza set-prompt`; appended to the default system prompt and deleted automatically when the environment is removed

A **finalizer** ensures all child resources (including the PostgreSQL deployment and credentials secret) are cleaned up even when the `Cellenza` is force-deleted.

---

## License

Copyright 2026 ihsenalaya — Licensed under the [Apache License 2.0](LICENSE).
