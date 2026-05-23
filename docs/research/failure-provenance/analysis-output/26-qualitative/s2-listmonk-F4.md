# Case study — s2-listmonk / F4

**Family:** application  
**Ground truth:** component=`backend` category=`application`  
**Bundle size:** 2164 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T18:29:27Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-9h44z container/migration | Processing triggers for libc-bin (2.41-12+deb13u2) ... INSERT 0 3 PASS migration… |
| KubernetesEvent | Job/postgres-migrate | Job/postgres-migrate: Job has reached the specified backoff limit |
| KubernetesEvent | Pod/postgres-migrate-9h44z | Pod/postgres-migrate-9h44z: 0/2 nodes are available: 2 Insufficient cpu. no new … |
| PodLog | pod/postgres-6c6dc4b876-q9k9x container/postgres | 2026-05-23 18:27:49.993 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | Ready | status=False reason=DatabaseMigrationFailed message=database migration job previ… |
| PreviewCondition | MigrationReady | status=False reason=JobRunning message=Database migration job postgres-migrate i… |

## C5 LLM diagnoses

### LLM-A

- component: `Kubernetes Job`  
- category:  `infrastructure`  
- cause:     The database migration job failed due to insufficient CPU resources on the nodes, which prevented the job from running successfully.

### LLM-B

- component: `Database migration script`  
- category:  `application`  
- cause:     The database migration job failed due to a syntax error in the migration script, causing the job to reach its backoff limit.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
