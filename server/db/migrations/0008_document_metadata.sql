-- Document metadata a person can correct, and where each value came from (ATLAS_DESIGN.md §8, ATLAS.md §9).
-- owner_id is a plain uuid: users belong to the access module, which this module does not reference.

-- +goose Up
ALTER TABLE documents.documents
  ADD COLUMN status        text NOT NULL DEFAULT 'unknown' CHECK (status IN ('current', 'draft', 'obsolete', 'unknown')),
  ADD COLUMN department    text NOT NULL DEFAULT '',
  ADD COLUMN owner_id      uuid,
  ADD COLUMN approved_at   date,
  ADD COLUMN review_due_at date,
  ADD COLUMN language      text NOT NULL DEFAULT '',
  ADD COLUMN attributes    jsonb NOT NULL DEFAULT '{}',
  ADD COLUMN summary       text NOT NULL DEFAULT '';

-- One row per field. A manual row wins over extraction, and survives re-indexing.
CREATE TABLE documents.field_provenance (
  tenant_id   uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  document_id uuid NOT NULL REFERENCES documents.documents (id) ON DELETE CASCADE,
  field       text NOT NULL,
  value       text NOT NULL,
  source      text NOT NULL CHECK (source IN ('manual', 'titleblock', 'path', 'erp', 'llm')),
  confidence  real NOT NULL CHECK (confidence BETWEEN 0 AND 1),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (document_id, field)
);
ALTER TABLE documents.field_provenance ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON documents.field_provenance USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE documents.field_provenance;
ALTER TABLE documents.documents
  DROP COLUMN status, DROP COLUMN department, DROP COLUMN owner_id, DROP COLUMN approved_at,
  DROP COLUMN review_due_at, DROP COLUMN language, DROP COLUMN attributes, DROP COLUMN summary;
