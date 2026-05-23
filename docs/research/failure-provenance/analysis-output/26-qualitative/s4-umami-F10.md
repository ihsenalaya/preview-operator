# Case study — s4-umami / F10

**Family:** test-reliability  
**Ground truth:** component=`test-suite` category=`test-reliability`  
**Bundle size:** 2960 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T21:48:47Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| PreviewCondition | TestSuiteReady | status=False reason=TestSuiteFailed message=smoke=Failed(2p/3f), regression=Fail… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| TestResult | smoke | phase=Failed passed=2 failed=3 PASS smoke healthz FAIL smoke login: login failed… |
| TestResult | regression | phase=Failed passed=5 failed=3 PASS regression run_log_clean PASS regression hea… |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `Test Suite`  
- category:  `test-reliability`  
- cause:     The test suite failed due to multiple test failures across smoke, regression, and e2e tests, indicating potential issues with the application functionality or configuration.

### LLM-B

- component: `Application`  
- category:  `application`  
- cause:     The application is failing multiple test cases across smoke, regression, and e2e test suites, indicating a potential issue with the application code or configuration introduced in the pull request.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
