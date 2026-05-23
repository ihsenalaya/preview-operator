# Case study — s3-healthchecks / F10

**Family:** test-reliability  
**Ground truth:** component=`test-suite` category=`test-reliability`  
**Bundle size:** 2989 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T21:50:07Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | TestSuiteReady | status=False reason=TestSuiteFailed message=smoke=Failed(3p/1f), regression=Fail… |
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| TestResult | e2e | phase=Failed passed=3 failed=5 PASS e2e run_log_clean PASS e2e entity_count_matc… |
| TestResult | smoke | phase=Failed passed=3 failed=1 PASS smoke healthz FAIL smoke checks_list: not 20… |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `Test Suite`  
- category:  `application`  
- cause:     The test suite failed due to multiple errors in the end-to-end (e2e) tests, indicating potential issues with the application logic or configuration.

### LLM-B

- component: `check management service`  
- category:  `application`  
- cause:     Multiple test suites (smoke, regression, e2e) failed with specific errors related to check creation and deletion, indicating a potential issue with the application's check management functionality.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
