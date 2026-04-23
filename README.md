# cellenza-operator

A Kubernetes operator that provisions **ephemeral preview environments** for pull requests. Each `Cellenza` resource creates a dedicated namespace with its own deployment, service, ingress, resource quota — and optionally a **PostgreSQL database with auto-generated credentials** — and tears it all down automatically when the TTL expires.

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

### 3. Install the operator

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
  --version 0.1.0 \
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
```

The operator will:
1. Generate a unique username (`preview_42`) and a cryptographically random password
2. Store them in a Secret `postgres-credentials` in the PR namespace
3. Start a `postgres:15-alpine` deployment
4. Block the app pod with an init container (`busybox`) until PostgreSQL is ready
5. Inject the credentials into the app container as environment variables

**Credentials are generated once and never overwritten**, even if the Cellenza is updated.

The app receives these environment variables automatically:

| Variable | Example value |
|---|---|
| `POSTGRES_USER` | `preview_42` |
| `POSTGRES_PASSWORD` | `a3f8c2...` (64-char hex) |
| `POSTGRES_DB` | `appdb` |
| `DATABASE_URL` | `postgresql://preview_42:a3f8c2...@postgres:5432/appdb?sslmode=disable` |

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
| `status.conditions` | Kubernetes-standard conditions: `Ready`, `Approved`, `Expired`, `DatabaseReady` |

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

# Check controller logs
kubectl logs -n cellenza-operator-system \
  deployment/cellenza-operator -f
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
> kubectl apply -f https://github.com/ihsenalaya/cellenza-operator/releases/latest/download/crds.yaml
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

### Run tests

```bash
make test
```

### Release a new version

```bash
git tag v0.2.0
git push origin v0.2.0
```

GitHub Actions will:
1. Build and push the Docker image to GHCR
2. Package the Helm chart and publish it to GitHub Releases and GitHub Pages

---

## Architecture

```
cellenza-operator/
├── api/v1alpha1/          # CRD types (CellenzaSpec, CellenzaStatus)
├── internal/
│   ├── controller/        # Reconciliation loop
│   └── webhook/v1alpha1/  # Defaulter + Validator admission webhooks
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
- `Deployment` `app` — runs the specified image; includes a `busybox` init container that blocks startup until PostgreSQL is ready
- `Service` `app` — ClusterIP service for the app
- `Ingress` — exposes the environment at `pr-<number>.preview.localtest.me`

A **finalizer** ensures all child resources (including the PostgreSQL deployment and credentials secret) are cleaned up even when the `Cellenza` is force-deleted.

---

## License

Copyright 2026 ihsenalaya — Licensed under the [Apache License 2.0](LICENSE).
