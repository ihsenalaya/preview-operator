# Case study — s1-flask-catalog / F10

**Family:** test-reliability  
**Ground truth:** component=`test-suite` category=`test-reliability`  
**Bundle size:** 4176 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T11:11:06Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| ChangedFile | tests/e2e.py | type=backend |
| GitDiff | ihsenalaya/idp-preview | diff stored in ConfigMap "preview-diff-pr-10001" (key diff.patch) |
| PreviewCondition | AIEnrichmentReady | status=True reason=AIEnrichmentCompleted message=AI enrichment seed=Succeeded, t… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| … | … | (`5` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `backend`  
- category:  `application`  
- cause:     The failure in the preview environment is primarily due to the contract test failing with an HTTP 500 error, indicating an internal server error when submitting a request. This suggests an issue within the application logic or backend service handling the contract test.

### LLM-B

- component: `e2e test suite`  
- category:  `test-reliability`  
- cause:     The failure is likely due to changes in the e2e test suite, specifically in the tests/e2e.py file, which introduced timeouts in the e2e tests and a 500 Internal Server Error in the contract tests.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
