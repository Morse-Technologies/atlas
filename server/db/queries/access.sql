-- name: CreateTenant :one
INSERT INTO access.tenants (org_name) VALUES (@org_name) RETURNING id;

-- name: ListTenants :many
SELECT id, org_name FROM access.tenants ORDER BY created_at;

-- name: CountUsers :one
SELECT count(*) FROM access.users;

-- Serializes first-run setup so two requests cannot both create the first admin.
-- name: LockSetup :exec
SELECT pg_advisory_xact_lock(@key::bigint);

-- name: CreateUser :one
INSERT INTO access.users (name, email, password_hash, totp_secret_enc, hue)
VALUES (@name, @email, @password_hash, @totp_secret_enc, @hue) RETURNING id;

-- name: AddRole :exec
INSERT INTO access.user_roles (user_id, role) VALUES (@user_id, @role) ON CONFLICT DO NOTHING;

-- name: GetUserByEmail :one
SELECT * FROM access.users WHERE lower(email) = lower(@email);

-- name: GetUser :one
SELECT * FROM access.users WHERE id = @id;

-- name: ListUsers :many
SELECT * FROM access.users WHERE id > @after ORDER BY id LIMIT @page_size;

-- name: ListRolesOf :many
SELECT user_id, role FROM access.user_roles WHERE user_id = ANY(@user_ids::uuid[]) ORDER BY role;

-- name: ClearRoles :exec
DELETE FROM access.user_roles WHERE user_id = @user_id;

-- name: LockAdmins :many
SELECT user_id FROM access.user_roles WHERE role = 'admin' FOR UPDATE;

-- name: DeleteUser :execrows
DELETE FROM access.users WHERE id = @id;

-- name: RecordFailedLogin :exec
UPDATE access.users
SET failed_logins = failed_logins + 1,
    locked_until = CASE WHEN failed_logins + 1 >= @max_failures THEN @locked_until::timestamptz ELSE locked_until END
WHERE id = @id;

-- name: RecordLogin :exec
UPDATE access.users SET failed_logins = 0, locked_until = NULL, last_active_at = @now WHERE id = @id;

-- name: CreateChallenge :exec
INSERT INTO access.login_challenges (tenant_id, user_id, token_hash, expires_at)
VALUES (@tenant_id, @user_id, @token_hash, @expires_at);

-- name: GetChallenge :one
SELECT * FROM access.login_challenges WHERE token_hash = @token_hash AND expires_at > @now;

-- name: BumpChallengeAttempts :one
UPDATE access.login_challenges SET attempts = attempts + 1 WHERE id = @id RETURNING attempts;

-- name: DeleteChallenge :exec
DELETE FROM access.login_challenges WHERE id = @id;

-- name: CreateSession :exec
INSERT INTO access.sessions (tenant_id, user_id, token_hash, expires_at)
VALUES (@tenant_id, @user_id, @token_hash, @expires_at);

-- name: GetSession :one
SELECT * FROM access.sessions WHERE token_hash = @token_hash AND expires_at > @now;

-- name: TouchSession :exec
UPDATE access.sessions SET last_seen_at = @now WHERE id = @id AND last_seen_at < @stale_before;

-- name: DeleteSession :exec
DELETE FROM access.sessions WHERE id = @id;

-- name: DeleteExpiredSessions :exec
DELETE FROM access.sessions WHERE expires_at <= @now;

-- name: GetDefaultACLSet :one
SELECT id FROM access.acl_sets WHERE is_default;

-- name: CreateDefaultACLSet :one
INSERT INTO access.acl_sets (origin, is_default) VALUES ('core', true)
ON CONFLICT (tenant_id) WHERE is_default DO UPDATE SET origin = access.acl_sets.origin
RETURNING id;

-- name: LockCurrentTenantSettings :one
SELECT org_name, settings FROM access.tenants
WHERE id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid FOR UPDATE;

-- name: GetCurrentTenantSettings :one
SELECT org_name, settings FROM access.tenants
WHERE id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid;

-- name: SaveCurrentTenantSettings :exec
UPDATE access.tenants SET org_name = @org_name, settings = @settings
WHERE id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid;

-- name: SetPasswordHash :execrows
UPDATE access.users SET password_hash = @password_hash, failed_logins = 0, locked_until = NULL WHERE id = @id;

-- name: SetTOTPSecret :execrows
UPDATE access.users SET totp_secret_enc = @totp_secret_enc WHERE id = @id;

-- name: DeleteSessionsOf :exec
DELETE FROM access.sessions WHERE user_id = @user_id;

-- name: DeleteOtherSessionsOf :exec
DELETE FROM access.sessions WHERE user_id = @user_id AND id <> @keep;

-- name: DeleteChallengesOf :exec
DELETE FROM access.login_challenges WHERE user_id = @user_id;

-- name: CreateReadersACLSet :one
INSERT INTO access.acl_sets (origin, allow) VALUES ('source', @readers::uuid[]) RETURNING id;

-- name: SetACLSetReaders :execrows
UPDATE access.acl_sets SET allow = @readers::uuid[] WHERE id = @id AND NOT is_default;

-- name: GetACLSetReaders :one
SELECT allow FROM access.acl_sets WHERE id = @id;

-- name: DeleteACLSet :exec
DELETE FROM access.acl_sets WHERE id = @id AND NOT is_default;

-- name: ListReadableACLSets :many
SELECT access.readable_acl_sets(@person::uuid) AS id;
