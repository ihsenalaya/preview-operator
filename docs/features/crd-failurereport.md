# FailureReport CRD — Durable Failure Evidence & Diagnosis

The **FailureReport** Custom Resource is a W3C PROV-aligned evidence bundle that captures why a Preview failed. It includes collected evidence items (logs, events, traces) and one or more probable cause diagnoses grounded in that evidence.

## Scope

🌍 **Cluster-scoped** — FailureReport CRs exist at the cluster level, enabling cross-namespace failure analysis and pattern detection across all Previews.

---

## What it's for

- Capture and persist failure evidence (pod logs, events, test results, trace spans) for post-mortem analysis
- Provide grounded diagnoses: every probable cause is backed by specific evidence items
- Enable reproducible debugging: the FailureReport is immutable and self-contained
- Support research: each evidence item has a stable, deterministic ID for citation and correlation
- Integrate with failure analysis agents: agents query FailureReports to understand patterns

---

## What it does

When a Preview reaches `phase: Failed`:

1. **Controller creates FailureReport** in the Preview's namespace
2. **Collector goroutines gather evidence**:
   - Pod logs (stderr/stdout from all containers)
   - Kubernetes events (Warnings from the namespace)
   - Job completion status and exit codes
   - Test results (from TestRun, if available)
   - Trace spans (if OTel is enabled)
   - Metrics (if Prometheus is available)
3. **Each evidence item gets a stable ID** derived from its logical identity
4. **Failure-analyst agent (optional) reads the FailureReport** and produces diagnoses
5. **Diagnoses reference evidence by ID** (grounding constraint: every diagnosis must cite specific evidence)
6. **FailureReport is appended to** — never mutated — new diagnoses are added, previous ones remain

---

## API Overview

```yaml
apiVersion: platform.company.io/v1alpha1
kind: FailureReport
metadata:
  name: pr-42-failure-abc123
  namespace: preview-pr-42
spec:
  # Reference to the failed Preview
  previewRef:
    kind: Preview
    name: pr-42
    namespace: default

  # When the failure was detected
  detectedAt: "2026-06-01T10:15:00Z"

  # Phase before failure occurred
  failedPhase: Running

  # Collected evidence items (append-only)
  evidence:
    - id: job-smoke-tests-exit-code
      type: JobLog
      source: preview-operator
      resource: job/smoke-tests
      message: "Exit code: 1"
      timestamp: "2026-06-01T10:14:55Z"
      relevance: high

    - id: pod-backend-stderr
      type: PodLog
      source: preview-operator
      resource: pod/svc-backend-abc123
      message: |
        Traceback (most recent call last):
          File "app.py", line 42, in get_products
            conn = db.connect()
        DatabaseError: connection refused
      timestamp: "2026-06-01T10:14:50Z"
      relevance: high
      redacted: false

    - id: event-ns-evicted
      type: KubernetesEvent
      source: kubelet
      resource: node/aks-node-1
      message: "Pod evicted due to memory pressure"
      timestamp: "2026-06-01T10:14:30Z"
      relevance: high

  # Probable causes grounded in evidence
  diagnoses:
    - probableCause: "Database connection refused; backend crash during test execution."
      component: backend
      severity: critical
      category: application
      evidenceRefs:
        - pod-backend-stderr
        - job-smoke-tests-exit-code
      suggestedFix: |
        1. Check if PostgreSQL Service is running:
           kubectl get svc postgres -n preview-pr-42
        2. Verify Pod logs for startup errors:
           kubectl logs -n preview-pr-42 -l app=postgres
      debugCommands:
        - "kubectl describe pod -n preview-pr-42 -l app=postgres"
        - "kubectl logs -n preview-pr-42 -l app=postgres --tail=50"

status:
  # Report lifecycle
  phase: Diagnosed  # Collecting | Ready | Diagnosed | Acknowledged
  readyAt: "2026-06-01T10:15:30Z"
  diagnosedAt: "2026-06-01T10:16:00Z"

  # Link to PR comment if published
  publishedInComment: 123456789
```

---

## Spec Fields

### References

| Field | Type | Purpose |
|-------|------|---------|
| `previewRef` | ObjectReference | The Preview that failed |

### Collection Metadata

| Field | Type | Purpose |
|-------|------|---------|
| `detectedAt` | time | When the failure was first detected |
| `failedPhase` | string | Preview phase when failure occurred (Provisioning, Running, etc.) |

### Evidence

| Field | Type | Purpose |
|-------|------|---------|
| `evidence[]` | []FailureEvidenceItem | Collected items: logs, events, metrics, traces |

#### FailureEvidenceItem structure

| Field | Type | Purpose |
|-------|------|---------|
| `id` | string | Stable, deterministic identifier (for citation in diagnoses) |
| `type` | enum | Item classification: `GitDiff` \| `ChangedFile` \| `KubernetesEvent` \| `PodLog` \| `JobLog` \| `TestResult` \| `TraceSpan` \| `Metric` \| `PreviewCondition` \| `ReconcileEvent` |
| `source` | string | What produced the evidence (e.g., `preview-operator`, `kubelet`, `otel-collector`) |
| `resource` | string | Kubernetes object or file path (e.g., `pod/svc-backend-abc123`, `file:app.py`) |
| `message` | string | Evidence content (redacted if sensitive) |
| `timestamp` | time | When the evidence was observed |
| `relevance` | enum | `high` \| `medium` \| `low` (ranked by probable cause) |
| `redacted` | bool | True if secrets were redacted from message |

### Diagnoses

| Field | Type | Purpose |
|-------|------|---------|
| `diagnoses[]` | []FailureDiagnosis | Probable causes grounded in evidence |

#### FailureDiagnosis structure

| Field | Type | Purpose |
|-------|------|---------|
| `probableCause` | string | Human-readable probable root cause |
| `component` | string | Component most likely responsible (backend, database, infrastructure, etc.) |
| `severity` | enum | `critical` \| `warning` \| `info` |
| `category` | enum | Failure family: `database` \| `configuration` \| `infrastructure` \| `application` \| `observability` \| `test-reliability` \| `unknown` |
| `evidenceRefs[]` | []string | IDs of evidence items that ground this diagnosis |
| `suggestedFix` | string | Actionable steps to resolve the failure |
| `debugCommands[]` | []string | kubectl commands for further investigation |

---

## Status Fields

| Field | Type | Meaning |
|-------|------|---------|
| `phase` | string | Report lifecycle: `Collecting` (gathering evidence) → `Ready` (collection complete) → `Diagnosed` (agent produced diagnoses) → `Acknowledged` (human reviewed) |
| `readyAt` | time | When evidence collection completed |
| `diagnosedAt` | time | When agent analysis finished |
| `publishedInComment` | int | GitHub PR comment ID if published (idempotent) |

---

## Evidence Type Catalog

### GitDiff

Raw unified diff from `git diff base...head`. Used by analysis agents to understand what changed semantically.

### ChangedFile

Individual file changed in the PR. Includes path, type classification (backend, frontend, migration, etc.).

### KubernetesEvent

Cluster events (Warnings, Normal events from controller-manager, kubelet, etc.).

| Example | Relevance |
|---------|-----------|
| Pod evicted due to memory pressure | high |
| PVC pending (volume not provisioned) | high |
| Node NotReady | high |
| BackOff restarting failed container | medium |

### PodLog

stderr/stdout captured from a running or completed Pod.

| Example | Relevance |
|---------|-----------|
| `Traceback: DatabaseError: connection refused` | high |
| `Starting app on port 8080` | low |

### JobLog

Captured from a Job's Pod. Exit codes, command output.

| Example | Relevance |
|---------|-----------|
| Job exit code 1 | medium |
| `AssertionError: expected 200, got 500` | high |

### TestResult

From TestRun: which tests passed/failed, durations, failure messages.

### TraceSpan

From Jaeger/OTel: spans that timed out, had exceptions, or were in the critical path.

| Example | Relevance |
|---------|-----------|
| Span `db.query` times out at 30s | high |
| Span `http.request` ends with exception | high |

### Metric

From Prometheus: high resource usage, missing metrics, etc.

| Example | Relevance |
|---------|-----------|
| Memory usage 512Mi (at pod limit) | high |
| HTTP error rate 100% | high |

### PreviewCondition

From Preview CR status. Phase transitions, condition messages.

### ReconcileEvent

From ReconcileEvent CRs: controller lifecycle events (Provisioned, TestStarted, Error, etc.).

---

## Lifecycle

```
Preview fails (any phase)
           │
           ▼
Controller detects failure, creates FailureReport with phase=Collecting
           │
           ├─ Goroutine 1: gather Pod logs
           ├─ Goroutine 2: gather K8s events
           ├─ Goroutine 3: gather test results
           ├─ Goroutine 4: fetch traces (if OTel enabled)
           └─ Goroutine 5: query metrics (if Prometheus available)
           │
           ▼
All goroutines complete → phase=Ready, readyAt=now()
           │
           ▼
[Optional] failure-analyst-agent reads FailureReport
           ├─ Analyzes evidence for probable causes
           └─ Appends diagnoses to spec.diagnoses[]
                        │
                        ▼
           phase=Diagnosed, diagnosedAt=now()
           │
           ▼
[Optional] Controller publishes FailureReport summary to PR comment
           │
           ├─ Posts structured diagnosis (risk level, causes, suggested fix)
           └─ publishedInComment=<comment-id>
                        │
                        ▼
           phase=Acknowledged (once human reviews)
```

---

## Kubernetes Operations

### Check if a Preview has a FailureReport

```bash
kubectl get failurereport -n preview-pr-42
```

### View evidence collection progress

```bash
# Full FailureReport
kubectl get failurereport pr-42-failure-abc123 -n preview-pr-42 -o yaml

# Just evidence items
kubectl get failurereport pr-42-failure-abc123 -n preview-pr-42 \
  -o jsonpath='{.spec.evidence}' | jq '.[] | {id, type, relevance}'

# Just diagnoses
kubectl get failurereport pr-42-failure-abc123 -n preview-pr-42 \
  -o jsonpath='{.spec.diagnoses}' | jq '.[] | {probableCause, category, severity}'
```

### Extract high-relevance evidence

```bash
kubectl get failurereport pr-42-failure-abc123 -n preview-pr-42 \
  -o jsonpath='{.spec.evidence[?(@.relevance=="high")]}' | jq '.[] | {id, type, message}'
```

### Copy FailureReport for offline analysis

```bash
kubectl get failurereport pr-42-failure-abc123 -n preview-pr-42 -o yaml > failure-report.yaml
```

---

## Relationships

```
FailureReport (1) ◄─── Preview (1)
  ├─ references → Preview (previewRef)
  │
  ├─ references → Pod (in evidence items)
  ├─ references → Job (in evidence items)
  ├─ references → Event (in evidence items)
  ├─ references → Span (Jaeger)
  │
  └─ referenced by ← failure-analyst-agent (optional)
       (reads evidence, produces diagnoses)
```

- **One FailureReport** per failed Preview
- **Multiple evidence items** accumulated during collection
- **Multiple diagnoses** appended as agents analyze

---

## Grounding Constraint (Research)

Every diagnosis must reference existing evidence:

```python
def validate_grounding(report):
    evidence_ids = {item.id for item in report.spec.evidence}
    for diagnosis in report.spec.diagnoses:
        for ref_id in diagnosis.evidenceRefs:
            assert ref_id in evidence_ids, f"Diagnosis references unknown evidence: {ref_id}"
```

This constraint is validated by the controller and enforced in the webhook (when enabled).

---

## Analysis by Agents

The failure-analyst-agent uses FailureReport as input:

1. **Reads** spec.evidence (all collected items)
2. **Infers** probable causes from evidence patterns
3. **Assigns** confidence scores
4. **Produces** diagnoses with suggested fixes
5. **Appends** to spec.diagnoses[] (controller permissions required)

---

## Troubleshooting

### Evidence collection seems incomplete

```bash
# Check controller logs
kubectl logs -n preview-operator-system deployment/preview-operator \
  --tail=50 | grep -i "failurereport\|evidence"

# Check if report is still in Collecting phase
kubectl get failurereport -n preview-pr-42 -o jsonpath='{.status.phase}'
```

### Diagnoses missing

The failure-analyst-agent may be disabled or not running:

```bash
# Check if agent is deployed
kubectl get agent failure-analyst-agent -n kagent-system

# Check agent logs
kubectl logs -n kagent-system -l app=failure-analyst-agent --tail=50
```

### Evidence redacted but you need full content

Secrets are redacted automatically. To view raw evidence:

```bash
# Check which items are redacted
kubectl get failurereport -n preview-pr-42 \
  -o jsonpath='{.spec.evidence[?(@.redacted==true)]}' | jq '.[] | {id, type, redacted}'

# Read raw logs directly from Pod (if still running)
kubectl logs -n preview-pr-42 pod/svc-backend-abc123
```

---

## Reference: W3C PROV Alignment

The FailureReport schema aligns with W3C PROV-O (Provenance Ontology):

- **FailureEvidenceItem** → PROV Entity (something that existed/was observed)
- **FailureDiagnosis** → PROV Activity + Agent assertion (something that was inferred)
- **evidenceRefs** → PROV wasDerivedFrom (derivation relationship)

This alignment enables:
- Standard citation in academic papers
- Machine-readable provenance chains
- Integration with PROV tools and visualizers
