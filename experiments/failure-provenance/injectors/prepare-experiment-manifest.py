#!/usr/bin/env python3
"""prepare-experiment-manifest.py — make a generated Preview manifest experiment-ready.

generate_preview_manifest.py (in the idp-preview repo) emits a manifest that is
missing two things the failure-provenance experiment needs. This filter reads a
manifest on stdin and writes it to stdout with both inserted:

  1. spec.database.migration — without it the operator reports
     MigrationReady=TaskDisabled and never runs the migration Job, so F1's
     faulty Alembic migration is inert and other runs lack a consistent schema.

  2. spec.aiEnrichment.seed.enabled — AI enrichment is the catalogue seed.
     aiSeedEnabled() skips it unless an explicit seed config is present (the
     changeContext heuristic returns false when detectedImpacts.requiresSeedData
     is unset, which the generator never sets). Without the seed the catalogue
     is empty and the regression/e2e suites fail as baseline noise.

The filter is fail-loud: if an expected anchor is absent (e.g. the upstream
generator changed) it exits non-zero rather than emitting a manifest that
silently disables a step. It tolerates aiEnrichment.enabled being true OR false
so it composes with the F9 injector, which disables AI enrichment.
"""
import sys

# --- 1. database.migration ---------------------------------------------------
DB_ANCHOR = "  database:\n    enabled: true\n    databaseName: appdb\n"
DB_MIGRATION = (
    "    migration:\n"
    "      enabled: true\n"
    '      command: ["python", "-m", "alembic", "upgrade", "head"]\n'
)

# --- 2. aiEnrichment.seed ----------------------------------------------------
SEED_BLOCK = "    seed:\n      enabled: true\n"


def insert_migration(manifest: str) -> str:
    count = manifest.count(DB_ANCHOR)
    if count != 1:
        sys.stderr.write(
            f"prepare-experiment-manifest: expected one database anchor, found {count}\n"
        )
        sys.exit(1)
    return manifest.replace(DB_ANCHOR, DB_ANCHOR + DB_MIGRATION, 1)


def insert_seed(manifest: str) -> str:
    # aiEnrichment.enabled is "true" normally, "false" after the F9 injector.
    for val in ("true", "false"):
        anchor = f"  aiEnrichment:\n    enabled: {val}\n"
        if anchor in manifest:
            return manifest.replace(anchor, anchor + SEED_BLOCK, 1)
    sys.stderr.write("prepare-experiment-manifest: aiEnrichment anchor not found\n")
    sys.exit(1)


def main() -> int:
    manifest = sys.stdin.read()
    manifest = insert_migration(manifest)
    manifest = insert_seed(manifest)
    sys.stdout.write(manifest)
    return 0


if __name__ == "__main__":
    sys.exit(main())
