# Case study — s4-umami / F4

**Family:** application  
**Ground truth:** component=`backend` category=`application`  
**Bundle size:** 2154 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T18:28:41Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-mhjmd container/migration | 7 migrations found in prisma/migrations No pending migrations to apply. PASS mig… |
| KubernetesEvent | Job/postgres-migrate | Job/postgres-migrate: Job has reached the specified backoff limit |
| KubernetesEvent | Pod/postgres-migrate-kjzkl | Pod/postgres-migrate-kjzkl: 0/2 nodes are available: 2 Insufficient cpu. no new … |
| PodLog | pod/postgres-7bf76dbc46-hjkfp container/postgres | 2026-05-23 18:27:50.092 UTC [43] LOG:  database system is ready to accept connec… |
| PreviewCondition | Ready | status=False reason=DatabaseMigrationFailed message=database migration job previ… |
| PreviewCondition | MigrationReady | status=False reason=JobRunning message=Database migration job postgres-migrate i… |

## C5 LLM diagnoses

### LLM-A

- component: `Kubernetes Job`  
- category:  `infrastructure`  
- cause:     The database migration job failed due to insufficient CPU resources, which prevented the job from running successfully.

### LLM-B

- component: `postgres-migrate Job`  
- category:  `infrastructure`  
- cause:     The database migration job failed due to insufficient CPU resources, preventing the pod from scheduling and causing the job to reach its backoff limit.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
