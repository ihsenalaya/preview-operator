# Case study — s3-healthchecks / F7

**Family:** infrastructure  
**Ground truth:** component=`service` category=`infrastructure`  
**Bundle size:** 3196 bytes — **Evidence level:** C5  
**Detected at:** `2026-05-23T17:14:26Z`  

## Evidence items captured

| Type | Resource | Message (truncated) |
|---|---|---|
| JobLog | pod/postgres-migrate-pmpd5 container/migration | HINT: See https://github.com/healthchecks/healthchecks#sending-emails PASS migra… |
| KubernetesEvent | Pod/postgres-f6d5d944d-tl4cf | Pod/postgres-f6d5d944d-tl4cf: 0/3 nodes are available: 3 Insufficient cpu. no ne… |
| KubernetesEvent | Pod/svc-backend-5467f484cb-qp7rw | Pod/svc-backend-5467f484cb-qp7rw: Readiness probe failed: Get "http://10.244.2.9… |
| KubernetesEvent | Pod/postgres-migrate-pmpd5 | Pod/postgres-migrate-pmpd5: 0/3 nodes are available: 3 Insufficient cpu. no new … |
| PodLog | pod/svc-backend-5467f484cb-qp7rw container/backe | uWSGI running as root, you can use --uid/--gid/--chroot options *** WARNING: you… |
| PodLog | pod/postgres-f6d5d944d-tl4cf container/postgres | 2026-05-23 17:10:50.194 UTC [45] LOG:  database system was shut down at 2026-05-… |
| PreviewCondition | MigrationReady | status=True reason=JobAlreadySucceeded message=Database migration job already co… |
| PreviewCondition | SeedReady | status=True reason=TaskDisabled message=Database seed task is disabled |
| … | … | (`1` more items not shown) |

## C5 LLM diagnoses

### LLM-A

- component: `Kubernetes Node Scheduler`  
- category:  `infrastructure`  
- cause:     The preview environment failed primarily due to insufficient CPU resources on the nodes, which prevented the necessary pods from being scheduled.

### LLM-B

- component: `Kubernetes cluster resource allocation`  
- category:  `infrastructure`  
- cause:     Insufficient CPU resources are preventing Pods from being scheduled, leading to deployment failures and readiness probe issues.

## Commentary

Neither LLM matched ground truth strictly. Either the diagnosis names a symptom (test pod, downstream service) or uses a different vocabulary than the matcher expects.

_Rep `r1` is used as representative; the other 9 reps for this scenario are tabulated in the per-cell tables of `results-augmented.csv` and `results-multiapp/runs.csv`._
