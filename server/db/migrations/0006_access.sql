-- Users, roles, sessions and login challenges (ATLAS.md §13).
-- users and user_roles are tenant-scoped by RLS. sessions and login_challenges are looked up by the hash of a
-- secret before the tenant is known, so they carry tenant_id explicitly and have no RLS (like access.tenants).

-- +goose Up
CREATE TABLE access.users (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id       uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid REFERENCES access.tenants (id),
  name            text NOT NULL,
  email           text NOT NULL,
  department      text NOT NULL DEFAULT '',
  password_hash   text NOT NULL,
  totp_secret_enc bytea,
  is_active       boolean NOT NULL DEFAULT true,
  failed_logins   int NOT NULL DEFAULT 0,
  locked_until    timestamptz,
  last_active_at  timestamptz,
  hue             int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email ON access.users (tenant_id, lower(email));

CREATE TABLE access.user_roles (
  tenant_id uuid NOT NULL DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  user_id   uuid NOT NULL REFERENCES access.users (id) ON DELETE CASCADE,
  role      text NOT NULL CHECK (role IN ('viewer', 'editor_metadata', 'curator', 'organizer_approver', 'admin', 'auditor')),
  PRIMARY KEY (user_id, role)
);

CREATE TABLE access.sessions (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id    uuid NOT NULL REFERENCES access.tenants (id),
  user_id      uuid NOT NULL REFERENCES access.users (id) ON DELETE CASCADE,
  token_hash   bytea NOT NULL UNIQUE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL
);
CREATE INDEX sessions_user ON access.sessions (user_id);

CREATE TABLE access.login_challenges (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  tenant_id  uuid NOT NULL REFERENCES access.tenants (id),
  user_id    uuid NOT NULL REFERENCES access.users (id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  attempts   int NOT NULL DEFAULT 0,
  expires_at timestamptz NOT NULL
);

ALTER TABLE access.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE access.user_roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON access.users USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
CREATE POLICY tenant_isolation ON access.user_roles USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- +goose Down
DROP TABLE access.login_challenges, access.sessions, access.user_roles, access.users;
