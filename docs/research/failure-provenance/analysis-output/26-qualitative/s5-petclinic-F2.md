# Case study — s5-petclinic / F2

**Family:** configuration  
**Ground truth:** component=`app-deployment` category=`configuration`  
**Bundle size:** 2465 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T16:21:16Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-dq8wh container/migration | INFO  PetClinicApplication - Started PetClinicApplication in 65.39 seconds (proc… |
| KubernetesEvent | Pod/svc-backend-55ff5f457f-kv5rq | Pod/svc-backend-55ff5f457f-kv5rq: Readiness probe failed: Get "http://10.244.0.1… |
| KubernetesEvent | Pod/svc-backend-55ff5f457f-kv5rq | Pod/svc-backend-55ff5f457f-kv5rq: 0/3 nodes are available: 3 Insufficient cpu. n… |
| PodLog | pod/postgres-8699696565-zgkpw container/postgres | 2026-05-23 16:18:43.332 UTC [44] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-backend-55ff5f457f"… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |

## C5 LLM diagnoses

### LLM-A

- component: `svc-backend`  
- category:  `infrastructure`  
- cause:     The readiness probe for the backend service failed due to insufficient CPU resources, preventing the pod from starting successfully.

### LLM-B

- component: `—`  
- category:  `—`  
- cause:     —

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
