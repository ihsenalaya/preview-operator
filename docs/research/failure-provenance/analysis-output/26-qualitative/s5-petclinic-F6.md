# Case study — s5-petclinic / F6

**Family:** infrastructure  
**Ground truth:** component=`app-deployment` category=`infrastructure`  
**Bundle size:** 2425 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T16:31:25Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-4w5sf container/migration | INFO  PetClinicApplication - Started PetClinicApplication in 76.581 seconds (pro… |
| KubernetesEvent | Pod/svc-probe-797f6c65b6-wpkx2 | Pod/svc-probe-797f6c65b6-wpkx2: Readiness probe failed: Get "http://10.244.3.174… |
| KubernetesEvent | Pod/svc-backend-755c99fc99-9bwqs | Pod/svc-backend-755c99fc99-9bwqs: Readiness probe failed: Get "http://10.244.3.2… |
| PodLog | pod/postgres-7f6bf68689-2lkr8 container/postgres | 2026-05-23 16:28:46.772 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-backend-755c99fc99"… |

## C5 LLM diagnoses

### LLM-A

- component: `svc-backend`  
- category:  `application`  
- cause:     The readiness probes for the backend service failed due to connection refusals, indicating that the service was not ready to accept traffic, likely due to a deployment issue.

### LLM-B

- component: `svc-backend`  
- category:  `configuration`  
- cause:     The backend service deployment failed to progress due to readiness probe failures, likely caused by a misconfiguration in the service's health check endpoint or network connectivity issues.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
