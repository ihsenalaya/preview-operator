# Case study — s3-healthchecks / F4

**Family:** application  
**Ground truth:** component=`backend` category=`application`  
**Bundle size:** 2051 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T19:46:33Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-95fb5 container/migration | No migrations to apply. Your models in app(s): 'api' have changes that are not y… |
| KubernetesEvent | Job/postgres-migrate | Job/postgres-migrate: Job has reached the specified backoff limit |
| PodLog | pod/postgres-f984ddd69-pj669 container/postgres | 2026-05-23 19:45:39.389 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | Ready | status=False reason=DatabaseMigrationFailed message=database migration job previ… |
| PreviewCondition | MigrationReady | status=False reason=JobRunning message=Database migration job postgres-migrate i… |

## C5 LLM diagnoses

### LLM-A

- component: `database migration job`  
- category:  `database`  
- cause:     The database migration job failed because there are changes in the application models that have not been reflected in a migration, leading to the job reaching its backoff limit.

### LLM-B

- component: `Database migration script`  
- category:  `application`  
- cause:     The database migration job failed due to a syntax error in the migration script, causing the job to reach its backoff limit.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
