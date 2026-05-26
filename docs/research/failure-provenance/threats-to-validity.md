# Threats to Validity

This chapter follows the standard four-category structure (internal, external,
construct, conclusion validity) and ends with the mitigations built into the
methodology. It must be revisited after Phase 5, when the actual results are known.

---

## 1. Internal validity

*Are the observed effects actually caused by the treatment (structured evidence /
provenance) rather than by something else?*

- **Injected failures may not represent all real failures.** The ten scenarios
  (F1–F10) are deliberate, deterministic faults. Real-world failures can be compound,
  intermittent, or environmental in ways the matrix does not capture. The evaluation
  measures behaviour on the chosen fault classes, not on the full space of failures.
- **Diagnostic scoring may be subjective.** Deciding whether a diagnosis is "correct",
  whether an evidence item is "relevant", or whether a claim is "supported" involves
  human judgement.
- **Traces may not always be available.** Trace evidence depends on app-level
  OpenTelemetry instrumentation and a trace backend that the operator does not itself
  provide. Runs without a trace backend lack trace evidence by construction, which can
  confound any comparison that involves F8 or trace-dependent items.
- **LLM in the loop.** When the diagnostic step is the kagent LLM, output depends on
  the prompt, the model, and the model version — factors orthogonal to the evidence
  treatment unless explicitly controlled.
- **Shared-cluster interference.** Concurrency experiments (1/5/10 parallel previews)
  introduce resource contention that can affect timing metrics independently of the
  treatment.

## 2. External validity

*Do the results generalise beyond this study?*

- **The demo app may be simpler than production systems.** Experiments run against the
  repository's `demo-app/`. A small demo application has fewer services, simpler
  failure modes, and smaller logs/traces than a production system; absolute numbers
  (overhead, MTTD, bundle size) will differ at production scale.
- **Results from Kind may differ from managed Kubernetes.** Kind is a single-host,
  container-based cluster. Managed Kubernetes (AKS) differs in networking, storage,
  scheduling, and node count. Overhead and timing metrics are especially sensitive to
  this.
- **One operator implementation may not generalise.** The study evaluates a single
  operator (`preview-operator`). A different operator, a different preview platform, or
  a different CI integration could behave differently. The *concept* (operator-captured
  provenance) is intended to generalise; the *measurements* are specific to this
  implementation.
- **One demo application, one technology stack.** The application stack (PostgreSQL,
  the chosen languages, the chosen test frameworks) is fixed. Other stacks may surface
  different evidence and different failure signatures.

## 3. Construct validity

*Do the metrics measure what they claim to measure?*

- **RCA accuracy is difficult to measure.** "Correct diagnosis" is operationalised as
  the three-part rule in `methodology.md` §4 (component + category + ≥1 valid evidence
  reference). This is a defensible but not unique definition; a stricter or looser rule
  would shift the numbers.
- **Hallucination rate requires careful annotation.** Classifying a claim as
  "unsupported" depends on the annotator's reading of both the claim and the evidence.
  The construct ("hallucination") is itself contested in the literature.
- **Evidence relevance may be subjective.** Evidence Precision and Recall depend on
  which items are deemed "relevant" to the true cause; reasonable annotators may
  disagree at the margin.
- **Time to diagnosis conflates work and waiting.** MTTD includes evidence-collection
  time, queueing, and reconcile cadence, not only "reasoning" time. It measures the
  developer-visible latency, which is the intended construct, but it is not a pure
  measure of diagnostic effort.
- **Recommendation usefulness.** Whether a recommendation is "actionable" is a proxy
  for real developer value, which would ideally be measured with a user study (out of
  scope here).

## 4. Conclusion validity

*Are the statistical conclusions sound?*

- **The number of runs may be small.** ≥10 repetitions per cell is modest for
  estimating rates close to 0 or 1, and for high-variance scenarios. Confidence
  intervals may be wide.
- **Flaky tests may affect results.** F10 is intentionally flaky; flakiness can also
  leak into other scenarios and add noise to accuracy and timing metrics.
- **LLM behaviour may change with model/version.** Re-running the evaluation against a
  newer model can change the results; conclusions are conditional on the recorded model
  and version.
- **Multiple comparisons.** Comparing five configurations across ten scenarios and five
  RQs invites false positives if every pairwise difference is tested; the analysis
  should pre-register the comparisons of interest or correct for multiplicity.

---

## 5. Mitigations

The methodology builds in the following mitigations; each maps to the threats above.

| Mitigation | Addresses |
|------------|-----------|
| **Deterministic fault injection** — faults applied by script, no manual editing during a run | Internal (controlled cause) |
| **Known ground truth** — `(component, category, expected evidence)` fixed before any run | Internal, construct |
| **Repeated runs** — ≥10 repetitions per cell, more for high-variance cells | Conclusion |
| **Confidence intervals** — every metric reported with a CI (Wilson for proportions) | Conclusion |
| **Independent scoring rubric** — fixed `scoring-rubric.md`; two annotators where feasible; report inter-rater agreement (Cohen's κ) | Construct |
| **LLM pinning** — fixed model and version, temperature 0 for the diagnostic step, recorded in every result row | Internal, conclusion |
| **Separate Kind and AKS results** — never pooled | External |
| **Negative control (F10)** — flaky scenario detects over-confident hallucination | Construct (hallucination) |
| **Isolated overhead runs** — RQ5 measured on an idle host, concurrency runs labelled | Internal (interference) |
| **Trace availability recorded** — each run records whether a trace backend was present | Internal, construct |
| **Pre-registered comparisons** — the comparisons of interest (C1 vs C4, C4 vs C5, grounded vs free-form) are declared before analysis | Conclusion (multiplicity) |
| **Reproducible artifact** — harness and scripts released so others can re-run | External (independent replication) |
| **Raw-artifact traceability** — every number linked to raw evidence via the run key | Construct, conclusion |

---

## 6. Honest scope statement (for the paper)

The article should state plainly:

- This is a **systems / tool evaluation** of one operator implementation against one
  demo application, primarily on a local Kind cluster. It is **not** a generalisability
  study across many systems.
- The headline contribution is the **operator-controlled capture, structuring,
  linking, and grounding** of failure evidence — not a new RCA algorithm and not a
  claim that evidence is otherwise entirely lost (the cluster-scoped `Preview.status`
  already retains a snapshot).
- Absolute numbers (overhead, MTTD, bundle size) are **environment-specific** and
  should be read as evidence of feasibility and of *relative* differences between
  configurations, not as universal constants.

---

## 7. Defects discovered during execution and how they were closed

This section documents the threats that were surfaced *during* the matrix
runs, in chronological order. They are recorded so that an independent
reviewer can verify our closure path against the live `PROGRESS.md` log.

### 7.1 F7 race condition (multi-app subjects only) — closed

The original F7 injector patched the operator-managed Service selector at
`post_deploy`. The operator's reconciler reverts any change to the Service
spec within ~3 s. Whether the test Jobs observed a broken Service therefore
depended on which loop (test Job or operator reconcile) won the race. The
listmonk F7 captures `r1`-`r3` (lucky races) and `r4`-`r10` (lost races
→ no-report) demonstrated the non-determinism.

The fault model was redesigned at the **meta.yaml level**: `services[0].port`
is set to 19999, an unbound port. The operator generates the Service routing
to 19999, the application binds to its standard port, and the resulting
"connection refused" propagates to a deterministic FailureReport on every
rep. All 17 race-condition F7 captures (3 listmonk + 4 umami + 10 petclinic)
were invalidated and re-collected with the new mechanism. Documented in
commit `ca5a88a` and in `EVALUATION-DRAFT.md §10`.

**Threat-to-validity impact**: the previously published S1 F7 numbers
(commit `2e81a648` Phase 5a freeze) remain valid because S1 was unaffected
by the race (the operator reconciles less aggressively on the
single-application path); only multi-app needed the redesign.

### 7.2 RQ3 timing breakdown — partial mitigation

`status.collectionDurationMillis` was set with the `omitempty` JSON tag.
Bundle assembly is sub-millisecond on the demo workloads, so the field
rounded to zero and was omitted from the serialised report. Operator
patched (commit `e0eae6f`) to also emit `collectionDurationMicros`,
`collectionAllocBytes`, and `collectionAllocCount`. A subset re-run with
the patched image populates these fields on real captures.

### 7.3 RQ5 paired ON/OFF baseline — pending

The pre-registration required a paired `EVIDENCE_COLLECTION=on` vs `off`
re-run on the same scenarios. ON runs are the full matrix; OFF is pending
the post-patch subset re-run. Until that is filled, RQ5's overhead claim
covers only the storage sub-component (`BundleSizeBytes`) plus the
sub-millisecond duration estimate from the patched operator. Tracked as
work unit W7 in `Q1-COMPLIANCE.md`.

### 7.4 Cohen κ on hallucination scoring — partial

The pre-registration requires two annotators on a 20 % shuffled subsample.
A proxy automated annotator (Mistral-Large-3) was generated as the second
LLM family; a Claude-Opus AI-assisted first pass populated `correct_human`
with per-row reasoning published in `claude-annotation-audit.csv`. The
**canonical κ** for the article requires the project-lead human reviewer
to validate the AI-assisted column blind, override disagreements, and
sign off. This is the only true human-only gate left in the closure plan.
Tracked as W15 in `Q1-COMPLIANCE.md`.

### 7.5 Multi-app baselines — partial

S1 has B0 (vanilla LLM raw kubectl), B2a (K8sGPT v0.4.21), and B2b
(Kagent k8s-agent). Multi-app extension is pending; symmetry between S1
and multi-app baselines is required for the cross-application
generalisation claim. Tracked as W2 + W3.

---

## 8. Scaling and retention

The evaluation produces approximately **100 `FailureReport`s** across all
scenarios and repetitions — comfortably within etcd's capacity. The
evaluated implementation writes every report to etcd and applies
**no retention policy**: it deliberately keeps every report for the
duration of the campaign to avoid confounding the runtime measurements
with archival activity.

**This is not a deployment configuration.** At realistic CI scale the
in-etcd-only, no-retention design does not hold:

| Quantity | Value |
|---|---|
| Typical `FailureReport` size | 10 – 100 kB (`evidenceItems` + provenance graph + diagnosis) |
| etcd default storage budget | 2 – 8 GB per cluster |
| Practical in-cluster ceiling | ≈ 10⁴ – 10⁵ reports |
| LIST performance degradation | well before that ceiling |

**What the article specifies (and does NOT evaluate):**
the CRD reserves `status.storageRef` for an external object store, the
lifecycle is specified as four phases (`Captured` → `Persisted` →
`Archived` → `Expired`), and a dedicated retention controller is
sketched (see `failure-evidence-model.md §11`). The full two-tier
deployment and the controller implementation are deployment concerns
**out of scope** for this evaluation; they are listed as future work in
`article-outline.md` (and the article LaTeX `§ Future work`).

**Why this does not invalidate the present claims.** RQ1 (preservation)
is measured on individual reports, not aggregates. RQ2–RQ5 are
per-report metrics that do not depend on the total number of reports
in etcd. The scaling limit affects deployability, not the validity of
the per-report measurements presented here. The honest claim is:
*"The capture mechanism works; the in-cluster-only storage choice
caps the deployment scale at ~10⁴ – 10⁵ reports, and the tiered design
that lifts this cap is specified but not yet implemented."*

**Mitigation in the article.** The scaling limit is acknowledged
explicitly in the Threats to validity section of the LaTeX article
(*Scaling and retention* paragraph) and the tiered storage design is
introduced in §`sec:approach-storage`. Reviewers asking about scale are
directed to those two locations.

---

## 9. Post-teardown comparator replay harness — synthesised cluster state

The article reports a regime-symmetric K8sGPT-PT and Kagent-PT
comparator (§sec:eval-baselines). Both are computed by **replaying** the
operator-captured `evidenceItems` into a disposable namespace on a live
Kubernetes API server and then invoking the practitioner tool against
that namespace. The replay harness (`analysis/k8sgpt-replay.py`,
`analysis/kagent-replay.py`) decodes each `FailureReport`'s evidence
items into a minimal synthetic `Pod`/`Job`/`Service`/`Deployment`
bundle, applies it to a fresh namespace on the AKS cluster, patches
the status subresource (e.g.\ `CrashLoopBackOff`, `ImagePullBackOff`,
`BackoffLimitExceeded`, missing-Service-selector), runs the tool,
saves the JSON output, and deletes the namespace.

### 9.1 Threats specific to the replay synthesis

1. **Operator-curated synthesis** — the replay harness produces a
   *clean* Kubernetes state derived from the operator's structured
   evidence. K8sGPT and Kagent therefore receive a more deterministic
   signal than they would see on a live failing cluster (where node
   noise, races, and unrelated `ConfigMap` lookups can confuse the
   analyzer). The harness mitigates this by limiting K8sGPT's
   analyzers to `Pod,Job,Service,Deployment,ReplicaSet,StatefulSet,
   CronJob,PersistentVolumeClaim,ConfigMap` so cluster-wide Node
   noise is excluded; the per-scenario aligned matcher still credits
   only the actual failing-component role.

2. **Pod/Job synthesis fidelity** — the synthesiser sets
   `containerStatus.state.waiting.reason` and `Job.status.conditions`
   to mirror the captured failure, but it does **not** reproduce
   container logs (the operator's evidenceItems contain only
   `PodLog` snippets at the bundle level, not on-disk container
   logs the kubelet would expose). K8sGPT and Kagent therefore
   diagnose from status fields + events rather than from logs.
   This is consistent across the five subjects; the comparison
   between the operator's diagnoser and the comparators is
   "structured-evidence vs structured-evidence".

3. **Kagent reasoning-chain variance** — Kagent is agentic and
   issues multiple intermediate LLM and kubectl calls per
   diagnosis. Two replays of the same capture can land on slightly
   different probable causes; we control this by recording the
   replay seed in the JSON envelope and re-using it on `--force`
   replays, but inter-run variance remains a residual threat.

4. **Replay harness is single-cluster** — all 509 capture-cells go
   through the same AKS cluster (`idp-preview-test`). Cross-provider
   generality of the comparator numbers is therefore not claimed;
   the replay establishes the *regime-symmetry* (live-tool, but on
   operator-captured evidence), not provider-portability.

### 9.2 Five-subject treatment (Phase 5c)

The original Phase-5 design ran B2a/B2b live only on S1 and the
S2--S5 multi-app subset. Phase 5c (`analysis/k8sgpt-replay.py`,
`analysis/kagent-replay.py`, `--force --max-reps 20`) extends the
post-teardown comparators uniformly to all five subjects (449
distinct captures; 509 capture-cells exposed in discovery because
`--max-reps 20` admits a few F-cells with $>10$ reps).
This removes the S1 privilege from the comparator analysis: the same
synthesis pipeline + the same tool invocation produces the
K8sGPT-PT and Kagent-PT numbers reported in the article.

### 9.3 Rep-parity backfill across S2--S5 (Phase 5e/5f)

A subsequent audit (2026-05-25 12:35 UTC) revealed that the option-A
refactor of 2026-05-23 had left S2--S5 with only 7 reports for the
fault subset $\{F1, F2, F3, F6, F7\}$ versus 10 reports for S1, a
60-cell asymmetry. Phases 5e + 5f re-captured the missing reps using
the same orchestrator (`multiapp/run-matrix.py`) with the original
fault injectors unchanged. Phase 5e closed 46/60 captures (F1, F2, F6,
F7 succeeded for all four subjects); Phase 5f closed the remaining 14
F3 captures by raising `REPORT_TIMEOUT_S` from 900\,s to 1200\,s --- a
direct consequence of the operator's
`provisioningDeadline = 15 * time.Minute`
([`internal/controller/preview_controller.go:188`](../../internal/controller/preview_controller.go#L188)).
F3 (invalid image tag) leaves the migration Job pod in
ImagePullBackOff forever and never reaches JobFailed; the operator's
`provisioningDeadline` backstop is the only path to a FailureReport
in that scenario, so a sub-15\,min orchestrator timeout produces a
spurious `no-report` even though the operator would have captured the
report a few seconds later. The 1200\,s setting gives a 5\,min margin
and matches the timeout in `analysis/k8sgpt-replay.py` for
namespace synthesis. After Phase 5f, S2--S5 reach 10 reports per
fault for the in-scope $\{F1, F2, F3, F6, F7\}$ subset, fully
matching S1.

**Methodological note.** The 15-minute capture floor for F3 is an
operator design choice (avoid creating premature FailureReports for
transient ImagePullBackOff during legitimate deploys), not an
artefact of this study. Practitioners reading the article should note
that for fault classes where the dependent resource never transitions
to a terminal-failed phase, the operator's `provisioningDeadline` is
the dominant component of MTTD --- a real cost of evidence
completeness that we report in §RQ3 rather than mask.
