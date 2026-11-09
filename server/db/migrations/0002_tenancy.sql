-- Tenants (ATLAS.md §10.2). Every tenant-owned table defaults tenant_id from the transaction setting
-- written by platform/db.InTx, and its RLS policy compares against the same setting. Unset setting = no rows.

-- +goose Up
-- The tenant registry itself has no RLS: it is how a process finds the tenant to scope to.
CREATE TABLE access.tenants (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  org_name   text NOT NULL,
  settings   jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE access.tenants;
