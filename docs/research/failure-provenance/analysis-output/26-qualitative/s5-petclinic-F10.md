# Case study — s5-petclinic / F10

**Family:** test-reliability  
**Ground truth:** component=`test-suite` category=`test-reliability`  
**Bundle size:** 2579 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T21:49:54Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-k9hdf container/migration | INFO  PetClinicApplication - Started PetClinicApplication in 65.107 seconds (pro… |
| KubernetesEvent | Pod/svc-backend-6967c559d5-7phcx | Pod/svc-backend-6967c559d5-7phcx: Readiness probe failed: Get "http://10.244.0.1… |
| PodLog | pod/svc-backend-6967c559d5-7phcx container/backe | INFO  ServletWebServerApplicationContext - Root WebApplicationContext: initializ… |
| PodLog | pod/postgres-7d84d888c6-xjww7 container/postgres | 2026-05-23 21:47:03.662 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-backend-6967c559d5"… |

## C5 LLM diagnoses

### LLM-A

- component: `svc-backend`  
- category:  `application`  
- cause:     The readiness probe for the backend service failed due to a connection refusal, indicating that the service was not ready to accept traffic, likely due to a failure in the application startup process.

### LLM-B

- component: `svc-backend`  
- category:  `configuration`  
- cause:     The backend service is failing its readiness probe due to a connection refusal on port 9966, likely caused by a misconfiguration in the service or pod networking.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
