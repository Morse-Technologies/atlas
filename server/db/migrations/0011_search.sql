-- Search: ACL sets, chunks with full-text and vector indexes, and the answers that cite them
-- (ATLAS.md §10.2, §11). Modules do not reference each other's tables: ids that cross a module are plain uuids.

-- +goose Up
-- ACL sets: who may read a group of chunks. Core has one default set that every signed-in person belongs to;
-- Pro adds one per source folder, filled from the source's permissions.
CREATE TABLE access.acl_sets (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id  uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid REFERENCES access.tenants (id),
  origin     text NOT NULL,
  allow      uuid[] NOT NULL DEFAULT '{}',
  deny       uuid[] NOT NULL DEFAULT '{}',
  is_default boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX acl_sets_one_default ON access.acl_sets (tenant_id) WHERE is_default;
ALTER TABLE access.acl_sets ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON access.acl_sets USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

CREATE TABLE documents.chunks (
  id                uuid PRIMARY KEY, -- uuid v5 of (version, ord): reprocessing a version gives the same ids
  tenant_id         uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  version_id        uuid NOT NULL REFERENCES documents.document_versions (id) ON DELETE CASCADE,
  document_id       uuid NOT NULL,
  ord               int NOT NULL,
  page              int NOT NULL,
  section           text NOT NULL DEFAULT '',
  context_header    text NOT NULL DEFAULT '',
  text              text NOT NULL,
  codes             text NOT NULL DEFAULT '',
  tokens            int NOT NULL,
  bbox              jsonb,
  -- regconfig column: the text-to-regconfig cast is not immutable and cannot be used in a generated column.
  lang              regconfig NOT NULL DEFAULT 'simple',
  tsv               tsvector GENERATED ALWAYS AS (to_tsvector(lang, text) || to_tsvector('simple', codes)) STORED,
  suspect_injection boolean NOT NULL DEFAULT false,
  is_current        boolean NOT NULL DEFAULT false,
  acl_set_id        uuid,
  UNIQUE (version_id, ord)
);
CREATE INDEX chunks_tsv ON documents.chunks USING gin (tsv) WHERE is_current;
CREATE INDEX chunks_document ON documents.chunks (document_id);

-- Vectors live apart from the chunk so a new embedding model can be built next to the old one. The filters the
-- vector query needs are copied here: with them in another table, iterative HNSW scans lose recall.
CREATE TABLE documents.chunk_embeddings (
  tenant_id   uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  chunk_id    uuid NOT NULL REFERENCES documents.chunks (id) ON DELETE CASCADE,
  embed_model text NOT NULL,
  embedding   vector NOT NULL,
  acl_set_id  uuid,
  is_current  boolean NOT NULL DEFAULT false,
  PRIMARY KEY (chunk_id, embed_model)
);
-- An index on a column without a fixed size needs a cast; queries must use the same cast and the same WHERE.
CREATE INDEX chunk_embeddings_bge_m3 ON documents.chunk_embeddings
  USING hnsw ((embedding::vector(1024)) vector_cosine_ops) WHERE embed_model = 'bge-m3';

ALTER TABLE documents.chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents.chunk_embeddings ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON documents.chunks USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON documents.chunk_embeddings USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

CREATE TABLE search.conversations (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id  uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  user_id    uuid NOT NULL,
  title      text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conversations_user ON search.conversations (user_id, id DESC);

-- Questions and answers can hold personal data, so they live here, where they can be erased. The audit log
-- records only the ids and the outcome of each answer.
CREATE TABLE search.answers (
  id                  uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id           uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  conversation_id     uuid NOT NULL REFERENCES search.conversations (id) ON DELETE CASCADE,
  user_id             uuid NOT NULL,
  question            text NOT NULL,
  asked_at            timestamptz NOT NULL,
  outcome             text NOT NULL CHECK (outcome IN ('answered', 'not_found', 'refused', 'error')),
  text                text NOT NULL DEFAULT '',
  notes               text[] NOT NULL DEFAULT '{}',
  nearby              uuid[] NOT NULL DEFAULT '{}',
  latency_ms          int NOT NULL DEFAULT 0,
  model               text NOT NULL DEFAULT '',
  scope               text,
  feedback            text CHECK (feedback IN ('up', 'down')),
  -- what is needed to reproduce the answer (AI Act art. 12)
  prompt_version      text NOT NULL DEFAULT '',
  embed_model         text NOT NULL DEFAULT '',
  rerank_model        text NOT NULL DEFAULT '',
  not_found_threshold real NOT NULL DEFAULT 0,
  context_hash        text NOT NULL DEFAULT ''
);
CREATE INDEX answers_conversation ON search.answers (conversation_id, asked_at);

CREATE TABLE search.citations (
  tenant_id   uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  answer_id   uuid NOT NULL REFERENCES search.answers (id) ON DELETE CASCADE,
  n           int NOT NULL,
  document_id uuid NOT NULL,
  version_id  uuid NOT NULL,
  chunk_id    uuid NOT NULL,
  label       text NOT NULL DEFAULT '', -- the document's code or title when it was cited
  page        int NOT NULL,
  section     text NOT NULL DEFAULT '',
  quote       text NOT NULL,
  score       real NOT NULL,
  bbox        jsonb,
  PRIMARY KEY (answer_id, n)
);

CREATE TABLE search.answer_candidates (
  tenant_id     uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  answer_id     uuid NOT NULL REFERENCES search.answers (id) ON DELETE CASCADE,
  chunk_id      uuid NOT NULL,
  version_id    uuid NOT NULL,
  rank_lexical  int,
  rank_vector   int,
  rrf_score     real NOT NULL,
  rerank_score  real,
  sent_to_llm   boolean NOT NULL DEFAULT false,
  PRIMARY KEY (answer_id, chunk_id)
);

ALTER TABLE search.conversations ENABLE ROW LEVEL SECURITY;
ALTER TABLE search.answers ENABLE ROW LEVEL SECURITY;
ALTER TABLE search.citations ENABLE ROW LEVEL SECURITY;
ALTER TABLE search.answer_candidates ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON search.conversations USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON search.answers USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON search.citations USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON search.answer_candidates USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- Published by documents for search: the only way search reads passages (ATLAS.md §5.2). security_invoker makes
-- the caller's row-level security apply, as on the tables.
CREATE VIEW documents.v_searchable_chunks WITH (security_invoker = true) AS
SELECT c.id AS chunk_id, c.version_id, c.document_id, c.ord, c.page, c.section, c.context_header, c.text, c.tsv,
       c.bbox, c.suspect_injection, c.acl_set_id, d.title AS document_title, d.code AS document_code
FROM documents.chunks c JOIN documents.documents d ON d.id = c.document_id
WHERE c.is_current AND d.deleted_at IS NULL;

CREATE VIEW documents.v_searchable_vectors WITH (security_invoker = true) AS
SELECT e.chunk_id, e.embed_model, e.embedding, e.acl_set_id FROM documents.chunk_embeddings e WHERE e.is_current;

-- +goose Down
DROP VIEW documents.v_searchable_vectors, documents.v_searchable_chunks;
DROP TABLE search.answer_candidates, search.citations, search.answers, search.conversations;
DROP TABLE documents.chunk_embeddings, documents.chunks;
DROP TABLE access.acl_sets;
