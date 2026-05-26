# Case study — s4-umami / F6

**Family:** infrastructure  
**Ground truth:** component=`app-deployment` category=`infrastructure`  
**Bundle size:** 2069 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T16:30:23Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-qrlhf container/migration | └─ migration.sql └─ 07_add_tag/ All migrations have been successfully applied. P… |
| KubernetesEvent | Pod/svc-probe-946b87694-ksg9g | Pod/svc-probe-946b87694-ksg9g: Readiness probe failed: Get "http://10.244.2.110:… |
| PodLog | pod/postgres-65c797d9d8-bpz2l container/postgres | 2026-05-23 16:28:48.818 UTC [44] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-probe-946b87694" ha… |

## C5 LLM diagnoses

### LLM-A

- component: `svc-probe`  
- category:  `application`  
- cause:     The readiness probe for the svc-probe pod failed due to a connection refusal, indicating that the service was not ready to accept traffic, which led to the deployment timing out.

### LLM-B

- component: `svc-probe pod`  
- category:  `application`  
- cause:     The readiness probe for the svc-probe pod is failing due to a connection refusal on port 9090, indicating that the application is not listening on the expected port.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
