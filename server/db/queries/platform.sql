-- name: PurgeOldKeys :exec
DELETE FROM platform.idempotency_keys WHERE created_at < @older_than;

-- name: ClaimKey :execrows
INSERT INTO platform.idempotency_keys (user_id, key, request_hash) VALUES (@user_id, @key, @request_hash)
ON CONFLICT DO NOTHING;

-- name: GetKey :one
SELECT request_hash, status, content_type, body, created_at FROM platform.idempotency_keys WHERE user_id = @user_id AND key = @key;

-- name: FinishKey :exec
UPDATE platform.idempotency_keys SET status = @status, content_type = @content_type, body = @body
WHERE user_id = @user_id AND key = @key;

-- name: ReleaseKey :exec
DELETE FROM platform.idempotency_keys WHERE user_id = @user_id AND key = @key;
