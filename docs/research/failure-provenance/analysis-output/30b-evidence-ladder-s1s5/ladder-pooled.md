# Evidence ladder L1 → L4 — pooled S1-S5 (aligned top-1)

Pooled across all five subjects (S1 flask-catalog, S2 listmonk, S3 healthchecks,
S4 umami, S5 petclinic). Wilson 95 % CI in brackets, n in parentheses.

| Rung | rule-grounded | llm-grounded | llm-freeform | B0 (vanilla LLM) |
|---|---|---|---|---|
| L1 (logs only) | 0.023 [0.012, 0.041] (n=440) | 0.034 [0.024, 0.047] (n=1000) | 0.027 [0.018, 0.040] (n=880) | — |
| L2 (raw kubectl) | — | — | — | 0.050 [0.025, 0.100] (n=139) |
| L3 (FR, no prov.) | 0.211 [0.176, 0.252] (n=440) | 0.186 [0.163, 0.211] (n=1000) | 0.163 [0.140, 0.188] (n=880) | — |
| L4 (FR + prov.) | 0.211 [0.176, 0.252] (n=440) | 0.187 [0.164, 0.212] (n=1000) | 0.157 [0.134, 0.182] (n=880) | — |

## Reading

- **L1 → L2** is the *evidence-volume* effect alone — moving from logs to raw kubectl bundles.
- **L2 → L3** is the *operator-side capture + typed schema* contribution (no PROV linkage).
- **L3 → L4** is the *PROV-graph* contribution on top of the typed bundle.
- B0 uses the FAIR baseline (vanilla LLM reads raw kubectl output, *not* operator
  evidenceItems formatted as kubectl-like text). S2-S5 B0 has only 40 rows (10/subject)
  because that was the manageable cost for the fair-baseline rerun.

## Per-subject breakdown

See `ladder-per-subject.csv` and `ladder-per-subject.png` for per-subject ladders.
Pooled numbers above weight subjects by their n (S1 has more captures → more weight).

## Honesty notes

- L1, L3, L4 are post-teardown reads of the operator bundle (offline scoring of the
  FailureReport CR contents) — uniform regime across S1-S5.
- L2 is also offline but consumes raw kubectl artefacts (events.txt, pods.txt, jobs.txt, logs/)
  captured before teardown via `collect-results.sh`. Same regime, different input shape.

## Source

- L1/L3/L4: `analysis-output/35-unified-rescore/per-subject-results.csv` (11600 rows)
- L2 (S1): `analysis-output/10-b0-report/results-b0.csv`
- L2 (S2-S5): `results-matrix/results-b0-multiapp-fair.csv`
- Script: `experiments/failure-provenance/analysis/30b-evidence-ladder-s1s5.py`
