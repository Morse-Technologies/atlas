-- The installation signs the audit reports it exports, so a report can be checked later and a changed byte shows.
-- One Ed25519 pair per tenant, made the first time it is needed. The private half is sealed with ATLAS_SECRET_KEY.

-- +goose Up
CREATE TABLE platform.signing_keys (
  tenant_id          uuid PRIMARY KEY DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid REFERENCES access.tenants (id),
  key_id             text NOT NULL,
  public_key         bytea NOT NULL,
  private_key_sealed bytea NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE platform.signing_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON platform.signing_keys USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE platform.signing_keys;
