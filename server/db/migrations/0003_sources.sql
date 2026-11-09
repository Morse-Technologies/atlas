-- Sources, scanned files, scans, pipeline steps and quarantine (ATLAS.md §6, §7.4, §8.1).

-- +goose Up
CREATE TABLE sources.sources (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id       uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  name            text NOT NULL,
  kind            text NOT NULL CHECK (kind IN ('folder')),
  root_path       text NOT NULL,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'error')),
  max_file_bytes  bigint NOT NULL DEFAULT 209715200, -- 200 MB, ATLAS.md §8.1
  rescan_interval interval NOT NULL DEFAULT '15 minutes',
  scan_generation bigint NOT NULL DEFAULT 0,
  last_scan_at    timestamptz,
  last_error      text,
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sources.files (
  tenant_id       uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  source_id       uuid NOT NULL REFERENCES sources.sources (id) ON DELETE CASCADE,
  external_id     text NOT NULL,
  size            bigint NOT NULL,
  modified_at     timestamptz NOT NULL,
  content_hash    text,
  seen_generation bigint NOT NULL,
  state           text NOT NULL DEFAULT 'present' CHECK (state IN ('present', 'deleted')),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (source_id, external_id)
);
CREATE INDEX files_hash ON sources.files (source_id, content_hash);

CREATE TABLE sources.scans (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id      uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  source_id      uuid NOT NULL REFERENCES sources.sources (id) ON DELETE CASCADE,
  generation     bigint NOT NULL,
  status         text NOT NULL CHECK (status IN ('completed', 'failed', 'held')),
  discovered     int NOT NULL,
  changed        int NOT NULL,
  moved          int NOT NULL,
  deleted        int NOT NULL,
  held_deletions int NOT NULL,
  error          text,
  started_at     timestamptz NOT NULL,
  finished_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sources.pipeline_steps (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id    uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  source_id    uuid NOT NULL,
  external_id  text NOT NULL,
  content_hash text,
  step         text NOT NULL,
  status       text NOT NULL CHECK (status IN ('done', 'skipped', 'failed')),
  detail       text,
  at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pipeline_steps_file ON sources.pipeline_steps (source_id, external_id, at);

CREATE TABLE sources.quarantine (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id   uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  source_id   uuid NOT NULL REFERENCES sources.sources (id) ON DELETE CASCADE,
  external_id text NOT NULL,
  reason      text NOT NULL CHECK (reason IN ('encrypted', 'corrupt', 'unsupported', 'too_large', 'hostile')),
  detail      text NOT NULL,
  at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (source_id, external_id)
);

ALTER TABLE sources.sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE sources.files ENABLE ROW LEVEL SECURITY;
ALTER TABLE sources.scans ENABLE ROW LEVEL SECURITY;
ALTER TABLE sources.pipeline_steps ENABLE ROW LEVEL SECURITY;
ALTER TABLE sources.quarantine ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sources.sources USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON sources.files USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON sources.scans USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON sources.pipeline_steps USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON sources.quarantine USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE sources.quarantine, sources.pipeline_steps, sources.scans, sources.files, sources.sources;
