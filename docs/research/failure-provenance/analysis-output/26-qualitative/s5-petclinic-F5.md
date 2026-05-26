# Case study — s5-petclinic / F5

**Family:** application  
**Ground truth:** component=`frontend` category=`application`  
**Bundle size:** 2933 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T20:15:11Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-cwm5l container/migration | INFO  PetClinicApplication - Started PetClinicApplication in 76.739 seconds (pro… |
| KubernetesEvent | Pod/svc-probe-6745bb575-4wb7c | Pod/svc-probe-6745bb575-4wb7c: 0/4 nodes are available: 4 Insufficient cpu. no n… |
| KubernetesEvent | Pod/svc-backend-544c4df6c8-jvdxw | Pod/svc-backend-544c4df6c8-jvdxw: Readiness probe failed: Get "http://10.244.0.7… |
| PodLog | pod/svc-backend-544c4df6c8-jvdxw container/backe | INFO  ServletWebServerApplicationContext - Root WebApplicationContext: initializ… |
| PodLog | pod/postgres-cd8844db4-lhxwz container/postgres | 2026-05-23 20:12:18.076 UTC [46] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-probe-6745bb575" ha… |

## C5 LLM diagnoses

### LLM-A

- component: `Kubernetes Scheduler`  
- category:  `infrastructure`  
- cause:     The preview environment failed primarily due to insufficient CPU resources, which prevented the svc-probe pod from being scheduled, leading to a timeout in the deployment process.

### LLM-B

- component: `Kubernetes cluster resource allocation`  
- category:  `infrastructure`  
- cause:     Insufficient CPU resources are preventing pods from being scheduled, leading to deployment failures and readiness probe issues.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
