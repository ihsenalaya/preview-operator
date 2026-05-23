# Case study — s3-healthchecks / F1

**Family:** database  
**Ground truth:** component=`migration-job` category=`database`  
**Bundle size:** 1492 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T15:46:28Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| KubernetesEvent | Job/postgres-migrate | Job/postgres-migrate: Job has reached the specified backoff limit |
| PodLog | pod/postgres-559b67d86-jdgmr container/postgres | 2026-05-23 15:45:47.974 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | MigrationReady | status=False reason=JobRunning message=Database migration job postgres-migrate i… |
| PreviewCondition | Ready | status=False reason=DatabaseMigrationFailed message=database migration job previ… |

## C5 LLM diagnoses

### LLM-A

- component: `postgres-migrate job`  
- category:  `application`  
- cause:     The database migration job 'postgres-migrate' failed to complete successfully and reached the specified backoff limit, indicating a problem with the migration process.

### LLM-B

- component: `PostgreSQL database migration job`  
- category:  `database`  
- cause:     The database migration job failed due to reaching the specified backoff limit, likely caused by repeated shutdowns of the PostgreSQL database system.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
