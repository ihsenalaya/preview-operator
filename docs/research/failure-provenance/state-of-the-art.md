# State of the Art

This chapter surveys the work this article builds on and positions against. Citation
keys refer to `bibliography.bib`. Where a claim about an external system or paper could
not be verified from a primary source while writing, it is marked **TODO_VERIFY** and
must be checked before submission.

---

## A. Kubernetes preview environments

A *preview environment* (also "ephemeral environment" or "review app") is a temporary,
isolated deployment created automatically for a pull request so that reviewers can
exercise the change in a realistic runtime before merge. The environment is short-lived:
it is created when the PR is opened or updated and destroyed when the PR is closed or
its time-to-live expires.

- **Argo CD ApplicationSet — Pull Request generator** `[argocd_pr_generator]`. The
  ApplicationSet controller can generate one Argo CD `Application` per open pull
  request, producing a per-PR deployment that is removed when the PR closes. This is
  the most widely documented GitOps-native mechanism for PR previews.
- **Signadot** `[signadot_preview_envs]`. A platform for *request-scoped* preview
  environments ("sandboxes") that route a subset of traffic through changed workloads
  inside a shared cluster, avoiding a full per-PR stack. TODO_VERIFY the exact
  isolation mechanism and current product terminology.
- **Okteto** `[okteto_preview_envs]`. A developer platform that creates preview
  environments per pull request, with namespace-level isolation and automatic cleanup.
  TODO_VERIFY current feature naming.
- **Jenkins X preview environments** `[jenkinsx_preview_envs]`. Jenkins X popularised
  the term "preview environment" in a CI/CD pipeline context, creating a deployment per
  PR as a pipeline stage. TODO_VERIFY whether to cite given the project's current
  maintenance status.
- **Review-app style operators.** Several smaller open-source operators implement
  per-PR "review apps" on Kubernetes. TODO_VERIFY a specific, citable example before
  including it; otherwise omit.

**Relation to this work.** These platforms focus on *creating and destroying* the
environment efficiently. They treat the environment as disposable: when it is gone, so
is its runtime state. None of them, to our knowledge, treats the *failure evidence*
produced inside the environment as a first-class, structured, preserved artifact linked
to the originating change. That is the gap this article addresses.

---

## B. Kubernetes operators and Custom Resources

- **Custom Resources** `[kubernetes_custom_resources]`. Kubernetes lets users extend
  its API with Custom Resource Definitions (CRDs), adding domain-specific object types
  served by the API server.
- **The operator pattern** `[kubernetes_operator_pattern]`. An *operator* is a custom
  controller that encodes operational knowledge for a specific application or domain,
  acting on Custom Resources.
- **The reconciliation loop** `[kubernetes_controller_concepts]`. A controller observes
  the desired state (the resource spec) and the actual cluster state, and drives the
  latter towards the former. Reconciliation is level-triggered and idempotent: the same
  desired state must yield the same result regardless of how many times reconcile runs.
- **Finalizers** `[kubernetes_finalizers]`. A finalizer is a key on an object that
  blocks deletion until the controller has performed cleanup and removed the key. This
  is the standard mechanism for running pre-deletion logic — and the natural hook for
  *persisting evidence before namespace teardown*.

**Relation to this work.** The operator pattern is the substrate of this article. The
reconcile loop is where evidence is captured; the finalizer is where persistence is
sequenced before teardown. The novelty is not the pattern but *what* is reconciled:
failure evidence and its provenance.

---

## C. Kubernetes-native test orchestration

- **Testkube** `[testkube_workflows]`. Testkube runs tests as first-class workloads
  inside the cluster. *Test Workflows* describe multi-step test execution declaratively.
- **Testkube CRDs** `[testkube_crds]`. Testkube exposes tests, test suites, and
  workflows as Custom Resources, making test execution and results part of the
  Kubernetes API surface.
- **Testkube AI / agent features** `[testkube_ai_agents]`. Testkube has introduced
  AI-assisted features for test analysis. TODO_VERIFY the exact current capability and
  naming before citing as "AI agents".

**Relation to this work.** Testkube establishes that tests, suites, and results belong
in the Kubernetes API as Custom Resources. The operator studied here follows the same
principle with its `TestPlan` and `TestRun` resources. This article extends the idea
beyond *test results* to *failure evidence and provenance*: not only "which test
failed" but "what change, which resources, which observable signals, and which probable
cause", linked together.

---

## D. Kubernetes RCA and LLM-based RCA

Root cause analysis for cloud and Kubernetes systems is an active area.

- **OpenRCA** `[openrca_2025]`. A benchmark evaluating whether large language models
  can locate the root cause of software failures from telemetry, with a dataset and an
  evaluation protocol. TODO_VERIFY venue, year, and author list against the primary
  publication.
- **SynergyRCA** `[synergyrca_2025]`. Reported work on RCA combining multiple signal
  sources / reasoning strategies. TODO_VERIFY title, venue, year, and authors — this
  entry must be confirmed from a primary source or removed.
- **Graph-augmented LLM RCA** `[graph_llm_rca]`. A line of work augments LLM-based
  diagnosis with graph structure (service graphs, causal graphs, dependency graphs) to
  constrain and ground reasoning. TODO_VERIFY a specific citable paper; if none can be
  confirmed, present this as a research direction rather than a single citation.
- **LLM-based software failure diagnosis / surveys** `[llm_rca_survey]`. Surveys of
  LLMs for software testing, debugging, and failure diagnosis. TODO_VERIFY a concrete,
  citable survey.

**Relation to this work.** This article explicitly does **not** claim a new RCA method.
It treats the diagnostic step — whether the operator's rule-based `inferRootCause` or
the LLM-based kagent troubleshooter — as a *consumer* of evidence. The contribution is
upstream of RCA: producing structured, grounded, change-scoped evidence, and
*constraining* the AI diagnosis to cite it. Graph-augmented RCA is the closest prior
art; the difference is that the graph here is a *provenance* graph rooted in a PR diff
and built by the operator, not a service/causal graph mined from telemetry.

---

## E. Observability — logs, metrics, traces

- **OpenTelemetry** `[opentelemetry_docs]`. A vendor-neutral standard and toolkit for
  generating, collecting, and exporting telemetry: logs, metrics, and traces, with a
  common data model and semantic conventions.
- **OpenTelemetry Operator** `[opentelemetry_operator]`. A Kubernetes operator that
  manages OpenTelemetry Collectors and performs *auto-instrumentation* by injecting
  language-specific instrumentation into application pods via the `Instrumentation`
  custom resource.
- **The three signals.** Logs (discrete events), metrics (aggregated measurements), and
  traces (causal, distributed request paths) are complementary; effective diagnosis
  typically correlates them.

**Relation to this work.** The operator studied here integrates with the OpenTelemetry
Operator: it wires `OTEL_SERVICE_NAME` and the auto-instrumentation annotations onto
the application pod. **Honesty note for the article:** the operator does *not* itself
collect or store trace spans — tracing relies on app-level auto-instrumentation and an
external trace backend. Trace-backed evidence in the failure bundle is therefore
*conditional* on that infrastructure being present, and the article must report it as
such rather than claiming the operator captures traces directly.

---

## F. Provenance

- **W3C PROV overview** `[w3c_prov_overview]`. PROV is a W3C family of documents for
  representing provenance — the record of entities, activities, and agents involved in
  producing or influencing a thing.
- **PROV-DM** `[w3c_prov_dm]`. The PROV Data Model defines the core types — *Entity*,
  *Activity*, *Agent* — and relations such as `wasGeneratedBy`, `used`,
  `wasDerivedFrom`, `wasAssociatedWith`, and `wasAttributedTo`.
- **Provenance graphs.** Provenance is naturally a directed graph: nodes are entities,
  activities, and agents; edges are the provenance relations. Such graphs support
  auditing, reproducibility, and trust.

**Relation to this work.** This article uses PROV as the conceptual backbone of the
failure provenance graph: changed files and evidence items are *Entities*; reconcile,
deployment, migration, test execution, evidence collection, and diagnosis are
*Activities*; the developer, CI, operator, API server, and AI agent are *Agents*. The
mapping is given in `failure-evidence-model.md`. Applying PROV to RCA evidence is not
itself novel as a modelling choice; the contribution is the *specific* PROV-aligned
model for *change-scoped failure evidence in ephemeral preview environments*, built and
preserved by an operator.

---

## G. Gap analysis

> Existing work addresses Kubernetes root cause analysis using LLMs, telemetry, and
> graph-based reasoning, while industrial platforms provide pull-request preview
> environments and Kubernetes-native test orchestration. However, these capabilities
> are rarely studied together in the context of ephemeral, change-scoped preview
> environments, where failure evidence may disappear when the namespace is deleted.
> This work studies how a Kubernetes operator can capture, structure, and preserve
> failure evidence during reconciliation and expose it as a provenance artifact for
> reliable AI-assisted root cause analysis.

Three specific sub-gaps follow from the survey:

1. **Evidence volatility (vs. A, E).** Preview platforms optimise creation/destruction;
   observability tooling assumes a long-lived backend. Neither targets the case where
   the *environment itself* — and its namespace-scoped evidence — is deliberately
   short-lived. *Qualification (be precise in the paper):* a cluster-scoped custom
   resource can already retain a status snapshot past namespace deletion; the real gap
   is that such a snapshot is unstructured and overwritten, not that nothing survives.
2. **Missing change-to-symptom linkage (vs. C, D, F).** Test orchestrators record test
   results; RCA tools reason over telemetry; provenance models exist in the abstract.
   No widely studied system links *PR diff → Kubernetes resources → failed test →
   observable evidence → probable cause* as one operator-built provenance artifact.
3. **Ungrounded AI diagnosis (vs. D).** LLM-based RCA can produce unsupported
   explanations. Graph-augmented RCA constrains reasoning, but the constraint is
   typically a service/causal graph, not an *auditable evidence bundle* whose item IDs
   the diagnosis must cite.

This article addresses (1)–(3) by making the operator the capture point, the evidence
bundle the structured artifact, the provenance graph the linkage, and an evidence-ID
grounding constraint the mechanism against ungrounded diagnosis.

---

## H. Companion work — PostgreSQL isolation and seed quality

The same `preview-operator` is also the subject of a separate, already-prepared
**companion article** `[postgres_isolation_companion]` that studies the overhead of
PostgreSQL inter-suite isolation (`spec.database.isolationMode`: `restore` vs
`migration`) and the quality of AI-generated seed data (mutation detection). Those
questions are **deliberately excluded** from the present article to avoid overlap. The
two articles share the operator and the demo application but address disjoint research
questions: this article studies failure evidence and provenance; the companion article
studies database-isolation cost and seed quality. Where the two intersect — for
example, evidence-collection overhead measured on a preview that also uses database
isolation — this article attributes the isolation cost to the companion study and
reports only the evidence-collection delta (see `research-questions.md`, RQ5).

---

## Verification checklist (resolve before submission)

- [ ] `signadot_preview_envs` — isolation mechanism and current terminology.
- [ ] `okteto_preview_envs` — current feature naming.
- [ ] `jenkinsx_preview_envs` — confirm relevance / maintenance status; keep or drop.
- [ ] Review-app operator — find one concrete citable example or omit section A's last bullet.
- [ ] `testkube_ai_agents` — confirm the exact AI capability and its name.
- [ ] `openrca_2025` — confirm venue, year, authors.
- [ ] `synergyrca_2025` — confirm the work exists and its metadata, or remove.
- [ ] `graph_llm_rca` — find a specific citable paper or reframe as a direction.
- [ ] `llm_rca_survey` — find a concrete survey to cite.
