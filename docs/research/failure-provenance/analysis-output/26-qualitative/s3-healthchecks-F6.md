# Case study — s3-healthchecks / F6

**Family:** infrastructure  
**Ground truth:** component=`app-deployment` category=`infrastructure`  
**Bundle size:** 2487 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T16:51:39Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-wdphd container/migration | HINT: See https://github.com/healthchecks/healthchecks#sending-emails PASS migra… |
| KubernetesEvent | Pod/svc-probe-575d64546b-jbn8z | Pod/svc-probe-575d64546b-jbn8z: Readiness probe failed: Get "http://10.244.2.53:… |
| PodLog | pod/postgres-5bfcffdb5f-pbp4t container/postgres | 2026-05-23 16:49:57.696 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PodLog | pod/svc-backend-7599965bfb-t6kfs container/backe | uWSGI running as root, you can use --uid/--gid/--chroot options *** WARNING: you… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| PreviewCondition | Ready | status=False reason=DeploymentFailed message=ReplicaSet "svc-probe-575d64546b" h… |

## C5 LLM diagnoses

### LLM-A

- component: `svc-probe`  
- category:  `application`  
- cause:     The readiness probe for the svc-probe pod failed due to a connection refusal, indicating that the service was not ready to accept traffic, which likely caused the deployment to time out.

### LLM-B

- component: `svc-probe pod`  
- category:  `application`  
- cause:     The readiness probe for the svc-probe pod is failing due to a connection refusal on port 9090, indicating that the application is not listening on the expected port or is not running correctly.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
