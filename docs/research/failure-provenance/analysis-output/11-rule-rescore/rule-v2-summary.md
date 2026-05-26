# Rule diagnoser — v1 (frozen) vs v2 (offline fix) — aligned top-1

Per-scenario aligned top-1, pooled across the 5 evidence levels (50 cells each).

v1 = the operator's frozen rule engine (unchanged).

v2 = the same rule engine with two over-match defects fixed offline:
  1. `ruleInvalidMigration` no longer treats a generic Python traceback as a SQL error; the SQL-error predicate is scoped to migration/alembic-resourced log lines.
  2. `ruleContractBreak` defers to the more-specific rule when the changed-file evidence routes elsewhere (frontend / seed / tests/).

| Scenario | n | v1 aligned | v2 aligned | Δ |
|---|---|---|---|---|
| F1 | 50 | 100.0% (50/50) | 100.0% (50/50) | 0 |
| F2 | 50 |   0.0% (0/50) |  80.0% (40/50) | +40 |
| F3 | 50 |  80.0% (40/50) |  80.0% (40/50) | 0 |
| F4 | 50 |  60.0% (30/50) |  60.0% (30/50) | 0 |
| F5 | 50 |   0.0% (0/50) |  40.0% (20/50) | +20 |
| F6 | 50 |  54.0% (27/50) |  54.0% (27/50) | 0 |
| F7 | 50 |  54.0% (27/50) |  54.0% (27/50) | 0 |
| F8 | 50 |  60.0% (30/50) |  60.0% (30/50) | 0 |
| F9 | 50 |   0.0% (0/50) |   0.0% (0/50) | 0 |
| F10 | 50 |   0.0% (0/50) |  40.0% (20/50) | +20 |

**Pooled:** v1 = 204/500 (40.8%); v2 = 284/500 (56.8%); Δ = +80 cells (16.0 pp).

_The v1 numbers are the canonical RQ2 figure (the frozen image is what the article evaluates). The v2 column is a sensitivity check that quantifies what a one-day patch to the rule engine would have bought, without claiming the operator already does this._
