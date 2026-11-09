-- Graph and alerts (ATLAS.md §12): what documents mention, how they relate, and what a curator should look at.
-- Layers 1-2 only (Core): links found by patterns and by equal codes. Documents are referenced by plain uuid.

-- +goose Up
-- Published by documents for graph and the rules: the facts about a document, with deleted ones left out.
CREATE VIEW documents.v_documents WITH (security_invoker = true) AS
SELECT d.id, d.source_id, d.external_id AS path, d.title, d.code, d.doc_type, d.status, d.department, d.owner_id,
       d.review_due_at, d.current_version_id, d.created_at
FROM documents.documents d WHERE d.deleted_at IS NULL AND d.current_version_id IS NOT NULL;

-- Current passages flagged as possible instructions to an AI.
CREATE VIEW documents.v_suspect_passages WITH (security_invoker = true) AS
SELECT c.document_id, c.id AS chunk_id, c.page, c.text FROM documents.chunks c WHERE c.is_current AND c.suspect_injection;

-- What each document mentions: codes (to link documents) and entities such as standards.
CREATE TABLE graph.mentions (
  tenant_id   uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  document_id uuid NOT NULL,
  entity_type text NOT NULL,
  label       text NOT NULL,
  count       int NOT NULL,
  PRIMARY KEY (document_id, entity_type, label)
);
CREATE INDEX mentions_entity ON graph.mentions (entity_type, label);

CREATE TABLE graph.edges (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id   uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  src         uuid NOT NULL,
  dst         uuid NOT NULL,
  kind        text NOT NULL CHECK (kind IN ('references', 'cites_norm', 'mentions', 'owned_by', 'contradicts', 'duplicates', 'belongs_to')),
  layer       int NOT NULL CHECK (layer BETWEEN 1 AND 4),
  status      text NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'suggested', 'rejected')),
  produced_by text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (src, dst, kind, produced_by)
);
CREATE INDEX edges_dst ON graph.edges (dst);

CREATE TABLE graph.alerts (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id       uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  rule            text NOT NULL CHECK (rule IN ('OBSOLETE_STANDARD', 'CONTRADICTION', 'NO_OWNER', 'ENTITY_NO_DOCS', 'SUSPECT_CONTENT',
                                                'REVIEW_OVERDUE', 'OLD_VERSION_CIRCULATING', 'DUPLICATE')),
  severity        text NOT NULL CHECK (severity IN ('high', 'medium', 'low')),
  status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'ack', 'resolved', 'dismissed')),
  title           text NOT NULL,
  detail          text NOT NULL,
  document_ids    uuid[] NOT NULL,
  entity_type     text,
  entity_label    text,
  assignee_id     uuid,
  resolution_note text NOT NULL DEFAULT '',
  fingerprint     text NOT NULL, -- rule + subject: the same problem is one alert, however often it is found
  evidence        jsonb NOT NULL DEFAULT '[]',
  first_seen_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, fingerprint)
);

ALTER TABLE graph.mentions ENABLE ROW LEVEL SECURITY;
ALTER TABLE graph.edges ENABLE ROW LEVEL SECURITY;
ALTER TABLE graph.alerts ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON graph.mentions USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON graph.edges USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON graph.alerts USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE graph.alerts, graph.edges, graph.mentions;
DROP VIEW documents.v_suspect_passages, documents.v_documents;
