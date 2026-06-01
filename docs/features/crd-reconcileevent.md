# ReconcileEvent CRD — Controller Lifecycle Audit Log

The **ReconcileEvent** Custom Resource is an append-only audit log written by the controller each time something material happens to a Preview. Events record state transitions, test starts/completions, and errors, forming a deterministic timeline that agents can query for historical signal.

## Scope

🔒 **Namespaced** — ReconcileEvent CRs are created in the same namespace as the Preview they belong to (`preview-pr-<N>`). Events accumulate and are cleaned up after a TTL (default 7 days).

---

## What it's for

- Create an immutable, ordered audit trail of what happened to a Preview
- Provide historical signal to agents (e.g., test-strategist-agent reads recent events to detect patterns)
- Enable deterministic replay: given events + CR state, the sequence of controller actions is recoverable
- Support analysis: queries like "how many times has this Preview had database failures?" are answerable

---

## What it does

Whenever the controller transitions a Preview to a new phase or finishes a significant operation:

1. **Controller creates ReconcileEvent** in the Preview's namespace
2. **Event records**: event type (Provisioned, TestStarted, TestFinished, Error, Ready), phase, timestamp, outcome
3. **Denormalized fields** (filePatterns, testSuite) allow agents to query without joining back to the Preview
4. **CorrelationID** ties events to the reconcile cycle that produced them
5. **Events accumulate** — never deleted, forming a queryable history
6. **Garbage collection** removes old events after a TTL (default 7 days)

---

## API Overview

```yaml
apiVersion: platform.company.io/v1alpha1
kind: ReconcileEvent
metadata:
  name: pr-42-event-001
  namespace: preview-pr-42
spec:
  # Reference to the Preview this event belongs to
  previewRef:
    kind: Preview
    name: pr-42
    namespace: default

  # Event classification
  type: Provisioned  # Provisioned | TestStarted | TestFinished | Error | Ready

  # Test suite (when type is TestStarted or TestFinished)
  testSuite: smoke

  # Brief result string
  outcome: "Succeeded"

  # Human-readable description
  message: "Smoke tests completed with 2 passed, 0 failed."

  # Denormalized file patterns from the Preview's changeContext
  filePatterns:
    - "app.py"
    - "tests/regression.py"

  # When the event occurred
  occurredAt: "2026-06-01T10:14:55Z"

  # Ties events to the reconcile cycle
  correlationID: "reconcile-12345-67890"
```

---

## Event Types

### Provisioned

The preview namespace, database, and services are ready.

```yaml
type: Provisioned
message: "Namespace preview-pr-42 created with isolation policy. PostgreSQL running. Services deployed."
outcome: "Succeeded"
```

### TestStarted

A test suite is beginning execution.

```yaml
type: TestStarted
testSuite: regression
message: "Regression test suite starting."
outcome: "Running"
occurredAt: "2026-06-01T10:15:00Z"
```

### TestFinished

A test suite completed.

```yaml
type: TestFinished
testSuite: regression
outcome: "Succeeded"  # or "Failed", "PartiallyFailed"
message: "Regression tests finished: 9 passed, 0 failed. Duration: 45s."
occurredAt: "2026-06-01T10:15:45Z"
```

### Error

Something went wrong during reconciliation.

```yaml
type: Error
message: "Database migration job failed: exit code 1. Check logs: kubectl logs -n preview-pr-42 job/postgres-migrate"
outcome: "DatabaseMigrationFailed"  # or other specific error
occurredAt: "2026-06-01T10:10:00Z"
```

### Ready

Preview is fully provisioned and ready for use (all services, database, tests).

```yaml
type: Ready
message: "Preview is Running and ready at http://pr-42.preview.example.com"
outcome: "Success"
occurredAt: "2026-06-01T10:20:00Z"
```

---

## Spec Fields

| Field | Type | Purpose |
|-------|------|---------|
| `previewRef` | ObjectReference | The Preview this event belongs to |
| `type` | enum | Event classification: `Provisioned` \| `TestStarted` \| `TestFinished` \| `Error` \| `Ready` |
| `testSuite` | string | Test category (when type is TestStarted/TestFinished) |
| `outcome` | string | Brief result (e.g., "Succeeded", "Failed:exit-1") |
| `message` | string | Human-readable description |
| `filePatterns[]` | []string | Denormalized changed files from Preview.spec.changeContext |
| `occurredAt` | time | When the event happened |
| `correlationID` | string | Ties events to the reconcile cycle |

---

## Lifecycle

```
Preview created → Reconciliation starts (correlationID=abc123)
           │
           ├─ Namespace provisioned
           │  └─ ReconcileEvent {type: Provisioned, outcome: Succeeded}
           │
           ├─ PostgreSQL provisioned
           │  (no event, unless type is Provisioned with message)
           │
           ├─ Services deployed
           │  (event is part of Provisioned)
           │
           ├─ Database migration starts
           │  (not an event until TestStarted, or until Error)
           │
           ├─ Smoke tests start
           │  └─ ReconcileEvent {type: TestStarted, testSuite: smoke}
           │
           ├─ Smoke tests finish
           │  └─ ReconcileEvent {type: TestFinished, testSuite: smoke, outcome: Succeeded}
           │
           ├─ Regression tests start
           │  └─ ReconcileEvent {type: TestStarted, testSuite: regression}
           │
           ├─ Regression tests finish
           │  └─ ReconcileEvent {type: TestFinished, testSuite: regression, outcome: Failed}
           │
           └─ Reconciliation error
              └─ ReconcileEvent {type: Error, outcome: RegressionTestsFailed}
```

---

## Kubernetes Operations

### List all events for a Preview

```bash
kubectl get reconcileevent -n preview-pr-42 --sort-by=.spec.occurredAt
```

### View events in chronological order

```bash
kubectl get reconcileevent -n preview-pr-42 \
  -o jsonpath='{.items[*]}' | jq '.[] | {type: .spec.type, testSuite: .spec.testSuite, outcome: .spec.outcome, message: .spec.message}'
```

### Find error events

```bash
kubectl get reconcileevent -n preview-pr-42 \
  -o jsonpath='{.items[?(@.spec.type=="Error")]}' | jq '.[] | {outcome: .spec.outcome, message: .spec.message, occurredAt: .spec.occurredAt}'
```

### Find test events for a specific suite

```bash
kubectl get reconcileevent -n preview-pr-42 \
  -o jsonpath='{.items[?(@.spec.testSuite=="regression")]}' | jq '.[] | {type: .spec.type, outcome: .spec.outcome, occurredAt: .spec.occurredAt}'
```

### Query events by correlation ID

```bash
# Find all events for a specific reconcile cycle
CORR_ID="reconcile-12345-67890"
kubectl get reconcileevent -n preview-pr-42 \
  -o jsonpath="{.items[?(@.spec.correlationID==\"$CORR_ID\")]}" | jq '.[] | {type: .spec.type, outcome: .spec.outcome}'
```

### Watch events in real-time

```bash
kubectl get reconcileevent -n preview-pr-42 -w
```

---

## Denormalized Fields (filePatterns)

To avoid querying the Preview CR, ReconcileEvent includes a denormalized copy of changed file patterns. This allows agents to ask "which Previews changed database files?" without joining:

```bash
# Find all Previews that changed migrations
kubectl get reconcileevent -A \
  -o jsonpath='{.items[?(@.spec.filePatterns[*]=="db/migrations/")]}' \
  | jq '.[] | {namespace: .metadata.namespace, filePatterns: .spec.filePatterns}'
```

---

## Relationships

```
ReconcileEvent (N) ◄─── Preview (1)
  ├─ references → Preview (previewRef)
  │
  └─ read by ← test-strategist-agent (optional)
       (queries recent events to detect patterns like flaky tests)
```

- **Multiple ReconcileEvents** per Preview (one per significant state transition)
- **No ownership**: ReconcileEvents are not garbage-collected with the Preview
- **Separate TTL**: Events are cleaned up after 7 days by default

---

## Agent Integration: Test-Strategist Pattern

The test-strategist-agent uses ReconcileEvents to detect test flakiness:

```python
# Pseudo-code: query recent events to detect flaky tests
events = query_reconcile_events(preview_name, limit=10)
test_flakiness = {}
for event in events:
    if event.type == "TestFinished":
        suite = event.testSuite
        if suite not in test_flakiness:
            test_flakiness[suite] = {"passed": 0, "failed": 0}
        if event.outcome == "Succeeded":
            test_flakiness[suite]["passed"] += 1
        else:
            test_flakiness[suite]["failed"] += 1

# Use flakiness to adjust confidence:
for suite, results in test_flakiness.items():
    fail_rate = results["failed"] / (results["passed"] + results["failed"])
    if fail_rate > 0.3:  # >30% failure rate
        confidence -= 10  # Reduce confidence due to flakiness
```

This pattern enables the agent to skip flaky tests automatically.

---

## Garbage Collection

ReconcileEvents are cleaned up by the controller after a TTL (default 7 days):

```bash
# Configure TTL in the operator config
kubectl patch configmap -n preview-operator-system preview-operator-config \
  --type=merge -p '{"data":{"reconcileEventTTL":"168h"}}'
```

---

## Admission Webhooks

No webhooks for ReconcileEvent. The controller is the exclusive writer.

---

## Troubleshooting

### No events appearing

```bash
# Check if controller is running
kubectl get deployment -n preview-operator-system preview-operator

# Check controller logs for event creation errors
kubectl logs -n preview-operator-system deployment/preview-operator \
  --tail=50 | grep -i reconcileevent
```

### Missing events for a specific phase

```bash
# Check if the controller reached that phase
kubectl get preview pr-42 -o jsonpath='{.status.phase}'

# If phase changed but no event, check logs for errors
kubectl logs -n preview-operator-system deployment/preview-operator \
  --tail=100 | grep -A 5 -B 5 "pr-42"
```

### Too many old events (cleanup not working)

```bash
# Manually delete old events
kubectl delete reconcileevent -n preview-pr-42 \
  --field-selector metadata.creationTimestamp\<2026-05-25T00:00:00Z
```

---

## Reference: Audit Trail Example

A complete event timeline for a successful preview:

```
time     type          testSuite  outcome          message
────────────────────────────────────────────────────────────────────────
10:05:00 Provisioned   -          Succeeded        "Namespace created, PostgreSQL running"
10:05:15 TestStarted   smoke      Running          "Smoke tests starting"
10:05:20 TestFinished  smoke      Succeeded        "Smoke: 2 passed, 0 failed (5s)"
10:05:25 TestStarted   regression Running          "Regression tests starting"
10:06:10 TestFinished  regression Succeeded        "Regression: 9 passed, 0 failed (45s)"
10:06:15 TestStarted   e2e        Running          "E2E tests starting"
10:07:05 TestFinished  e2e        Succeeded        "E2E: 6 passed, 0 failed (50s)"
10:07:10 Ready         -          Success          "Preview is Running at http://pr-42.preview.example.com"
```

This timeline is machine-queryable and forms the basis for agent pattern detection.
