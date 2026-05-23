# B0 baseline vs operator engines — aligned top-1, per scenario

B0 = vanilla LLM (`gpt-4o-mini-2024-07-18`, temperature 0) prompted with raw kubectl output (events, pods, deployments, services, endpoints, jobs, container logs) collected from each failing preview namespace. No operator artifacts (FailureReport, evidenceRefs, reconcile events) are exposed to the model. This is the floor: what an SRE would get by pasting kubectl into a chat LLM.

The operator column is the pooled C1-C5 aligned top-1 across the best-performing engine per scenario (matcher: per-scenario alias table, identical to B0).

| Scenario | n (B0) | B0 aligned top-1 | B0 category top-1 | Best operator engine | Operator aligned top-1 | gap |
|---|---|---|---|---|---|---|
| F1 | 10 |  20.0% (2/10) |   0.0% (0/10) | rule-grounded | 100.0% (50/50) | +80 pp |
| F2 | 10 |   0.0% (0/10) |   0.0% (0/10) | llm-freeform |  68.0% (34/50) | +68 pp |
| F3 | 10 |   0.0% (0/10) |  90.0% (9/10) | rule-grounded |  80.0% (40/50) | +80 pp |
| F4 | 9 |   0.0% (0/9) |  55.6% (5/9) | rule-grounded |  60.0% (30/50) | +60 pp |
| F5 | 10 |   0.0% (0/10) |  30.0% (3/10) | rule-grounded |   0.0% (0/50) | +0 pp |
| F6 | 10 |   0.0% (0/10) |  20.0% (2/10) | rule-grounded |  54.0% (27/50) | +54 pp |
| F7 | 10 |  20.0% (2/10) |  10.0% (1/10) | rule-grounded |  54.0% (27/50) | +34 pp |
| F8 | 10 |   0.0% (0/10) |   0.0% (0/10) | rule-grounded |  60.0% (30/50) | +60 pp |
| F9 | 10 |   0.0% (0/10) |   0.0% (0/10) | rule-grounded |   0.0% (0/50) | +0 pp |
| F10 | 10 |   0.0% (0/10) |  30.0% (3/10) | rule-grounded |   0.0% (0/50) | +0 pp |

**Pooled B0 (n = 99):**
- strict top-1:   0/99 (  0.0%) — Wilson 95% CI ['  0.0%', '  3.7%']
- aligned top-1:  4/99 (  4.0%) — Wilson 95% CI ['  1.6%', '  9.9%']
- category top-1: 23/99 ( 23.2%) — Wilson 95% CI [' 16.0%', ' 32.5%']

**Failure-mode observation.** Across the 99 B0 calls, the vanilla LLM systematically blames the *symptom-bearing* object — typically a test pod (`e2e-tests`, `microcks-import`, `ai-tests`) or the database pod (`postgres`) — instead of the *upstream component* at fault. F4 (broken backend route) is blamed on `postgres` 8/9 times; F8 (latency in backend) is blamed on `e2e-tests` 6/10 times; F9 (bad seed data) is blamed on `e2e-tests` 9/10 times. The operator's FailureReport closes this gap by linking each test-suite failure to its provenance — the changed file, the originating workload, the SQL or HTTP error in the upstream pod's log — which is the evidence the LLM needs to land on the right component.

_Cost: ~USD 0.10 for the full B0 baseline (99 calls). Model: gpt-4o-mini-2024-07-18 via Azure OpenAI, temperature 0._
