# Preview Operator

> **A Kubernetes operator that reconciles `Preview` custom resources into fully isolated environments — multi-service stack, ephemeral PostgreSQL, sequential test pipeline, OpenTelemetry, AI-generated seed data, and GitHub integration.**

The operator does **not** watch GitHub pull requests. It only reconciles `Preview` CRs. The typical integration point is a CI pipeline (GitHub Actions, GitLab CI, …) that creates the CR when a PR is opened and deletes it when the PR is closed.

### Full workflow — CI + Operator

```
 Pull request opened
        │
        ▼
 CI pipeline (GitHub Actions)
   ├── build image → push to registry
   ├── classify diff → detect changed files, impacted layers
   └── kubectl apply -f - <<EOF
       apiVersion: platform.company.io/v1alpha1
       kind: Preview
       metadata:
         name: pr-42
       spec:
         branch: feature/my-feature
         prNumber: 42
         image: ghcr.io/myorg/myapp:sha-abc
         ttl: 48h
         database:        { enabled: true, migration: { enabled: true, command: [...] } }
         aiEnrichment:    { enabled: true, apiSecretRef: { name: ai-api-key, key: api-key }, temperature: "0.7" }
         testSuite:       { enabled: true, smoke: {}, regression: { enabled: true } }
         kagent:          { enabled: true }
         github:          { enabled: true, owner: myorg, repo: myapp, ... }
         changeContext:   { diffRef: {...}, changedFiles: [...], diffPatchRef: "preview-diff-pr-42" }
       EOF
        │
        ▼
 Preview Operator (reconcile loop takes over)
   ├── create namespace preview-pr-42 (NetworkPolicy, ResourceQuota, PSS labels)
   ├── provision ephemeral PostgreSQL + run migrations + static seed
   ├── deploy application (Deployment + Service + VirtualService/Ingress)
   ├── wait for pods ready → phase = Running → post GitHub Deployment status
   ├── AI enrichment: pg_dump → LLM → seed.sql + test.py → Jobs
   ├── diff-analyzer agent → structured PR comment (what changed, what to watch)
   ├── test-strategist agent (if mode=Auto) → decide which suites to run
   └── test pipeline: smoke → contract (Microcks) → regression → E2E
         └── on failure: kagent troubleshooter → diagnosis posted to PR comment
        │
        ▼
 Pull request closed
        │
        ▼
 CI pipeline
   └── kubectl delete preview pr-42
         └── operator finalizer: namespace deleted, GitHub: inactive
```

---

## Table of Contents

0. [Release notes — 1.1.0](#release-notes--110)
1. [Feature Matrix](#1-feature-matrix)
2. [General Architecture](#2-general-architecture)
   - [Namespace Security — NetworkPolicy & Pod Security Standards](#namespace-security--networkpolicy--pod-security-standards)
3. [Installation](#3-installation)
4. [Controller Deep Dive](#4-controller-deep-dive)
5. [Feature Guides](#5-feature-guides) → detailed per-feature docs in [`docs/features/`](docs/features/)
6. [Complete CR Reference](#6-complete-cr-reference)
7. [Status Fields Reference](#7-status-fields-reference)
8. [Helm Values Reference](#8-helm-values-reference)
9. [Development & Release](#9-development--release)
10. [Debugging & Troubleshooting](#10-debugging--troubleshooting)
11. [Security](#11-security)

---

## Release notes — 1.1.0

Version 1.1.0 ships the **failure-provenance** capability: the operator now
captures a structured, auditable evidence bundle for every failed preview and
preserves it past namespace teardown. The bundle materialises a W3C
PROV-aligned graph linking the change context, the preview resources, the
captured evidence, and the diagnosis. Two new CLIs (`fp-diagnose`,
`fp-score`) consume the bundle for offline diagnosis and scoring.

### New CRD — `FailureReport` (cluster-scoped, shortname `fr`)

Every failed `Preview` now produces a `FailureReport` in the same cluster.
Being **cluster-scoped** it survives deletion of the preview namespace —
the audit trail is no longer lost when the preview is torn down.

```bash
kubectl get failurereports
# NAME                PHASE       PREVIEW   PR    SUITE       LEVEL   COMPONENT        AGE
# pr-42-failure       Persisted   pr-42     42    regression  C5      app              2m

kubectl get fr pr-42-failure -o jsonpath='{.status.diagnosis}' | jq .
kubectl get fr pr-42-failure -o jsonpath='{.status.evidenceItems[*].type}'
```

Each report carries:
- `evidenceItems[]` — typed evidence (`GitDiff`, `ChangedFile`,
  `KubernetesEvent`, `PodLog`, `JobLog`, `TestResult`, `TraceSpan`,
  `Metric`, `PreviewCondition`, `ReconcileEvent`). IDs are deterministic
  → idempotent re-reconciliation.
- `diagnosis` — probable cause, `component`, `category` (one of `database`,
  `configuration`, `infrastructure`, `application`, `observability`,
  `test-reliability`, `unknown`), `confidence`, and `evidenceRefs` that
  must each point to an existing `evidenceItem.id` (grounding constraint).
- `provenanceGraph` — W3C PROV nodes/edges (`entity`, `activity`, `agent`)
  linking the PR, the preview, the evidence, and the diagnosis. Only
  populated at evidence level C5.
- `collectionDurationMicros`, `collectionAllocBytes`, `bundleSizeBytes` —
  in-process collection overhead (sub-millisecond, typically a few KB).
- `preservedAfterTeardown: true` once the namespace has been deleted.

### Evidence levels (`EVIDENCE_LEVEL` env var)

The collector emits the same `FailureReport` shape at five comparison
configurations. The operator captures C5 once; downstream tools can
down-sample to a lower level at diagnosis time.

| Level | Content |
|-------|---------|
| `C1` | pod/job logs only |
| `C2` | logs + Kubernetes events |
| `C3` | logs + events + test results |
| `C4` | full evidence bundle (all collectors) |
| `C5` (default) | full bundle **plus** the provenance graph |

```bash
# Turn evidence capture off entirely (RQ5 overhead baseline)
kubectl set env deployment/preview-operator \
  EVIDENCE_COLLECTION=disabled \
  -n preview-operator-system

# Set a specific level
kubectl set env deployment/preview-operator \
  EVIDENCE_LEVEL=C4 \
  -n preview-operator-system
```

### Provisioning deadline

A preview that stays in `Provisioning` for more than **15 minutes** is now
failed automatically with reason `ProvisioningTimeout`. This guarantees a
`FailureReport` is produced even for previews that would otherwise
reconcile forever (image-pull stuck, dependency never ready, etc.).

### New CLIs — `fp-diagnose` and `fp-score`

Both are shipped with the operator image and are also published as
standalone binaries.

```bash
# Diagnose offline (deterministic, no network)
kubectl get fr pr-42-failure -o json | fp-diagnose --engine rule

# Diagnose with an LLM (OpenAI-compatible endpoint)
fp-diagnose --report report.json --engine llm \
  --mode grounded --model gpt-4o-mini

# Score a run against the scenarios.yaml ground truth
fp-score --report report.json --result diag.json --scenario F1 \
         --run-id F1-C4-003 --cluster-type aks
fp-score --csv-header                # print the columns and exit
```

| Engine | Mode | What it does |
|--------|------|---------------|
| `rule` (default) | `grounded` | Deterministic offline rules; every cited evidence ID must exist in the bundle |
| `rule` | `freeform` | Same rules with the grounding constraint disabled (ablation baseline) |
| `llm` | `grounded` | LLM-backed diagnosis where cited IDs are post-validated |
| `llm` | `freeform` | LLM-backed diagnosis with no grounding constraint |

### Other 1.1.0 changes

- Operator-captured app-pod logs now fall back to the **previous container
  instance** when the current pod has crashed — diagnostics no longer come
  up empty on `CrashLoopBackOff`.
- Migration-rule diagnoser tightened: token-aware match avoids the
  over-match defect observed in 1.0.x.
- AI enrichment retries `429 Too Many Requests` with exponential backoff;
  AI seed enabled regardless of `changeContext` heuristic when an explicit
  `seed.enabled: true` is set (continuation of the 1.0.46 fix).
- DB reconcile conflict path requeues instead of failing — fewer spurious
  `Failed` previews under contention.
- New ClusterRole verbs: `failurereports`,
  `failurereports/status` (`get;list;watch;create;update;patch;delete`).

### Upgrade

```bash
kubectl apply -f charts/preview-operator/crds/platform.company.io_previews.yaml
kubectl apply -f charts/preview-operator/crds/platform.company.io_failurereports.yaml
helm upgrade preview-operator ./charts/preview-operator \
  --namespace preview-operator-system \
  --set image.tag=1.1.0 --reuse-values
kubectl -n preview-operator-system rollout status deployment/preview-operator --timeout=120s
```

> **Apply the new `failurereports` CRD before rolling the operator** —
> if the deployment starts first it will log `no kind "FailureReport"`
> errors until the CRD is admitted.

---

## Release notes — 1.0.48

Version 1.0.48 makes the kagent test-selection decision visible in the PR
comment. Versions 1.0.46–1.0.47 fix defects that broke the test pipeline on
real (multi-PR, AKS) clusters. All fixes are in this chart/image. Note the
**kagent 0.9.2 requirement** below.

### PR comment shows the test suites kagent chose to run (`preview-operator`) — 1.0.48

**Problem.** The "🧠 Stratégie kagent" section of the test-results comment
only rendered `canSkip` (skipped suites). When the agent decides that every
suite must run — so `canSkip` is empty — the comment showed a rationale and a
confidence score but no list of suites, making the agent's decision look
absent.

**Fix.** The comment builder now also renders a **"Suites à exécuter"** table
from `plan.Spec.MustRun`, with the per-suite reason, before the skipped-suites
table. The agent's test-selection decision is always visible, whether it runs
or skips suites.

### Reliable e2e checkpoint restore (`preview-extension`) — 1.0.46

**Problem.** The e2e suite calls `reset_db()` before every test, which hits
the extension's `POST /api/previews/<pr>/checkpoints/<name>/restore`
endpoint. The extension used to patch `spec.database.checkpointRestore` and
wait for the operator to clear it. The operator services that request with a
**single, name-derived Job** (`checkpoint-restore-<name>`). When the e2e
suite fires several `reset_db()` calls in quick succession, that one job is
deleted and recreated under the same name — the recreate races the previous
delete (`AlreadyExists`), or the operator reuses the still-terminating Job.
The handshake stalls past the 60s client timeout, `reset_db()` fails, the
e2e Job retries, and the operator requeues every 2s (the "infinite loop").

**Fix.** `handleCheckpointRestore` now performs the restore itself: it
creates a **uniquely-named Job** (`ext-restore-<checkpoint>-<timestamp>`)
that truncates the public tables and replays `db-checkpoint-<name>`, waits
for that specific job, and deletes it. A fresh name per call removes the
collision entirely. Measured: restore returns in ~6s, no requeue loop.

The extension ClusterRole now also grants `batch/jobs`
(`get,list,watch,create,delete`) — see `config/extension/rbac.yaml`.

### AI seed honours an explicit `seed.enabled` (`preview-operator`) — 1.0.46

**Problem.** `aiSeedEnabled()` skipped the AI seed whenever
`spec.changeContext` was present and `detectedImpacts.RequiresSeedData` was
false — e.g. for any PR without a schema migration. A skipped seed leaves
the database empty, so the post-seed checkpoint is empty, and the regression
and e2e suites run against zero rows (`product_detail`/`related` → 404,
`catalog_page_loads` finds no grid).

**Fix.** `aiSeedEnabled()` now treats an explicit `aiEnrichment.seed`
configuration as authoritative: if `seed.enabled` is set it is honoured
directly, and the `changeContext` heuristic only applies when no explicit
seed configuration is present. Since the CRD defaults `seed.enabled` to
`true`, the AI seed now runs for every enriched preview unless explicitly
disabled.

### kagent troubleshooter agent URL (`preview-operator`) — 1.0.47

**Problem.** `callKagentAgent` used `kagentFailureAnalystURL`, which
hard-codes a `failure-analyst-agent` service that is not deployed. Every A2A
call failed with a DNS error, `status.kagent.phase` never left `Failed`, and
the troubleshooter analysis never ran on a failed test suite.

**Fix.** `callKagentAgent` now uses `kagentAgentURL`, honouring
`spec.kagent.agentName` (default `preview-troubleshooter-agent`). The unused
`kagentFailureAnalystURL` helper is removed.

### Requires kagent 0.9.2

The kagent agents (diff-analyzer, test-strategist, troubleshooter) must run
on **kagent 0.9.2**. kagent 0.9.4 regressed A2A session handling: the session
is created under `user_id=admin@kagent.dev` but the ADK runner looks it up
under `user_id=A2A_USER_<ctx>`, so every agent run fails with
`SessionNotFoundError`. Pin kagent to 0.9.2 — see Step 6c.

### Upgrade

```bash
kubectl apply -f charts/preview-operator/crds/platform.company.io_previews.yaml
helm upgrade preview-operator ./charts/preview-operator \
  --namespace preview-operator-system \
  --set image.tag=1.0.48 --reuse-values
kubectl -n preview-operator-system rollout status deployment/preview-operator --timeout=120s

# the extension is versioned with the operator — redeploy it too
kubectl apply -f config/extension/rbac.yaml
kubectl apply -f config/extension/deployment.yaml
kubectl -n preview-operator-system rollout status deployment/preview-extension --timeout=120s
```

---

## 1. Feature Matrix

| Feature | Flag / Field | Default |
|---------|-------------|---------|
| Isolated namespace per Preview CR | always on | — |
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
| **OpenAPI contract testing (Microcks)** | `spec.testSuite.contractTesting.enabled` | `false` |
| **Auto spec import into Microcks** | `spec.testSuite.contractTesting.specURL` | — |
| Regression tests | `spec.testSuite.regression.enabled` | `false` |
| E2E tests (Playwright) | `spec.testSuite.e2e.enabled` | `false` |
| AI seed data + tests | `spec.aiEnrichment.enabled` | `false` |
| AI sampling temperature | `spec.aiEnrichment.temperature` (string, `"0".."2"`) | `"0.2"` |
| AI-only rerun | `spec.aiEnrichment.rerunRequested` | `false` |
| **AI failure analysis (kagent)** | `spec.kagent.enabled` | `false` |
| **AI test selection (test-strategist)** | `spec.testStrategy.mode: Auto` | `FullSuite` |
| Smart failure diagnostics | always on when `Failed` | — |
| Copilot Extension commands | sidecar server | optional |
| **NetworkPolicy per namespace** | always on | deny cross-PR ingress; allow ingress-nginx + istio-system + intra-pod |
| **Pod Security Standards labels** | always on | `enforce: baseline`, `warn: restricted` |
| **FailureReport CRD** (cluster-scoped, survives teardown) | `EVIDENCE_COLLECTION` env | `enabled` |
| **Evidence level (C1..C5)** | `EVIDENCE_LEVEL` env | `C5` (full + provenance graph) |
| **Provisioning deadline backstop** | always on | 15 min → `Failed` with `ProvisioningTimeout` |

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
│    ├── reconcileExposure()       → VirtualService (Istio) or Ingress (auto-detected)│
│    ├── handleAppAvailability()   → RequeueAfter=10s if not yet ready      │
│    ├── markRunningStatus()       → phase=Running, URL, conditions         │
│    ├── syncGitHubAfterStatus()   → Deployment: success + PR comment       │
│    │                                                                        │
│    ├── reconcileAIEnrichment()   → (if enabled)                           │
│    │     schema-dump → generate → ai-seed → ai-tests                      │
│    │     RequeueAfter=10s until Succeeded or Failed                        │
│    │                                                                        │
│    ├── reconcileTestSuite()      → (if enabled AND AI done or disabled)   │
│    │     saving → smoke → import-spec → contract →                        │
│    │     restore-regression → regression → restore-e2e → e2e              │
│    │     RequeueAfter=5s at each step                                      │
│    │                                                                        │
│    └── triggerKagentAnalysis()   → (if tests.phase=Failed)                │
│          Creates ephemeral Job (curlimages/curl) → POST A2A JSON-RPC      │
│          → preview-troubleshooter-agent                                    │
│          Cooldown guard: no retry within 5 min                             │
│          Posts structured diagnosis as GitHub PR comment                   │
│                                                                            │
│  RequeueAfter = ttlRemaining (keeps controller alive until expiry)        │
└───────────────────────────┬────────────────────────────────────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         ▼                  ▼                  ▼
   kubectl delete     TTL expired        rerunRequested=true
   (CI on PR close)   r.Delete(prev)     AI-only rerun cycle
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
│     ├── VirtualService/Ingress    │  pr-1.preview.ihsenalaya.xyz
│     ├── ConfigMap: ai-enrichment  │
│     ├── ConfigMap: db-checkpoint-after-seed │
│     └── Jobs: postgres-migrate, ai-seed,   │
│               smoke-tests, e2e-tests, …    ┘
│
├── preview-pr-2/                   ← PR #2 is entirely independent
└── preview-pr-N/ …
```

### Namespace Security — NetworkPolicy & Pod Security Standards

Every preview namespace is hardened automatically by the operator **before any workload is created**. There is no opt-in flag — both controls are always on.

#### NetworkPolicy `preview-isolation`

```
┌── Namespace: preview-pr-42 ──────────────────────────────────────┐
│                                                                    │
│  NetworkPolicy: preview-isolation                                  │
│                                                                    │
│  Ingress — three sources allowed, everything else denied:         │
│    ① pods within the same namespace  (app ↔ postgres, tests ↔ app)│
│    ② pods in namespace "ingress-nginx"  (nginx public traffic in) │
│    ③ pods in namespace "istio-system"   (Istio gateway traffic in)│
│                                                                    │
│  Egress — unrestricted:                                            │
│    ① AI API calls  (Azure OpenAI / OpenAI)                        │
│    ② GitHub API    (PR comments, Deployment status)               │
│    ③ GHCR          (image pulls)                                  │
│    ④ kube-dns      (service discovery)                            │
│                                                                    │
│  Result: preview-pr-42 cannot reach preview-pr-43, production,    │
│  or any other namespace — even if a pod is compromised.           │
└────────────────────────────────────────────────────────────────────┘
```

The policy is created in `reconcileNamespace()`, the first step of provisioning. If creation fails the controller returns an error and retries — no deployment proceeds without it.

```bash
# Verify the policy is in place
kubectl get networkpolicy -n preview-pr-42
# NAME                POD-SELECTOR   AGE
# preview-isolation   <none>         5s

kubectl describe networkpolicy preview-isolation -n preview-pr-42
```

#### Pod Security Standards labels

```yaml
# Applied to the namespace at creation time
pod-security.kubernetes.io/enforce: baseline    # blocks: privileged containers,
                                                #   hostPath mounts, host networking,
                                                #   host ports, host PID/IPC
pod-security.kubernetes.io/warn:    restricted  # surfaces further gaps (seccomp,
                                                #   runAsNonRoot, readOnlyRootFilesystem)
                                                #   without blocking workloads
```

`enforce: baseline` is strict enough to block the most dangerous misconfigurations. `warn: restricted` logs actionable warnings in the API server audit log without rejecting Pods that aren't yet hardened for the full restricted profile.

```bash
# Verify the labels are set
kubectl get namespace preview-pr-42 \
  -o jsonpath='{.metadata.labels}' | jq 'with_entries(select(.key | startswith("pod-security")))'
# {
#   "pod-security.kubernetes.io/enforce": "baseline",
#   "pod-security.kubernetes.io/warn": "restricted"
# }
```

For the full threat model, RBAC table, PAT → GitHub App migration path, and known limitations, see [SECURITY.md](SECURITY.md).

---

## 3. Installation

### Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Kubernetes | 1.25+ | Kind, k3s, GKE, AKS, EKS, or any conformant cluster |
| Helm | 3.12+ | |
| cert-manager | 1.13+ | Required for webhook TLS — skip with `--set webhook.enabled=false` |
| ingress-nginx **or** Istio | any recent | Exposes preview URLs — operator auto-detects which is installed |
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

Preview URLs will be reachable at `http://pr-42.preview.localtest.me:8080` via port-forward. For public URLs without port-forward, install Istio and set `--set previewDomain=preview.<YOUR_ZONE>` (see Step 3b).

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

### Step 3 — Install ingress-nginx (local / fallback)

> Skip if using Istio (Step 3b). The operator auto-detects which is available.

**Kind clusters** — admission webhooks must be disabled:

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.admissionWebhooks.enabled=false \
  --wait

kubectl -n ingress-nginx rollout status deployment/ingress-nginx-controller --timeout=120s
```

**Production clusters:**

```bash
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx --create-namespace --wait
```

### Step 3b — Install Istio (recommended for AKS / public URLs)

The operator creates a `VirtualService` per Preview CR pointing to a shared `Gateway`. Each environment gets a public URL — no port-forward.

```bash
# Install istioctl
curl -sL "https://github.com/istio/istio/releases/download/1.23.0/istioctl-1.23.0-linux-amd64.tar.gz" \
  | tar -xz -C /usr/local/bin

# Install Istio with ingress gateway
istioctl install --set profile=minimal \
  --set components.ingressGateways[0].enabled=true \
  --set components.ingressGateways[0].name=istio-ingressgateway -y

# Get gateway IP
ISTIO_IP=$(kubectl get svc istio-ingressgateway -n istio-system \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Wildcard DNS (Azure example)
az network dns record-set a add-record \
  --zone-name <ZONE> --resource-group <RG> \
  --record-set-name "*.preview" --ipv4-address "$ISTIO_IP" --ttl 300

# Shared gateway (one per cluster)
kubectl apply -f - <<EOF
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: preview-gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
    - port:
        number: 80
        name: http
        protocol: HTTP
      hosts:
        - "*.preview.<ZONE>"
EOF
```

Then pass `--set previewDomain=preview.<ZONE>` when installing the operator (Step 6).

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

#### Production / AKS — install from the published OCI chart

The chart is published to GHCR (private package — authenticate with a PAT that
has `read:packages`). CRDs are bundled in the chart and applied on first install.

```bash
helm registry login ghcr.io -u <github-user>   # PAT with read:packages

helm install preview-operator oci://ghcr.io/ihsenalaya/charts/preview-operator \
  --version 1.1.0 \
  --namespace preview-operator-system \
  --create-namespace \
  --set image.tag=1.1.0 \
  --set previewDomain=preview.ihsenalaya.xyz \
  --set "ai.apiURL=https://<AOAI_RESOURCE>.openai.azure.com/openai/deployments/gpt-4o-mini"

kubectl -n preview-operator-system rollout status deployment/preview-operator --timeout=120s
```

> For a fully GitOps-managed install (Argo CD App-of-Apps covering the operator
> and every dependency), see the `gitops/` directory in the
> [idp-preview](https://github.com/ihsenalaya/idp-preview) repository.

#### Local development — Kind

Build the image locally and load it into Kind (no registry push needed for local dev):

```bash
# 1. Build
cd preview-operator
docker build -t ghcr.io/ihsenalaya/preview-operator:1.1.0 .

# 2. Load into Kind
kind load docker-image ghcr.io/ihsenalaya/preview-operator:1.1.0

# Also apply the new FailureReport CRD (added in 1.1.0)
kubectl apply -f charts/preview-operator/crds/platform.company.io_failurereports.yaml

# 3. Apply CRD manually (Helm does not update CRDs on upgrade)
kubectl apply -f charts/preview-operator/crds/platform.company.io_previews.yaml

# 4. Install the Helm chart from the local directory
helm install preview-operator ./charts/preview-operator \
  --namespace preview-operator-system \
  --create-namespace \
  --set image.tag=1.1.0 \
  --set previewDomain=preview.ihsenalaya.xyz \
  --set "ai.apiURL=https://<AOAI_RESOURCE>.openai.azure.com/openai/deployments/gpt-4o-mini"

kubectl -n preview-operator-system rollout status deployment/preview-operator --timeout=120s
kubectl get crd previews.platform.company.io
```

**Without cert-manager (disable webhooks):**

```bash
helm install preview-operator ./charts/preview-operator \
  --namespace preview-operator-system \
  --create-namespace \
  --set image.tag=1.1.0 \
  --set webhook.enabled=false

```

**Verify:**

```bash
kubectl get pods -n preview-operator-system
# preview-operator-xxxxx   1/1   Running   0   30s

kubectl get crd previews.platform.company.io
# NAME                           CREATED AT
# previews.platform.company.io  2026-05-09T10:19:27Z
```

### Step 6b — Install Microcks

```bash
NODE_IP=$(kubectl get nodes -o wide | grep control-plane | awk '{print $6}')

helm install microcks microcks/microcks \
  --namespace microcks \
  --create-namespace \
  --set "microcks.url=microcks.${NODE_IP}.nip.io" \
  --set "microcks.ingressClassName=nginx" \
  --set "microcks.generateCert=false" \
  --set "keycloak.url=keycloak.${NODE_IP}.nip.io" \
  --set "keycloak.ingressClassName=nginx" \
  --set "keycloak.generateCert=false"

kubectl -n microcks rollout status deployment/microcks --timeout=180s
```

The operator's contract test jobs call `http://microcks.microcks.svc.cluster.local:8080` — in-cluster, no ingress required.

### Step 6c — Install kagent

> **Pin kagent to 0.9.2.** Do not omit `--version` — the default resolves to
> 0.9.4, which regressed A2A session handling (`SessionNotFoundError` on
> every agent run; see *Release notes — 1.0.47*). If 0.9.4 is already
> installed, downgrading also requires resetting the kagent database — see
> the idp-preview README, Step 4c.

```bash
# CRDs first — required before main chart; pin 0.9.2
helm install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --version 0.9.2 \
  --namespace kagent-system \
  --create-namespace

helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version 0.9.2 \
  --namespace kagent-system

kubectl -n kagent-system rollout status deployment/kagent-controller --timeout=120s
```

**Create the Azure OpenAI secret and configure the ModelConfig:**

```bash
# Get the API key
AOAI_KEY=$(az cognitiveservices account keys list \
  --name "preview-openai" --resource-group "<YOUR_RG>" \
  --query "key1" -o tsv)

# Secret for kagent agents
kubectl create secret generic kagent-openai \
  --namespace kagent-system \
  --from-literal=OPENAI_API_KEY="$AOAI_KEY"

# Secret for operator AI enrichment
kubectl create secret generic azure-openai-credentials \
  --namespace preview-operator-system \
  --from-literal=api-key="$AOAI_KEY"

# Configure ModelConfig
kubectl patch modelconfig default-model-config -n kagent-system --type=merge -p '{
  "spec": {
    "provider": "AzureOpenAI",
    "model": "gpt-4o-mini",
    "apiKeySecret": "kagent-openai",
    "apiKeySecretKey": "OPENAI_API_KEY",
    "azureOpenAI": {
      "azureEndpoint": "https://preview-openai-<ID>.openai.azure.com",
      "azureDeployment": "gpt-4o-mini",
      "apiVersion": "2024-10-21"
    }
  }
}'

# Deploy the troubleshooter agent (from idp-preview repo)
kubectl apply -f ../idp-preview/k8s/kagent/rbac-readonly.yaml
kubectl apply -f ../idp-preview/k8s/kagent/preview-troubleshooter-agent.yaml
```

### Upgrading the operator

```bash
# Rebuild and reload
docker build -t ghcr.io/ihsenalaya/preview-operator:<NEW_VERSION> .
kind load docker-image ghcr.io/ihsenalaya/preview-operator:<NEW_VERSION>

# Apply updated CRD first
kubectl apply -f charts/preview-operator/crds/platform.company.io_previews.yaml

# Upgrade Helm release
helm upgrade preview-operator ./charts/preview-operator \
  --namespace preview-operator-system \
  --set image.tag=<NEW_VERSION> \
  --reuse-values

kubectl -n preview-operator-system rollout status deployment/preview-operator --timeout=120s
```

> **`previewDomain` is baked into `values.yaml`** (`preview.ihsenalaya.xyz`).
> `--reuse-values` preserves it across upgrades — never drop it or the operator falls back to `localtest.me`.

> **Why CRD first?** If a new operator version writes a new status field not in the CRD schema, the API server silently strips it, causing an **infinite reconcile loop**. Always apply the CRD before the operator image.

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

### Webhook behaviors (defaulter + validator)

The admission webhook runs before any CR reaches the controller. Two behaviors are important to know:

| Trigger | Webhook action |
|---------|---------------|
| `spec.resourceTier: large` | Automatically sets `spec.requiresApproval: true` — cannot be bypassed |
| `spec.replicas > 3` | Admission **warning** posted: *"running N replicas in a preview env is expensive"* — not a rejection, but visible in `kubectl apply` output |
| `spec.telemetry.enabled: true` | Rejects if `spec.telemetry.autoInstrumentation` is nil — the field is required when telemetry is on |
| `spec.telemetry.autoInstrumentation.language: go` | Rejects if `goTargetExecutable` is not set — required for Go binary path injection |
| `spec.database.migration.enabled: true` | Rejects if `spec.database.migration.command` is empty |
| `spec.database.seed.enabled: true` | Rejects if `spec.database.seed.command` is empty |
| `spec.github.enabled: true` | Rejects if any of `owner`, `repo`, `deploymentId` (must be > 0), or `tokenSecretRef.name` are missing |
| `spec.aiEnrichment.enabled: true` | Defaults `spec.aiEnrichment.seed` and `spec.aiEnrichment.tests` to `{enabled: true}` if omitted |

Disable the webhook entirely with `--set webhook.enabled=false` (no TLS/cert-manager needed, but the above validations are skipped).

### Source files

```
internal/controller/
├── preview_controller.go    Main reconcile loop, namespace, quota, deployments, ingress
├── ai_enrichment.go          AI schema dump, generation, seed job, test job, prompt handling
├── checkpoint.go             DB checkpoint save/restore via pg_dump / psql Jobs
├── diagnostics.go            Failure root-cause analysis, log collection, debug commands
├── github.go                 GitHub Deployment status and PR comment publishing
├── kagent.go                 kagent failure analysis + test-strategist agent trigger (A2A)
├── testplan_strategy.go      TestPlan state machine (Auto/Manual/FullSuite modes)
└── tests.go                  Smoke, regression, E2E job orchestration and step state machine

internal/policy/
├── testplan_policy.go        IsSuiteSelected, ValidatePlan, ConfidenceThreshold, FullSuitePlan
└── …
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
           │     see docs/features/ai-enrichment.md
           │     → RequeueAfter=10s while Running
           │
           ├─ reconcileTestStrategy()   [if testSuite.enabled AND AI done]
           │     mode=Auto:
           │       create TestPlan stub → A2A call to test-strategist-agent
           │       agent fills mustRun/canSkip → promoted to Ready
           │       accept if confidence ≥ threshold; fallback=Full on timeout
           │     mode=FullSuite: skip agent, run all enabled suites
           │     see docs/features/ai-test-strategist.md
           │     → RequeueAfter=10s while AwaitingTestPlan
           │
           └─ reconcileTestSuite()      [if testSuite.enabled AND plan accepted]
                 only suites in mustRun/shouldRun are scheduled
                 canSkip suites → phase=Skipped immediately
                 see docs/features/test-suites.md
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
│  ConfigMap        │ preview-test-suite          │ testSuite.enabled            │
│  Job              │ suite-checkpoint-save       │ step=saving                  │
│  Job              │ smoke-tests                 │ step=smoke                   │
│  Job              │ microcks-import             │ step=import-spec (specURL)   │
│  Job              │ microcks-contract-tests     │ step=contract                │
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
                 │ testStrategy.mode=Auto
                 │ (waiting for test-strategist-agent to fill TestPlan)
                 ▼
        AwaitingTestPlan ──────────────────────────────► Running
           (kagent agent reads diff,                     (TestPlan accepted,
            historical ReconcileEvents,                   test suite starts)
            decides which suites to run)
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

## 5. Feature Guides

Each capability has a dedicated, code-grounded guide under
[`docs/features/`](docs/features/). Every guide follows the same shape —
**Introduction → What it's for → What it does → diagram → Relationships →
Configuration → Reference** — so you can scan any feature the same way. The
[Feature Matrix](#1-feature-matrix) above lists the flags and defaults; the guides
explain how each one works.

### Platform

| Guide | What it covers |
|-------|----------------|
| [Lifecycle & Provisioning](docs/features/lifecycle.md) | The core reconcile loop: one namespace per PR, phases, resource tiers, approval gate, TTL, the 15-min provisioning deadline. |
| [Ephemeral PostgreSQL](docs/features/ephemeral-postgres.md) | Per-preview throwaway database: migrations, static seed, injected credentials, on-demand reset. |
| [Database Checkpoints](docs/features/database-checkpoints.md) | Save/restore a deterministic DB state so each test suite starts from the same seed. |
| [Networking & Exposure](docs/features/networking-exposure.md) | Services, Ingress / Istio routing, the public preview URL, path-based multi-service routing. |
| [Security & Isolation](docs/features/security.md) | NetworkPolicy, Pod Security Standards, ResourceQuota, and bounded-blast-radius RBAC. |
| [Observability](docs/features/observability.md) | OpenTelemetry auto-instrumentation wiring for preview workloads. |

### Testing

| Guide | What it covers |
|-------|----------------|
| [Test Suites](docs/features/test-suites.md) | Smoke, OpenAPI contract (Microcks), regression, and E2E (Playwright) suites and how they run. |
| [Authoring Tests](docs/features/authoring-tests.md) | **How to add your own tests** — the `/app/tests/` contract, injected env vars, and command/image overrides. |
| [AI Test Strategist](docs/features/ai-test-strategist.md) | The kagent agent that picks which suites to run from the PR diff, via the `TestPlan` CRD. |
| [Change Context](docs/features/change-context.md) | The PR diff as a first-class reconciliation input (deterministic gate vs. advisory signals). |

### AI & diagnostics

| Guide | What it covers |
|-------|----------------|
| [AI Enrichment](docs/features/ai-enrichment.md) | LLM-generated seed data and targeted tests, run automatically after the preview is ready. |
| [AI Failure Analysis (kagent)](docs/features/ai-failure-analysis.md) | Root-cause analysis on a failed preview, surfaced in `status.kagent` and the PR comment. |
| [Failure Provenance](docs/features/failure-provenance.md) | The `FailureReport` CRD: durable, PROV-aligned evidence bundles plus the `fp-diagnose` / `fp-score` CLIs. |
| [Customizing AI Prompts](docs/features/ai-prompts.md) | **How to change the AI prompts** — per-PR (`set-prompt`), global (Helm), and per-agent (Agent CR). |
| [MCP Servers & Agent Tools](docs/features/mcp-servers.md) | The MCP tool servers the kagent agents use and how to grant an agent new tools. |
| [kagent — Architecture & Internals](docs/features/kagent-architecture.md) | **Deep dive:** agent creation, the A2A protocol, authentication, Azure OpenAI wiring, and every agent. |

### Integration

| Guide | What it covers |
|-------|----------------|
| [GitHub Integration](docs/features/github-integration.md) | Deployment statuses and PR comments (results table + AI sections). |
| [Copilot Extension](docs/features/copilot-extension.md) | `@preview` ChatOps commands driven from GitHub Copilot Chat. |

> New here? Start with [Lifecycle & Provisioning](docs/features/lifecycle.md) — it is the
> loop every other feature plugs into. The full index is in
> [docs/features/README.md](docs/features/README.md).

---

## 6. Complete CR Reference

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
    contractTesting:
      enabled: true
      microcksURL: http://microcks.microcks.svc.cluster.local:8080
      apiName: "Preview Catalog API"  # must match info.title in openapi.yaml
      apiVersion: "1.0.0"             # must match info.version
      specURL: https://raw.githubusercontent.com/OWNER/REPO/BRANCH/api/openapi.yaml
      importUsername: manager         # default "manager"
      importPassword: microcks123     # default "microcks123"
      # credentialsSecretName: microcks-creds  # alternative: read from Secret
      # keycloakURL: http://microcks-keycloak.microcks.svc.cluster.local:8080/realms/microcks
      testRunner: OPEN_API_SCHEMA     # OPEN_API_SCHEMA | HTTP | POSTMAN
      timeoutSeconds: 60
    regression:
      enabled: true
    migration:
      enabled: true  # validates Alembic scripts; runs before regression/e2e when migration files detected in diff
    e2e:
      enabled: true

  # ── kagent AI failure analysis ─────────────────────────────────────────────
  kagent:
    enabled: true
    namespace: kagent-system
    agentName: preview-troubleshooter-agent         # triggered on test suite failure
    diffAnalyzerAgentName: preview-diff-analyzer    # triggered on first Running (diff comment)
    testStrategistAgentName: test-strategist-agent  # triggered when TestPlan is Pending (Auto mode)

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

  # ── PR Diff Context (set by generate_preview_manifest.py) ──────────────────
  changeContext:
    diffRef:
      provider: GitHub
      repository: acme/myapp
      prNumber: 42
      baseSHA: "abc123"
      headSHA: "def456"
    summary:
      changedFilesCount: 5
      additions: 120
      deletions: 30
    changedFiles:
      - path: api/routes/orders.py
        type: backend
      - path: migrations/20260510_add_status.py
        type: database-migration
    detectedImpacts:
      database: true
      apiContract: false
      backend: true
      frontend: false
      requiresSeedData: true
      requiresContractTests: false
      requiresRegressionTests: true
    diffPatchRef: preview-diff-pr-42   # ConfigMap in preview namespace, key: diff.patch
```

---

## 7. Status Fields Reference

```bash
kubectl get preview pr-42 -o jsonpath='{.status}' | jq .
```

| Field | Description |
|---|---|
| `status.phase` | `Pending` / `Provisioning` / `Running` / `AwaitingTestPlan` / `Failed` / `Terminating` |
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
| `status.aiEnrichment.rerunOnly` | `true` while an AI-only rerun (no deployment change) is in progress |
| `status.aiEnrichment.seedStatus` | `Skipped` / `Running` / `Succeeded` / `Failed` |
| `status.aiEnrichment.testsStatus` | `Skipped` / `Running` / `Succeeded` / `Failed` |
| `status.aiEnrichment.testResults` | `["PASS: test_health", "FAIL: test_order_stock — …"]` |
| `status.aiEnrichment.summary` | Human-readable enrichment summary posted to the GitHub PR comment |
| `status.aiEnrichment.error` | Latest enrichment error message (non-fatal, enrichment retried) |
| `status.aiEnrichment.completedAt` | Timestamp |
| `status.tests.phase` | `Running` / `Succeeded` / `Failed` |
| `status.tests.step` | Current step: `saving` / `smoke` / `import-spec` / `contract` / `restore-regression` / `regression` / `restore-e2e` / `e2e` |
| `status.tests.smoke.phase` | Job phase |
| `status.tests.smoke.passed` | int |
| `status.tests.smoke.failed` | int |
| `status.tests.contract.phase` | `Succeeded` / `Failed` |
| `status.tests.contract.passed` | int — Microcks test steps passed |
| `status.tests.contract.failed` | int — Microcks test steps failed |
| `status.tests.contract.output` | `["PASS contract GET /api/products", "FAIL contract …"]` |
| `status.tests.regression.*` | Same structure as smoke |
| `status.tests.e2e.*` | Same structure as smoke |
| `status.kagent.phase` | `Running` / `Succeeded` / `Failed` — failure analysis agent (post-test) |
| `status.kagent.triggeredAt` | Timestamp of last trigger |
| `status.kagent.commentId` | GitHub comment ID where analysis was posted |
| `status.diffAnalysis.phase` | `Running` / `Succeeded` / `Failed` — diff analysis agent (on first Running) |
| `status.diffAnalysis.triggeredAt` | Timestamp when diff analysis started |
| `status.diffAnalysis.commentId` | GitHub PR comment ID where diff analysis was posted |
| `status.diffAnalysis.analysis` | Raw text from the diff-analyzer agent |
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
| `status.conditions` | `Ready`, `Approved`, `Expired`, `DatabaseReady`, `MigrationReady`, `SeedReady`, `AIEnrichmentReady`, `ContractTestReady`, `TestSuiteReady` |

---

## 8. Helm Values Reference

```yaml
replicaCount: 1

image:
  repository: ghcr.io/ihsenalaya/preview-operator
  pullPolicy: IfNotPresent
  tag: ""                       # defaults to Chart.appVersion (1.0.47)

# ── AI Enrichment ──────────────────────────────────────────────────────────────
ai:
  # LLM endpoint — supports OpenAI, Azure OpenAI, or GitHub Models (free tier)
  # Azure OpenAI:   https://<resource>.openai.azure.com/openai/deployments/<model>
  # OpenAI:         https://api.openai.com/v1
  # GitHub Models:  https://models.inference.ai.azure.com  (default — free tier)
  apiURL: "https://models.inference.ai.azure.com"

  # Override the default system prompt for seed + test generation.
  # Leave empty to use the built-in prompt (recommended).
  # Override via: --set-file ai.systemPrompt=./my-prompt.txt
  systemPrompt: ""

# Base domain for preview URLs — each PR gets http://pr-<N>.<previewDomain>
# Requires a wildcard DNS record: *.<previewDomain> → Istio Gateway or Ingress IP
# Example: "preview.ihsenalaya.xyz"
# Leave empty to use the localtest.me fallback (no DNS setup, requires port-forward)
previewDomain: ""

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

## 9. Development & Release

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

## 10. Debugging & Troubleshooting

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

## 11. Security

Every preview namespace is isolated by a **NetworkPolicy** (`preview-isolation`) created automatically by the operator:

- **Ingress**: allows only traffic from same-namespace pods and the `ingress-nginx` namespace. All other cross-namespace ingress is denied — a pod in `preview-pr-42` cannot reach `preview-pr-43`.
- **Egress**: unrestricted (required for AI API, GitHub API, image pulls). Restrict to known CIDRs for environments containing PII.

Every preview namespace also carries **Pod Security Standard** labels:

```yaml
pod-security.kubernetes.io/enforce: baseline    # blocks privileged containers, hostPath mounts, host networking
pod-security.kubernetes.io/warn:    restricted   # surfaces further hardening opportunities without blocking workloads
```

For the full threat model, RBAC table, PAT vs GitHub App security analysis, and known limitations, see [SECURITY.md](SECURITY.md).

For architecture, demo scenario, measured timings, failure modes, and production-readiness assessment of each component, see [docs/kubecon-demo.md](docs/kubecon-demo.md).

---

## License

Copyright 2026 ihsenalaya — Licensed under the [Apache License 2.0](LICENSE).
