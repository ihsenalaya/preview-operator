# Scoring Rubric

This rubric defines how to score each experimental run so that results are
reproducible and inter-rater agreement can be measured. Apply it consistently;
where resources allow, use two independent annotators and report Cohen's κ.

All metric definitions are in `docs/research/failure-provenance/metrics.md`. This
document is the *annotation procedure* for the subjective parts.

---

## 1. RCA correctness (Top-1 / Top-3)

A diagnosis is **correct** only if it satisfies **all three** conditions, scored
against the scenario's `ground_truth` in `scenarios.yaml`:

1. **Component** — names the correct component (e.g. `migration-job`).
2. **Category** — names the correct category (`database`, `configuration`,
   `infrastructure`, `application`, `observability`, `test-reliability`).
3. **Evidence** — references at least one evidence item that genuinely supports
   the cause (a valid `FailureEvidenceItem.ID` present in the bundle).

Scoring:

- `top1_correct = 1` if the **first** ranked hypothesis is correct, else `0`.
- `top3_correct = 1` if **any** of the first three ranked hypotheses is correct,
  else `0`. Fewer than three hypotheses: empty slots count as incorrect.

Partial matches (right component, wrong category — or no evidence reference) score
`0` but are recorded in the `notes` column for the discussion section.

---

## 2. Evidence precision

For the evidence items the bundle returned, judge each as **relevant** or
**not relevant** to the true root cause:

- *Relevant* — the item genuinely helps identify the true component/category.
- *Not relevant* — unrelated noise for this scenario.

```
evidence_precision = relevant_items_returned / total_items_returned
```

Tie-breaking guidance: an item is relevant if a competent engineer would cite it
when explaining the true cause. An item that is merely *present* but plays no part
in the explanation is not relevant.

---

## 3. Evidence recall

Against the scenario's `expected_*_evidence` fields in `scenarios.yaml` (the items
a complete bundle should contain):

```
evidence_recall = expected_relevant_items_captured / expected_relevant_items_total
```

An expected item that is inherently absent for the scenario (e.g. container logs in
F3, where the container never starts) is **excluded from the denominator** — do not
penalise the bundle for evidence that cannot exist.

---

## 4. Hallucination rate

Decompose the diagnosis text into **atomic claims** (one factual assertion each).
Classify every claim:

- **Supported** — the claim references an evidence item ID that exists in the
  bundle *and* that item genuinely backs the claim.
- **Unsupported (hallucinated)** — the claim references no evidence ID, OR
  references an ID not present in the bundle, OR references an item that does not
  actually support it.

```
hallucination_rate = unsupported_claims / total_claims
```

Negative control: in **F10 (flaky test)**, any claim that confidently blames a
real infrastructure or application component is *unsupported by construction* —
there is no such cause. A non-zero hallucination rate is expected and correct here
only if the system over-claims; a system that correctly reports "flaky / no stable
cause" scores `0`.

---

## 5. Recommendation usefulness

Score the recommendation attached to the diagnosis. Use **one** scale for the whole
study and state which in the paper.

**Binary (recommended for clarity)** — `recommendation_score` ∈ {0, 1}:

- `0` — not actionable, generic, or not linked to any captured evidence.
- `1` — actionable *and* directly linked to a specific captured evidence item.

**Likert (use only if a finer comparison is needed)** — `recommendation_score` ∈ {1..5}:

| Score | Meaning |
|-------|---------|
| 1 | Useless or misleading |
| 2 | Vague; not actionable as written |
| 3 | Plausible but generic; not evidence-linked |
| 4 | Actionable; weakly linked to evidence |
| 5 | Directly actionable, specific, and evidence-linked |

---

## 6. Filling one CSV row

One run → one row in a copy of `results-template.csv`:

| Column | Source |
|--------|--------|
| `scenario_id`, `run_id`, `cluster_type`, `configuration` | run identity |
| `failure_detected_at`, `diagnosis_available_at` | operator timestamps |
| `mttd_seconds` | `diagnosis_available_at − failure_detected_at` |
| `top1_correct`, `top3_correct` | §1 |
| `evidence_precision`, `evidence_recall` | §2, §3 |
| `hallucination_rate` | §4 |
| `recommendation_score` | §5 |
| `bundle_size_bytes`, `storage_overhead` | size of the persisted FailureReport |
| `cpu_overhead`, `memory_overhead` | operator resource delta (collection on − off) |
| `namespace_deleted` | `true` once teardown completed |
| `evidence_survived` | `true` if the FailureReport was readable after teardown |
| `notes` | partial matches, anomalies, model/version, anything noteworthy |

Always record the LLM model and version in `notes` for LLM-in-the-loop runs.

---

## 7. Inter-rater agreement

For the subjective metrics (§1 evidence check, §2, §4, §5): have two annotators
score an overlapping sample independently, compute Cohen's κ, and report it. If κ
is low, refine this rubric and re-score before reporting any result.
