# Failure Evidence Model

This chapter defines the failure evidence model in detail and maps it onto the
**W3C PROV** model (`[w3c_prov_overview]`, `[w3c_prov_dm]`). The model is the
conceptual specification behind the Go types added in Phase 2 (`FailureEvidenceBundle`,
`FailureEvidenceItem`, `FailureDiagnosis`) and the `FailureReport` artifact (Phase 3).

W3C PROV has three core types — **Entity**, **Activity**, **Agent** — and a set of
relations between them. The failure evidence model is expressed entirely in those terms
so that a `FailureReport` *is* a provenance record, not merely a log dump.

---

## 1. Entities

An *Entity* (PROV) is a thing — physical or digital — whose provenance is recorded. In
this model, entities are the artifacts and observable facts of a preview failure.

| Entity | Description | Source in the operator |
|--------|-------------|------------------------|
| `PullRequest` | The pull request under preview | `spec.changeContext.diffRef` (PR number, repository) |
| `Commit` | A commit in the PR (base / head) | `diffRef.baseSHA`, `diffRef.headSHA` |
| `ChangedFile` | One file changed by the PR, with its classified type | `spec.changeContext.changedFiles[]` |
| `Preview` | The `Preview` custom resource for this environment | the `Preview` CR |
| `Namespace` | The ephemeral namespace | `status.namespaceName` |
| `Deployment` | An application deployment in the namespace | Kubernetes API |
| `Pod` | A pod (application, database, test, job) | Kubernetes API |
| `Job` | A one-shot job (migration, seed, test, AI task) | Kubernetes API |
| `Service` | A service exposing a workload | Kubernetes API |
| `TestSuite` | A test suite (smoke / contract / regression / e2e / migration) | `status.tests`, `TestPlan` |
| `TestCase` | An individual test case | `TestRun` results |
| `LogEntry` | A significant log line from a pod or job | `DiagnosticsStatus.significantLogs`, `podLogs` |
| `KubernetesEvent` | A Kubernetes event (typically a warning) | `DiagnosticsStatus.lastEvents` |
| `TraceSpan` | A distributed-trace span (conditional on OTel) | external trace backend |
| `MetricSample` | A metric measurement (conditional on OTel) | external metrics backend |
| `Diagnosis` | A probable cause produced by the diagnostic step | `DiagnosticsStatus`, `status.kagent.analysis` |
| `Recommendation` | A suggested remediation action | `DiagnosticsStatus.recommendations` |

`TraceSpan` and `MetricSample` are **conditional entities**: they exist in the model
only when app-level OpenTelemetry instrumentation and a backend are deployed
(`methodology.md` §2). The article must not present them as always-captured.

---

## 2. Activities

An *Activity* (PROV) is something that occurs over time and acts upon or with entities
— it *generates*, *uses*, or *transforms* entities.

| Activity | Description | Where it runs |
|----------|-------------|---------------|
| `Reconciliation` | One pass of the operator's reconcile loop | Preview controller |
| `Deployment` | Creating/updating the application workloads | reconcile (provisioning) |
| `Migration` | Running the database migration job | reconcile (database) |
| `TestExecution` | Running a test suite | reconcile (test pipeline) |
| `EvidenceCollection` | Collecting evidence into the bundle **(new)** | reconcile (failure path) |
| `DiagnosisGeneration` | Producing a probable cause from the bundle | `inferRootCause` / kagent |
| `NamespaceTeardown` | Deleting the namespace and child resources | reconcile (finalizer) |

`EvidenceCollection` is the new activity introduced by this article. It is positioned
on the failure path of `Reconciliation`, before `NamespaceTeardown`.

---

## 3. Agents

An *Agent* (PROV) bears responsibility for an activity or an entity.

| Agent | Description | PROV role |
|-------|-------------|-----------|
| `Developer` | The author of the pull request | responsible for `PullRequest`, `Commit`, `ChangedFile` |
| `CI Pipeline` | GitHub Actions: classifies the diff, creates the `Preview` CR | responsible for `Preview` creation |
| `Preview Operator` | The controller | responsible for `Reconciliation`, `EvidenceCollection`, `NamespaceTeardown` |
| `Kubernetes API Server` | Serves and stores the resources | responsible for resource state |
| `kagent / AI Agent` | The LLM-backed troubleshooter / analyzer | responsible for `DiagnosisGeneration` (AI path) |

---

## 4. Relations

The model uses the standard PROV relations plus three domain-specific relations.
PROV relations are written with their PROV names; domain relations are flagged.

| Relation | Type | Meaning in this model |
|----------|------|-----------------------|
| `wasDerivedFrom` | PROV | An entity was derived from another (e.g. `Diagnosis` ⟵ `LogEntry`) |
| `wasGeneratedBy` | PROV | An entity was produced by an activity (e.g. `LogEntry` ⟵ `TestExecution`) |
| `used` | PROV | An activity consumed an entity (e.g. `DiagnosisGeneration` used the bundle) |
| `wasAssociatedWith` | PROV | An activity was associated with an agent (e.g. `Reconciliation` ⟷ `Preview Operator`) |
| `wasAttributedTo` | PROV | An entity is attributed to an agent (e.g. `ChangedFile` ⟷ `Developer`) |
| `wasInformedBy` | PROV | One activity was informed by another (e.g. `DiagnosisGeneration` ⟵ `EvidenceCollection`) |
| `caused` | domain | A change/condition caused a failure (e.g. `ChangedFile` `caused` `FailedTest`) |
| `observedIn` | domain | Evidence in which a fact was observed (e.g. fault `observedIn` `KubernetesEvent`) |
| `supports` | domain | An evidence item supports a diagnosis claim (the **grounding** relation) |

The `supports` relation is the backbone of RQ4: every `Diagnosis` must be connected by
`supports` to at least one evidence entity.

---

## 5. The provenance chain

Combining the above, a single preview failure produces this provenance record:

```
Developer        --wasAttributedTo--      ChangedFile
ChangedFile      --wasDerivedFrom-->      Commit  --wasDerivedFrom--> PullRequest
CI Pipeline      --wasAssociatedWith--    (Preview creation)
Preview          --wasGeneratedBy-->      (Preview creation)
Reconciliation   --wasAssociatedWith--    Preview Operator
Namespace        --wasGeneratedBy-->      Reconciliation
Deployment/Job/Service --wasGeneratedBy-->Deployment / Migration
TestExecution    --used-->                Deployment
FailedTest       --wasGeneratedBy-->      TestExecution
ChangedFile      --caused-->              FailedTest
LogEntry/Event/TraceSpan --wasGeneratedBy-> Pod / Job / TestExecution
fault            --observedIn-->          LogEntry / KubernetesEvent / TraceSpan
EvidenceCollection --used-->              LogEntry, KubernetesEvent, FailedTest, ChangedFile, ...
FailureEvidenceBundle --wasGeneratedBy--> EvidenceCollection
DiagnosisGeneration --used-->             FailureEvidenceBundle
DiagnosisGeneration --wasInformedBy-->    EvidenceCollection
Diagnosis        --wasGeneratedBy-->      DiagnosisGeneration
Diagnosis        --wasAssociatedWith--    kagent / AI Agent
LogEntry/Event/... --supports-->          Diagnosis
Recommendation   --wasDerivedFrom-->      Diagnosis
NamespaceTeardown --wasInformedBy-->      (FailureReport persisted)
```

This is the textual form of Diagram 3 in `architecture.md`.

---

## 6. Go type mapping (Phase 2 scaffolding)

The model maps to the Go types introduced in Phase 2:

```text
FailureEvidenceBundle   <-->  the provenance record for one failure
  .PreviewName/.Namespace/.PRNumber/.CommitSHA  <-->  Preview, Namespace, PullRequest, Commit entities
  .FailedSuite/.FailedTest                      <-->  TestSuite, TestCase entities
  .ChangeContext                                <-->  ChangedFile entities + caused relations
  .Kubernetes                                   <-->  Deployment/Pod/Job/Service entities
  .TestResults []TestEvidence                   <-->  TestExecution activity outputs
  .Telemetry                                    <-->  TraceSpan/MetricSample (conditional) entities
  .Diagnosis *FailureDiagnosis                  <-->  Diagnosis + Recommendation entities

FailureEvidenceItem     <-->  one Entity (LogEntry / KubernetesEvent / TraceSpan / ...)
  .ID                   <-->  the addressable identity used by `supports`
  .Type                 <-->  the entity sub-type (see §7)
  .Source/.Resource     <-->  wasGeneratedBy provenance (which Pod/Job/activity)
  .Relevance            <-->  ranking for Evidence Precision / Recall
  .Redacted             <-->  redaction applied before persistence

FailureDiagnosis        <-->  Diagnosis entity
  .EvidenceRefs []string<-->  the `supports` edges (must reference existing item IDs)
  .Recommendations      <-->  Recommendation entities
```

---

## 7. Evidence item types

`FailureEvidenceItem.Type` is drawn from a closed vocabulary. Each maps to a PROV
entity sub-type:

| `Type` value | PROV entity | Typical source |
|--------------|-------------|----------------|
| `GitDiff` | `Entity` | the raw PR diff (`spec.changeContext.diffPatch` / ConfigMap) |
| `ChangedFile` | `Entity` | `spec.changeContext.changedFiles[]` |
| `KubernetesEvent` | `Entity` | namespace warning events |
| `PodLog` | `Entity` | application/database pod logs |
| `JobLog` | `Entity` | migration/seed/test job logs |
| `TestResult` | `Entity` | `TestSuiteStatus` / `TestRun` |
| `TraceSpan` | `Entity` (conditional) | external trace backend |
| `Metric` | `Entity` (conditional) | external metrics backend |
| `PreviewCondition` | `Entity` | `Preview.status.conditions[]` |
| `ReconcileEvent` | `Entity` | `ReconcileEvent` CRD |

---

## 8. Evidence reference IDs

Each `FailureEvidenceItem` has a **stable, deterministic ID** so that:

- the `supports` relation (and `FailureDiagnosis.EvidenceRefs`) can reference items
  unambiguously;
- repeated reconciliation produces the *same* IDs for the *same* evidence — required
  for the *Reconcile Idempotency* metric (`metrics.md` §11).

Recommended ID scheme (to be implemented in Phase 2):

```
ID = <type>-<short-hash>
short-hash = first 12 hex chars of SHA-256( type || " " || source || " " || resource || " " || canonical-content )
```

- The hash inputs are the *logical identity* of the item, not volatile fields
  (timestamps, counters) — otherwise IDs would change every reconcile.
- `canonical-content` is the redacted, normalised content (so redaction does not change
  the ID across runs once applied consistently).
- IDs are unique within a bundle; a collision means two items have the same logical
  identity and must be deduplicated (this is what `duplicate_evidence_items` checks).

---

## 9. Redaction

Before an evidence item is persisted, secret-bearing content is redacted:

- Redact values of keys/patterns such as `password`, `token`, `secret`, `api[-_]?key`,
  `authorization`, connection strings with embedded credentials, and bearer tokens.
- Replace the value with a fixed placeholder (e.g. `«redacted»`) — never with the
  partial value.
- Set `FailureEvidenceItem.Redacted = true` when any redaction was applied.
- Redaction runs **before** ID computation uses `canonical-content`, so redacted and
  unredacted runs of the same evidence still produce the same ID.

Redaction is a unit-tested component (Phase 2): see the test list in `prompt.txt`
Part 3 and the scaffolding plan.

---

## 10. Why PROV

Mapping the model onto PROV gives three concrete benefits for the article:

1. **Auditability.** A reviewer (human or tool) can follow `supports` / `wasDerivedFrom`
   edges from any recommendation back to the change that caused the failure.
2. **Grounding.** The `supports` relation makes "ungrounded diagnosis" a precise,
   checkable condition: a `Diagnosis` with no `supports` edge fails the grounding
   constraint (RQ4).
3. **Interoperability.** Because the model is PROV-aligned, a `FailureReport` can in
   principle be exported to any PROV-compatible tooling — noted as future work, not
   claimed as implemented.
