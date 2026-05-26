# Case study — s5-petclinic / F1

**Family:** database  
**Ground truth:** component=`migration-job` category=`database`  
**Bundle size:** 2197 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T16:17:39Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| KubernetesEvent | Pod/postgres-84ff6c7484-kjmtn | Pod/postgres-84ff6c7484-kjmtn: 0/2 nodes are available: 2 Insufficient cpu. no n… |
| KubernetesEvent | Job/postgres-migrate | Job/postgres-migrate: Job has reached the specified backoff limit |
| KubernetesEvent | Pod/postgres-migrate-9tz54 | Pod/postgres-migrate-9tz54: 0/2 nodes are available: 2 Insufficient cpu. no new … |
| PodLog | pod/postgres-84ff6c7484-kjmtn container/postgres | 2026-05-23 16:16:57.942 UTC [44] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | Ready | status=False reason=DatabaseMigrationFailed message=database migration job previ… |
| PreviewCondition | MigrationReady | status=False reason=JobRunning message=Database migration job postgres-migrate i… |

## C5 LLM diagnoses

### LLM-A

- component: `Kubernetes Cluster`  
- category:  `infrastructure`  
- cause:     The preview environment failed due to insufficient CPU resources available for the pods, which prevented the database migration job from running successfully.

### LLM-B

- component: `PostgreSQL Pod and Migration Job`  
- category:  `infrastructure`  
- cause:     Insufficient CPU resources on available nodes prevented the PostgreSQL pods from scheduling, causing the database migration job to fail after reaching its backoff limit.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
