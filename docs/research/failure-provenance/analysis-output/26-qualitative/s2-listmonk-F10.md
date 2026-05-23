# Case study — s2-listmonk / F10

**Family:** test-reliability  
**Ground truth:** component=`test-suite` category=`test-reliability`  
**Bundle size:** 3183 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T21:50:08Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| PreviewCondition | TestSuiteReady | status=False reason=TestSuiteFailed message=smoke=Failed(3p/3f), regression=Fail… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| TestResult | regression | phase=Failed passed=4 failed=7 PASS regression run_log_clean PASS regression hea… |
| TestResult | smoke | phase=Failed passed=3 failed=3 PASS smoke healthz FAIL smoke lists_get: not 200 … |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `application`  
- category:  `application`  
- cause:     The test suite failed due to multiple test failures, including critical errors such as status 500 responses and incorrect response shapes, indicating potential issues in the application logic or API.

### LLM-B

- component: `Application API`  
- category:  `application`  
- cause:     Multiple test suites (smoke, regression, e2e) failed with specific API endpoint errors (e.g., status 500, bad response shape), indicating a potential issue with the application's API handling or configuration.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
