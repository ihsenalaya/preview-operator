# TestRun CRD — Test Execution Tracking

The **TestRun** Custom Resource tracks the execution of a test suite within a Preview. It is created after a TestPlan is accepted, and records the results of each test Job as they complete.

## Scope

🔒 **Namespaced** — TestRun CRs are created in the same namespace as the Preview they belong to (`preview-pr-<N>`). Multiple TestRuns accumulate in the namespace for historical analysis.

---

## What it's for

- Persist test execution history and results in etcd for audit and analysis
- Provide a durable record of which tests ran, their outcomes, and timing
- Enable per-test result tracking (passed, failed, skipped)
- Support historical queries: which tests have been flaky, which always fail, etc.

---

## What it does

When a TestPlan is accepted:

1. **Controller creates TestRun** in the Preview's namespace
2. **Controller launches test Jobs** (smoke, regression, E2E, etc.)
3. **As each Job completes**, controller appends a TestRunResult to `status.results[]`
4. **TestRun phase progresses**: `Pending` → `Running` → `Succeeded` / `Failed` / `PartiallyFailed`
5. **Results are immutable**: once a test result is appended, it never changes (audit trail)

---

## API Overview

```yaml
apiVersion: platform.company.io/v1alpha1
kind: TestRun
metadata:
  name: pr-42-run-abc123
  namespace: preview-pr-42
spec:
  # Reference to the Preview this run belongs to
  previewRef:
    kind: Preview
    name: pr-42
    namespace: default

  # Reference to the TestPlan that was accepted
  testPlanRef:
    kind: TestPlan
    name: pr-42-abc123

  # Final set of tests chosen after policy application
  selectedTests:
    - suite: smoke
      name: "*"
    - suite: regression
      name: "*"
    - suite: e2e
      name: "*"

  # When the controller began launching test jobs
  startedAt: "2026-06-01T10:05:00Z"

  # Propagated from the accepted TestPlan
  correlationID: "reconcile-12345-67890"

status:
  # Current phase of the test run
  phase: Running  # Pending | Running | Succeeded | Failed | PartiallyFailed

  # Per-test outcomes appended as jobs complete
  results:
    - suite: smoke
      name: smoke
      status: Succeeded
      durationSeconds: 5
      logsURL: "gs://bucket/preview-pr-42/smoke-tests.log"

    - suite: regression
      name: regression
      status: Running
      durationSeconds: 0

    - suite: e2e
      name: e2e
      status: Pending

  # When all test jobs completed
  finishedAt: null
```

---

## Spec Fields

| Field | Type | Purpose |
|-------|------|---------|
| `previewRef` | ObjectReference | The Preview CR that initiated this run |
| `testPlanRef` | ObjectReference | The TestPlan that was accepted (determines which tests run) |
| `selectedTests[]` | []TestSelector | The final set of test suites + names (after applying mustRun, shouldRun) |
| `startedAt` | time | When the controller began launching test jobs |
| `correlationID` | string | Propagated from TestPlan for correlation across related CRs |

---

## Status Fields

| Field | Type | Meaning |
|-------|------|---------|
| `phase` | string | Lifecycle: `Pending` → `Running` → `Succeeded` / `Failed` / `PartiallyFailed` |
| `results[]` | []TestRunResult | Appended as each test Job completes; immutable |
| `finishedAt` | time | When the last test Job completed |

### TestRunResult structure

| Field | Type | Meaning |
|-------|------|---------|
| `suite` | string | Test category (e.g., `smoke`, `regression`, `e2e`) |
| `name` | string | Test identifier within the suite |
| `status` | string | Outcome: `Pending` \| `Running` \| `Succeeded` \| `Failed` \| `Skipped` |
| `durationSeconds` | int | How long the test took |
| `logsURL` | string | Link to raw test logs (e.g., GCS, S3, or in-cluster) |
| `junitURL` | string | Link to JUnit XML report (optional) |

---

## Lifecycle

```
TestPlan accepted (phase=Ready)
           │
           ▼
Controller creates TestRun with phase=Pending
           │
           ▼
Controller launches test Jobs (smoke, regression, E2E)
           │
           ├─ Phase→Running
           │
           ├─ Smoke Job completes
           │   ├─ Append TestRunResult {suite: smoke, status: Succeeded, …}
           │   └─ RequeueAfter=10s to check next Job
           │
           ├─ Regression Job completes
           │   ├─ Append TestRunResult {suite: regression, status: Failed, …}
           │   └─ Continue (even though one failed)
           │
           ├─ E2E Job completes
           │   ├─ Append TestRunResult {suite: e2e, status: Succeeded, …}
           │   └─ All jobs done
           │
           ▼
Phase → Succeeded (all passed) / Failed (any failed) / PartiallyFailed (some passed, some failed)
finishedAt = now()
```

---

## Kubernetes Operations

### Create a TestRun manually (rarely needed)

```bash
kubectl apply -f - <<EOF
apiVersion: platform.company.io/v1alpha1
kind: TestRun
metadata:
  name: pr-42-manual-run
  namespace: preview-pr-42
spec:
  previewRef:
    kind: Preview
    name: pr-42
  selectedTests:
    - suite: smoke
      name: "*"
    - suite: regression
      name: "*"
  startedAt: "2026-06-01T10:05:00Z"
EOF
```

### Check TestRun status

```bash
# List TestRuns in the namespace
kubectl get testrun -n preview-pr-42

# View full status
kubectl get testrun pr-42-run-abc123 -n preview-pr-42 -o jsonpath='{.status}' | jq .

# Watch as it progresses
kubectl get testrun -n preview-pr-42 -w

# See results
kubectl get testrun pr-42-run-abc123 -n preview-pr-42 \
  -o jsonpath='{.status.results}' | jq .
```

### Extract test results

```bash
# Passed tests
kubectl get testrun pr-42-run-abc123 -n preview-pr-42 \
  -o jsonpath='{.status.results[?(@.status=="Succeeded")]}' | jq '.[] | .suite + ": " + .name'

# Failed tests
kubectl get testrun pr-42-run-abc123 -n preview-pr-42 \
  -o jsonpath='{.status.results[?(@.status=="Failed")]}' | jq '.[] | .suite + ": " + .name'

# Test durations
kubectl get testrun pr-42-run-abc123 -n preview-pr-42 \
  -o jsonpath='{.status.results[*]}' | jq '.[] | {suite, durationSeconds}'
```

---

## Ownership & Relationships

### Ownership Chain

```
Preview (cluster-scoped)
  │
  └─ OWNS ──────→ TestRun (namespaced)
                  ├─ REFERENCES ──→ Preview (previewRef)
                  ├─ REFERENCES ──→ TestPlan (testPlanRef, may be deleted)
                  └─ WRITTEN BY ──→ preview-operator (controller)
```

**Preview OWNS TestRun:**
- TestRun lives in preview namespace (preview-pr-<N>)
- When Preview is deleted → TestRun automatically deleted
- Multiple TestRuns accumulate in namespace (historical record)
- All cleaned up together when Preview finalizer runs

**TestRun references TestPlan:**
- TestRun.spec.testPlanRef points to the accepted TestPlan
- Both owned by same Preview → deleted together
- TestRun is immutable (read-only for analysis after completion)

### Complete Relationship Map

```
Preview (1)
  │
  ├─ OWNS ──→ TestPlan (decision)
  │
  ├─ OWNS ──→ TestRun (results)  ◄── REFERENCES TestPlan
  │           ├─ Contains: test results (immutable)
  │           ├─ Appended by: preview-operator
  │           └─ Read by: failure-analyst-agent (for diagnostics)
  │
  └─ OWNS ──→ ReconcileEvent (audit log)
              └─ Appended by: preview-operator
```

---

## Historical Analysis

TestRun is append-only, so you can query historical patterns:

### Find all failed tests across all runs

```bash
kubectl get testrun -n preview-pr-42 -o json | \
  jq '.items[].status.results[] | select(.status == "Failed") | .suite + ": " + .name' | sort | uniq -c
```

### Find slowest test

```bash
kubectl get testrun -n preview-pr-42 -o json | \
  jq '.items[].status.results | max_by(.durationSeconds) | .suite + " took " + (.durationSeconds|tostring) + "s"'
```

### Aggregate pass rate by suite

```bash
kubectl get testrun -n preview-pr-42 -o json | jq '
  .items[].status.results
  | group_by(.suite)
  | map({
      suite: .[0].suite,
      total: length,
      passed: map(select(.status == "Succeeded")) | length,
      passRate: (map(select(.status == "Succeeded")) | length / length * 100)
    })
  | .[]
'
```

---

## Admission Webhooks

No webhooks for TestRun. The controller is the source of truth for results.

---

## Troubleshooting

### TestRun stuck in Running

```bash
# Check if test Jobs are still running
kubectl get jobs -n preview-pr-42 | grep -E "(smoke|regression|e2e)"

# Check Job status
kubectl describe job smoke-tests -n preview-pr-42

# Check Job logs if stalled
kubectl logs -n preview-pr-42 job/smoke-tests --tail=100
```

### No results appended to status

```bash
# Verify controller has permission to patch TestRun
kubectl auth can-i patch testrun --as=system:serviceaccount:preview-operator-system:preview-operator

# Check controller logs
kubectl logs -n preview-operator-system deployment/preview-operator --tail=50 | grep -i testrun
```

### Result details missing (logsURL, junitURL)

The controller must configure where test artifacts are stored. Check the operator configuration:

```bash
kubectl get configmap -n preview-operator-system preview-operator-config -o yaml
```

Look for artifact storage settings (e.g., GCS bucket, S3 prefix).
