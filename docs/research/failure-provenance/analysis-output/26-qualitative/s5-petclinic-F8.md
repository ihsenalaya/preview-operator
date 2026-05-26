# Case study — s5-petclinic / F8

**Family:** observability  
**Ground truth:** component=`backend` category=`observability`  
**Bundle size:** 1847 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T18:36:31Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-8wss9 container/migration | INFO  PetClinicApplication - Started PetClinicApplication in 61.209 seconds (pro… |
| KubernetesEvent | Job/postgres-migrate | Job/postgres-migrate: Job has reached the specified backoff limit |
| PodLog | pod/postgres-848966c5dc-6q2x4 container/postgres | 2026-05-23 18:33:40.532 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | Ready | status=False reason=DatabaseMigrationFailed message=database migration job previ… |
| PreviewCondition | MigrationReady | status=False reason=JobRunning message=Database migration job postgres-migrate i… |

## C5 LLM diagnoses

### LLM-A

- component: `database migration job`  
- category:  `application`  
- cause:     The database migration job failed due to a syntax error in the migration script, which caused the job to reach its backoff limit and ultimately fail.

### LLM-B

- component: `database migration script`  
- category:  `application`  
- cause:     A syntax error in the migration script caused the database migration job to fail, leading to the job reaching its backoff limit and the preview environment failing.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
