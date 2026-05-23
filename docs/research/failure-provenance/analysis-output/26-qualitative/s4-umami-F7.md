# Case study — s4-umami / F7

**Family:** infrastructure  
**Ground truth:** component=`service` category=`infrastructure`  
**Bundle size:** 2079 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T17:10:06Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-lcj2m container/migration | └─ migration.sql └─ 07_add_tag/ All migrations have been successfully applied. P… |
| KubernetesEvent | Pod/svc-backend-64bd965947-vqlwf | Pod/svc-backend-64bd965947-vqlwf: Readiness probe failed: Get "http://10.244.0.1… |
| PodLog | pod/postgres-694ddf65f-xwcxc container/postgres | 2026-05-23 17:08:34.456 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-backend-64bd965947"… |

## C5 LLM diagnoses

### LLM-A

- component: `svc-backend`  
- category:  `application`  
- cause:     The readiness probe for the backend service failed, indicating that the service was not able to start properly, likely due to a misconfiguration or an issue in the application code.

### LLM-B

- component: `svc-backend`  
- category:  `application`  
- cause:     The backend service deployment failed due to a readiness probe failure, likely caused by the backend service being unable to connect to the database or another required service.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
