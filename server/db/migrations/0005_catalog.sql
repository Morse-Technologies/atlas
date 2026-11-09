-- Catalog: entity types and document-type taxonomy as data (ATLAS.md §5.4). Core values are seeded per
-- tenant by internal/catalog.SeedCore when the tenant is created; packs add their own rows later.

-- +goose Up
CREATE TABLE catalog.entity_types (
  tenant_id            uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  key                  text NOT NULL,
  labels               jsonb NOT NULL,
  gliner_labels        jsonb NOT NULL DEFAULT '{}',
  code_patterns        text[] NOT NULL DEFAULT '{}',
  contradiction_anchor boolean NOT NULL DEFAULT false,
  pack_id              text,
  PRIMARY KEY (tenant_id, key)
);

CREATE TABLE catalog.doc_types (
  tenant_id  uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  key        text NOT NULL,
  parent_key text,
  core_type  text NOT NULL,
  labels     jsonb NOT NULL,
  pack_id    text,
  PRIMARY KEY (tenant_id, key)
);

ALTER TABLE catalog.entity_types ENABLE ROW LEVEL SECURITY;
ALTER TABLE catalog.doc_types ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON catalog.entity_types USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON catalog.doc_types USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE catalog.doc_types, catalog.entity_types;
