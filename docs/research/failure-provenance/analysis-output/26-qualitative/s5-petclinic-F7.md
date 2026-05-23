# Case study — s5-petclinic / F7

**Family:** infrastructure  
**Ground truth:** component=`service` category=`infrastructure`  
**Bundle size:** 2584 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T17:11:06Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-xm7ht container/migration | INFO  PetClinicApplication - Started PetClinicApplication in 80.098 seconds (pro… |
| KubernetesEvent | Pod/svc-backend-6564ffc779-4s25s | Pod/svc-backend-6564ffc779-4s25s: Readiness probe failed: Get "http://10.244.1.6… |
| PodLog | pod/postgres-679f7f7d9d-56knr container/postgres | 2026-05-23 17:08:25.238 UTC [44] LOG:  database system was shut down at 2026-05-… |
| PodLog | pod/svc-backend-6564ffc779-4s25s container/backe | INFO  ServletWebServerApplicationContext - Root WebApplicationContext: initializ… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-backend-6564ffc779"… |

## C5 LLM diagnoses

### LLM-A

- component: `svc-backend`  
- category:  `application`  
- cause:     The readiness probe for the backend service failed due to a connection refusal, indicating that the service was not ready to accept traffic, likely due to a failure in the deployment process.

### LLM-B

- component: `—`  
- category:  `—`  
- cause:     —

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
