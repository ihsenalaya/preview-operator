# Preview Operator — High-Level Design (HLD)

A comprehensive visual architecture of the Preview Operator ecosystem, including Custom Resources, kagent Agents, MCP Servers, and GitHub integration.

---

## 🏗️ System Overview

```
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                                   KUBERNETES CLUSTER                                  │
│                                                                                       │
│  ┌──────────────────────────────────────────────────────────────────────────────┐   │
│  │                        PREVIEW OPERATOR CONTROL PLANE                        │   │
│  │                                                                              │   │
│  │  ┌─────────────────────────────────────────────────────────────────────┐   │   │
│  │  │  preview-operator Deployment (preview-operator-system namespace)    │   │   │
│  │  │                                                                     │   │   │
│  │  │  ┌─────────────────────────────────────────────────────────────┐   │   │   │
│  │  │  │  Reconciliation Loop (controller-runtime)                   │   │   │   │
│  │  │  │                                                             │   │   │   │
│  │  │  │  Watches: Preview ──────────────────────────────────────┐  │   │   │   │
│  │  │  │              TestPlan                                  │  │   │   │   │
│  │  │  │              TestRun                                   │  │   │   │   │
│  │  │  │              ReconcileEvent                            │  │   │   │   │
│  │  │  │                                                        ▼  │   │   │   │
│  │  │  │              ┌────────────────────────────────────────┐   │   │   │   │
│  │  │  │              │   Reconcile(Preview)                   │   │   │   │   │
│  │  │  │              │                                        │   │   │   │   │
│  │  │  │              │  1. Create Namespace                  │   │   │   │   │
│  │  │  │              │  2. Provision PostgreSQL              │   │   │   │   │
│  │  │  │              │  3. Deploy Services                   │   │   │   │   │
│  │  │  │              │  4. Run Tests (via TestPlan)          │   │   │   │   │
│  │  │  │              │  5. Create TestRun + ReconcileEvents  │   │   │   │   │
│  │  │  │              │  6. AI Enrichment + Failure Analysis  │   │   │   │   │
│  │  │  │              └────────────────────────────────────────┘   │   │   │   │
│  │  │  │                                                             │   │   │   │
│  │  │  └─────────────────────────────────────────────────────────────┘   │   │   │
│  │  │                                                                      │   │   │
│  │  └──────────────────────────────────────────────────────────────────────┘   │   │
│  │                                                                              │   │
│  └──────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                       │
│  ┌──────────────────────────────────────────────────────────────────────────────┐   │
│  │                          CUSTOM RESOURCES (CRDs)                             │   │
│  │                                                                              │   │
│  │  Cluster-Scoped:           Namespaced:                                      │   │
│  │  ┌──────────────────┐      ┌──────────────────────────────┐                │   │
│  │  │ 🎯 Preview      │      │ 📋 TestPlan                  │                │   │
│  │  │ (identity,      │      │ (mustRun/shouldRun/canSkip)  │                │   │
│  │  │  lifecycle,     │      └──────────────────────────────┘                │   │
│  │  │  config)        │      ┌──────────────────────────────┐                │   │
│  │  └──────────────────┘      │ 🧪 TestRun                   │                │   │
│  │  ┌──────────────────┐      │ (test results, immutable)    │                │   │
│  │  │ 🚨 FailureReport│      └──────────────────────────────┘                │   │
│  │  │ (W3C PROV       │      ┌──────────────────────────────┐                │   │
│  │  │  evidence +     │      │ 📍 ReconcileEvent            │                │   │
│  │  │  diagnosis)     │      │ (audit log, append-only)     │                │   │
│  │  └──────────────────┘      └──────────────────────────────┘                │   │
│  │                                                                              │   │
│  └──────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                       │
└─────────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────────┐
│                           kagent INTELLIGENCE LAYER                                  │
│                                                                                       │
│  ┌────────────────────────────┐  ┌────────────────────────────┐                     │
│  │  🤖 test-strategist-agent  │  │  🔍 preview-diff-analyzer  │                     │
│  │                            │  │                            │                     │
│  │  ┌──────────────────────┐  │  │  ┌──────────────────────┐  │                     │
│  │  │ Reads:               │  │  │  │ Reads:               │  │                     │
│  │  │ • Changed Files      │  │  │  │ • Changed Files      │  │                     │
│  │  │ • PR Diff            │  │  │  │ • PR Diff            │  │                     │
│  │  │ • ReconcileEvents    │  │  │  │ • Preview Config     │  │                     │
│  │  │                      │  │  │  │                      │  │                     │
│  │  │ Writes:              │  │  │  │ Writes:              │  │                     │
│  │  │ • TestPlan           │  │  │  │ • PR Comment         │  │                     │
│  │  │   (mustRun/canSkip)  │  │  │  │ • Analysis Summary   │  │                     │
│  │  └──────────────────────┘  │  │  └──────────────────────┘  │                     │
│  └────────────────────────────┘  └────────────────────────────┘                     │
│                                                                                       │
│  ┌────────────────────────────────────────────────────────────┐                     │
│  │  🔧 preview-troubleshooter-agent                           │                     │
│  │                                                            │                     │
│  │  ┌──────────────────────────────────────────────────────┐  │                     │
│  │  │ Triggered on: Test failure                           │  │                     │
│  │  │                                                      │  │                     │
│  │  │ Reads:                                               │  │                     │
│  │  │ • Pod logs, events, metrics                          │  │                     │
│  │  │ • Job completion status                              │  │                     │
│  │  │ • Trace spans (Jaeger)                               │  │                     │
│  │  │ • FailureReport evidence                             │  │                     │
│  │  │                                                      │  │                     │
│  │  │ Writes:                                              │  │                     │
│  │  │ • FailureReport diagnoses                            │  │                     │
│  │  │ • PR Comment (root cause + fix)                      │  │                     │
│  │  └──────────────────────────────────────────────────────┘  │                     │
│  └────────────────────────────────────────────────────────────┘                     │
│                                                                                       │
└─────────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────────┐
│                          MCP SERVERS (Tool Access)                                   │
│                                                                                       │
│  ┌─────────────────────────────┐  ┌────────────────────────────┐                    │
│  │  🔧 k8s-tool-server         │  │  📊 jaeger-mcp-server      │                    │
│  │  (read-only K8s access)     │  │  (trace queries)           │                    │
│  │                             │  │                            │                    │
│  │  Tools:                     │  │  Tools:                    │                    │
│  │  • kubectl get/describe     │  │  • Find spans by service   │                    │
│  │  • read logs                │  │  • Error rate analysis     │                    │
│  │  • list events              │  │  • Latency distribution    │                    │
│  │  • query metrics            │  │  • Dependency graph        │                    │
│  └─────────────────────────────┘  └────────────────────────────┘                    │
│                                                                                       │
│  ┌────────────────────────────────┐  ┌──────────────────────────┐                   │
│  │  🐙 github-mcp-server          │  │  📝 preview-extension    │                   │
│  │  (GitHub API integration)      │  │  (REST API for Copilot)  │                   │
│  │                                │  │                          │                   │
│  │  Tools:                        │  │  Endpoints:              │                   │
│  │  • Create PR comment           │  │  • @preview list         │                   │
│  │  • Update deployment status    │  │  • @preview status       │                   │
│  │  • Fetch PR diff               │  │  • @preview run-sql      │                   │
│  │  • Create/update issues        │  │  • @preview extend       │                   │
│  └────────────────────────────────┘  └──────────────────────────┘                   │
│                                                                                       │
└─────────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────────┐
│                         DATA FLOW & INTERACTIONS                                     │
│                                                                                       │
│                                                                                       │
│  GitHub                                                                              │
│    │                                                                                 │
│    │ 1. PR opened / updated                                                         │
│    ▼                                                                                 │
│  GitHub Actions Runner (in-cluster)                                                │
│    │                                                                                 │
│    │ 2. Build image → Push to GHCR                                                 │
│    │ 3. kubectl apply Preview CR                                                   │
│    ▼                                                                                 │
│  Preview Controller (watches Preview)                                               │
│    │                                                                                 │
│    ├─→ 4a. Create Namespace + Database                                             │
│    ├─→ 4b. Deploy Services                                                         │
│    │                                                                                 │
│    ├─→ 5. Create TestPlan (if mode=Auto)                                           │
│    │     └─→ Spawn Job → call test-strategist-agent                                │
│    │         ├─ Reads: Preview.spec.changeContext                                  │
│    │         └─ Writes: TestPlan.spec.mustRun/canSkip                              │
│    │                                                                                 │
│    ├─→ 6a. AI Enrichment (generate seed data + tests)                              │
│    ├─→ 6b. Create TestRun                                                          │
│    │                                                                                 │
│    ├─→ 7. Run Test Jobs (smoke, regression, E2E)                                   │
│    │     └─→ Append results to TestRun.status.results[]                            │
│    │                                                                                 │
│    ├─→ 8. Write ReconcileEvent                                                     │
│    │     (Provisioned → TestStarted → TestFinished → Ready/Error)                 │
│    │                                                                                 │
│    ├─→ 9. On error: Create FailureReport                                           │
│    │     ├─ Collect pod logs, events, metrics                                      │
│    │     ├─ Spawn failure-analyst-agent job                                        │
│    │     │   └─ Reads: FailureReport.spec.evidence                                │
│    │     │   └─ Writes: FailureReport.spec.diagnoses                               │
│    │     └─ (Optional) Call preview-diff-analyzer for change analysis             │
│    │                                                                                 │
│    └─→ 10. Post GitHub Deployment + PR Comment (via github-mcp-server)            │
│            ├─ Table: test results (passed/failed)                                   │
│            ├─ Strategy section (if mode=Auto)                                       │
│            └─ kagent diagnoses (if failure)                                         │
│                                                                                       │
│  GitHub (PR Updated)                                                                │
│    │                                                                                 │
│    └─ Developer reads results, optionally uses @preview Copilot commands          │
│       (via preview-extension REST API)                                              │
│                                                                                       │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 🔄 CRD Lifecycle & Relationships

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                          PREVIEW LIFECYCLE                                      │
│                                                                                  │
│   GitHub PR                                                                      │
│        │                                                                         │
│        ▼                                                                         │
│   ┌──────────────────────┐                                                      │
│   │ ⭐ Preview CR       │                                                       │
│   │ (cluster-scoped)    │                                                       │
│   │                     │                                                       │
│   │ spec:               │                                                       │
│   │ • prNumber          │                                                       │
│   │ • image             │                                                       │
│   │ • database.enabled  │                                                       │
│   │ • testStrategy.mode │                                                       │
│   └──────────────────────┘                                                      │
│        │                                                                         │
│        ├─ owns ──────────────────────────────────────────┐                     │
│        │                                                  │                     │
│        ▼                                                  ▼                     │
│   ┌──────────────────────┐                    ┌──────────────────────┐         │
│   │ 📋 TestPlan CR      │                    │ 🚨 FailureReport CR  │         │
│   │ (namespaced)        │                    │ (cluster-scoped)     │         │
│   │                     │                    │                      │         │
│   │ spec:               │                    │ spec:                │         │
│   │ • previewRef        │                    │ • previewRef         │         │
│   │ • mustRun[]         │                    │ • evidence[]         │         │
│   │ • canSkip[]         │                    │ • diagnoses[]        │         │
│   │ • confidence        │                    │                      │         │
│   │ • rationale         │                    │ status:              │         │
│   │                     │                    │ • phase (Collecting) │         │
│   │ status:             │                    │ • diagnosedAt        │         │
│   │ • phase (Ready)     │                    │                      │         │
│   │ • acceptedByController│                  └──────────────────────┘         │
│   └──────────────────────┘                                                     │
│        │                                                                        │
│        │ accepted ─────────────────────────┐                                  │
│        ▼                                    ▼                                  │
│   ┌──────────────────────┐            ┌──────────────────────┐               │
│   │ 🧪 TestRun CR       │            │ 📍 ReconcileEvent CR│               │
│   │ (namespaced)        │            │ (namespaced)        │               │
│   │                     │            │                     │               │
│   │ spec:               │            │ spec:               │               │
│   │ • previewRef        │            │ • type (Provisioned)│               │
│   │ • testPlanRef       │            │ • testSuite (smoke) │               │
│   │ • selectedTests[]   │            │ • outcome           │               │
│   │                     │            │ • occurredAt        │               │
│   │ status:             │            │ • correlationID     │               │
│   │ • results[]         │            │                     │               │
│   │   (appended)        │            │ (append-only log)   │               │
│   │ • phase (Running)   │            └──────────────────────┘               │
│   └──────────────────────┘                                                   │
│        │                                                                      │
│        └─ all owned by Preview ──────────────────────────────────────────┘   │
│                                                                                │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 🤖 Agent Architecture & Tool Access

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                     kagent AGENT EXECUTION MODEL                             │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │  Controller Context                                                    │ │
│  │  (preview-operator-system namespace)                                  │ │
│  │                                                                        │ │
│  │  ┌──────────────────────────────────────────────────────────────────┐ │ │
│  │  │ When TestPlan.spec.generatedBy = "Agent":                        │ │ │
│  │  │                                                                  │ │ │
│  │  │ 1. Create ephemeral Job (curlimages/curl, TTL=5min)            │ │ │
│  │  │    ├─ Mounts: serviceaccount, CA certs                         │ │ │
│  │  │    └─ Command: curl -X POST                                     │ │ │
│  │  │        http://test-strategist-agent.kagent-system/api/invoke   │ │ │
│  │  │                                                                  │ │ │
│  │  │ 2. Job payload includes:                                        │ │ │
│  │  │    ├─ Preview.spec.changeContext.changedFiles[]                │ │ │
│  │  │    ├─ Preview.spec.changeContext.diffPatch                     │ │ │
│  │  │    └─ recent ReconcileEvents (flakiness history)               │ │ │
│  │  │                                                                  │ │ │
│  │  │ 3. Agent processes (Agent-to-Agent protocol):                   │ │ │
│  │  │    ├─ LLM reasoning (Azure OpenAI)                              │ │ │
│  │  │    └─ Tool calls (k8s-tool-server, jaeger-mcp-server)          │ │ │
│  │  │                                                                  │ │ │
│  │  │ 4. Agent returns: TestPlan.spec mutations                       │ │ │
│  │  │    └─ TestPlan patched in-place                                 │ │ │
│  │  │                                                                  │ │ │
│  │  │ 5. Controller validates confidence & accepts plan               │ │ │
│  │  └──────────────────────────────────────────────────────────────────┘ │ │
│  │                                                                        │ │
│  │  ┌──────────────────────────────────────────────────────────────────┐ │ │
│  │  │ On Test Failure:                                                 │ │ │
│  │  │                                                                  │ │ │
│  │  │ 1. Create FailureReport (phase=Collecting)                      │ │ │
│  │  │    ├─ Spawn 5 goroutines: collect logs, events, metrics         │ │ │
│  │  │    └─ FailureReport.status → phase=Ready                        │ │ │
│  │  │                                                                  │ │ │
│  │  │ 2. Trigger failure-analyst-agent Job                            │ │ │
│  │  │    ├─ Job reads: FailureReport.spec.evidence[]                 │ │ │
│  │  │    ├─ Tool calls: k8s-tool-server (read-only)                   │ │ │
│  │  │    └─ Appends: FailureReport.spec.diagnoses[]                  │ │ │
│  │  │       (with evidenceRefs grounding)                              │ │ │
│  │  │                                                                  │ │ │
│  │  │ 3. (Optional) Call preview-diff-analyzer                        │ │ │
│  │  │    └─ Posts analysis summary to GitHub PR                       │ │ │
│  │  │                                                                  │ │ │
│  │  │ 4. Controller publishes PR comment (via github-mcp-server)      │ │ │
│  │  │    ├─ Risk level (HIGH / MEDIUM / LOW)                          │ │ │
│  │  │    ├─ Evidence summary                                          │ │ │
│  │  │    ├─ Probable cause                                            │ │ │
│  │  │    ├─ Suggested fix                                             │ │ │
│  │  │    └─ Debug commands                                            │ │ │
│  │  └──────────────────────────────────────────────────────────────────┘ │ │
│  │                                                                        │ │
│  └────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────────┐
│                          MCP SERVER ROUTING                                  │
│                                                                              │
│  Agent A2A Request                                                           │
│       │                                                                      │
│       ├─ Tool: "read_pod_logs"      ──→ k8s-tool-server                    │
│       ├─ Tool: "list_events"        ──→ k8s-tool-server                    │
│       ├─ Tool: "query_traces"       ──→ jaeger-mcp-server                  │
│       ├─ Tool: "get_metrics"        ──→ prometheus-mcp-server              │
│       ├─ Tool: "create_comment"     ──→ github-mcp-server                  │
│       └─ Tool: "update_deployment"  ──→ github-mcp-server                  │
│                                                                              │
│  Each MCP server:                                                            │
│  ✓ Read-only access (except github-mcp-server for mutations)               │
│  ✓ Authenticated via RBAC (k8s) or GitHub token (github)                   │
│  ✓ Runs in kagent-system namespace                                         │
│  ✓ Exposed as RemoteMCPServer CRs                                          │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 📊 Data Movement: GitHub ↔ Operator ↔ Agents

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                          CONTINUOUS FLOW                                     │
│                                                                              │
│  PHASE 1: PR Opened                                                          │
│  ─────────────────                                                           │
│  GitHub PR              github-mcp-server        preview-operator            │
│     ├─ Webhook ────────────┐                         ▲                      │
│     │                       │                         │                      │
│     └─ Actions Runner ──────┼──→ Build image ────────┤                      │
│                             │                         │                      │
│                             └─────→ kubectl apply     │                      │
│                                     Preview CR ───────┘                      │
│                                                                              │
│  PHASE 2: Provisioning (Operator owns)                                       │
│  ──────────────────────────────────────                                      │
│  preview-operator                          test-strategist-agent            │
│    ├─ Create namespace ─────────────────────────────────┐                   │
│    ├─ Provision database                                │                   │
│    │                                                    │ (if mode=Auto)    │
│    ├─ Deploy services ────────────────────────────────┐ │                   │
│    │                                                   │ │                   │
│    ├─ Create TestPlan stub                            │ │                   │
│    │    └─ Spawn Job ──────────────────────────────→  │ │                   │
│    │                                                   │ │                   │
│    │              [Agent processes]                    │ │                   │
│    │              • LLM thinks                         │ │                   │
│    │              • k8s-tool-server calls              │ │                   │
│    │              • jaeger-mcp-server calls            │ │                   │
│    │                                                   │ │                   │
│    │         ←──────────────────────────────────────  │ │                   │
│    │              [Patched TestPlan returned]          │ │                   │
│    │                                                   │ │                   │
│    └─ Accept TestPlan ────────────────────────────────┘ │                   │
│                                                         │                   │
│    ├─ Create TestRun                                   │                   │
│    └─ Run test Jobs ────────────────────────────────────┘                   │
│                                                                              │
│  PHASE 3: Results & Failure Analysis                                         │
│  ───────────────────────────────────────                                     │
│  preview-operator              failure-analyst-agent        github-mcp-server │
│    │                                                            │              │
│    ├─ TestRun complete ──────────────→ (if failed)             │              │
│    │                                       │                    │              │
│    ├─ Create FailureReport ───→ Job ──→  [Agent processes]    │              │
│    │  (phase=Collecting)              • Evidence analysis      │              │
│    │                                  • Diagnosis generation   │              │
│    │                                      │                    │              │
│    │                                      └────→ Diagnoses    │              │
│    │                                                 appended   │              │
│    │                                                            │              │
│    └─ Post PR comment ──────────────────────────────────────→  │              │
│       └─ Results table + kagent analysis                       │              │
│                                                                 │              │
│                                                    ←─────────────┘             │
│                                     (via github-mcp-server)                   │
│                                                                              │
│  PHASE 4: Developer Interaction                                              │
│  ──────────────────────────────────                                          │
│  GitHub Copilot Chat (@preview commands)  ←→  preview-extension REST API    │
│       │                                              │                       │
│       └─────────────────────→ kubectl ────→ preview-operator                │
│             (@preview list)                                                 │
│             (@preview extend pr-42 24h)                                     │
│             (@preview run-sql pr-42 ...)                                    │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 🎯 Key Interactions

| Actor | Reads | Writes | Trigger |
|-------|-------|--------|---------|
| **preview-operator** | Preview, TestPlan, ReconcileEvent | Preview status, TestRun, ReconcileEvent, FailureReport | CR changes, timers |
| **test-strategist-agent** | Preview.changeContext, ReconcileEvents | TestPlan.spec | Job invocation (A2A) |
| **preview-diff-analyzer** | Preview.changeContext | PR comment (analysis) | Job invocation (A2A) |
| **failure-analyst-agent** | FailureReport.evidence | FailureReport.diagnoses | Job invocation (A2A) |
| **k8s-tool-server** | Pod logs, events, metrics (read-only) | — | Agent tool calls |
| **jaeger-mcp-server** | Trace spans, latency data | — | Agent tool calls |
| **github-mcp-server** | PR files, commits | PR comments, deployment status | Agent tool calls + controller |
| **preview-extension** | Preview CR, TestRun | checkpoint restore requests | HTTP (Copilot API) |

---

## 🚀 Deployment Topology

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        KUBERNETES NAMESPACES                            │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │  preview-operator-system                                        │  │
│  │  ├─ preview-operator Deployment                                │  │
│  │  ├─ preview-extension REST API                                 │  │
│  │  └─ webhook (mutating + validating)                           │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │  kagent-system                                                  │  │
│  │  ├─ test-strategist-agent Agent                               │  │
│  │  ├─ preview-diff-analyzer Agent                               │  │
│  │  ├─ failure-analyst-agent Agent                               │  │
│  │  ├─ k8s-tool-server Deployment                                │  │
│  │  ├─ jaeger-mcp-server Deployment                              │  │
│  │  ├─ github-mcp-server Deployment                              │  │
│  │  ├─ prometheus-mcp-server Deployment                          │  │
│  │  └─ kagent-controller Deployment                              │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │  github-runner                                                  │  │
│  │  └─ runner pod (runs CI/CD jobs, builds images)               │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │  preview-pr-N (one per active PR)                              │  │
│  │  ├─ svc-backend Deployment                                    │  │
│  │  ├─ svc-frontend Deployment                                   │  │
│  │  ├─ postgres StatefulSet                                      │  │
│  │  ├─ Test Jobs (smoke, regression, e2e, etc.)                 │  │
│  │  ├─ AI Jobs (schema-dump, generate, seed, tests)             │  │
│  │  ├─ ConfigMaps (database checkpoints, AI artifacts)          │  │
│  │  ├─ NetworkPolicy (preview-isolation)                        │  │
│  │  └─ Resources owned by Preview CR (finalizer-protected)      │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 📈 Event Timeline (Example PR)

```
Time    Event                          Operator State              CRD State
────────────────────────────────────────────────────────────────────────────
10:00   PR #42 opened                  (watching GitHub)           —
10:01   Image built, pushed to GHCR    (observing)                 —
10:02   kubectl apply Preview pr-42    Reconcile #1 starts         Preview created
        • Create namespace                                          
        • Provision PostgreSQL                                     
        • Deploy services                                          
10:05   Services ready                 Create TestPlan stub       TestPlan: Pending
        • Spawn agent Job               (mode=Auto)                ReconcileEvent: Provisioned
10:06   Agent analyzes diff            Agent running               —
        • test-strategist-agent                                    
10:08   Agent returns plan             Accept TestPlan            TestPlan: Ready
        • mustRun=[smoke, regression]   • Create TestRun           TestRun: Pending
        • canSkip=[contract]                                       ReconcileEvent: TestStarted
10:09   Smoke tests run                 (await completion)         TestRun: Running
10:15   Smoke tests pass                Append result              TestRun: results[0]
        • Regression tests start        ReconcileEvent: TestStarted
10:30   Regression tests pass           Append result              TestRun: results[1]
10:31   Phase → Running                 Post GitHub comment        Preview: Running
        • All tests complete             (results + kagent section)  ReconcileEvent: Ready
10:32   Developer reads PR comment     (waiting for user action)  —
10:35   Developer clicks "Preview"     (in comment)                —
        • Opens pr-42.preview.example.com

[Success path]

OR

[Failure path @ 10:20: Regression fails]
10:20   Regression tests fail          Create FailureReport        FailureReport: Collecting
        • Exit code 1                   • Collect pod logs           ReconcileEvent: Error
        • Spawn failure-analyst-agent    • Gather events            
10:22   Agent analyzes evidence        Append diagnoses            FailureReport: Diagnosed
        • failure-analyst-agent         Post PR comment:
                                        - Root cause
                                        - Suggested fix
                                        - Debug commands
10:23   Developer reads analysis       —                           —
        Uses @preview run-sql to debug
```

---

## 🎭 Responsible Agents Per Feature

```
Feature                     Owner                    MCP Servers
─────────────────────────────────────────────────────────────────────
Test Selection              test-strategist-agent    k8s-tool-server
(mustRun/canSkip decision)                          jaeger-mcp-server

PR Diff Analysis            preview-diff-analyzer    github-mcp-server
(impact classification)

Failure Root Cause          failure-analyst-agent    k8s-tool-server
(diagnosis + fix)                                    jaeger-mcp-server
                                                    prometheus-mcp-server

Preview Lifecycle           preview-operator         (no tools)
(provisioning, cleanup)

GitHub Integration          preview-operator         github-mcp-server

Copilot Chat Commands       preview-extension        (REST API)
```

---

## 🔐 Security Model

```
┌──────────────────────────────────────────────────────────────────┐
│  RBAC & Tool Access Control                                      │
│                                                                  │
│  preview-operator SA:                                            │
│  ✓ Full cluster access (for provisioning + cleanup)             │
│                                                                  │
│  test-strategist-agent SA:                                       │
│  ✓ Read-only: Previews, ReconcileEvents, Nodes, PVs            │
│  ✓ Write: TestPlans (in preview namespaces)                     │
│                                                                  │
│  preview-troubleshooter-agent SA:                                │
│  ✓ Read-only: Pods, Events, Jobs (all namespaces)              │
│  ✓ Read: Traces (via jaeger-mcp-server)                         │
│  ✓ Write: FailureReports (cluster-scoped)                       │
│  ✓ Create comments: PRs (via github-mcp-server PAT)             │
│                                                                  │
│  k8s-tool-server (MCP):                                          │
│  ✓ Read-only access (enforced by k8s API)                       │
│  ✓ No secrets exposed (redaction in FailureReport)              │
│                                                                  │
│  github-mcp-server (MCP):                                        │
│  ✓ Uses GitHub PAT (stored in Secret)                           │
│  ✓ Scoped: repo, write:packages, deployments                    │
│  ✓ Write-only (no read of private content)                      │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## 📚 Cross-Reference

- **CRD Details**: See [`docs/features/crd-*.md`](../features/)
- **kagent Architecture**: See [`docs/features/kagent-architecture.md`](../features/kagent-architecture.md)
- **MCP Servers**: See [`docs/features/mcp-servers.md`](../features/mcp-servers.md)
- **Deployment**: See [`docs/deployment/`](../deployment/)
- **API Reference**: See [`api/v1alpha1/`](../../api/v1alpha1/)
