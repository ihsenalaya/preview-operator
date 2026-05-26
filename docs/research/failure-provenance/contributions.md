# Contributions

This article makes five contributions. The first three are the **scientific core**
(named in the abstract); the last two are the **engineering and reproducibility**
contributions that support them.

> Honesty constraint (applies to every contribution): the base operator already
> captures a diagnostic snapshot into the cluster-scoped `Preview.status.diagnostics`,
> which already survives namespace deletion. The contributions below are about
> *structuring*, *linking*, *grounding*, and *evaluating* that evidence — not about
> "saving logs that would otherwise be lost". Phrase the paper accordingly.

---

## Contribution 1 — Failure Evidence Bundle

A **Kubernetes-native failure evidence model**: a structured artifact that captures, in
one place and before namespace teardown, the evidence needed to diagnose a preview
failure.

The bundle aggregates:

- **PR change context** — pull request, commit SHAs, classified changed files, detected
  impacts (sourced from the existing `ChangeContextSpec`).
- **Kubernetes state** — deployments, pods, jobs, services, and their conditions.
- **Test results** — per-suite and per-case outcomes (sourced from `TestSuiteStatus`,
  `TestPlan`, `TestRun`).
- **Logs** — significant pod and job log excerpts (sourced from
  `DiagnosticsStatus.SignificantLogs` / `PodLogs`).
- **Events** — recent Kubernetes warning events (`DiagnosticsStatus.LastEvents`).
- **Telemetry** — trace spans and metric samples *where app-level OpenTelemetry
  instrumentation and a trace backend are available* (see limitation below).
- **Diagnostic output** — probable cause, component, category, confidence, and
  recommendations.

Each item is a typed, addressable `FailureEvidenceItem` with a stable ID, a source, a
relevance label, and a redaction flag. The model is defined in
`failure-evidence-model.md`.

**Novelty claim.** Not "we collect logs and events" (the operator already does), but
"we define a *typed, addressable, change-scoped evidence model* for ephemeral preview
environments, assembled inside the operator's reconcile loop".

**Limitation.** Trace/metric evidence depends on app-level OpenTelemetry
auto-instrumentation (the operator only wires it via the OpenTelemetry Operator;
it does not collect spans itself). Trace-backed evidence is reported separately and
marked as dependent on external infrastructure.

---

## Contribution 2 — Failure Provenance Graph

A **failure provenance graph** that links change context, infrastructure, tests, and
observable evidence to a diagnosis, mapped onto the **W3C PROV** model.

The graph connects:

```
PullRequest → Commit → ChangedFile → Preview(CR) → Namespace → Resource
  → TestSuite → FailedTest → ObservableEvidence → ProbableCause → Recommendation
```

- Nodes are PROV *Entities* (artifacts) and *Activities* (reconciliation, deployment,
  migration, test execution, evidence collection, diagnosis).
- Edges are PROV relations (`wasDerivedFrom`, `wasGeneratedBy`, `wasAssociatedWith`,
  `used`) plus domain relations (`caused`, `observedIn`, `supports`).
- *Agents* are the developer, the CI pipeline, the operator, the Kubernetes API server,
  and the AI agent (kagent).

**Novelty claim.** A provenance graph that is *change-scoped* (rooted in a single PR
diff) and *operator-built* (assembled deterministically during reconciliation), giving
every diagnosis an auditable path back to the change that caused it.

---

## Contribution 3 — Operator-Controlled, Evidence-Grounded RCA

An AI-assisted RCA workflow in which **the operator controls evidence capture** and the
**AI agent must ground every hypothesis in collected evidence**.

- The operator assembles the evidence bundle; the AI agent (kagent troubleshooter)
  consumes it rather than gathering evidence itself.
- A **grounding constraint** requires every diagnostic claim to reference one or more
  `FailureEvidenceItem` IDs that exist in the bundle. Claims that reference
  non-existent IDs are rejected or flagged.
- This makes hypotheses auditable and is the mechanism evaluated in RQ4
  (hallucination control).

**Novelty claim.** Not "we use an LLM for RCA" (established), but "the operator is the
trusted evidence boundary, and the diagnosis is *constructively constrained* to cite
that evidence".

---

## Contribution 4 — Controlled Evaluation

A **controlled, reproducible evaluation** using deterministic fault injection across
six failure families:

- Kubernetes/infrastructure (F3 image tag, F6 readiness timeout, F7 service selector),
- database (F1 invalid migration, F9 incorrect seed data),
- application (F4 broken API endpoint, F5 frontend breaking change),
- configuration (F2 missing environment variable),
- observability/performance (F8 artificial latency),
- test reliability (F10 flaky test).

The evaluation measures RCA accuracy, evidence completeness, hallucination rate, time
to diagnosis, and overhead across five evidence configurations (C1–C5), with ground
truth defined per scenario (`experiments.md`, `metrics.md`).

---

## Contribution 5 — Reproducible Artifact

A **reproducible research artifact**: the implementation, the fault-injection harness,
the evaluation scripts, and this documentation package, released in the
`preview-operator` repository.

- Documentation: `docs/research/failure-provenance/` (this directory).
- Implementation: the `internal/evidence/` Go package — evidence model types,
  deterministic IDs, collectors, redaction, idempotent report generation, unit
  tested (≈89 % coverage).
- The `FailureReport` artifact: a cluster-scoped CRD
  (`api/v1alpha1/failurereport_types.go`), wired into the operator failure paths
  (`internal/controller/failurereport.go`) so it is produced for every failed
  preview.
- Experiment harness: `experiments/failure-provenance/` (scenarios, run/collect
  scripts, scoring rubric, results CSV).

The artifact builds, vets, and tests green (`go build`, `go vet`, `make test`).
It targets one-command reproduction of the evaluation on a local Kind cluster.

---

## Mapping to research questions

| Contribution | Validated by |
|--------------|--------------|
| C1 Failure Evidence Bundle | RQ1 (preservation), RQ2 (accuracy) |
| C2 Failure Provenance Graph | RQ2 (accuracy), RQ3 (time to diagnosis) |
| C3 Evidence-grounded RCA | RQ4 (hallucination control) |
| C4 Controlled evaluation | RQ1–RQ5 |
| C5 Reproducible artifact | all RQs (reproducibility) |

## What this article does NOT claim

- It does not claim Kubernetes RCA, LLM-based RCA, or graph-based RCA are new.
- It does not propose a new RCA algorithm or a new language model.
- It does not claim preview environments or Kubernetes-native test orchestration
  are new.
- It does not claim that, without this work, failure evidence is entirely lost —
  the cluster-scoped `Preview.status` already retains a snapshot.

The contribution is the **operator-controlled capture, structuring, linking, grounding,
and evaluation** of failure evidence for *ephemeral, change-scoped* preview
environments.
