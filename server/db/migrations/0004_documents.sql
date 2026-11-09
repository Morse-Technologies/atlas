-- Documents, versions and parsed blocks (ATLAS_DESIGN.md §5.2 with ATLAS.md §10.2).
-- source_id is a plain uuid: modules do not reference each other's tables (ATLAS.md §5.2).

-- +goose Up
CREATE TABLE documents.documents (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id          uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  source_id          uuid NOT NULL,
  external_id        text NOT NULL,
  title              text NOT NULL,
  code               text NOT NULL DEFAULT '',
  doc_type           text NOT NULL DEFAULT 'other',
  current_version_id uuid,
  deleted_at         timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (source_id, external_id)
);

CREATE TABLE documents.document_versions (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id    uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  document_id  uuid NOT NULL REFERENCES documents.documents (id) ON DELETE CASCADE,
  content_hash text NOT NULL,
  mime         text NOT NULL,
  size         bigint NOT NULL,
  state        text NOT NULL DEFAULT 'processing'
               CHECK (state IN ('processing', 'parsed', 'superseded', 'quarantined')),
  pages        int,
  created_at   timestamptz NOT NULL DEFAULT now(),
  parsed_at    timestamptz,
  UNIQUE (document_id, content_hash)
);

CREATE TABLE documents.blocks (
  tenant_id  uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  version_id uuid NOT NULL REFERENCES documents.document_versions (id) ON DELETE CASCADE,
  ord        int NOT NULL,
  kind       text NOT NULL CHECK (kind IN ('heading', 'paragraph', 'table', 'list_item', 'step', 'warning')),
  page       int NOT NULL,
  bbox       jsonb, -- {x, y, w, h} in PDF points, origin top-left (what the viewer expects)
  text       text NOT NULL,
  PRIMARY KEY (version_id, ord)
);

ALTER TABLE documents.documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents.document_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents.blocks ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON documents.documents USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON documents.document_versions USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON documents.blocks USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE documents.blocks, documents.document_versions, documents.documents;
