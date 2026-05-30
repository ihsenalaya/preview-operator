# RBAC Design — Bounded Blast Radius

## The security argument

The AI test-strategist agent runs in the cluster with a ServiceAccount named
`kagent-test-strategist`. This document explains why compromising this agent
cannot break production environments or other tenants.

---

## What the agent CAN do

```
get/list/watch:        previews, testplans, reconcileevents
create/update/patch:   testplans, testplans/status
```

That is all. Enforced by Kubernetes RBAC, not by code trust or agent "good behavior."

---

## What the agent CANNOT do

| Resource | Agent access |
|---|---|
| apps/deployments | ❌ none |
| batch/jobs | ❌ none |
| core/pods | ❌ none |
| core/secrets | ❌ none |
| core/configmaps | ❌ none |
| core/namespaces | ❌ none |
| testruns | ❌ none (write) — controller-only |
| reconcileevents | ❌ none (write) — controller-only |

---

## Bounded blast radius

An attacker who compromises the agent binary or its AI model context can at worst:

1. **Write a malicious TestPlan** (e.g., `canSkip: [migration]` to skip a critical migration test).
2. **What the controller does**: validates the plan. If structurally invalid (`mustRun ∩ canSkip ≠ ∅`), marks Rejected and falls back to FullSuite. If confidence is below threshold (default 70), marks Rejected and falls back to FullSuite.
3. **Result**: worst case is the controller runs the full test suite rather than the selected one. No workloads are modified. No data is exfiltrated (the agent has no Secret access).

This is the core architectural property: **blast radius is bounded by the controller's policy, not by the agent's good behavior**.

---

## Verification

To verify the boundary is enforced:

```bash
# Should be denied
kubectl auth can-i create deployments \
  --as=system:serviceaccount:kagent-system:kagent-test-strategist
# → no

kubectl auth can-i create jobs \
  --as=system:serviceaccount:kagent-system:kagent-test-strategist
# → no

kubectl auth can-i get secrets \
  --as=system:serviceaccount:kagent-system:kagent-test-strategist
# → no

# Should be allowed
kubectl auth can-i create testplans.platform.company.io \
  --as=system:serviceaccount:kagent-system:kagent-test-strategist
# → yes

kubectl auth can-i get previews.platform.company.io \
  --as=system:serviceaccount:kagent-system:kagent-test-strategist
# → yes
```

---

## Controller ServiceAccount

The controller (Deployment and ServiceAccount `controller-manager`, bound to ClusterRole `manager-role`) has full
reconcile permissions on all new CRDs (TestPlan, TestRun, ReconcileEvent) plus
the existing workload permissions (Deployment, Job, Service, Namespace, etc.).

The controller intentionally imports NO LLM SDK and makes NO HTTP calls to AI
services. Its code is fully auditable by reading `internal/controller/`.

---

## Why this architecture matters for KubeCon

Traditional AI integrations often give the AI direct API access to mutate
infrastructure — a deployment, a config, a secret. If the AI hallucinates or
is compromised, the blast radius is unbounded.

In this architecture, the AI writes declarative intent (a `TestPlan`) and the
controller enforces policy before acting. The AI is one more reconciler in the
system — treated with the same skepticism as any external input.

> "We don't trust the AI more than we trust any other API client."
