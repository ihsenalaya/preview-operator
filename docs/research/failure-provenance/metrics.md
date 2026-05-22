# Metrics

This chapter defines every metric used in the evaluation precisely enough to be
implemented in `experiments/failure-provenance/collect-results.sh` and scored with
`scoring-rubric.md`. Each metric lists its formula, its unit, how it is collected, and
the research question it serves.

Notation: a *run* is one execution of one (scenario, configuration) pair. A *cell* is
the set of repetitions for one (scenario, configuration) pair.

---

## 1. Evidence Preservation Rate

- **Definition.** Fraction of the evidence items expected for a scenario that the
  operator actually captured into the bundle.
- **Formula.**
  ```
  EvidencePreservationRate = preserved_evidence_items / expected_evidence_items
  ```
- **Unit.** Ratio in [0, 1].
- **Collection.** `expected_evidence_items` is the per-scenario list in `experiments.md`
  (the "expected evidence" fields). `preserved_evidence_items` is the count of those
  that appear in the captured `FailureReport` / bundle. Matching is by evidence *type*
  and *resource*, not by exact string.
- **Serves.** RQ1.
- **Notes.** Report per scenario and per failure family. An item that is inherently
  absent for a scenario (e.g. container logs in F3) is **not** counted in the
  denominator.

## 2. Snapshot Survival Rate

- **Definition.** Fraction of the captured evidence that is still readable after the
  preview namespace has been deleted.
- **Formula.**
  ```
  SnapshotSurvivalRate = available_evidence_after_namespace_deletion
                         / captured_evidence_before_deletion
  ```
- **Unit.** Ratio in [0, 1].
- **Collection.** Count evidence items in the persisted artifact before namespace
  deletion (step 9 of the experimental flow) and again after (step 11). Read only from
  the persisted artifact, never from the deleted namespace.
- **Serves.** RQ1.
- **Notes.** For evidence written to a cluster-scoped resource or external store, this
  is expected to be 1.0; any value below 1.0 indicates an artifact that was wrongly
  stored namespace-scoped, and is a finding to report.

## 3. Top-1 RCA Accuracy

- **Definition.** Fraction of failures whose *first* (highest-ranked) diagnosis is
  correct.
- **Formula.**
  ```
  Top1Accuracy = correct_first_diagnosis / total_failures
  ```
- **Unit.** Ratio in [0, 1].
- **Collection.** "Correct" follows the three-part rule in `methodology.md` §4
  (right component **and** right category **and** ≥1 valid evidence reference).
- **Serves.** RQ2.
- **Notes.** Report per configuration C1–C5 with a proportion confidence interval
  (e.g. Wilson 95%).

## 4. Top-3 RCA Accuracy

- **Definition.** Fraction of failures for which a correct diagnosis appears among the
  top three ranked hypotheses.
- **Formula.**
  ```
  Top3Accuracy = root_cause_in_top_3_hypotheses / total_failures
  ```
- **Unit.** Ratio in [0, 1].
- **Collection.** As Top-1, but the correctness rule is satisfied by any of the first
  three hypotheses. If the diagnostic step emits fewer than three hypotheses, the
  missing slots count as incorrect.
- **Serves.** RQ2.

## 5. Mean Time To Diagnosis (MTTD)

- **Definition.** Wall-clock time between failure detection and a diagnosis becoming
  available.
- **Formula.**
  ```
  TimeToDiagnosis = diagnosis_available_timestamp - failure_detected_timestamp
  MTTD            = mean(TimeToDiagnosis over a cell)
  ```
- **Unit.** Seconds (`mttd_seconds` in the results CSV).
- **Collection.** `failure_detected_timestamp` = when the operator records the failure
  (step 6). `diagnosis_available_timestamp` = when the diagnosis is written to the
  artifact (step 9). Both come from the operator, not from wall-clock estimation.
- **Serves.** RQ3.
- **Notes.** Report mean **and** variance/CI — RQ3 cares about predictability, not only
  the mean. MTTD includes evidence-collection time; do not also count that time in the
  RQ5 overhead total (avoid double-counting — state which metric owns it).

## 6. Evidence Precision

- **Definition.** Of the evidence items returned in the bundle, the fraction that are
  actually relevant to the true root cause.
- **Formula.**
  ```
  EvidencePrecision = relevant_evidence_items / total_evidence_items_returned
  ```
- **Unit.** Ratio in [0, 1].
- **Collection.** Relevance is judged against the scenario ground truth using
  `scoring-rubric.md`. An item is *relevant* if it genuinely supports identifying the
  true component/category.
- **Serves.** RQ2 (evidence quality), RQ4 (bundle does not drown the diagnosis in
  noise).

## 7. Evidence Recall

- **Definition.** Of the evidence items that *should* have been captured for the true
  root cause, the fraction that were captured.
- **Formula.**
  ```
  EvidenceRecall = relevant_evidence_items_captured
                   / relevant_evidence_items_expected
  ```
- **Unit.** Ratio in [0, 1].
- **Collection.** Denominator = the per-scenario expected relevant items
  (`experiments.md`). Numerator = how many were captured.
- **Serves.** RQ1, RQ2.
- **Notes.** Precision and recall are reported together; an F1-style harmonic mean may
  be added for a single summary figure.

## 8. Hallucination Rate

- **Definition.** Fraction of diagnostic claims that are not supported by any captured
  evidence item.
- **Formula.**
  ```
  HallucinationRate = unsupported_diagnostic_claims / total_diagnostic_claims
  ```
- **Unit.** Ratio in [0, 1].
- **Collection.** Decompose each diagnosis into atomic *claims*. A claim is
  *unsupported* if it references no evidence ID, or references an evidence ID that does
  not exist in the bundle, or references an item that does not actually support the
  claim. Annotate with `scoring-rubric.md`; where possible use two annotators and
  report inter-rater agreement (e.g. Cohen's κ).
- **Serves.** RQ4.
- **Notes.** Compare *free-form* vs *grounded* diagnosis. F10 (flaky test) is the key
  negative control: confidently blaming a real component there is a hallucination.

## 9. Recommendation Usefulness

- **Definition.** Whether the recommendation attached to a diagnosis is actionable and
  evidence-linked.
- **Scale.** Two options — pick one and use it consistently:
  - **Binary (0/1).** `0` = not actionable; `1` = actionable and directly linked to a
    captured evidence item.
  - **Likert (1–5).** `1` = useless … `5` = directly actionable, specific, and
    evidence-linked.
- **Unit.** Ordinal score (`recommendation_score` in the results CSV).
- **Collection.** Scored with `scoring-rubric.md`. The article must state which scale
  was used; the binary scale is recommended for clarity unless a finer comparison is
  needed.
- **Serves.** RQ2 (practical value), RQ4 (recommendations grounded in evidence).

## 10. Collection Overhead

- **Definition.** The additional cost introduced by enabling failure evidence
  collection, measured as the difference between *collection on* and *collection off*.
- **Components measured.**
  - **Time.** Additional seconds added to the preview lifecycle.
  - **CPU.** Additional operator CPU (`cpu_overhead`).
  - **Memory.** Additional operator memory (`memory_overhead`).
  - **Storage.** Size in bytes of the persisted artifact (`bundle_size_bytes`,
    `storage_overhead`).
  - **API calls.** Number of Kubernetes API calls attributable to collection, where
    measurable.
- **Formula (per component X).**
  ```
  OverheadX = X_with_collection_enabled - X_with_collection_disabled
  ```
- **Unit.** Seconds, cores/millicores, bytes, call count.
- **Collection.** Run matched pairs on an otherwise idle host (`methodology.md` §6).
  Operator CPU/memory from metrics or `kubectl top`; API calls from controller metrics
  or the apiserver audit log where available.
- **Serves.** RQ5.
- **Notes.** This metric measures the overhead of *failure evidence collection only*.
  The overhead of database inter-suite isolation (`spec.database.isolationMode`:
  `restore` vs `migration`) is **out of scope** here — it is measured in the companion
  PostgreSQL article `[postgres_isolation_companion]`. Do not attribute isolation cost
  to RQ5.

## 11. Reconcile Idempotency

- **Definition.** Whether repeated reconciliation of the same failed `Preview` produces
  a stable artifact with no duplication.
- **Measures.**
  - `duplicate_reports` — count of duplicate `FailureReport` artifacts for one failure
    (target: 0).
  - `duplicate_evidence_items` — count of evidence items with a colliding logical
    identity within one bundle (target: 0).
  - `status_stable` — boolean: the artifact is byte-identical (modulo timestamps)
    across N extra reconciles after the first capture.
- **Formula.**
  ```
  IdempotencyOK = (duplicate_reports == 0)
                  && (duplicate_evidence_items == 0)
                  && status_stable
  ```
- **Unit.** Counts and a boolean.
- **Collection.** After the first capture, trigger ≥3 extra reconciles (e.g. annotate
  the `Preview`) and diff the artifact. Evidence IDs must be deterministic for this to
  hold (see `failure-evidence-model.md`).
- **Serves.** RQ1 (artifact integrity); supports the Contribution 1/2 correctness
  claims and the unit tests in Phase 2.

---

## Metric → research-question map

| Metric | RQ1 | RQ2 | RQ3 | RQ4 | RQ5 |
|--------|:---:|:---:|:---:|:---:|:---:|
| 1 Evidence Preservation Rate | ● | | | | |
| 2 Snapshot Survival Rate | ● | | | | |
| 3 Top-1 RCA Accuracy | | ● | | | |
| 4 Top-3 RCA Accuracy | | ● | | | |
| 5 Mean Time To Diagnosis | | | ● | | |
| 6 Evidence Precision | | ● | | ● | |
| 7 Evidence Recall | ● | ● | | | |
| 8 Hallucination Rate | | | | ● | |
| 9 Recommendation Usefulness | | ● | | ● | |
| 10 Collection Overhead | | | | | ● |
| 11 Reconcile Idempotency | ● | | | | |

Database-isolation overhead and AI seed-data quality are **out of scope** for this
article; they are measured in the companion PostgreSQL article
`[postgres_isolation_companion]` (see `research-questions.md`, Scope).

---

## Reporting rules

- Report every metric with mean and a confidence interval over the cell's repetitions.
- Keep **Kind** and **AKS** results in separate tables; never pool them.
- Tie every reported number to its raw artifacts via the `(scenario_id, run_id,
  cluster_type, configuration)` key (`methodology.md` §7).
- State the LLM model and version for every LLM-in-the-loop result.
