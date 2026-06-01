# Feature Guides

Detailed, code-grounded guides for each capability of the Preview Operator. Every
guide follows the same shape: **Introduction → What it's for → What it does →
How it works (diagram) → Relationships → Configuration → Reference.**

Start with [Lifecycle & Provisioning](./lifecycle.md) — it is the core loop that
every other feature plugs into.

## Platform

| Guide | What it covers |
|-------|----------------|
| [Lifecycle & Provisioning](./lifecycle.md) | The core reconcile loop: one namespace per PR, phases, resource tiers, approval gate, TTL, provisioning deadline. |
| [Database Management](./database-management.md) | **Scenarios + where to put your files** — schema/migrations, seed, AI seed, isolation, reset. |
| [Ephemeral PostgreSQL](./ephemeral-postgres.md) | Per-preview throwaway database: migrations, static seed, injected credentials, on-demand reset. |
| [Database Checkpoints](./database-checkpoints.md) | Save/restore a deterministic DB state so each test suite starts from the same seed. |
| [Networking & Exposure](./networking-exposure.md) | Services, Ingress / Istio routing, the public preview URL, path-based multi-service routing. |
| [Security & Isolation](./security.md) | NetworkPolicy, Pod Security Standards, ResourceQuota, and bounded-blast-radius RBAC. |
| [Observability](./observability.md) | OpenTelemetry auto-instrumentation wiring for preview workloads. |

## Testing

| Guide | What it covers |
|-------|----------------|
| [Test Suites](./test-suites.md) | Smoke, OpenAPI contract (Microcks), regression, and E2E (Playwright) suites and how they run. |
| [Authoring Tests](./authoring-tests.md) | **How to add your own tests** — the `/app/tests/` contract, env vars, and command/image overrides. |
| [Microcks — Contract Testing](./microcks-contract-testing.md) | **Deep dive:** the import/test Jobs, Keycloak auth, and the OpenAPI contract-testing protocol. |
| [AI Test Strategist](./ai-test-strategist.md) | The kagent agent that picks which suites to run from the PR diff, via the `TestPlan` CRD. |
| [Change Context](./change-context.md) | The PR diff as a first-class reconciliation input (deterministic gate vs. advisory signals). |

## AI & diagnostics

| Guide | What it covers |
|-------|----------------|
| [AI Enrichment](./ai-enrichment.md) | LLM-generated seed data and targeted tests, run automatically after the preview is ready. |
| [AI Failure Analysis (kagent)](./ai-failure-analysis.md) | Root-cause analysis on a failed preview, surfaced in `status.kagent` and the PR comment. |
| [Failure Provenance](./failure-provenance.md) | The `FailureReport` CRD: durable, PROV-aligned evidence bundles plus the `fp-diagnose` / `fp-score` CLIs. |
| [Customizing AI Prompts](./ai-prompts.md) | **How to change the AI prompts** — per-PR, global, and per-agent. |
| [MCP Servers & Agent Tools](./mcp-servers.md) | The MCP tool servers the kagent agents use and how to grant new tools. |
| [kagent — Architecture & Internals](./kagent-architecture.md) | **Deep dive:** agent creation, the A2A protocol, authentication, Azure OpenAI, and every agent. |

## Integration

| Guide | What it covers |
|-------|----------------|
| [GitHub Integration](./github-integration.md) | Deployment statuses and PR comments (results table + AI sections). |
| [Copilot Extension](./copilot-extension.md) | `@preview` ChatOps commands driven from GitHub Copilot Chat. |

## Custom Resources (CRDs)

The Preview Operator defines five Custom Resource Types. Each is documented comprehensively:

| CRD | Shortname | What it represents |
|-----|-----------|-------------------|
| [Preview](./crd-preview.md) | `prev` | The core abstraction: a preview environment request with full lifecycle (Pending → Provisioning → Running → Terminating) |
| [TestPlan](./crd-testplan.md) | `tp` | Intelligent test selection: agent-generated or manual decision on which test suites to run (mustRun / shouldRun / canSkip) |
| [TestRun](./crd-testrun.md) | `tr` | Test execution tracking: persists outcomes of each test Job as they complete; immutable audit trail |
| [FailureReport](./crd-failurereport.md) | — | Durable failure evidence bundle (W3C PROV-aligned): collected evidence items + diagnoses grounded in that evidence |
| [ReconcileEvent](./crd-reconcileevent.md) | `re` | Controller lifecycle audit log (append-only): records state transitions, test starts/completions, and errors for agent pattern detection |

---

For installation, the full reconcile sequence, and troubleshooting, see the
[main README](https://github.com/ihsenalaya/preview-operator/blob/main/README.md). The demo walkthrough lives in
[../kubecon-demo.md](https://github.com/ihsenalaya/preview-operator/blob/main/docs/kubecon-demo.md).
