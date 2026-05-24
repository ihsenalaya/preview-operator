# Evidence ladder L1 → L4 (S1 only, aligned top-1)

Controlled comparison addressing reviewer critique on RCA accuracy. The 
ladder isolates the contribution of *provenance structure* vs *typed evidence* vs *raw kubectl* vs *logs only*:

| Rung | What the diagnoser sees | Source |
|---|---|---|
| **L1** | Pod / job logs only | C1 of operator matrix |
| **L2** | Raw `kubectl get/logs/...` dumps (no operator artefact) | B0 vanilla-LLM baseline |
| **L3** | Full typed `FailureReport` bundle (no PROV graph) | C4 of operator matrix |
| **L4** | `FailureReport` + W3C-PROV provenance graph | C5 of operator matrix |

Each cell shows aligned top-1 with Wilson 95 % CI; n is the number of (scenario × rep) pairs.

## Aligned top-1 by rung × engine

| Rung | rule-grounded | llm-grounded | llm-freeform | B0 (vanilla LLM) |
|---|---|---|---|---|
| L1 (logs only) | 0.100 [0.055, 0.174] (n=100) | 0.140 [0.085, 0.221] (n=100) | 0.140 [0.085, 0.221] (n=100) | — |
| L2 (raw kubectl) | — | — | — | 0.040 [0.016, 0.099] (n=99) |
| L3 (FR, no prov.) | 0.580 [0.482, 0.672] (n=100) | 0.440 [0.347, 0.538] (n=100) | 0.360 [0.273, 0.458] (n=100) | — |
| L4 (FR + prov.) | 0.580 [0.482, 0.672] (n=100) | 0.480 [0.385, 0.577] (n=100) | 0.330 [0.246, 0.427] (n=100) | — |

## Reading

- **L1 → L2** is the *evidence-volume* effect alone (no operator-side 
  structuring). Each delta isolates *what richer raw evidence buys you* 
  before structuring.
- **L2 → L3** is the contribution of **operator-side capture + typed schema** 
  *without* PROV linkage. Reads off how much *structuring* (vs raw kubectl) buys.
- **L3 → L4** is the contribution of the **provenance graph** on top of the 
  typed bundle. Reads off how much *PROV linkage* (vs flat typed evidence) buys.
- L2 is reported under `engine = B0`, which is the vanilla LLM (`gpt-4o-mini`) 
  reading raw `kubectl` output. The operator engines (rule / llm-grounded / 
  llm-freeform) consume the operator's bundle at the appropriate level.

## Honesty notes (regime symmetry)

- L1, L3, and L4 are **post-teardown** (the operator persists the bundle in a 
  cluster-scoped CR; the diagnoser reads it offline). L2 (raw kubectl) is 
  captured **before** teardown but read offline — the artefacts are stored in 
  `results-matrix/F*/r*/artifacts/{events,pods,deployments,logs}.txt`. 
  So all four rungs operate on **frozen text** in the same regime; the 
  cluster is not interrogated again.
- The K8sGPT / Kagent comparators reported in §5.8 of the article run on the 
  *live* cluster and are kept as a separate practitioner contextualisation. 
  A post-teardown K8sGPT run on the same `evidenceItems` is queued as future 
  work (script: `28-k8sgpt-post-teardown.py`).

Source: `analysis-output/08-vocab-rescore/results-rescored.csv` (operator),
        `analysis-output/10-b0-report/results-b0.csv` (B0 baseline).
Script: `experiments/failure-provenance/analysis/30-evidence-ladder.py`.
