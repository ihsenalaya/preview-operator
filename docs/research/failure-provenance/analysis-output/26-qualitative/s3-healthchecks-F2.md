# Case study — s3-healthchecks / F2

**Family:** configuration  
**Ground truth:** component=`app-deployment` category=`configuration`  
**Bundle size:** 2786 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T15:50:46Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Approved | status=True reason=Approved message=Approved by:  |
| PreviewCondition | TestSuiteReady | status=False reason=TestSuiteFailed message=smoke=Failed(1p/2f), regression=Fail… |
| PreviewCondition | DatabaseReady | status=True reason=PostgreSQLProvisioned message=PostgreSQL 15 running — credent… |
| PreviewCondition | Ready | status=True reason=AllResourcesReady message=Preview environment running at http… |
| TestResult | smoke | phase=Failed passed=1 failed=2 PASS smoke healthz FAIL smoke checks_list: HTTPCo… |
| TestResult | regression | phase=Failed passed=2 failed=2 PASS regression run_log_clean PASS regression hea… |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `svc-backend`  
- category:  `application`  
- cause:     The preview environment failed due to timeouts when attempting to connect to the backend service, indicating potential issues with service availability or configuration.

### LLM-B

- component: `svc-backend`  
- category:  `application`  
- cause:     The application is experiencing timeouts when connecting to the backend service, likely due to misconfigured or overloaded backend service.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
