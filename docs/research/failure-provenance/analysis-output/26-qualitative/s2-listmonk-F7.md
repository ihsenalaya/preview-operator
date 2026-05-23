# Case study — s2-listmonk / F7

**Family:** infrastructure  
**Ground truth:** component=`service` category=`infrastructure`  
**Bundle size:** 2625 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T17:10:37Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-qvc99 container/migration | Processing triggers for libc-bin (2.41-12+deb13u2) ... INSERT 0 3 PASS migration… |
| KubernetesEvent | Pod/svc-backend-5cfd5db674-mt79v | Pod/svc-backend-5cfd5db674-mt79v: Readiness probe failed: Get "http://10.244.2.6… |
| PodLog | pod/svc-backend-5cfd5db674-mt79v container/backe | 2026/05/23 17:09:34 init.go:593: media upload provider: filesystem 2026/05/23 17… |
| PodLog | pod/postgres-6584b7c445-ht78s container/postgres | done 2026-05-23 17:08:37.887 UTC [1] LOG:  listening on Unix socket "/var/run/po… |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-backend-5cfd5db674"… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |

## C5 LLM diagnoses

### LLM-A

- component: `database`  
- category:  `database`  
- cause:     The backend service failed to start because it could not connect to the database, which does not exist. This indicates a database setup issue.

### LLM-B

- component: `svc-backend`  
- category:  `database`  
- cause:     The backend service failed to start because it could not connect to the 'listmonk' database, which does not exist.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
