# Preview CRD — The Core Custom Resource

The **Preview** Custom Resource Definition (CRD) is the central abstraction of the platform. Every pull request is represented as one Preview CR, which the operator reconciles continuously until the preview is terminated.

---

## What it's for

- Declare a preview environment request with spec (image, database, tests, AI settings)
- Track the entire lifecycle: Pending → Provisioning → Running → Terminating
- Persist observed state (status) so the controller can resume after restarts
- Control approval gates, resource allocation, time-to-live (TTL), and feature toggles

---

## What it does

When you create or update a Preview CR:

1. **Validation webhook** ensures required fields are present and configuration is sound
2. **Controller reconcile loop** creates resources in dependency order:
   - Namespace with isolation policy (NetworkPolicy, Pod Security Standards)
   - PostgreSQL instance (if `spec.database.enabled`)
   - One Deployment per service (backend, frontend, etc.)
   - Istio VirtualService or Nginx Ingress for public access
   - Test pipeline Jobs (smoke, regression, E2E)
3. **Status subresource** records progress: which phase, URL, test results, errors
4. **Finalizer** ensures cleanup runs when the CR is deleted
5. **Garbage collection** deletes all owned resources (Deployments, Services, ConfigMaps, Jobs)

---

## API Overview

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Preview
metadata:
  name: pr-42
  namespace: default
spec:
  # Identity
  branch: feature/my-feature
  prNumber: 42
  image: ghcr.io/myorg/myapp:sha-abc123
  ttl: 48h
  resourceTier: medium

  # Database (optional)
  database:
    enabled: true
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
    seed:
      enabled: true
      command: ["python", "scripts/seed.py"]

  # Multi-service routing (optional)
  services:
    - name: backend
      image: ghcr.io/myorg/myapp:sha-abc123
      port: 8080
      pathPrefix: /api
    - name: frontend
      image: ghcr.io/myorg/myapp:sha-abc123
      port: 3000
      pathPrefix: /

  # Tests (optional)
  testSuite:
    enabled: true
    smoke: {}
    regression:
      enabled: true
    e2e:
      enabled: true

  # AI enrichment (optional)
  aiEnrichment:
    enabled: true
    model: gpt-4o-mini
    seed:
      enabled: true
    tests:
      enabled: true

  # GitHub integration (optional)
  github:
    enabled: true
    owner: myorg
    repo: myapp
    deploymentId: 123456789
    environment: pr-42
    tokenSecretRef:
      name: github-token
      namespace: preview-operator-system
      key: token

status:
  phase: Running
  url: http://pr-42.preview.example.com
  readyAt: "2026-06-01T10:00:00Z"
  expiresAt: "2026-06-03T10:00:00Z"
  tests:
    phase: Succeeded
    smoke:
      phase: Succeeded
      passed: 2
      failed: 0
    regression:
      phase: Succeeded
      passed: 9
      failed: 0
    e2e:
      phase: Succeeded
      passed: 6
      failed: 0
```

---

## Spec Fields

### Identity

| Field | Type | Purpose |
|-------|------|---------|
| `branch` | string | Git branch name (informational) |
| `prNumber` | int | Pull request number for GitHub integration |
| `image` | string | Container image (used if `services[]` is empty) |
| `ttl` | duration | Time-to-live before auto-deletion (default 48h) |
| `resourceTier` | enum | Resource limits: `small` \| `medium` \| `large` |

### Database

When `database.enabled=true`, the operator creates a PostgreSQL Deployment, Service, and Secret. Credentials are injected into app Pods as `DATABASE_URL`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`.

| Field | Type | Purpose |
|-------|------|---------|
| `enabled` | bool | Provision PostgreSQL (default: false) |
| `version` | string | PostgreSQL major version (default: "15") |
| `databaseName` | string | Logical database name (default: "appdb") |
| `migration.enabled` | bool | Run migration Job before app deployment |
| `migration.command` | []string | Entrypoint (e.g., Alembic upgrade) |
| `seed.enabled` | bool | Run seed Job after migration |
| `seed.command` | []string | Entrypoint (e.g., SQL or Python script) |
| `resetRequested` | bool | Trigger database reset; operator clears after reset starts |
| `checkpointSave` | string | Trigger pg_dump snapshot; operator clears after Job completes |
| `checkpointRestore` | string | Trigger restore from named snapshot; operator clears after Job completes |

### Services (multi-service routing)

When `services[]` is set, the operator creates one Deployment + Service per entry. The ingress routes based on `pathPrefix`.

| Field | Type | Purpose |
|-------|------|---------|
| `name` | string | Service identifier (also used as Deployment name) |
| `image` | string | Container image (overrides `spec.image` if set) |
| `port` | int | Container port (e.g., 8080) |
| `pathPrefix` | string | Ingress route prefix (e.g., `/api`, `/`) |
| `env` | []EnvVar | Environment variables injected into the Pod |

### Test Suite

Controls which test suites are enabled. When `testStrategy.mode=Auto`, the test-strategist agent picks which suites actually run.

| Field | Type | Purpose |
|-------|------|---------|
| `enabled` | bool | Enable test suite orchestration |
| `smoke` | object | Built-in smoke tests (always runs if enabled) |
| `contract` | object | OpenAPI contract testing (Microcks) |
| `migration` | object | Validate database migration scripts |
| `regression` | object | Run `tests/regression.py` |
| `e2e` | object | Playwright E2E tests (`tests/e2e.py`) |

### AI Enrichment

Automatically generates seed data and targeted tests using an LLM.

| Field | Type | Purpose |
|-------|------|---------|
| `enabled` | bool | Enable AI enrichment |
| `model` | string | Model to use (e.g., `gpt-4o-mini`, `gpt-4o`) |
| `seed.enabled` | bool | Generate and run seed.sql |
| `tests.enabled` | bool | Generate and run test.py |
| `rerunRequested` | bool | Trigger AI-only rerun (without re-running full test suite) |

### GitHub Integration

Integrate with GitHub Deployments API and post PR comments with results.

| Field | Type | Purpose |
|-------|------|---------|
| `enabled` | bool | Post deployment status and comments |
| `owner` | string | GitHub repository owner |
| `repo` | string | Repository name |
| `deploymentId` | int | GitHub Deployment ID (returned by `createDeployment()`) |
| `environment` | string | Deployment environment name |
| `tokenSecretRef` | ObjectReference | Secret containing GitHub PAT |

---

## Status Fields

The controller updates these fields as the preview progresses. You can read status with:

```bash
kubectl get preview pr-42 -o jsonpath='{.status}' | jq .
```

| Field | Type | Meaning |
|-------|------|---------|
| `phase` | string | Lifecycle phase: `Pending` \| `Provisioning` \| `Running` \| `Terminating` \| `Failed` |
| `url` | string | Public preview URL (e.g., `http://pr-42.preview.example.com`) |
| `namespaceName` | string | Kubernetes namespace (e.g., `preview-pr-42`) |
| `readyAt` | time | When the preview reached Running phase |
| `expiresAt` | time | When the preview will be auto-deleted |
| `database.ready` | bool | PostgreSQL Deployment is Running |
| `database.host` | string | Service DNS name (`postgres`) |
| `database.migration` | string | Migration Job status: `Pending` \| `Running` \| `Succeeded` \| `Failed` |
| `database.seed` | string | Seed Job status |
| `aiEnrichment.phase` | string | AI pipeline status: `Pending` \| `Running` \| `Succeeded` \| `Failed` |
| `aiEnrichment.seedStatus` | string | AI seed Job status |
| `aiEnrichment.testsStatus` | string | AI test Job status |
| `tests.phase` | string | Test suite status: `Pending` \| `Running` \| `Succeeded` \| `Failed` |
| `tests.smoke` | object | Smoke test results: `{phase, passed, failed}` |
| `tests.regression` | object | Regression test results |
| `tests.e2e` | object | E2E test results |
| `github.deploymentState` | string | GitHub Deployment status: `queued` \| `in_progress` \| `success` \| `failure` \| `inactive` |

---

## Lifecycle Phases

```
Pending
  ├─ requiresApproval=true?
  └─ Wait for approvedBy to be set

Provisioning
  ├─ Create namespace + isolation policy
  ├─ Create PostgreSQL + migration + seed jobs
  ├─ Create service Deployments
  ├─ Create Ingress/VirtualService
  └─ Post GitHub comment: "🔄 Provisioning…"

Running
  ├─ All services ready
  ├─ Post GitHub Deployment: success + URL
  ├─ Post PR comment: "## Preview Ready"
  │
  ├─ [Optional] AI enrichment pipeline
  ├─ [Optional] Test strategy (agent picks test suites)
  └─ [Optional] Test suite orchestration

Terminating (on PR closed / TTL expired)
  ├─ Delete all namespace resources
  ├─ Delete namespace
  └─ Post GitHub Deployment: inactive

Failed (on reconcile error)
  └─ Capture diagnostics + pod logs in status
```

---

## Common Operations

### Create a preview

```bash
kubectl apply -f - <<EOF
apiVersion: platform.company.io/v1alpha1
kind: Preview
metadata:
  name: pr-42
spec:
  prNumber: 42
  branch: feature/my-feature
  image: ghcr.io/myorg/myapp:latest
  database:
    enabled: true
  testSuite:
    enabled: true
EOF
```

### Check status

```bash
# Quick overview
kubectl get preview pr-42

# Full status (JSON)
kubectl get preview pr-42 -o jsonpath='{.status}' | jq .

# Watch as it progresses
kubectl get preview pr-42 -w
```

### Extend TTL

```bash
# Add 24 hours to expiration
kubectl patch preview pr-42 --type=merge -p '{"spec":{"ttl":"72h"}}'
```

### Trigger database reset

```bash
kubectl patch preview pr-42 --type=merge -p '{"spec":{"database":{"resetRequested":true}}}'
# Controller clears the flag after reset starts
```

### Trigger AI-only rerun

```bash
kubectl patch preview pr-42 --type=merge \
  -p '{"spec":{"aiEnrichment":{"rerunRequested":true}}}'
```

### Delete a preview

```bash
kubectl delete preview pr-42
# Finalizer ensures cleanup runs — namespace deleted, GitHub Deployment marked inactive
```

---

## Admission Webhooks (Defaulter + Validator)

Before the CR reaches the controller, two webhooks run:

### Defaulter (Mutating)

| Condition | Action |
|-----------|--------|
| `spec.resourceTier: large` | Automatically sets `spec.requiresApproval: true` |
| `spec.aiEnrichment.enabled: true` | Defaults `seed.enabled=true` and `tests.enabled=true` if omitted |

### Validator (Validating)

| Trigger | Rejection |
|---------|-----------|
| `spec.resourceTier: large` | Requires explicit `spec.requiresApproval: true` |
| `spec.database.migration.enabled: true` && no `spec.database.migration.command` | ❌ Invalid |
| `spec.database.seed.enabled: true` && no `spec.database.seed.command` | ❌ Invalid |
| `spec.github.enabled: true` && missing owner/repo/deploymentId/tokenSecretRef | ❌ Invalid |
| `spec.telemetry.enabled: true` && no `spec.telemetry.autoInstrumentation` | ❌ Invalid |

---

## Environment Variables (injected automatically)

Every service Pod receives these environment variables:

| Variable | Example |
|----------|---------|
| `PREVIEW_PR` | `42` |
| `PREVIEW_BRANCH` | `feature/my-feature` |
| `PREVIEW_NAMESPACE` | `preview-pr-42` |
| `DATABASE_URL` | `postgresql://preview_42:pw@postgres:5432/appdb` |
| `POSTGRES_USER` | `preview_42` |
| `POSTGRES_PASSWORD` | *(generated)* |
| `POSTGRES_DB` | `appdb` |
| `OTEL_SERVICE_NAME` | `idp-preview-pr-42` |
| `OTEL_RESOURCE_ATTRIBUTES` | `preview.name=pr-42,preview.pr_number=42,…` |

---

## Resource Tiers

| Tier | CPU request | CPU limit | Memory request | Memory limit |
|------|-------------|-----------|----------------|--------------|
| `small` | 100m | 250m | 128Mi | 256Mi |
| `medium` | 200m | 500m | 256Mi | 512Mi |
| `large` | 500m | 2000m | 512Mi | 2Gi |

Tier `large` triggers automatic `spec.requiresApproval: true` (cannot be overridden).

---

## Finalizer

The Preview controller installs a finalizer on every CR: `platform.company.io/preview-finalizer`. When the CR is deleted, the finalizer ensures the controller runs cleanup:

1. Delete all owned resources (Deployments, Services, ConfigMaps, Jobs, Pods)
2. Delete the namespace
3. Post GitHub Deployment: inactive
4. Remove the finalizer (allows the CR to be garbage-collected)

If you force-delete a Preview without waiting for the finalizer, the namespace may remain (orphaned). To manually clean up:

```bash
# Find orphaned namespaces
kubectl get ns | grep preview-pr-

# Delete manually
kubectl delete ns preview-pr-42
```

---

## Relationships

```
Preview (1)
  ├─ owns → Namespace (1)
  ├─ owns → Deployment (1 per service)
  ├─ owns → Service (1 per service)
  ├─ owns → ConfigMap (for database checkpoints, AI artifacts)
  ├─ owns → Secret (database credentials)
  ├─ owns → Job (migration, seed, smoke, regression, E2E, AI jobs)
  ├─ owns → Ingress or VirtualService (1)
  ├─ references → GitHub Deployment (via API, not in-cluster)
  ├─ references → TestPlan (when strategy.mode=Auto)
  ├─ references → TestRun (for test results tracking)
  └─ references → ReconcileEvent (for agent historical signal)
```

---

## Troubleshooting

### Preview stuck in Provisioning

```bash
kubectl describe preview pr-42
kubectl get events -n preview-pr-42 --sort-by='.lastTimestamp'
kubectl get pods -n preview-pr-42
```

### Preview Failed with diagnostics

```bash
kubectl get preview pr-42 -o jsonpath='{.status.diagnostics}' | jq .
# .podLogs      → last 30 lines from app Pod
# .lastEvents   → recent Warning events
# .debugCommands → kubectl commands for further investigation
```

### Database not ready

```bash
kubectl get deployment -n preview-pr-42 postgres
kubectl logs -n preview-pr-42 -l app=postgres
```
