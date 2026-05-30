# Ephemeral PostgreSQL

> A throwaway, per-Preview PostgreSQL instance with auto-injected credentials, optional migrations and seed Jobs, and on-demand reset.

## Introduction
When a `Preview` sets `spec.database.enabled: true`, the operator provisions a dedicated PostgreSQL instance inside the preview's isolated namespace. It generates stable credentials, exposes them as environment variables to every application container, and optionally runs a migration Job and a seed Job before the app starts. The whole database lives and dies with the preview — no shared staging database, no manual cleanup.

## What it's for
Preview environments built from a pull request need a real database, but sharing one risks cross-PR data bleed and irreproducible state. This feature gives each `Preview` its own disposable PostgreSQL so schema changes, migrations, and seed data in a branch are tested in full isolation and discarded when the preview is torn down.

## What it does
- Creates a `postgres-credentials` Secret holding `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, and `DATABASE_URL` — generated **once** and never overwritten.
- Deploys a `postgres` Deployment (`postgres:<version>-alpine`, `Recreate` strategy, `pg_isready` readiness/liveness probes) and a `postgres` ClusterIP Service on port 5432.
- Optionally runs a one-shot `postgres-migrate` Job (`spec.database.migration`) before the app is deployed.
- Optionally runs a one-shot `postgres-seed` Job (`spec.database.seed`) after migration and before the app is deployed.
- Injects the four DB env vars into every application container and into both task Jobs.
- Adds a `busybox` `wait-for-postgres` init container to app pods and task Jobs so nothing starts until Postgres accepts TCP connections.
- Re-runs migration and seed from scratch on demand when `spec.database.resetRequested` is set.

## How it works

```mermaid
sequenceDiagram
    participant R as PreviewReconciler
    participant S as Secret postgres-credentials
    participant PG as Deployment/Service postgres
    participant M as Job postgres-migrate
    participant SD as Job postgres-seed
    participant A as App Deployment

    R->>S: reconcilePostgresSecret (create once)
    R->>PG: reconcilePostgres Service + Deployment
    Note over R,PG: not ready? RequeueAfter=10s
    R->>M: reconcileDatabaseTask (if migration.enabled)
    Note over M: init waits for pg, runs command; must succeed
    R->>SD: reconcileDatabaseTask (if seed.enabled)
    Note over SD: init waits for pg, runs command; must succeed
    R->>A: reconcileDeployment (DB env + wait-for-postgres init)
```

`reconcileDatabaseWait` calls `reconcileDatabase`, which provisions the Secret, Service, and Deployment, then runs the migration task and the seed task in that order. Each step gates the next: the controller requeues every 10s until Postgres is ready, and a task must succeed before the next one (and ultimately the app Deployment) is reconciled. If a task Job fails, the Preview transitions to `Failed` and the Job logs are captured in `status.diagnostics`. Optimistic-concurrency conflicts requeue rather than failing the preview.

## Relationships with other components
- [Database Checkpoints](./database-checkpoints.md) — save/restore point-in-time snapshots of the same database (`spec.database.checkpointSave/Restore`).
- [AI Enrichment](./ai-enrichment.md) — generates AI seed data against this database after the environment is Running.
- [Test Suites](./test-suites.md) — runs smoke/regression/E2E against the provisioned database and uses checkpoints for inter-suite isolation.

## Configuration

| Field | Type | Default | Purpose |
|---|---|---|---|
| `spec.database.enabled` | bool | `false` | Provision PostgreSQL for this Preview |
| `spec.database.version` | string | `"15"` | PostgreSQL major version → image `postgres:<version>-alpine` |
| `spec.database.databaseName` | string | `"appdb"` | Logical database name (`POSTGRES_DB`) |
| `spec.database.migration.enabled` | bool | `false` | Run the migration Job before the app |
| `spec.database.migration.image` | string | `spec.image` | Image for the migration Job |
| `spec.database.migration.command` | []string | — | Migration entrypoint command |
| `spec.database.migration.args` | []string | — | Migration arguments |
| `spec.database.seed.enabled` | bool | `false` | Run the seed Job after migration |
| `spec.database.seed.image` | string | `spec.image` | Image for the seed Job |
| `spec.database.seed.command` | []string | — | Seed entrypoint command |
| `spec.database.seed.args` | []string | — | Seed arguments |
| `spec.database.resetRequested` | bool | `false` | Delete and re-run migration + seed Jobs; auto-cleared |

Auto-injected env vars (into every app container and both task Jobs): `POSTGRES_USER` (`preview_<prNumber>`), `POSTGRES_PASSWORD` (64-char hex), `POSTGRES_DB`, and `DATABASE_URL` (`postgresql://<user>:<pw>@postgres:5432/<db>?sslmode=disable`).

```yaml
apiVersion: platform.company.io/v1alpha1
kind: Preview
metadata:
  name: pr-42
spec:
  branch: feature/orders
  prNumber: 42
  image: ghcr.io/acme/app:sha-abc
  database:
    enabled: true
    version: "15"
    databaseName: appdb
    migration:
      enabled: true
      command: ["python", "-m", "alembic", "upgrade", "head"]
    seed:
      enabled: true
      command: ["python", "scripts/seed_preview.py"]
```

Trigger a reset (clears and replays migration + seed) without recreating the environment:

```bash
kubectl patch preview pr-42 --type=merge \
  -p '{"spec":{"database":{"resetRequested":true}}}'
```

## Reference
- [`../../api/v1alpha1/preview_types.go`](../../api/v1alpha1/preview_types.go) — `DatabaseSpec`, `DatabaseTaskSpec`, defaults, JSON tags
- [`../../internal/controller/preview_controller.go`](../../internal/controller/preview_controller.go) — `reconcileDatabase`, `reconcilePostgresSecret`, `reconcilePostgresDeployment`, `reconcileDatabaseTask`, `databaseTaskJob`, `handleResetRequested`, `reconcileDeployment`
- [`../../internal/controller/checkpoint.go`](../../internal/controller/checkpoint.go) — checkpoint save/restore (see [Database Checkpoints](./database-checkpoints.md))
- Related docs: [Database Checkpoints](./database-checkpoints.md), [AI Enrichment](./ai-enrichment.md), [Test Suites](./test-suites.md)
