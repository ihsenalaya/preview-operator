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
