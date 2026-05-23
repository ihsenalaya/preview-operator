# Case study — s2-listmonk / F2

**Family:** configuration  
**Ground truth:** component=`app-deployment` category=`configuration`  
**Bundle size:** 2472 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T14:36:43Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| PreviewCondition | TestSuiteReady | status=False reason=TestSuiteFailed message=smoke=Failed(1p/4f), regression=Fail… |
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| TestResult | smoke | phase=Failed passed=1 failed=4 PASS smoke healthz FAIL smoke lists_get: not 200 … |
| TestResult | regression | phase=Failed passed=2 failed=0 PASS regression run_log_clean PASS regression hea… |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `application`  
- category:  `application`  
- cause:     The test suite failed due to multiple smoke tests returning a 200 status code, indicating that the application may not be functioning correctly in the preview environment.

### LLM-B

- component: `Test Suite`  
- category:  `test-reliability`  
- cause:     The preview environment failed due to multiple test suite failures, specifically in the smoke, regression, and e2e tests, indicating potential issues with the application's functionality.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
