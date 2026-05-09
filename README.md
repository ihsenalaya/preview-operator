# Preview Operator

> **A Kubernetes operator that turns every pull request into a fully isolated preview environment — multi-service stack, ephemeral PostgreSQL, sequential test pipeline, OpenTelemetry, AI-generated seed data, and GitHub integration — all from a single custom resource.**

```bash
kubectl apply -f pr-42.yaml
# → http://pr-42.preview.localtest.me/        (frontend)
# → http://pr-42.preview.localtest.me/api     (backend)
# → operator runs AI enrichment → smoke → regression → E2E
# → results posted to the GitHub PR as a comment
# → kubectl delete preview pr-42 → full cleanup
```

---

## Table of Contents

1. [Feature Matrix](#1-feature-matrix)
2. [General Architecture](#2-general-architecture)
3. [Installation](#3-installation)
4. [Controller Deep Dive](#4-controller-deep-dive)
5. [Ephemeral PostgreSQL](#5-ephemeral-postgresql)
6. [Database Checkpoints](#6-database-checkpoints)
7. [Multi-Service Mode](#7-multi-service-mode)
8. [OpenTelemetry Auto-Instrumentation](#8-opentelemetry-auto-instrumentation)
9. [GitHub Integration](#9-github-integration)
10. [Automated Test Suite](#10-automated-test-suite)
11. [AI Enrichment](#11-ai-enrichment)
12. [Approval Gate](#12-approval-gate)
13. [Resource Tiers & Quota Management](#13-resource-tiers--quota-management)
14. [TTL & Auto-Expiry](#14-ttl--auto-expiry)
15. [Smart Diagnostics](#15-smart-diagnostics)
16. [Copilot Extension](#16-copilot-extension)
17. [Complete CR Reference](#17-complete-cr-reference)
18. [Status Fields Reference](#18-status-fields-reference)
19. [Helm Values Reference](#19-helm-values-reference)
20. [Development & Release](#20-development--release)
21. [Debugging & Troubleshooting](#21-debugging--troubleshooting)

---

## 1. Feature Matrix

| Feature | Flag / Field | Default |
|---------|-------------|---------|
| Isolated namespace per PR | always on | — |
| Ephemeral PostgreSQL | `spec.database.enabled` | `false` |
| DB migrations | `spec.database.migration.enabled` | `false` |
| Static DB seed | `spec.database.seed.enabled` | `false` |
| DB checkpoint save/restore | `spec.database.checkpointSave/Restore` | — |
| DB reset on demand | `spec.database.resetRequested` | `false` |
| Multi-service (frontend + backend) | `spec.services[]` | — |
| Single-service | `spec.image` | required |
| Resource tiers | `spec.resourceTier` | `medium` |
| Custom replicas | `spec.replicas` | `1` |
| Approval gate | `spec.requiresApproval` | `false` |
| TTL auto-expiry | `spec.ttl` | `48h` |
| OpenTelemetry auto-instrumentation | `spec.telemetry.enabled` | `false` |
| GitHub Deployment + PR comments | `spec.github.enabled` | `false` |
| Smoke tests | `spec.testSuite.smoke` | built-in |
| Regression tests | `spec.testSuite.regression.enabled` | `false` |
| E2E tests (Playwright) | `spec.testSuite.e2e.enabled` | `false` |
| AI seed data + tests | `spec.aiEnrichment.enabled` | `false` |
| AI-only rerun | `spec.aiEnrichment.rerunRequested` | `false` |
| Smart failure diagnostics | always on when `Failed` | — |
| Copilot Extension commands | sidecar server | optional |

---

## 2. General Architecture

### End-to-end flow — CR creation to deletion

```
┌────────────────────────────────────────────────────────────────────────────┐
│  kubectl apply / GitHub Actions (preview.yaml)                             │
│  apiVersion: platform.company.io/v1alpha1                                  │
│  kind: Preview  metadata.name: pr-42                                      │
└───────────────────────────┬────────────────────────────────────────────────┘
                            │  CR written to etcd
                            ▼
┌────────────────────────────────────────────────────────────────────────────┐
│  PreviewReconciler  (controller-runtime reconcile loop)                   │
│                                                                            │
│  1. Fetch CR                                                               │
│  2. Deletion? → handleDeletion() → finalizer teardown                     │
│  3. Add finalizer (first time)                                             │
│  4. Detect spec change → resetDerivedStateForNewGeneration()               │
│  5. TTL expired? → r.Delete(preview)                                      │
│  6. requiresApproval && !approvedBy → phase=Pending, RequeueAfter=30s     │
│  7. Set ExpiresAt (once), phase=Provisioning                               │
│                                                                            │
│  reconcileProvisioning()                                                   │
│    ├── reconcileNamespace()      → Namespace preview-pr-<N>               │
│    ├── reconcileResourceQuota()  → ResourceQuota preview-quota            │
│    ├── handleResetRequested()    → delete jobs, re-run DB                  │
│    ├── handleAIRerunRequested()  → delete AI artifacts, re-run AI          │
│    ├── reconcileDatabaseWait()   → Secret + Postgres + migrate + seed     │
│    │     RequeueAfter=10s while DB not ready                               │
│    ├── reconcileCheckpoints()    → save/restore jobs                       │
│    ├── reconcileDeployment()     → svc-backend + svc-frontend (or app)    │
│    ├── reconcileService()        → ClusterIP Services                      │
│    ├── reconcileIngress()        → nginx Ingress, path-based routing       │
│    ├── handleAppAvailability()   → RequeueAfter=10s if not yet ready      │
│    ├── markRunningStatus()       → phase=Running, URL, conditions         │
│    ├── syncGitHubAfterStatus()   → Deployment: success + PR comment       │
│    │                                                                        │
│    ├── reconcileAIEnrichment()   → (if enabled)                           │
│    │     schema-dump → generate → ai-seed → ai-tests                      │
│    │     RequeueAfter=10s until Succeeded or Failed                        │
│    │                                                                        │
│    └── reconcileTestSuite()      → (if enabled AND AI done or disabled)   │
│          saving → smoke → restore-regression → regression →                │
│          restore-e2e → e2e                                                 │
│          RequeueAfter=10s at each step                                     │
│                                                                            │
│  RequeueAfter = ttlRemaining (keeps controller alive until expiry)        │
└───────────────────────────┬────────────────────────────────────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         ▼                  ▼                  ▼
   kubectl delete     TTL expired        rerunRequested=true
   PR closed          r.Delete(cz)       AI-only rerun cycle
         │                  │
         └──────────────────┘
                   │  finalizer runs
         ┌─────────▼──────────────────┐
         │  handleDeletion()           │
         │  delete namespace           │
         │  delete AI prompt ConfigMap │
         │  remove finalizer           │
         │  GitHub: inactive           │
         └─────────────────────────────┘
```

### Namespace isolation model

```
cluster
│
├── preview-operator-system/       ← operator + extension + secrets
│     ├── preview-operator pod
│     ├── preview-extension pod
│     ├── Secret: preview-github-token
│     ├── Secret: ai-api-key
│     ├── ConfigMap: ai-prompt-template   (Helm-managed system prompt)
│     └── ConfigMap: ai-prompt-pr-42      (per-env override, optional)
│
├── preview-pr-1/                   ┐
│     ├── ResourceQuota             │  completely isolated namespace
│     ├── postgres Deployment       │  one per open Preview CR
│     ├── postgres Service          │
│     ├── Secret: postgres-credentials │
│     ├── svc-backend Deployment    │
│     ├── svc-backend Service       │
│     ├── svc-frontend Deployment   │
│     ├── svc-frontend Service      │
│     ├── Ingress: pr-1.preview.*   │
│     ├── ConfigMap: ai-enrichment  │
│     ├── ConfigMap: db-checkpoint-after-seed │
│     └── Jobs: postgres-migrate, ai-seed,   │
│               smoke-tests, e2e-tests, …    ┘
│
├── preview-pr-2/                   ← PR #2 is entirely independent
└── preview-pr-N/ …
```

---

## 3. Installation

### Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Kubernetes | 1.25+ | Kind, k3s, GKE, AKS, EKS, or any conformant cluster |
| Helm | 3.12+ | |
| cert-manager | 1.13+ | Required for webhook TLS — skip with `--set webhook.enabled=false` |
| ingress-nginx | any recent | Exposes preview URLs |
| kubectl | 1.25+ | |
| OpenTelemetry Operator | optional | Required only for `telemetry.autoInstrumentation` |

### Step 0 — Create a Kind cluster (local only)

```bash
cat <<EOF | kind create cluster --name preview --config=-
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

kubectl get nodes
# NAME                    STATUS   ROLES           AGE   VERSION
# preview-control-plane  Ready    control-plane   …     v1.35.0
```

Preview URLs will be reachable at `http://pr-42.preview.localtest.me:8080` — `localtest.me` resolves to `127.0.0.1`, no DNS configuration needed.

### Step 1 — Add Helm repositories

```bash
helm repo add jetstack       https://charts.jetstack.io
helm repo add ingress-nginx  https://kubernetes.github.io/ingress-nginx
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo add jaegertracing  https://jaegertracing.github.io/helm-charts
helm repo update
```

### Step 2 — Install cert-manager

```bash
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.20.2 \
  --set crds.enabled=true \
  --wait

kubectl -n cert-manager rollout status deployment/cert-manager --timeout=120s
kubectl -n cert-manager rollout status deployment/cert-manager-webhook --timeout=120s
```

### Step 3 — Install ingress-nginx

**Kind clusters** — admission webhooks must be disabled (webhook cert is self-signed and not trusted by the Kind API server):

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.admissionWebhooks.enabled=false \
  --wait

kubectl -n ingress-nginx rollout status deployment/ingress-nginx-controller --timeout=120s
```

**Production clusters** — keep the admission webhook:

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --wait
```

> If already installed without the flag and you see `x509: certificate signed by unknown authority`:
> ```bash
> kubectl delete validatingwebhookconfiguration ingress-nginx-admission --ignore-not-found
> helm upgrade ingress-nginx ingress-nginx/ingress-nginx \
>   --namespace ingress-nginx \
>   --set controller.admissionWebhooks.enabled=false --wait
> ```

### Step 4 — Install OpenTelemetry Operator (optional)

Required only when using `spec.telemetry.autoInstrumentation`.

```bash
helm install opentelemetry-operator open-telemetry/opentelemetry-operator \
  --namespace opentelemetry-operator-system \
  --create-namespace \
  --set admissionWebhooks.certManager.enabled=true \
  --set manager.collectorImage.repository=otel/opentelemetry-collector-contrib \
  --wait

kubectl -n opentelemetry-operator-system rollout status deployment/opentelemetry-operator --timeout=120s
```

### Step 5 — Install Jaeger (optional)

For local trace visualization:

```bash
helm install jaeger jaegertracing/jaeger \
  --namespace observability \
  --create-namespace \
  --set allInOne.enabled=true \
  --set provisionDataStore.cassandra=false \
  --set storage.type=memory \
  --set allInOne.extraEnv[0].name=COLLECTOR_OTLP_ENABLED \
  --set allInOne.extraEnv[0].value="true" \
  --wait

kubectl port-forward -n observability svc/jaeger 16686:16686
# → http://localhost:16686
```

### Step 6 — Install the Preview Operator

**Via OCI (GHCR) — recommended:**

```bash
# Always apply the CRD first when installing or upgrading
helm show crds oci://ghcr.io/ihsenalaya/charts/preview-operator --version 0.13.8 \
  | tail -n +3 \
  | kubectl apply -f -

helm install preview-operator \
  oci://ghcr.io/ihsenalaya/charts/preview-operator \
  --version 0.13.8 \
  --namespace preview-operator-system \
  --create-namespace \
  --wait

kubectl -n preview-operator-system rollout status deployment/preview-operator --timeout=120s
kubectl get crd previews.platform.company.io
```

> `tail -n +3` strips the two-line Helm OCI pull header (`Pulled: …` / `Digest: …`) that `helm show crds` prepends before the YAML.

**Without cert-manager (no admission webhooks):**

```bash
helm install preview-operator \
  oci://ghcr.io/ihsenalaya/charts/preview-operator \
  --version 0.13.8 \
  --namespace preview-operator-system \
  --create-namespace \
  --set webhook.enabled=false \
  --wait
```

> Without webhooks, spec defaults are not applied at admission time and invalid specs fail at reconcile time rather than being rejected at apply.

**Verify:**

```bash
kubectl get pods -n preview-operator-system
# preview-operator-647dc9db-xxxxx   1/1   Running   0   30s

kubectl get crd previews.platform.company.io
# NAME                             CREATED AT
# previews.platform.company.io   2026-05-08T09:00:00Z
```

### Upgrading the operator

```bash
# Step 1 — CRD first (Helm never auto-updates CRDs)
helm show crds oci://ghcr.io/ihsenalaya/charts/preview-operator --version 0.13.8 \
  | tail -n +3 \
  | kubectl apply -f -

# Step 2 — Operator image
helm upgrade preview-operator \
  oci://ghcr.io/ihsenalaya/charts/preview-operator \
  --version 0.13.8 \
  --namespace preview-operator-system

kubectl -n preview-operator-system rollout status deployment/preview-operator --timeout=120s
```

> **Why CRD first?** If a new operator version writes a new status field that is not in the CRD schema, the API server silently strips it on every write. The controller then re-writes it on the next reconcile, causing an **infinite reconcile loop** every few seconds. Always apply the CRD before the operator image.

### Uninstalling

```bash
helm uninstall preview-operator -n preview-operator-system

# CRD and existing Preview resources are preserved on purpose.
# Delete manually if needed:
kubectl delete crd previews.platform.company.io
```

### Configure AI enrichment

**OpenAI:**

```bash
kubectl create secret generic ai-api-key \
  --namespace preview-operator-system \
  --from-literal=api-key="sk-..."
```

**GitHub Models (free tier):**

```bash
kubectl create secret generic ai-api-key \
  --namespace preview-operator-system \
  --from-literal=api-key="<GITHUB_TOKEN>"

kubectl set env deployment/preview-operator \
  AI_API_URL=https://models.inference.ai.azure.com \
  -n preview-operator-system
```

**GitHub integration secret:**

```bash
kubectl create secret generic preview-github-token \
  --namespace preview-operator-system \
  --from-literal=token="<GITHUB_PAT>"
```

---

## 4. Controller Deep Dive

### Source files

```
internal/controller/
├── preview_controller.go   Main reconcile loop, namespace, quota, deployments, ingress
├── ai_enrichment.go         AI schema dump, generation, seed job, test job, prompt handling
├── checkpoint.go            DB checkpoint save/restore via pg_dump / psql Jobs
├── diagnostics.go           Failure root-cause analysis, log collection, debug commands
├── github.go                GitHub Deployment status and PR comment publishing
└── tests.go                 Smoke, regression, E2E job orchestration and step state machine
```

### Full reconcile sequence

```
Reconcile(ctx, Request{Name: "pr-42"})
     │
     ├─ 1. r.Get(preview)               → NotFound → return nil (deleted, no finalizer)
     │
     ├─ 2. DeletionTimestamp set?        → handleDeletion()
     │         ├── delete preview namespace (all child resources cascade)
     │         ├── delete ai-prompt-pr-42 ConfigMap (operator namespace)
     │         ├── GitHub Deployment → inactive
     │         └── controllerutil.RemoveFinalizer → r.Update()
     │
     ├─ 3. Add finalizer                 → r.Update() + Requeue
     │         platform.company.io/finalizer
     │
     ├─ 4. resetDerivedStateForNewGeneration()
     │         if Generation > ObservedGeneration AND no transient DB request:
     │           → delete smoke, regression, e2e, ai-seed, ai-tests, ai-schema-dump jobs
     │           → delete preview-test-suite, ai-enrichment ConfigMaps
     │           → clear status.tests, status.aiEnrichment, github comment IDs
     │           → status.ObservedGeneration = Generation
     │
     ├─ 5. isTTLExpired()                → r.Delete(preview) → triggers step 2
     │
     ├─ 6. !preview.IsApproved()        → phase=Pending, GitHub: queued
     │         RequeueAfter=30s
     │
     ├─ 7. ExpiresAt == nil              → parse spec.ttl, set status.ExpiresAt
     │         phase=Provisioning, syncGitHubAfterStatus()
     │
     └─ reconcileProvisioning()
           │
           ├─ reconcileNamespace()
           │     create Namespace preview-pr-42
           │     labels: preview-name=pr-42, branch=feat/…
           │
           ├─ reconcileResourceQuota()
           │     tier base limits + extensions for DB, AI, E2E, extra services
           │
           ├─ handleResetRequested()
           │     if spec.database.resetRequested:
           │       delete postgres-migrate + postgres-seed Jobs
           │       clear status.database.{migration,seed}
           │       clear spec.database.resetRequested
           │       → Requeue (DB reconciled on next loop)
           │
           ├─ handleAIRerunRequested()
           │     if spec.aiEnrichment.rerunRequested:
           │       delete ai-seed, ai-tests, ai-schema-dump jobs
           │       delete ai-enrichment ConfigMap
           │       clear status.aiEnrichment
           │       clear spec.aiEnrichment.rerunRequested
           │       set aiRerunOnly=true (skip test suite this cycle)
           │       → Requeue
           │
           ├─ reconcileDatabaseWait()
           │     ├── Secret postgres-credentials (create once, never overwrite)
           │     ├── Deployment postgres (pg:15-alpine, Recreate, pg_isready probe)
           │     ├── Service postgres (ClusterIP :5432)
           │     ├── Job postgres-migrate (optional, waits for pg_isready)
           │     ├── Job postgres-seed (optional, waits for migrate)
           │     └── not ready? → RequeueAfter=10s
           │
           ├─ reconcileCheckpoints()
           │     spec.database.checkpointSave    → checkpoint-save-<name> Job
           │     spec.database.checkpointRestore → checkpoint-restore-<name> Job
           │     auto-clears spec field after Job completion
           │
           ├─ reconcileDeployment()
           │     multi-service mode (spec.services[]):
           │       one Deployment per service: name=svc-<name>
           │       DB env vars injected into all
           │       wait-for-postgres init container on all
           │       OTel annotations if telemetry.enabled
           │     single-service mode (spec.image):
           │       Deployment name=app, port=80
           │
           ├─ reconcileService()
           │     ClusterIP Service per Deployment
           │
           ├─ reconcileIngress()
           │     multi-service: /api → svc-backend, / → svc-frontend
           │     single-service: pr-<N>.preview.localtest.me → app:80
           │     nginx.ingress.kubernetes.io/rewrite-target annotation
           │
           ├─ handleAppAvailability()
           │     wait for all Deployments to reach minAvailable
           │     → RequeueAfter=10s if not ready
           │
           ├─ markRunningStatus()
           │     phase=Running, URL, ReadyAt, conditions
           │     syncGitHubAfterStatus() → GitHub: success + PR comment
           │
           ├─ reconcileAIEnrichment()   [if aiEnrichment.enabled]
           │     see §11 — full diagram
           │     → RequeueAfter=10s while Running
           │
           └─ reconcileTestSuite()      [if testSuite.enabled AND AI done]
                 see §10 — full diagram
                 → RequeueAfter=10s at each step
```

### All Kubernetes resources created per Preview CR

```
┌────────────────────────────────────────────────────────────────────────────────┐
│  Resource Kind    │ Name                        │ Created when                 │
├────────────────────────────────────────────────────────────────────────────────┤
│  Namespace        │ preview-pr-<N>              │ always                       │
│  ResourceQuota    │ preview-quota              │ always                       │
│                   │                             │                              │
│  ── Database ─────┼─────────────────────────────┼──────────────────────────── │
│  Secret           │ postgres-credentials        │ database.enabled=true        │
│  Deployment       │ postgres                    │ database.enabled=true        │
│  Service          │ postgres                    │ database.enabled=true        │
│  Job              │ postgres-migrate            │ database.migration.enabled   │
│  Job              │ postgres-seed               │ database.seed.enabled        │
│                   │                             │                              │
│  ── Checkpoints ──┼─────────────────────────────┼──────────────────────────── │
│  Job              │ checkpoint-save-<name>      │ spec.database.checkpointSave │
│  ConfigMap        │ db-checkpoint-<name>        │ after checkpoint-save Job    │
│  Job              │ checkpoint-restore-<name>   │ spec.database.checkpointRestore │
│                   │                             │                              │
│  ── Services ─────┼─────────────────────────────┼──────────────────────────── │
│  Deployment       │ svc-<name> (per service)    │ spec.services[]              │
│  Service          │ svc-<name> (per service)    │ spec.services[]              │
│  Deployment       │ app                         │ spec.image (single-service)  │
│  Service          │ app                         │ spec.image (single-service)  │
│  Ingress          │ app                         │ always                       │
│                   │                             │                              │
│  ── AI Enrichment ┼─────────────────────────────┼──────────────────────────── │
│  Job              │ ai-schema-dump              │ aiEnrichment.enabled         │
│  ConfigMap        │ ai-enrichment               │ after ai-generate            │
│  Job              │ ai-seed                     │ aiEnrichment.seed.enabled    │
│  Job              │ ai-tests                    │ aiEnrichment.tests.enabled   │
│                   │                             │                              │
│  ── Test Suite ───┼─────────────────────────────┼──────────────────────────── │
│  ConfigMap        │ preview-test-suite         │ testSuite.enabled            │
│  Job              │ suite-checkpoint-save       │ step=saving                  │
│  Job              │ smoke-tests                 │ step=smoke                   │
│  Job              │ suite-restore-regression    │ step=restore-regression      │
│  Job              │ regression-tests            │ step=regression              │
│  Job              │ suite-restore-e2e           │ step=restore-e2e             │
│  Job              │ e2e-tests                   │ step=e2e                     │
└────────────────────────────────────────────────────────────────────────────────┘
```

### Phase state machine

```
                    requiresApproval=true
                    && approvedBy not set
                           │
  Created ──► Pending ─────┘
                 │
                 │ approved (or requiresApproval=false)
                 ▼
           Provisioning ──────────────────────► Failed
           (namespace, DB, services,                │
            ingress being created)                  │ (automatic diagnostics
                 │                                  │  collected + GitHub
                 │ all ready                        │  failure comment posted)
                 ▼
             Running
           (AI enrichment + test suite executing)
                 │
                 │ kubectl delete / TTL expired
                 ▼
           Terminating
           (finalizer: namespace deleted)
```

### Idempotence guarantees

Every function in the controller is idempotent — re-running the full reconcile loop produces the same result. Specifically:

- **Resources** are created with `controllerutil.CreateOrUpdate` — applying again is a no-op if unchanged.
- **Secrets** (`postgres-credentials`) are created once and never overwritten, preserving credentials across reconciles.
- **Jobs** are looked up by name before creation — a running job is not recreated.
- **GitHub notifications** check `status.github.deploymentState` and `lastNotifiedPhase` before calling the API — zero duplicate calls.
- **Status writes** include `ObservedGeneration` — the generation-reset logic only triggers on actual spec changes, not status changes.

### Generation reset — how spec changes are handled

When `spec` is updated (e.g. image tag bumped), the controller detects `Generation > ObservedGeneration`. It then:

1. Deletes all test and AI Jobs + their ConfigMaps
2. Resets `status.tests`, `status.aiEnrichment`, and GitHub comment IDs
3. Sets `ObservedGeneration = Generation`

This means a new image tag triggers a full re-run of AI enrichment and the test suite — the same experience as opening a new PR.

Transient mutations (`resetRequested`, `checkpointSave`, `checkpointRestore`, `rerunRequested`) are explicitly excluded from the generation-reset logic to avoid cascade.

---

## 5. Ephemeral PostgreSQL

### Configuration

```yaml
spec:
  database:
    enabled: true
    version: "15"          # optional, default "15"
    databaseName: appdb    # optional, default "appdb"
```

### What the controller creates

```
Secret postgres-credentials
  ├── POSTGRES_USER     = preview_42          (username derived from PR number)
  ├── POSTGRES_PASSWORD = a3f8c2… (64-char crypto/rand hex)
  ├── POSTGRES_DB       = appdb
  └── DATABASE_URL      = postgresql://preview_42:a3f8c2…@postgres:5432/appdb?sslmode=disable

Deployment postgres
  ├── Image: postgres:15-alpine
  ├── Strategy: Recreate (preserves data during pod restart)
  ├── ReadinessProbe: pg_isready -U preview_42
  └── LivenessProbe: pg_isready -U preview_42

Service postgres
  └── ClusterIP :5432  (DNS "postgres" within the preview namespace)
```

The Secret is created **once and never overwritten** — even if the Deployment is deleted and recreated, credentials remain stable.

### Environment variables auto-injected into every service pod

| Variable | Example value |
|---|---|
| `POSTGRES_USER` | `preview_42` |
| `POSTGRES_PASSWORD` | `a3f8c2…` (64-char hex) |
| `POSTGRES_DB` | `appdb` |
| `DATABASE_URL` | `postgresql://preview_42:a3f8c2…@postgres:5432/appdb?sslmode=disable` |

### Init container — wait for PostgreSQL

Every service Deployment includes a `busybox` init container that runs:

```bash
until nc -z postgres 5432; do sleep 2; done
```

This prevents the app from starting before PostgreSQL is accepting connections, avoiding connection refused errors at startup.

### Migrations and static seeds

```yaml
spec:
  database:
    enabled: true
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
      # image: ghcr.io/acme/migrations:sha-abc  # optional override
    seed:
      enabled: true
      command: ["python", "scripts/seed_preview.py"]
```

Both run as one-shot Kubernetes Jobs before the app Deployment is reconciled:

```
postgres ready
      │
      ▼
Job postgres-migrate  (init: wait for pg, then run migration command)
      │  must succeed
      ▼
Job postgres-seed     (init: wait for pg, then run seed command)
      │  must succeed
      ▼
reconcileDeployment() — app pod is created
```

DB credentials are auto-injected into both Jobs. If either Job fails, the controller transitions to `Failed` and captures the job logs in `status.diagnostics`.

```bash
# Check migration / seed state
kubectl get preview pr-42 -o jsonpath='{.status.database}' | jq .

# Inspect job logs if stuck in Provisioning
kubectl logs -n preview-pr-42 job/postgres-migrate
kubectl logs -n preview-pr-42 job/postgres-seed
```

### Database reset

Deletes and re-runs migration + seed jobs without deleting the environment:

```bash
# Via kubectl
kubectl patch preview pr-42 --type=merge \
  -p '{"spec":{"database":{"resetRequested":true}}}'

# Via Copilot Extension
@preview reset-db pr-42
```

The controller clears `resetRequested` automatically after deleting the jobs. On the next reconcile, both jobs run again from scratch.

### Read credentials

```bash
kubectl get secret postgres-credentials -n preview-pr-42 \
  -o jsonpath='{.data.DATABASE_URL}' | base64 -d
```

---

## 6. Database Checkpoints

A checkpoint is a point-in-time snapshot of the database content (`pg_dump --data-only`), stored in a Kubernetes ConfigMap in the preview namespace. Checkpoints can be saved and restored on demand — the controller creates a Job for each operation.

### How checkpoints work

```
checkpointSave="my-snapshot"
      │
      ▼
Job: checkpoint-save-my-snapshot
  pg_dump --data-only --no-owner -h postgres -U $USER $DB
  → output stored in ConfigMap db-checkpoint-my-snapshot (key: dump.sql)
  → max 950 KB (compressed)
      │
      ▼
spec.database.checkpointSave cleared automatically

checkpointRestore="my-snapshot"
      │
      ▼
Job: checkpoint-restore-my-snapshot
  psql: TRUNCATE … RESTART IDENTITY CASCADE (all public tables)
  psql: replay dump.sql from ConfigMap
      │
      ▼
spec.database.checkpointRestore cleared automatically
```

### Save a checkpoint

```bash
kubectl patch preview pr-42 --type=merge \
  -p '{"spec":{"database":{"checkpointSave":"before-order-flow"}}}'
```

### Restore a checkpoint

```bash
kubectl patch preview pr-42 --type=merge \
  -p '{"spec":{"database":{"checkpointRestore":"before-order-flow"}}}'
```

### List available checkpoints

```bash
kubectl get preview pr-42 -o jsonpath='{.status.database.checkpoints}'
# ["before-order-flow","after-seed"]

kubectl get configmaps -n preview-pr-42 -l preview.io/checkpoint=true
```

### Checkpoint name rules

- Lowercase alphanumeric + hyphens only: `[a-z0-9]([-a-z0-9]*[a-z0-9])?`
- Max 48 characters
- Must start and end with alphanumeric character

### Checkpoint API for E2E tests

The E2E Job receives `CHECKPOINT_API` pointing to the Preview Extension. Test scripts can trigger restores before each test:

```python
import os, requests

CHECKPOINT_API = os.environ.get("CHECKPOINT_API", "")

def reset_db():
    if not CHECKPOINT_API:
        return
    try:
        requests.post(
            f"{CHECKPOINT_API}/checkpoints/after-seed/restore",
            timeout=60
        )
    except Exception:
        pass  # graceful degradation

def run(name, fn):
    reset_db()     # clean DB before every Playwright test
    with sync_playwright() as p:
        ...
```

The extension patches `spec.database.checkpointRestore` → controller creates a restore Job → extension polls until complete → returns HTTP 200 → test starts.

---

## 7. Multi-Service Mode

When `spec.services[]` is set, `spec.image` is ignored. Each entry creates its own Deployment, Service, and optional ingress path.

### Configuration

```yaml
spec:
  services:
    - name: backend
      image: ghcr.io/acme/myapp:sha-abc
      port: 8080
      pathPrefix: /api          # ingress: /api/* → svc-backend:8080
    - name: frontend
      image: ghcr.io/acme/myapp:sha-abc
      port: 3000
      pathPrefix: /             # ingress: /* → svc-frontend:3000
      env:
        - name: APP_MODE
          value: frontend
        - name: PREVIEW_PR
          value: "42"
```

### What the controller creates

```
Deployment svc-backend   → port 8080, all DB env vars injected, init: wait-for-postgres
Service    svc-backend   → ClusterIP :8080
Deployment svc-frontend  → port 3000, DB env vars injected, APP_MODE=frontend
Service    svc-frontend  → ClusterIP :3000
Ingress    app           → /api/* → svc-backend:8080
                           /*     → svc-frontend:3000
```

Ingress path routing: longer prefixes match first, so `/api/products` hits the backend before `/` catches it.

### ServiceSpec fields

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | required | Deployment and Service named `svc-<name>` |
| `image` | string | required | Container image |
| `port` | int32 | `80` | Container port |
| `pathPrefix` | string | — | URL path routed to this service. Omit to deploy without ingress exposure |
| `replicas` | int32 | `spec.replicas` | Replica count for this specific service |
| `env` | EnvVar[] | — | Additional env vars injected into this service only |

### Behavior with add-ons

| Add-on | Multi-service behavior |
|---|---|
| Database | Credentials auto-injected into ALL services; init container on all pods |
| AI enrichment | `APP_URL` points to the service with `pathPrefix="/"` (frontend), falling back to first service |
| Regression tests | `APP_URL` = backend service URL, `FRONTEND_URL` = frontend service URL |
| E2E tests | `APP_URL` = `FRONTEND_URL` = frontend service URL |
| Telemetry | OTel annotations applied to all service Deployments |
| Resource quota | Tier headroom added for each additional service |

### Single-service mode (fallback)

When only `spec.image` is set (no `spec.services[]`):

```yaml
spec:
  image: ghcr.io/acme/myapp:sha-abc
  # creates: Deployment "app", Service "app", Ingress → app:80
```

---

## 8. OpenTelemetry Auto-Instrumentation

When the OpenTelemetry Operator is installed, the controller opts application pods into zero-code auto-instrumentation via pod annotations — no changes to the application image are required.

### Configuration

```yaml
spec:
  telemetry:
    enabled: true
    serviceName: myapp-pr-42     # sets OTEL_SERVICE_NAME
    autoInstrumentation:
      language: python            # python | java | nodejs | dotnet | go | sdk
      instrumentationRef: observability/python   # namespace/name of Instrumentation CR
      # pythonPlatform: musl      # for Alpine-based Python images
      # goTargetExecutable: /app/myservice  # required for Go
```

### What the controller injects into the Deployment pod template

```yaml
metadata:
  annotations:
    instrumentation.opentelemetry.io/inject-python: observability/python

spec:
  containers:
    - name: app
      env:
        - name: OTEL_SERVICE_NAME
          value: myapp-pr-42
        - name: OTEL_RESOURCE_ATTRIBUTES
          value: >
            preview.name=pr-42,
            preview.pr_number=42,
            preview.branch=feat/my-feature,
            k8s.namespace.name=preview-pr-42
```

The OTel Operator detects the annotation and injects the SDK as a sidecar (or init container depending on language). No changes to the `Dockerfile` or application code are needed.

### Language-specific notes

| Language | Notes |
|---|---|
| `python` | Auto-instruments Flask, FastAPI, Django, SQLAlchemy, requests |
| `python` (Alpine) | Add `pythonPlatform: musl` |
| `java` | JVM agent injected as init container |
| `nodejs` | Auto-instruments Express, http, grpc |
| `dotnet` | CLR profiler injected |
| `go` | Requires `goTargetExecutable: /path/to/binary` |

### Access traces in Jaeger

```bash
kubectl port-forward -n observability svc/jaeger 16686:16686
# → http://localhost:16686
# Select service: "myapp-pr-42"
```

---

## 9. GitHub Integration

The operator bridges Kubernetes state to GitHub — publishing deployment statuses and rich PR comments — directly from the reconcile loop, with no external webhook or polling service.

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
kubectl create secret generic preview-github-token \
  --namespace preview-operator-system \
  --from-literal=token="<GITHUB_PAT>"
```

> Use a long-lived PAT — not `GITHUB_TOKEN`. The Actions token expires shortly after the workflow finishes, while the operator continues running AI enrichment and tests for several minutes.

**Step 3 — Reference from the CR:**

```yaml
spec:
  github:
    enabled: true
    owner: acme
    repo: myapp
    deploymentId: 123456789   # returned by createDeployment()
    environment: pr-42
    commentOnReady: true
    tokenSecretRef:
      name: preview-github-token
      namespace: preview-operator-system
      key: token
```

### Phase → GitHub state mapping

```
┌──────────────┬──────────────────────┬────────────────────────────────────┐
│ Phase        │ GitHub Deployment    │ PR comment                         │
├──────────────┼──────────────────────┼────────────────────────────────────┤
│ Pending      │ queued               │ —                                  │
│ Provisioning │ in_progress          │ 🔄 Provisioning en cours…          │
│ Running      │ success + URL        │ ## Preview Preview Ready + URL    │
│              │                      │   + DB state + OTel + TTL          │
│ Failed       │ failure              │ ## Preview Preview Failed         │
│              │                      │   + root cause + pod logs + cmds   │
│ Terminating  │ inactive             │ — (posted by cleanup workflow)     │
└──────────────┴──────────────────────┴────────────────────────────────────┘
```

### Idempotence

Before every GitHub API call, the controller checks `status.github.deploymentState` and `lastNotifiedPhase`. If already written → zero API call, even across 100 reconcile loops. The PR comment is updated in-place using the stored `commentId`.

```bash
kubectl get preview pr-42 -o jsonpath='{.status.github}' | jq .
kubectl get preview pr-42 -o jsonpath='{.status.github.lastError}'
```

---

## 10. Automated Test Suite

The test suite runs **after AI enrichment** — so tests always run against a seeded, realistic database. The controller orchestrates six sequential Jobs, with database checkpoint restore before each suite and before each individual E2E test.

### Why sequential, checkpoint-based

Classic shared staging has two fatal problems:
- **Cross-PR pollution** — PR #28 and PR #29 share the same DB; test data from one breaks the other.
- **Unstable baseline** — staging accumulates data from previous runs.

The Preview controller eliminates both: each PR has its own namespace + DB, and a checkpoint is taken after the AI seed — then restored before each suite for a guaranteed, identical starting state.

### Full pipeline

```
AI seed completes (10 products, 3 categories, reviews, orders)
           │
           ▼
┌──────────────────────────────────────────────────────────────────────────┐
│  Job: suite-checkpoint-save                                              │
│  Image: postgres:15-alpine                                               │
│  pg_dump --data-only → ConfigMap "db-checkpoint-after-seed"             │
└──────────────────────────────┬───────────────────────────────────────────┘
                               │  step → "smoke"
           ┌───────────────────▼──────────────────────────────────────────┐
           │  Job: smoke-tests                                             │
           │  Image: python:3.12-slim  (script embedded in operator)      │
           │  APP_URL=http://svc-backend:8080                              │
           │    GET /healthz       → expect 200                           │
           │    GET /api/products  → expect 200                           │
           └───────────────────┬──────────────────────────────────────────┘
                               │  step → "restore-regression"
           ┌───────────────────▼──────────────────────────────────────────┐
           │  Job: suite-restore-regression                                │
           │  TRUNCATE all public tables RESTART IDENTITY CASCADE          │
           │  psql replay dump.sql from ConfigMap                          │
           └───────────────────┬──────────────────────────────────────────┘
                               │  step → "regression"
           ┌───────────────────▼──────────────────────────────────────────┐
           │  Job: regression-tests                                        │
           │  Image: app image (contains tests/regression.py)             │
           │  APP_URL=http://svc-backend:8080                              │
           │  FRONTEND_URL=http://svc-frontend:3000                        │
           │  python /app/tests/regression.py                             │
           └───────────────────┬──────────────────────────────────────────┘
                               │  step → "restore-e2e"
           ┌───────────────────▼──────────────────────────────────────────┐
           │  Job: suite-restore-e2e                                       │
           │  TRUNCATE + psql replay (DB back to post-seed state)          │
           └───────────────────┬──────────────────────────────────────────┘
                               │  step → "e2e"
           ┌───────────────────▼──────────────────────────────────────────┐
           │  Job: e2e-tests                                               │
           │  Init container: copy-tests (app image → emptyDir volume)    │
           │  Main container: playwright/python:v1.44.0-jammy             │
           │  APP_URL=FRONTEND_URL=http://svc-frontend:3000                │
           │  CHECKPOINT_API=http://preview-extension:8090/…             │
           │                                                               │
           │  reset_db() → test_catalog_page_loads                        │
           │  reset_db() → test_preview_badge_shown                       │
           │  reset_db() → test_product_detail_panel                      │
           │  reset_db() → test_related_section                           │
           │  reset_db() → test_discount_filter                           │
           │  reset_db() → test_close_detail                              │
           └───────────────────────────────────────────────────────────────┘
                               │
                     tests.phase = Succeeded
                     PR comment updated: results table
```

### Controller step state machine

The controller stores `status.tests.step` in etcd. Each reconcile reads the step and creates the corresponding Job:

```
Reconcile() reads status.tests.step
  ""                   → create suite-checkpoint-save, step="saving"
  "saving"             → job complete? → step="smoke"
                          still running → RequeueAfter=10s
  "smoke"              → job complete? → step="restore-regression"
  "restore-regression" → job complete? → step="regression"
  "regression"         → job complete? → step="restore-e2e"
  "restore-e2e"        → job complete? → step="e2e"
  "e2e"                → job complete? → tests.phase="Succeeded"
                          job failed   → tests.phase="Failed"
                          still running → RequeueAfter=10s
```

If the operator pod restarts mid-pipeline, it reads `status.tests.step` from etcd and resumes at the exact step where it left off — no state lost, no job duplicated.

### Two levels of DB isolation

| Level | Mechanism | Prevents |
|-------|-----------|---------|
| Between suites | `suite-restore-regression`, `suite-restore-e2e` | Regression suite modifying products, stock, orders → corrupting E2E data |
| Between each E2E test | `reset_db()` via Checkpoint API | One Playwright test changing page state → breaking next test's assertions |

### Job resource limits

| Job | CPU request/limit | Memory request/limit | Image |
|-----|------------------|---------------------|-------|
| suite-checkpoint-save | 50m / 500m | 128Mi / 512Mi | postgres:15-alpine |
| smoke-tests | 50m / 500m | 128Mi / 512Mi | python:3.12-slim |
| suite-restore-* | 50m / 500m | 128Mi / 512Mi | postgres:15-alpine |
| regression-tests | 50m / 500m | 128Mi / 512Mi | app image |
| e2e-tests | 200m / 1000m | 512Mi / 1Gi | playwright/python:v1.44.0 |

E2E gets more headroom — Chromium requires it.

### Required: test output format

The operator parses `PASS` and `FAIL` lines from job stdout to build the results table. Test scripts must follow this format:

```python
print(f"PASS regression {name}: {r.status_code}")
print(f"FAIL regression {name}: expected 200 got {r.status_code}")
print(f"Results: {passed} passed, {failed} failed")
sys.exit(1 if failed else 0)
```

The prefix word (`regression`, `smoke`, `e2e`) is the suite name shown in the PR comment.

### Enabling the test suite

```yaml
spec:
  testSuite:
    enabled: true
    smoke: {}             # built-in, no config needed
    regression:
      enabled: true       # requires tests/regression.py in the app image
    e2e:
      enabled: true       # requires tests/e2e.py in the app image
```

Tests scripts must be included in the Docker image:

```dockerfile
COPY tests/ ./tests/
```

### Reading results

```bash
# All results
kubectl get preview pr-42 -o jsonpath='{.status.tests}' | jq .

# Current step
kubectl get preview pr-42 -o jsonpath='{.status.tests.step}'

# Per-suite
kubectl get preview pr-42 -o jsonpath='{.status.tests.smoke}' | jq .
kubectl get preview pr-42 -o jsonpath='{.status.tests.regression}' | jq .
kubectl get preview pr-42 -o jsonpath='{.status.tests.e2e}' | jq .

# Live logs
kubectl logs -n preview-pr-42 job/smoke-tests -f
kubectl logs -n preview-pr-42 job/regression-tests -f
kubectl logs -n preview-pr-42 job/e2e-tests -f
```

### PR comment generated automatically

```
## Preview Test Suite Results

**Overall: ✅ Succeeded**

| Suite      | Status       | Passed | Failed |
|------------|--------------|--------|--------|
| Smoke      | ✅ Succeeded | 2      | 0      |
| Regression | ✅ Succeeded | 9      | 0      |
| E2E        | ✅ Succeeded | 6      | 0      |
```

---

## 11. AI Enrichment

After the preview reaches `Running`, the controller generates context-aware seed data and integration tests by sending the PR diff and live DB schema to an AI provider.

### Why AI enrichment runs before tests

Tests need seeded data — products, categories, reviews — to make meaningful assertions. The controller enforces this ordering with an explicit guard:

```go
// AI must be Succeeded or Failed before tests start
if aiPhase != "Succeeded" && aiPhase != "Failed" {
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}
```

If AI enrichment fails, the test suite still runs — just against an empty or partially populated database, which is still better than not running at all.

### Full enrichment pipeline

```
Preview Running
       │
       ▼
┌────────────────────────────────────────────────────────────────────────────┐
│  Job: ai-schema-dump                                                       │
│  Image: postgres:15-alpine                                                 │
│  pg_dump --schema-only → stdout → controller reads pod logs                │
│  → stored in ConfigMap "ai-enrichment" (key: schema)                      │
└────────────────────────────────┬───────────────────────────────────────────┘
                                 │
          ┌──────────────────────▼──────────────────────────────────────────┐
          │  Controller: fetch PR diff from GitHub API                       │
          │  GET /repos/{owner}/{repo}/pulls/{prNumber}/files               │
          │  Accept: application/vnd.github.diff                            │
          └──────────────────────┬──────────────────────────────────────────┘
                                 │
┌────────────────────────────────▼───────────────────────────────────────────┐
│  Controller: call AI API (HTTP — no Job needed for generation)             │
│                                                                             │
│  Prompt includes:                                                           │
│    ① DB schema (from ConfigMap)                                            │
│    ② PR diff (files changed)                                               │
│    ③ system prompt from ConfigMap "ai-prompt-template" (Helm-managed)     │
│    ④ extra instructions from ConfigMap "ai-prompt-pr-42" (per-env, opt.)  │
│                                                                             │
│  AI responds with:                                                          │
│    seed.sql → at least 10 products, 3 categories, 2 reviews/product       │
│               data coherent with the PR changes and schema                 │
│    test.py  → integration tests targeting modified code paths              │
│               PASS/FAIL format, same structure as regression.py            │
│                                                                             │
│  Both stored in ConfigMap "ai-enrichment" (keys: seed.sql, test.py)       │
│  status.aiEnrichment.phase = "Generating"                                  │
└────────────────────────────────┬───────────────────────────────────────────┘
                                 │
          ┌──────────────────────▼──────────────────────────────────────────┐
          │  Job: ai-seed                                                    │
          │  Image: postgres:15-alpine                                       │
          │  psql -h postgres -U $USER -d $DB -f /data/seed.sql             │
          │  → status.aiEnrichment.seedStatus = Succeeded / Failed          │
          └──────────────────────┬──────────────────────────────────────────┘
                                 │
          ┌──────────────────────▼──────────────────────────────────────────┐
          │  Job: ai-tests                                                   │
          │  Image: python:3.12-slim                                         │
          │  pip install requests && python /data/test.py                    │
          │  APP_URL=http://svc-backend:8080                                 │
          │  → status.aiEnrichment.testsStatus = Succeeded / Failed         │
          │  → PASS/FAIL lines parsed → status.aiEnrichment.testResults[]   │
          └──────────────────────┬──────────────────────────────────────────┘
                                 │
                       status.aiEnrichment.phase = Succeeded / Failed
                       PR comment updated with AI results
                                 │
                                 ▼
                       Test suite unblocked (§10)
```

### Configuration

```yaml
spec:
  aiEnrichment:
    enabled: true
    apiSecretRef:
      name: ai-api-key
      key: api-key
    githubTokenSecretRef:                  # optional dedicated token for PR diff
      name: preview-github-token
      namespace: preview-operator-system
      key: token
    model: gpt-4o-mini                     # gpt-4o-mini | gpt-4o | any OpenAI-compatible
    seed:
      enabled: true                        # default true when omitted
    tests:
      enabled: true                        # default true when omitted
    # rerunRequested: true                 # triggers AI-only rerun
```

### AI provider options

| Provider | API URL | Secret value |
|---|---|---|
| OpenAI (default) | `https://api.openai.com/v1` | `sk-…` |
| GitHub Models (free) | `https://models.inference.ai.azure.com` | GitHub PAT |
| Azure OpenAI | `https://<resource>.openai.azure.com/openai` | Azure key |

Override the URL globally:

```bash
kubectl set env deployment/preview-operator \
  AI_API_URL=https://models.inference.ai.azure.com \
  -n preview-operator-system
# or via Helm:
helm upgrade preview-operator … --set ai.apiURL=https://models.inference.ai.azure.com
```

### AI-only rerun

Replays the full AI pipeline (schema-dump → generate → seed → tests) without rebuilding the environment or re-running smoke/regression/E2E:

```bash
# Via Copilot Extension (preferred)
@preview retest-ai pr-42

# Via kubectl
kubectl patch preview pr-42 --type=merge \
  -p='{"spec":{"aiEnrichment":{"rerunRequested":true}}}'
```

Use this when you change the AI prompt, the model, or want fresh data.

### Prompt customization

**Per-environment (via Copilot Extension):**

```
@preview set-prompt pr-42 Generate luxury watch products. Swiss brands. Price €500–€10000.
@preview retest-ai pr-42
```

The extension creates ConfigMap `ai-prompt-pr-42` in `preview-operator-system`. The operator appends these instructions to the base system prompt. The ConfigMap is deleted when the environment is removed.

**Global system prompt (via Helm):**

```bash
helm upgrade preview-operator … \
  --set-file ai.systemPrompt=./my-prompt.txt
```

**Per-environment via kubectl:**

```bash
kubectl create configmap ai-prompt-pr-42 \
  --namespace preview-operator-system \
  --from-literal=instructions="Generate 15 products across 5 categories." \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Monitor enrichment

```bash
kubectl get preview pr-42 -o jsonpath='{.status.aiEnrichment}' | jq .
```

```json
{
  "phase": "Succeeded",
  "seedStatus": "Succeeded",
  "testsStatus": "Succeeded",
  "testResults": ["PASS: test_health", "PASS: test_create_product"],
  "completedAt": "2026-05-08T09:05:00Z"
}
```

---

## 12. Approval Gate

Block provisioning of sensitive or large environments until a human explicitly approves.

```yaml
spec:
  requiresApproval: true    # large tier always forces this (enforced by webhook)
```

The environment stays in `Pending` phase. The controller requeues every 30 seconds to check. Approve:

```bash
kubectl patch preview pr-99 --type=merge \
  -p '{"spec":{"approvedBy":"ihsenalaya"}}'
```

Once `approvedBy` is set, the environment immediately transitions to `Provisioning`.

---

## 13. Resource Tiers & Quota Management

```yaml
spec:
  resourceTier: medium    # small | medium | large
```

### Base tier limits

| Tier | CPU request | CPU limit | Memory request | Memory limit |
|------|-------------|-----------|----------------|--------------|
| `small` | 100m | 250m | 128Mi | 256Mi |
| `medium` | 200m | 500m | 256Mi | 512Mi |
| `large` | 500m | 2000m | 512Mi | 2Gi |

### Automatic quota extensions

The controller extends the namespace `ResourceQuota` based on what is enabled — no manual configuration needed:

| Add-on | Extra CPU limit | Extra RAM limit |
|--------|----------------|-----------------|
| `database.enabled=true` | +500m | +512Mi |
| `aiEnrichment.enabled=true` | +500m | +512Mi |
| `testSuite` smoke + regression | +1000m | +1Gi |
| `testSuite` E2E (Playwright/Chromium) | +1000m | +1Gi |
| each additional `spec.services[]` entry | +base tier | +base tier |

### Load testing with multiple replicas

```yaml
spec:
  replicas: 3       # applies to all services
  resourceTier: large
```

---

## 14. TTL & Auto-Expiry

```yaml
spec:
  ttl: 48h    # supports: 1h, 12h, 24h, 48h, 72h, 168h, etc.
```

At the start of provisioning, the controller sets `status.expiresAt = now + ttl`. On every reconcile, it checks `isTTLExpired()`. When the TTL is passed, it calls `r.Delete(preview)` — the finalizer runs and the namespace is removed.

The controller also requeues `RequeueAfter = ttlRemaining` on each successful reconcile so it wakes up precisely when expiry should trigger, without hammering the API server.

### Extend TTL

```bash
# Via Copilot Extension
@preview extend pr-42 24h

# Via kubectl (patch status directly)
kubectl patch preview pr-42 --type=merge \
  -p "{\"status\":{\"expiresAt\":\"$(date -u -d '+72 hours' +%Y-%m-%dT%H:%M:%SZ)\"}}"
```

### Check time remaining

```bash
kubectl get preview pr-42 -o jsonpath='{.status.expiresAt}'
```

---

## 15. Smart Diagnostics

On any failure, the controller automatically collects a structured diagnostic report stored in `status.diagnostics`. When GitHub integration is enabled, the same report is posted as a PR comment.

### What is collected

```
status.diagnostics
  ├── reason             machine-readable code (DeploymentFailed, DatabaseMigrationFailed, …)
  ├── component          resource most likely responsible (app, database, migration, ingress, …)
  ├── message            human-readable probable cause
  ├── confidence         low | medium | high
  ├── recommendations    ordered list of developer-facing next steps
  ├── significantLogs    high-signal log lines grouped by component + container
  ├── podLogs            last 30 lines from the crashed app container
  ├── lastEvents         recent Kubernetes Warning events from the preview namespace
  └── debugCommands      ready-to-paste kubectl commands
```

### Failure reasons mapped to components

| Reason | Component | Common cause |
|--------|-----------|-------------|
| `DeploymentFailed` | `app` | ImagePullBackOff, CrashLoopBackOff, quota exceeded |
| `DatabaseMigrationFailed` | `migration` | SQL error, non-idempotent migration |
| `DatabaseSeedFailed` | `seed` | Script error, FK violation |
| `NamespaceFailed` | `namespace` | RBAC, quota exceeded at cluster level |
| `IngressFailed` | `ingress` | x509 webhook, missing ingress class |
| `ServiceFailed` | `service` | Port conflict, selector mismatch |

### Read diagnostics

```bash
kubectl get preview pr-42 -o jsonpath='{.status.diagnostics}' | jq .
# .podLogs          → last 30 lines of the crashed container
# .lastEvents       → recent Warning events
# .debugCommands    → kubectl commands to investigate further
# .recommendations  → ordered next steps
```

### Example PR failure comment

```markdown
## Preview Preview Failed

**Environment:** pr-42 — **Namespace:** preview-pr-42

### Diagnosis
- Reason: `DeploymentFailed`
- Component: `app`
- Cause: **Container image cannot be pulled** — `Confidence: high`

### Recommendations
1. Verify that `ghcr.io/acme/myapp:sha-abc` exists and is accessible from the cluster.
2. Check that the Kaniko job succeeded and the image was pushed to GHCR.
3. Confirm the namespace has the required imagePullSecrets if the registry is private.

### Recent Events
- Pod/svc-backend-xyz: Error: ImagePullBackOff
- Pod/svc-backend-xyz: Failed to pull image "…": manifest unknown

### Debug Commands
kubectl describe preview pr-42
kubectl get pods -n preview-pr-42
kubectl get events -n preview-pr-42 --sort-by=.lastTimestamp
```

---

## 16. Copilot Extension

A companion server (`preview-extension`) that surfaces preview environment management directly inside GitHub Copilot Chat — no `kubectl` access needed for developers.

### Available commands

| Command | Description |
|---|---|
| `@preview list` | List all active environments with phase, branch, TTL |
| `@preview status pr-42` | Phase, URL, DB state, AI state, test results, TTL, crash diagnostics |
| `@preview logs pr-42` | Last 40 live lines from the app pod (falls back to status.diagnostics.podLogs) |
| `@preview extend pr-42 24h` | Extend TTL by the given duration (default 24h) |
| `@preview wake pr-42` | Set spec.replicas=1 to restart a scaled-down environment |
| `@preview reset-db pr-42` | Re-run migration + seed without deleting the environment |
| `@preview run-sql pr-42 <sql>` | Execute arbitrary SQL against the preview database |
| `@preview retest-ai pr-42` | Trigger AI-only rerun (keeps test suite results) |
| `@preview enrich pr-42` | Alias for `retest-ai` |
| `@preview set-prompt pr-42 <text>` | Set custom AI instructions for this environment |
| `@preview show-prompt pr-42` | Show current custom AI prompt |
| `@preview help` | Show all commands |

### Architecture

```
Developer → @preview status pr-42 (in Copilot Chat)
                │
                ▼
      GitHub Copilot Chat
                │  POST webhook (OpenAI SSE streaming)
                ▼
   preview-extension server  (port 8090)
                │  kubernetes/client-go
                ▼
      Kubernetes API Server
                │
                ▼
       Preview CR → response streamed to Copilot Chat
```

### Deploy the extension

```bash
kubectl apply -f config/extension/rbac.yaml
kubectl apply -f config/extension/deployment.yaml
kubectl -n preview-operator-system rollout status deployment/preview-extension --timeout=60s

# Expose for local Kind via ngrok
kubectl port-forward -n preview-operator-system svc/preview-extension 8090:8090 &
ngrok http 8090
# Copy HTTPS URL → paste as Webhook URL in GitHub App settings
```

### RBAC granted to the extension

| Resource | Verbs |
|---|---|
| `previews` | get, list, watch, patch, update |
| `previews/status` | get, patch, update |
| `pods` | get, list, watch |
| `pods/log` | get |
| `configmaps` (operator namespace) | get, create, patch, update, delete |

### Webhook secret (production)

```bash
kubectl create secret generic preview-extension-secret \
  --namespace preview-operator-system \
  --from-literal=webhook-secret="$(openssl rand -hex 32)"
```

---

## 17. Complete CR Reference

### All fields

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Preview
metadata:
  name: pr-42

spec:
  # ── Identity ──────────────────────────────────────────────────────────────
  branch: feature/my-feature         # required
  prNumber: 42                        # required — used in namespace, URL, username
  image: ghcr.io/acme/myapp:sha-abc  # required by webhook; ignored when services[] set
  ttl: 48h                            # default 48h
  resourceTier: medium                # small | medium | large — default medium
  replicas: 1                         # default 1

  # ── Approval gate ──────────────────────────────────────────────────────────
  requiresApproval: false
  approvedBy: ""                      # set to unblock when requiresApproval=true

  # ── Multi-service ──────────────────────────────────────────────────────────
  services:
    - name: backend
      image: ghcr.io/acme/myapp:sha-abc
      port: 8080
      pathPrefix: /api
    - name: frontend
      image: ghcr.io/acme/myapp:sha-abc
      port: 3000
      pathPrefix: /
      env:
        - name: APP_MODE
          value: frontend

  # ── Database ───────────────────────────────────────────────────────────────
  database:
    enabled: true
    version: "15"                     # default "15"
    databaseName: appdb               # default "appdb"
    migration:
      enabled: true
      image: ""                       # default spec.image
      command: ["python", "-m", "alembic", "upgrade", "head"]
      args: []
    seed:
      enabled: true
      image: ""
      command: ["python", "scripts/seed.py"]
    resetRequested: false             # set true → deletes + re-runs migration+seed
    checkpointSave: ""                # set name → creates pg_dump snapshot
    checkpointRestore: ""             # set name → TRUNCATE + replay

  # ── OpenTelemetry ──────────────────────────────────────────────────────────
  telemetry:
    enabled: true
    serviceName: myapp-pr-42
    autoInstrumentation:
      language: python                # python | java | nodejs | dotnet | go | sdk
      instrumentationRef: observability/python
      pythonPlatform: glibc           # glibc | musl (Alpine)
      goTargetExecutable: ""          # required for Go

  # ── Test Suite ─────────────────────────────────────────────────────────────
  testSuite:
    enabled: true
    smoke: {}                         # built-in, always enabled when testSuite.enabled
    regression:
      enabled: true
    e2e:
      enabled: true

  # ── AI Enrichment ──────────────────────────────────────────────────────────
  aiEnrichment:
    enabled: true
    apiSecretRef:
      name: ai-api-key
      key: api-key
    githubTokenSecretRef:
      name: preview-github-token
      namespace: preview-operator-system
      key: token
    model: gpt-4o-mini
    seed:
      enabled: true
    tests:
      enabled: true
    rerunRequested: false             # set true → triggers AI-only rerun

  # ── GitHub ─────────────────────────────────────────────────────────────────
  github:
    enabled: true
    owner: acme
    repo: myapp
    deploymentId: 123456789
    environment: pr-42
    commentOnReady: true
    tokenSecretRef:
      name: preview-github-token
      namespace: preview-operator-system
      key: token
```

---

## 18. Status Fields Reference

```bash
kubectl get preview pr-42 -o jsonpath='{.status}' | jq .
```

| Field | Description |
|---|---|
| `status.phase` | `Pending` / `Provisioning` / `Running` / `Failed` / `Terminating` |
| `status.url` | Preview URL (`http://pr-42.preview.localtest.me:8080`) |
| `status.namespaceName` | `preview-pr-42` |
| `status.readyAt` | Timestamp of first `Running` transition |
| `status.expiresAt` | Timestamp of auto-deletion |
| `status.observedGeneration` | Generation of spec when this status was last updated |
| `status.databaseSecretName` | `postgres-credentials` |
| `status.database.ready` | true when PostgreSQL + migrations + seed all succeeded |
| `status.database.host` | `postgres` |
| `status.database.databaseName` | e.g. `appdb` |
| `status.database.migration` | `Skipped` / `Running` / `Succeeded` / `Failed` |
| `status.database.seed` | `Skipped` / `Running` / `Succeeded` / `Failed` |
| `status.database.checkpoints` | `["after-seed","before-order-flow"]` |
| `status.aiEnrichment.phase` | `Pending` / `Generating` / `Running` / `Succeeded` / `Failed` |
| `status.aiEnrichment.seedStatus` | Job status |
| `status.aiEnrichment.testsStatus` | Job status |
| `status.aiEnrichment.testResults` | `["PASS: test_health", "FAIL: test_order_stock — …"]` |
| `status.aiEnrichment.completedAt` | Timestamp |
| `status.tests.phase` | `Running` / `Succeeded` / `Failed` |
| `status.tests.step` | Current pipeline step (`saving` / `smoke` / `regression` / `e2e` / …) |
| `status.tests.smoke.phase` | Job phase |
| `status.tests.smoke.passed` | int |
| `status.tests.smoke.failed` | int |
| `status.tests.regression.*` | Same structure as smoke |
| `status.tests.e2e.*` | Same structure as smoke |
| `status.github.deploymentState` | Last state sent to GitHub (`in_progress` / `success` / `failure`) |
| `status.github.lastNotifiedPhase` | Phase that triggered the last GitHub notification |
| `status.github.commentId` | PR comment ID — updated in-place on each notification |
| `status.github.lastEnvironmentUrl` | URL sent in the GitHub Deployment |
| `status.github.lastError` | Latest non-blocking GitHub API error |
| `status.diagnostics.reason` | Machine-readable failure code |
| `status.diagnostics.component` | Resource responsible (`app`, `database`, `migration`, …) |
| `status.diagnostics.message` | Human-readable probable cause |
| `status.diagnostics.confidence` | `low` / `medium` / `high` |
| `status.diagnostics.recommendations` | `["Check image tag …", "Verify …"]` |
| `status.diagnostics.significantLogs` | High-signal log lines per component |
| `status.diagnostics.podLogs` | Last 30 lines from the crashed container |
| `status.diagnostics.lastEvents` | Recent Kubernetes Warning events |
| `status.diagnostics.debugCommands` | Ready-to-paste kubectl commands |
| `status.conditions` | `Ready`, `Approved`, `Expired`, `DatabaseReady`, `MigrationReady`, `SeedReady`, `AIEnrichmentReady`, `TestSuiteReady` |

---

## 19. Helm Values Reference

```yaml
replicaCount: 1

image:
  repository: ghcr.io/ihsenalaya/preview-operator
  pullPolicy: IfNotPresent
  tag: ""                       # defaults to Chart.appVersion

ai:
  apiURL: "https://api.openai.com/v1"   # override for GitHub Models or Azure OpenAI
  systemPrompt: |               # override the default AI system prompt
    You are a developer tool for preview environments.
    Generate realistic data that matches the PR diff and DB schema.

imagePullSecrets: []

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

## 20. Development & Release

### Source layout

```
preview-operator/
├── api/v1alpha1/
│   ├── preview_types.go           CRD schema — PreviewSpec, PreviewStatus, all nested types
│   ├── groupversion_info.go        GroupVersion registration
│   └── zz_generated.deepcopy.go   auto-generated by controller-gen
├── cmd/
│   ├── main.go                     Operator entry point — sets up manager, reconciler, webhooks
│   └── extension/main.go           Copilot Extension HTTP server entry point
├── internal/
│   ├── controller/
│   │   ├── preview_controller.go  Main reconcile loop, provisioning, infra resources
│   │   ├── ai_enrichment.go        AI schema dump, API call, seed/test jobs, prompt handling
│   │   ├── checkpoint.go           DB checkpoint save/restore (pg_dump → ConfigMap → psql)
│   │   ├── diagnostics.go          Failure analysis, log collection, debug command generation
│   │   ├── github.go               GitHub Deployment status + PR comment publishing
│   │   └── tests.go                Smoke/regression/E2E job orchestration, step state machine
│   ├── extension/
│   │   ├── server.go               HTTP server, GitHub webhook validation, SSE streaming
│   │   ├── commands.go             @preview command implementations
│   │   └── checkpoint_api.go       REST API for E2E test checkpoint restore
│   └── webhook/v1alpha1/
│       └── preview_webhook.go     Defaulter (TTL, tier, model) + Validator (large→requiresApproval)
├── charts/
│   └── preview-operator/
│       ├── Chart.yaml              version + appVersion
│       ├── crds/                   CRD YAML (regenerated by make manifests)
│       ├── templates/              Deployment, RBAC, Service, webhook, cert-manager
│       └── files/ai-system-prompt.txt   Default AI system prompt
└── .github/workflows/
    ├── lint.yml                    golangci-lint
    ├── test.yml                    unit + envtest
    ├── test-e2e.yml                Kind e2e suite
    ├── docker-release.yml          operator image → GHCR on main/v* tag
    ├── extension-release.yml       extension image → GHCR
    └── helm-release.yml            chart → GitHub Releases + Pages + GHCR OCI
```

### Make commands

```bash
make install        # install CRDs into the cluster
make manifests      # regenerate CRD YAML from Go types (run after editing preview_types.go)
make generate       # regenerate deepcopy methods
make run            # run controller locally with your kubeconfig
make test           # unit + envtest integration tests
make test-e2e       # E2E against a real Kind cluster
make docker-build   # build operator image
make docker-push    # push operator image
```

### Build images locally

```bash
# Operator
make docker-build docker-push IMG=ghcr.io/ihsenalaya/preview-operator:dev

# Extension
docker build -f Dockerfile.extension -t ghcr.io/ihsenalaya/preview-extension:dev .
docker push ghcr.io/ihsenalaya/preview-extension:dev

# Load into Kind
kind load docker-image ghcr.io/ihsenalaya/preview-operator:dev --name preview
helm upgrade preview-operator … --set image.tag=dev --set image.pullPolicy=Never
```

### Release a new version

```bash
git tag v0.13.9
git push origin v0.13.9
```

GitHub Actions automatically:

1. Builds and pushes `ghcr.io/ihsenalaya/preview-operator:0.13.9`
2. Builds and pushes `ghcr.io/ihsenalaya/preview-extension:0.13.9`
3. Packages and publishes the Helm chart to GitHub Releases, Pages, and GHCR OCI

### CI workflows

| Workflow | Trigger | What it does |
|---|---|---|
| `lint.yml` | any push | `golangci-lint` |
| `test.yml` | any push | unit + envtest integration tests |
| `test-e2e.yml` | any push | Kind cluster, full operator e2e suite |
| `docker-release.yml` | push to `main` or `v*` | operator image → GHCR |
| `extension-release.yml` | push to `main` or `v*` | extension image → GHCR |
| `helm-release.yml` | `v*` tag only | Helm chart → Releases + Pages + GHCR OCI |

---

## 21. Debugging & Troubleshooting

### Infinite reconcile loop (every 2 seconds in controller logs)

**Symptom:** `kubectl logs -n preview-operator-system deployment/preview-operator -f` shows constant `Reconciling Preview` with no progress.

**Cause:** CRD schema is missing a field that the controller writes to `status`. The API server silently strips it on every write. The controller re-writes it on the next reconcile → strips again → infinite loop.

**Fix:** always apply the CRD before upgrading the operator image:

```bash
helm show crds oci://ghcr.io/ihsenalaya/charts/preview-operator --version 0.13.8 \
  | tail -n +3 | kubectl apply -f -
kubectl rollout restart deployment/preview-operator -n preview-operator-system
```

### Environment stuck in Provisioning

```bash
kubectl describe preview pr-42
kubectl get events -n preview-pr-42 --sort-by='.lastTimestamp'
kubectl get pods -n preview-pr-42
kubectl logs -n preview-pr-42 job/postgres-migrate   # if DB step
kubectl logs -n preview-pr-42 deployment/svc-backend  # if service step
```

### Preview Failed — read full diagnostics

```bash
kubectl get preview pr-42 -o jsonpath='{.status.diagnostics}' | jq .
```

### Ingress x509 certificate error (Kind)

**Symptom:** `x509: certificate signed by unknown authority`

```bash
kubectl delete validatingwebhookconfiguration ingress-nginx-admission --ignore-not-found
helm upgrade ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --set controller.admissionWebhooks.enabled=false --wait
kubectl delete preview pr-42 --ignore-not-found
git commit --allow-empty -m "ci: retrigger" && git push
```

### Image pull failing (imagePullPolicy: IfNotPresent)

**Symptom:** operator rollout uses old image despite new tag being pushed.

**Cause:** `IfNotPresent` means the existing image layer is reused without checking the registry. This is the default in the chart.

**Fix:** use a new tag, or temporarily set `pullPolicy: Always`:

```bash
helm upgrade preview-operator … --set image.pullPolicy=Always --set image.tag=0.13.8
kubectl rollout restart deployment/preview-operator -n preview-operator-system
```

### Job failed — inspect logs

```bash
# Any test or AI job
kubectl logs -n preview-pr-42 job/smoke-tests
kubectl logs -n preview-pr-42 job/regression-tests
kubectl logs -n preview-pr-42 job/e2e-tests
kubectl logs -n preview-pr-42 job/ai-seed
kubectl logs -n preview-pr-42 job/ai-tests
kubectl logs -n preview-pr-42 job/ai-schema-dump
```

### No traces in Jaeger

```bash
kubectl get pod -n preview-pr-42 -l app=svc-backend \
  -o jsonpath='{.items[0].metadata.annotations}' | jq .
# Expected: "instrumentation.opentelemetry.io/inject-python": "observability/python"
```

### Check operator is watching the CRD

```bash
kubectl get preview           # shortname: kubectl get prev
kubectl api-resources | grep preview
# previews   prev   platform.company.io/v1alpha1   false   Preview
```

### Watch all reconcile activity

```bash
kubectl logs -n preview-operator-system deployment/preview-operator -f \
  | grep -E 'Reconcil|ERROR|WARN|phase|step'
```

### Useful commands reference

```bash
# List all environments
kubectl get preview
kubectl get prev  # shortname

# Full status
kubectl get prev pr-42 -o jsonpath='{.status}' | jq .

# Watch phase changes
kubectl get prev pr-42 --watch

# Approve an environment
kubectl patch preview pr-42 --type=merge -p '{"spec":{"approvedBy":"your-username"}}'

# Delete environment (triggers finalizer)
kubectl delete preview pr-42

# Reset database
kubectl patch preview pr-42 --type=merge -p '{"spec":{"database":{"resetRequested":true}}}'

# Save checkpoint
kubectl patch preview pr-42 --type=merge -p '{"spec":{"database":{"checkpointSave":"my-snap"}}}'

# Restore checkpoint
kubectl patch preview pr-42 --type=merge -p '{"spec":{"database":{"checkpointRestore":"my-snap"}}}'

# Trigger AI rerun
kubectl patch preview pr-42 --type=merge -p '{"spec":{"aiEnrichment":{"rerunRequested":true}}}'

# Read DB credentials
kubectl get secret postgres-credentials -n preview-pr-42 \
  -o jsonpath='{.data.DATABASE_URL}' | base64 -d

# Watch operator logs
kubectl logs -n preview-operator-system deployment/preview-operator -f

# Watch all Jobs in a preview namespace
kubectl get jobs -n preview-pr-42 -w
```

---

## License

Copyright 2026 ihsenalaya — Licensed under the [Apache License 2.0](LICENSE).
