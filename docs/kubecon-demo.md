# KubeCon Demo — Preview Operator

> Architecture, demo script, known limits, failure modes, and what we'd do differently.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│  Developer — git push → GitHub PR opened                             │
└──────────────────────────────┬───────────────────────────────────────┘
                               │ pull_request event
                               ▼
┌──────────────────────────────────────────────────────────────────────┐
│  GitHub Actions (.github/workflows/preview.yaml)                     │
│  runs-on: [self-hosted, aks, test1]  ← runner pod inside AKS        │
│                                                                      │
│  1. Kaniko Job  → build image → push to GHCR                        │
│  2. kubectl apply Preview CR                                         │
└──────────────────────────────┬───────────────────────────────────────┘
                               │ CR written to etcd
                               ▼
┌──────────────────────────────────────────────────────────────────────┐
│  PreviewReconciler  (controller-runtime loop)                        │
│                                                                      │
│  Namespace  →  ResourceQuota  →  NetworkPolicy                       │
│  →  PostgreSQL  →  Migration  →  Seed                               │
│  →  Deployments  →  Services  →  Ingress                            │
│  →  markRunning  →  GitHub Deployment: success                       │
│                                                                      │
│  (async, same loop)                                                  │
│  →  TestStrategy (AI diff analysis) → selects which tests to run    │
│  →  AI schema-dump  →  AI generate  →  ai-seed  →  ai-tests        │
│  →  suite-checkpoint-save  →  smoke  →  regression  →  e2e         │
│  →  GitHub PR comment: results table                                 │
└──────────────────────────────┬───────────────────────────────────────┘
                               │
          ┌────────────────────┴────────────────────┐
          ▼                                         ▼
   preview-pr-N namespace                 preview-operator-system
   ├── postgres Deployment                ├── preview-operator pod
   ├── svc-backend Deployment             ├── preview-extension pod
   ├── svc-frontend Deployment            ├── Secret: github-token
   ├── NetworkPolicy: preview-isolation   └── Secret: ai-api-key
   ├── ResourceQuota: preview-quota
   ├── Ingress: pr-N.preview.ihsenalaya.xyz
   └── Jobs: migrate, seed, AI, tests, kagent
```

### Key design decisions

| Decision | Rationale |
|---|---|
| One namespace per PR | Complete resource isolation — no cross-PR data contamination |
| Controller-managed NetworkPolicy | Isolation enforced by the operator, not by convention |
| AI Test Strategist (TestPlan CRD) | Only runs tests relevant to the PR diff — faster feedback |
| Checkpoint-based DB reset | Deterministic test starting state — smoke/regression/E2E each see the same seed |
| AI enrichment before tests | Tests need seeded data; controller gates test suite on AI completion |
| Sequential job pipeline | Simpler to reason about, easier to observe |
| kagent for failure analysis | AI-powered root cause — surfaces the actual error, not just "pod crashed" |

---

## Demo Scenario (15 minutes)

### Prerequisites

```bash
kubectl get pods -n preview-operator-system
# preview-operator-xxx   1/1   Running

kubectl get crd | grep preview
# previews.platform.company.io
```

### Step 1 — Open a PR and show the workflow trigger (2 min)

Show `.github/workflows/preview.yaml` — how it builds the image with Kaniko and applies the CR:

```yaml
- name: Create preview environment
  run: |
    kubectl apply -f - <<EOF
    apiVersion: platform.company.io/v1alpha1
    kind: Preview
    metadata:
      name: pr-${{ github.event.number }}
    spec:
      branch: ${{ github.head_ref }}
      prNumber: ${{ github.event.number }}
      image: ghcr.io/${{ github.repository }}:${{ github.sha }}
      services:
        - name: backend
          image: ghcr.io/${{ github.repository }}:${{ github.sha }}
          port: 8080
          pathPrefix: /api
        - name: frontend
          image: ghcr.io/${{ github.repository }}:${{ github.sha }}
          port: 3000
          pathPrefix: /
      database:
        enabled: true
        migration:
          enabled: true
          command: ["python", "-m", "alembic", "upgrade", "head"]
      aiEnrichment:
        enabled: true
        model: gpt-4o-mini
      testSuite:
        enabled: true
    EOF
```

### Step 2 — Watch reconciliation live (3 min)

```bash
kubectl logs -n preview-operator-system deployment/preview-operator -f \
  | grep -E 'Reconcil|phase|step|ERROR'

# In parallel:
kubectl get preview pr-42 --watch
```

Show the progression: `Provisioning → Running`.

### Step 3 — Show the preview environment (2 min)

```bash
kubectl get preview pr-42 -o jsonpath='{.status.url}'
# → http://pr-42.preview.ihsenalaya.xyz
```

Open the URL in a browser — frontend catalog + backend /api/products.

### Step 4 — Show the GitHub PR comment (1 min)

The operator posts automatically:
```
## Preview Ready
URL: http://pr-42.preview.ihsenalaya.xyz
PostgreSQL: ready | Migration: Succeeded
Expires at: 2026-05-13T09:00:00Z
```

### Step 5 — AI Test Strategist (2 min)

```bash
kubectl get testplan pr-42 -o jsonpath='{.status}' | jq .
# { "phase": "Succeeded", "selectedTests": ["smoke", "regression"] }
```

The TestPlan CRD shows which tests were selected based on the PR diff — if only frontend files changed, E2E is skipped.

### Step 6 — AI enrichment (2 min)

```bash
kubectl get preview pr-42 -o jsonpath='{.status.aiEnrichment}' | jq .
# { "phase": "Succeeded", "seedStatus": "Succeeded", "testsStatus": "Succeeded" }

kubectl get configmap ai-enrichment -n preview-pr-42 -o jsonpath='{.data.seed\.sql}' | head -20
```

### Step 7 — Show the test suite (2 min)

```bash
kubectl get preview pr-42 -o jsonpath='{.status.tests}' | jq .
kubectl logs -n preview-pr-42 job/smoke-tests
kubectl logs -n preview-pr-42 job/regression-tests
```

### Step 8 — Trigger a failure and show kagent diagnostics (1 min)

```bash
kubectl patch preview pr-42 --type=merge -p '{"spec":{"image":"ghcr.io/fake/missing:notexist"}}'
# → phase transitions to Failed
kubectl get preview pr-42 -o jsonpath='{.status.kagent}' | jq .
# → AI-generated root cause analysis
```

### Step 9 — Cleanup (30 sec)

```bash
kubectl delete preview pr-42
# → finalizer runs → namespace preview-pr-42 deleted → GitHub: inactive
```

---

## Measured Metrics

Numbers from AKS cluster (Standard_D4s_v3 nodes), multi-service, DB + AI + tests enabled:

| Phase | Duration |
|---|---|
| Namespace + Quota + NetworkPolicy + Postgres ready | ~20s |
| Migration + Seed | ~30s |
| App Deployment Ready | ~25s |
| AI Test Strategist (TestPlan) | ~15s |
| AI enrichment (schema-dump + GPT-4o-mini + ai-seed + ai-tests) | ~60–90s |
| Smoke + Regression + E2E test suite | ~120–180s |
| **Total: PR opened → PR comment with full results** | **~5–6 min** |

Memory footprint (operator pod): 40–70 MB RSS

---

## Known Limits

1. **PAT not production-ready** — long-lived token, no auto-rotation. See [SECURITY.md](../SECURITY.md) for GitHub App migration path.

2. **AI enrichment is non-deterministic** — same PR diff can produce different seeds. Add `temperature=0` and model pinning for reproducibility.

3. **Checkpoint size capped at ~950 KB** — ConfigMap data limit. PVC-backed checkpoints needed for large datasets.

4. **kagent trigger Job is ephemeral** — if the operator pod restarts before kagent completes, the analysis is lost. A CRD-backed result store would fix this.

5. **TestPlan CRD is eventually consistent** — the controller requeues every 10s to check TestPlan status. A Watch on TestPlan events would be faster.

6. **No horizontal scaling** — leader election is enabled but the reconcile loop is single-threaded per CR. 50+ concurrent open PRs is untested.

7. **NetworkPolicy egress is fully open** — preview pods can reach the internet. Restrict to known CIDRs for environments containing PII.

---

## Failure Modes

| Failure | Symptom | Recovery |
|---|---|---|
| Image pull failure | `phase=Failed`, `diagnostics.reason=DeploymentFailed` | Fix image tag; patch `spec.image` |
| Migration SQL error | `status.database.migration=Failed` | Fix migration; set `resetRequested=true` |
| AI API rate-limited | `status.aiEnrichment.phase=Failed` | Retry: `rerunRequested=true` |
| Postgres OOM | App pod can't connect | Scale up resource tier; `resetRequested=true` |
| CRD out of sync | Infinite reconcile loop in logs | Apply CRD before upgrading operator |
| TestPlan stuck | `testplan.status.phase=Pending` > 2 min | Check kagent pod logs in operator ns |
| kagent trigger Job fails | `status.kagent.phase=Failed` | Verify kagent RBAC and pod SA |

---

## What We Would Do Differently in Production

### Already production-ready
- Controller idempotence (`CreateOrUpdate` everywhere)
- Status conditions follow Kubernetes API conventions
- GitHub state deduplication (`githubAlreadyNotified`)
- ResourceQuota per namespace (noisy-neighbor protection)
- TTL auto-expiry (cluster hygiene)
- NetworkPolicy namespace isolation
- Pod Security Standards labels (baseline enforce + restricted warn)

### Would add or change
1. **GitHub App instead of PAT** — 1-hour install tokens, scoped, auto-rotated
2. **Image digest pinning** — require `@sha256:` in `spec.image`, enforced by admission webhook
3. **Cosign signature verification** — verify image signature before scheduling
4. **Egress restriction** — scope to postgres CIDR, AI API, GitHub, registry only
5. **OPA/Gatekeeper policies** — prevent preview pods from creating `ClusterRoleBinding`
6. **Prometheus metrics** — `preview_reconcile_duration_seconds`, `preview_environment_count{phase}`, `preview_ai_enrichment_duration_seconds`
7. **Structured audit log** — emit audit events for all CR lifecycle transitions
8. **PVC-backed checkpoints** — remove the 950 KB ConfigMap size limit
9. **TestPlan Watch** — replace polling with a Watch on TestPlan events for faster test feedback
10. **kagent result store** — persist kagent analysis in a CRD for durability across operator restarts

---

## What Is Demo-Only vs Production-Ready

| Component | Status | Notes |
|---|---|---|
| Core reconcile loop | Production-ready | Idempotent, condition-based, TTL-aware |
| Ephemeral PostgreSQL | Production-ready | Per-environment credentials, secret isolation |
| DB migrations + seeds | Production-ready | Sequential job ordering, status tracking |
| DB checkpoints | Demo-only | ConfigMap size limit; needs PVC for production |
| Multi-service mode | Production-ready | Stable, tested with frontend + backend |
| NetworkPolicy isolation | Production-ready | Baseline PSS + ingress-nginx allowlist |
| GitHub Deployment status | Production-ready | Idempotent, deduplication logic |
| GitHub PR comments | Production-ready | In-place updates, no duplicate comments |
| GitHub PAT | Demo-only | Replace with GitHub App for production |
| AI Test Strategist | Production-ready | TestPlan CRD, diff-driven selection |
| AI enrichment | Demo-ready | Non-deterministic; add temperature=0 + validation |
| Smoke tests | Production-ready | Embedded in operator |
| Regression tests | Production-ready | Requires `tests/regression.py` in app image |
| E2E (Playwright) | Production-ready | Requires `tests/e2e.py` in app image |
| kagent failure analysis | Demo-ready | No persistent result store |
| Metrics | Not implemented | Prometheus scrape endpoint missing |
| Image signing | Not implemented | No Cosign verification |
