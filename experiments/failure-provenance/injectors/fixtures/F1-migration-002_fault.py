"""fp fault F1 — invalid SQL migration

Revision ID: 002_fp_fault
Revises: 001
Create Date: 2026-01-01

Injected by the failure-provenance experiment harness
(experiments/failure-provenance). The upgrade() runs deliberately invalid SQL
("CREATE INDX" instead of "CREATE INDEX"), so the migration Job fails with a
PostgreSQL syntax error. This file is never merged — it exists only to produce a
controlled, deterministic F1 failure with a known root cause.
"""
from typing import Sequence, Union

from alembic import op

revision: str = "002_fp_fault"
down_revision: Union[str, None] = "001"
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    # Invalid on purpose: "INDX" is not a SQL keyword (it should be "INDEX").
    # PostgreSQL rejects this with: syntax error at or near "INDX".
    op.execute("CREATE INDX idx_products_name ON products (name)")


def downgrade() -> None:
    op.execute("DROP INDEX IF EXISTS idx_products_name")
