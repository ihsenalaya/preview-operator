# Agent Contract — What the test-strategist agent receives and must produce

## Context

The agent is given a `TestPlan` CR in `status.phase=Pending`. It must:
1. Read the referenced `Preview` and its `spec.changeContext`.
2. Read recent `ReconcileEvent` resources (last 50 matching file patterns).
3. Produce a test plan and set `status.phase=Ready`.

The controller then validates the plan and either accepts it or falls back to FullSuite.

---

## Input resources

### TestPlan (the stub created by the controller)

```
spec.previewRef.name    → name of the Preview CR to read
spec.commitSHA          → the head commit this plan is for
spec.correlationID      → trace ID; echo it back in your output
```

### Preview (read via spec.previewRef)

```
spec.changeContext.changedFiles[]
  .path    → file path relative to repo root
  .type    → database-migration | api-contract | backend | frontend | docs | other

spec.changeContext.diffPatch
  Raw unified diff (git diff base...head), max 64 KiB.
  Read this to understand WHAT changed, not just which files.
  Examples of things only visible in the patch:
  - A new route added to app.py (even if openapi.yaml not updated)
  - A column removed from a migration file
  - A function renamed that breaks callers in other files

spec.changeContext.detectedImpacts
  .database           → bool
  .apiContract        → bool
  .backend            → bool
  .frontend           → bool
  .requiresSeedData   → bool
```

### ReconcileEvents (read by listing with label platform.company.io/preview-name)

Useful fields:
```
spec.type          → Provisioned | TestStarted | TestFinished | Error | Ready
spec.testSuite     → smoke | contract | regression | migration | e2e | load
spec.outcome       → Succeeded | Failed | Skipped
spec.filePatterns  → []string — denormalized changed files from that PR
spec.occurredAt    → timestamp
```

Query pattern: list ReconcileEvents with `type=TestFinished`, correlate
`spec.filePatterns` with current diff patterns to identify which suites
historically fail when similar files change.

---

## Output — fields to fill in the TestPlan spec

```yaml
spec:
  generatedBy: Agent        # required
  agentName: test-strategist-agent
  agentVersion: "1.0"
  confidence: <0-100>       # required; honest self-assessment
  mustRun:                  # at least one entry required
    - suite: <enum>
      name: "*"
      reason: <string>
  shouldRun: []             # recommended but not mandatory
  canSkip: []               # safe to skip; the controller respects this
  rationale: |              # required; quoted verbatim in PR comment
    <one paragraph>
  estimatedDurationSeconds: <int>
  generatedAt: <RFC3339>
  expiresAt: <RFC3339>      # typically now + 2h
  commitSHA: <echo from input spec.commitSHA>
  correlationID: <echo from input spec.correlationID>
```

After writing spec fields:
```
status.phase = Ready
```

---

## TestSelector enum values

| suite | When to use |
|---|---|
| smoke | always; basic liveness |
| contract | when api-contract files changed OR always if `detectedImpacts.apiContract=true` |
| regression | when backend files changed |
| migration | when database-migration files changed (MANDATORY per hard rules) |
| e2e | when frontend changed or full user flows might be affected |
| load | future; not currently used |

---

## Validation rules (enforced by controller)

The controller rejects plans that violate these rules:

1. `mustRun ∩ canSkip` must be empty. If any test appears in both lists, the plan is Rejected.
2. `confidence < threshold` (default 70) → plan is Rejected; controller falls back to FullSuite.

Write honest plans. A rejected plan with a good `rationale` is useful for debugging.
A bluffed high-confidence plan that is accepted and then wrong is dangerous.

---

## Hard rules (from system prompt — always enforced)

- Migration files changed → `migration` suite in `mustRun`, never in `canSkip`.
- `api/openapi.yaml` changed → `contract` suite in `mustRun`, never in `canSkip`.
- Only docs changed → `confidence: 95`, `mustRun: [smoke]`, everything else in `canSkip`.
- `smoke` suite is always in `mustRun` unless confidence is ≥ 95 and rationale explains the exception.
