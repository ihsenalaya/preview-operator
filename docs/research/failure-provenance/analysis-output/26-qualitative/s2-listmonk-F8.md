# Case study — s2-listmonk / F8

**Family:** observability  
**Ground truth:** component=`backend` category=`observability`  
**Bundle size:** 3118 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T19:12:38Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-5kvsc container/migration | Processing triggers for libc-bin (2.41-12+deb13u2) ... INSERT 0 3 PASS migration… |
| KubernetesEvent | Job/postgres-migrate | Job/postgres-migrate: Job has reached the specified backoff limit |
| PodLog | pod/svc-backend-6d856f6756-6nqjr container/backe | 2026/05/23 19:12:10 manager.go:498: error fetching campaigns: pq: cached plan mu… |
| PodLog | pod/postgres-dcffc7f68-72pbv container/postgres | 2026-05-23 19:12:10.710 UTC [75] ERROR:  cached plan must not change result type… |
| PreviewCondition | Ready | status=False reason=DatabaseMigrationFailed message=database migration job previ… |
| PreviewCondition | MigrationReady | status=False reason=JobRunning message=Database migration job postgres-migrate i… |

## C5 LLM diagnoses

### LLM-A

- component: `database migration job`  
- category:  `database`  
- cause:     The database migration job failed due to a syntax error in the migration script, which caused the job to reach its backoff limit and ultimately fail.

### LLM-B

- component: `database migration script`  
- category:  `database`  
- cause:     A database migration script contains a syntax error, causing the migration job to fail and reach its backoff limit, which in turn prevents the preview environment from becoming ready.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
