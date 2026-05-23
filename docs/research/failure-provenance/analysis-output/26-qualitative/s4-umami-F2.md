# Case study — s4-umami / F2

**Family:** configuration  
**Ground truth:** component=`app-deployment` category=`configuration`  
**Bundle size:** 2680 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T16:21:04Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| PreviewCondition | TestSuiteReady | status=False reason=TestSuiteFailed message=smoke=Failed(1p/3f), regression=Fail… |
| TestResult | regression | phase=Failed passed=2 failed=2 PASS regression run_log_clean PASS regression hea… |
| TestResult | smoke | phase=Failed passed=1 failed=3 PASS smoke healthz FAIL smoke login: Expecting va… |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `application`  
- category:  `application`  
- cause:     The test suite failed due to multiple test failures, specifically related to the endpoints returning unexpected values, indicating potential issues with the application logic or API responses.

### LLM-B

- component: `Application API`  
- category:  `application`  
- cause:     Multiple test suites (smoke, regression, e2e) failed with specific endpoint errors, indicating a potential issue with the application's API responses.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
