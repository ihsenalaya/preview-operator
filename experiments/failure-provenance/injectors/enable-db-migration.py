#!/usr/bin/env python3
"""enable-db-migration.py — add spec.database.migration to a Preview manifest.

generate_preview_manifest.py (in the idp-preview repo) emits spec.database
without a migration block, so the operator skips the migration Job and reports
MigrationReady=TaskDisabled. The failure-provenance experiment needs that Job
to run: scenario F1 injects a faulty Alembic migration whose failure is the
fault signal, and every other scenario needs a consistent, successful migration
step so the database schema is identical across runs.

This filter reads a manifest on stdin and writes it to stdout with the block
inserted. It is fail-loud: if the expected anchor is absent (e.g. the upstream
generator changed its database block) it exits non-zero rather than silently
emitting a manifest that disables the migration.
"""
import sys

# The exact database block emitted by generate_preview_manifest.py.
ANCHOR = "  database:\n    enabled: true\n    databaseName: appdb\n"

# Inserted directly after the anchor. The command is the idp-preview migration
# entrypoint documented in that repo's README (Alembic).
MIGRATION = (
    "    migration:\n"
    "      enabled: true\n"
    '      command: ["python", "-m", "alembic", "upgrade", "head"]\n'
)


def main() -> int:
    manifest = sys.stdin.read()
    count = manifest.count(ANCHOR)
    if count != 1:
        sys.stderr.write(
            f"enable-db-migration: expected exactly one database anchor, found {count}\n"
        )
        return 1
    sys.stdout.write(manifest.replace(ANCHOR, ANCHOR + MIGRATION, 1))
    return 0


if __name__ == "__main__":
    sys.exit(main())
