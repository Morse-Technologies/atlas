-- Idempotency keys for writes (ATLAS.md §10.2). A retry with the same key and the same request gets the first
-- answer back instead of repeating the change. Rows older than a day are purged on use.

-- +goose Up
CREATE TABLE platform.idempotency_keys (
  tenant_id    uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  user_id      uuid NOT NULL,
  key          text NOT NULL,
  request_hash text NOT NULL,
  status       int,          -- NULL while the first request is still running
  content_type text NOT NULL DEFAULT '',
  body         bytea NOT NULL DEFAULT ''::bytea,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, user_id, key)
);
CREATE INDEX idempotency_keys_age ON platform.idempotency_keys (created_at);
ALTER TABLE platform.idempotency_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.idempotency_keys USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE platform.idempotency_keys;
