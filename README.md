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
  • ResourceQuota  (based on resourceTier)
  • Secret         postgres-credentials  (unique credentials, generated once)
  • Deployment     postgres              (optional, when database.enabled=true)
  • Service        postgres
  • Deployment     app  (waits for postgres via init container)
  • Service        app
  • Ingress        →  pr-42.preview.localtest.me
  • OTEL annotations/env vars (optional, when telemetry.enabled=true)
      │
      ▼
Operator updates GitHub Deployment / PR comment
when github.enabled=true
      │
      ▼
Environment runs until TTL expires
or until the Cellenza is deleted
```

An **approval gate** is available for sensitive environments: set `requiresApproval: true` and the operator will block provisioning until a human sets `approvedBy`.

---

## Prerequisites

| Requirement | Version |
|---|---|
| Kubernetes | 1.25+ |
| cert-manager | 1.13+ (required for webhooks) |
| nginx ingress controller | any recent version |
| OpenTelemetry Operator | optional, required for app auto-instrumentation |
| GitHub token Secret | optional, required for `github.enabled=true` |
| Helm | 3.12+ |

---

## Installation

### 1. Add the Helm repository

```bash
helm repo add cellenza https://ihsenalaya.github.io/cellenza-operator
helm repo update
```

### 2. Install cert-manager (if not already present)

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  --set crds.enabled=true
```

### 3. Install ingress-nginx (if not already present)

Preview environments are exposed through Kubernetes `Ingress` resources. Install an ingress controller before creating `Cellenza` resources:

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace
```

For Kind/local clusters, map the ingress controller ports when creating the cluster, or use a local port-forward while testing:

```bash
kubectl port-forward -n ingress-nginx svc/ingress-nginx-controller 8080:80
```

### 4. Install OpenTelemetry Operator (optional)

Required only when using `telemetry.autoInstrumentation` in your `Cellenza` resources.

```bash
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo update
helm install opentelemetry-operator open-telemetry/opentelemetry-operator \
  --namespace opentelemetry-operator-system \
  --create-namespace \
  --set "manager.collectorImage.repository=otel/opentelemetry-collector-contrib" \
  --set admissionWebhooks.certManager.enabled=true \
  --wait
```

> cert-manager (step 2) must be installed before the OTel Operator.

### 5. Install Jaeger (optional)

A simple all-in-one Jaeger instance to receive and visualize traces. Skip this step if you already have a tracing backend.

```bash
helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
helm repo update
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

Then deploy the OTel Collector and the `Instrumentation` CR into the `observability` namespace:

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
  --create-namespace
```

The chart installs the CRD, RBAC, webhooks, and the controller in one shot.

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
  --version 0.9.0 \
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
kubectl create secret generic github-token-pr-42 \
  --namespace=cellenza-operator-system \
  --from-literal=token="$GITHUB_TOKEN"
```

> **Token expiry** — GitHub Apps issue short-lived tokens. If the controller logs `401 Unauthorized`, regenerate the token and replace the Secret:
> ```bash
> kubectl create secret generic github-token-pr-42 \
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
      name: github-token-pr-42
      namespace: cellenza-operator-system
      key: token
```

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

When the resource is deleted, the finalizer sends an `inactive` status before cleanup completes.

Read the credentials at any time:

```bash
kubectl get secret postgres-credentials -n preview-pr-42 \
  -o jsonpath='{.data.DATABASE_URL}' | base64 -d
```

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

### Resource tiers

When `database.enabled: true`, the operator automatically adds PostgreSQL headroom (+500m CPU / +512Mi RAM) to the namespace quota.

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
| `status.conditions` | Kubernetes-standard conditions: `Ready`, `Approved`, `Expired`, `DatabaseReady`, `MigrationReady`, `SeedReady` |

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
     cellenza-extension server
                │  (controller-runtime + kubernetes client)
                ▼
        Kubernetes API Server
                │
                ▼
         Cellenza CR → response streamed back to Copilot
```

The extension server is a separate binary (`cmd/extension/`) deployed in `cellenza-operator-system`. It speaks the GitHub Copilot Extension protocol (OpenAI-compatible SSE streaming) and talks to the Kubernetes API with the same privileges as a read/patch role on `Cellenza` resources.

### Available commands

| Command | Description |
|---|---|
| `@cellenza list` | List all active environments with phase and TTL |
| `@cellenza status pr-42` | Phase, URL, DB state, running time, TTL remaining |
| `@cellenza logs pr-42` | Last 40 lines from the app pod |
| `@cellenza extend pr-42 [24h]` | Extend TTL (patches `spec.ttl` and `status.expiresAt`) |
| `@cellenza wake pr-42` | Restart a scaled-down environment (sets `spec.replicas=1`) |
| `@cellenza reset-db pr-42` | Delete migration/seed jobs and re-run them (sets `spec.database.resetRequested=true`) |
| `@cellenza help` | Show all commands |

### Deploy

```bash
kubectl apply -f config/extension/rbac.yaml
kubectl apply -f config/extension/deployment.yaml
kubectl -n cellenza-operator-system rollout status deployment/cellenza-extension --timeout=60s
```

### Expose for local Kind (ngrok)

```bash
kubectl port-forward -n cellenza-operator-system svc/cellenza-extension 8090:8090 &
ngrok http 8090
# Copy the HTTPS URL → paste as webhook URL in your GitHub App settings
```

### Trigger a database reset from Copilot Chat

```
@cellenza reset-db pr-42
```

The extension patches `spec.database.resetRequested: true`. The controller detects this on the next reconcile loop, deletes both migration and seed jobs, clears `status.database`, and re-runs the full database setup sequence. The flag is cleared automatically once the reset starts.

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

The extension responds with the current phase, diagnostics, and actionable next steps:

```
pr-42 is Failed (since 14:08 UTC)

Diagnosis: Container image cannot be pulled (confidence: high)
  Image: ghcr.io/acme/myapp:does-not-exist
  Component: app

Recommendations:
  1. Verify the image tag exists in GHCR for this PR's CI build.
  2. Check imagePullSecrets if the registry is private.

Debug:
  kubectl get pods -n preview-pr-42
  kubectl describe deployment app -n preview-pr-42
```

They can then request the raw logs:

```
@cellenza logs pr-42
```

Or trigger a database reset after fixing a migration:

```
@cellenza reset-db pr-42
```

The controller patches `spec.database.resetRequested: true`, deletes the failed Jobs, and re-runs migration and seed on the next reconcile cycle.

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
  tag: ""          # defaults to Chart.appVersion

leaderElect: true  # set false for single-node dev clusters

resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

webhook:
  enabled: true    # requires cert-manager
  port: 9443

certManager:
  enabled: true
  issuerName: ""   # leave empty to create a self-signed issuer automatically
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
> kubectl apply -f https://raw.githubusercontent.com/ihsenalaya/cellenza-operator/v0.9.0/charts/cellenza-operator/crds/platform.company.io_cellenzas.yaml
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

- Go 1.24+
- Docker
- `kubectl` with access to a cluster
- `make`

### Run locally against a cluster

```bash
# Install CRDs
make install

# Run the controller locally (uses your current kubeconfig)
make run
```

### Build and push your own image

```bash
make docker-build docker-push IMG=ghcr.io/ihsenalaya/cellenza-operator:dev
```

### Build and publish the demo app image

The demo app image is built automatically by GitHub Actions when files under `demo-app/` are pushed to `main`:

```text
ghcr.io/ihsenalaya/cellenza-demo-app:latest
ghcr.io/ihsenalaya/cellenza-demo-app:<version>
```

To build it locally for a Kind cluster:

```bash
docker build -t cellenza-demo-app:db-logs demo-app
kind load docker-image cellenza-demo-app:db-logs --name cellenza-test
kubectl patch cellenza demo --type merge \
  -p '{"spec":{"image":"cellenza-demo-app:db-logs"}}'
```

### Run tests

```bash
make test
```

### Release a new version

```bash
git tag v0.5.1
git push origin v0.5.1
```

GitHub Actions will:
1. Build and push the Docker image to GHCR
2. Package the Helm chart and publish it to GitHub Releases and GitHub Pages

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

A **finalizer** ensures all child resources (including the PostgreSQL deployment and credentials secret) are cleaned up even when the `Cellenza` is force-deleted.

---

## License

Copyright 2026 ihsenalaya — Licensed under the [Apache License 2.0](LICENSE).
