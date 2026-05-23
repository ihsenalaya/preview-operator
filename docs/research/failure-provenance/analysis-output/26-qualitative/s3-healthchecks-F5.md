# Case study — s3-healthchecks / F5

**Family:** application  
**Ground truth:** component=`frontend` category=`application`  
**Bundle size:** 2876 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T20:15:00Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| PreviewCondition | TestSuiteReady | status=False reason=TestSuiteFailed message=smoke=Failed(3p/1f), regression=Succ… |
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| TestResult | regression | phase=Succeeded passed=9 failed=0 PASS regression run_log_clean PASS regression … |
| TestResult | e2e | phase=Succeeded passed=8 failed=0 PASS e2e run_log_clean PASS e2e entity_count_m… |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `frontend`  
- category:  `application`  
- cause:     The smoke test failed due to a 500 status error in the frontend, indicating a potential issue with the application code or configuration.

### LLM-B

- component: `frontend`  
- category:  `application`  
- cause:     The smoke test suite failed due to a 500 error on the frontend_root endpoint, indicating a potential issue with the application's frontend component.

## Commentary

Both LLMs hit the ground-truth component.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
