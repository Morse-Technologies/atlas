-- name: CurrentTenant :one
SELECT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid AS tenant_id;

-- Serializes writers of one tenant's chain until the transaction ends.
-- name: LockChain :exec
SELECT pg_advisory_xact_lock(hashtext((sqlc.arg(tenant_id)::uuid)::text));

-- name: LastHash :one
SELECT hash FROM audit.events ORDER BY id DESC LIMIT 1;

-- name: NextEventID :one
SELECT nextval(pg_get_serial_sequence('audit.events', 'id'))::bigint AS id;

-- name: InsertEvent :exec
INSERT INTO audit.events (id, at, actor_id, action, subject, detail, prev_hash, hash)
VALUES (@id, @at, @actor_id, @action, @subject, @detail, @prev_hash, @hash);

-- name: ListEventsNewestFirst :many
SELECT * FROM audit.events WHERE id < @before ORDER BY id DESC LIMIT @page_size;

-- name: ListEventsOldestFirst :many
SELECT * FROM audit.events WHERE id > @after ORDER BY id LIMIT @page_size;
