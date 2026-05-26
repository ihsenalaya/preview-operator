# Case study — s2-listmonk / F6

**Family:** infrastructure  
**Ground truth:** component=`app-deployment` category=`infrastructure`  
**Bundle size:** 2065 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T14:45:01Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-9ffsd container/migration | Processing triggers for libc-bin (2.41-12+deb13u2) ... INSERT 0 3 PASS migration… |
| KubernetesEvent | Pod/svc-probe-67966849f8-msnbv | Pod/svc-probe-67966849f8-msnbv: Readiness probe failed: Get "http://10.244.1.172… |
| PodLog | pod/postgres-98448bb58-l8zxm container/postgres | 2026-05-23 14:43:50.127 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-probe-67966849f8" h… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |

## C5 LLM diagnoses

### LLM-A

- component: `svc-probe`  
- category:  `infrastructure`  
- cause:     The readiness probe for the svc-probe pod failed due to a connection refusal, indicating that the service was not ready to accept traffic, which caused the deployment to time out.

### LLM-B

- component: `svc-probe pod`  
- category:  `application`  
- cause:     The readiness probe for the svc-probe pod is failing due to a connection refusal on port 9090, indicating that the application is not listening on the expected port or is not running correctly.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
