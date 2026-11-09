-- name: CreateFolderSource :one
INSERT INTO sources.sources (name, kind, root_path) VALUES (@name, 'folder', @root_path) RETURNING id;

-- name: ListSources :many
SELECT id, name, kind, root_path, status, last_scan_at, last_error, acl_set_id FROM sources.sources ORDER BY created_at;

-- name: ListDueSources :many
SELECT id FROM sources.sources
WHERE status = 'active' AND (last_scan_at IS NULL OR last_scan_at + rescan_interval <= now());

-- name: GetSourceRoot :one
SELECT root_path, max_file_bytes FROM sources.sources WHERE id = @id;

-- name: StartScan :one
UPDATE sources.sources SET scan_generation = scan_generation + 1 WHERE id = @id
RETURNING scan_generation, root_path;

-- name: ListPresentFiles :many
SELECT external_id, size, modified_at, content_hash FROM sources.files
WHERE source_id = @source_id AND state = 'present';

-- name: UpsertSeenFiles :exec
-- content_hash is left alone: it is the hash of the last fetched content, set only by fetch (or a move).
INSERT INTO sources.files (source_id, external_id, size, modified_at, seen_generation)
SELECT @source_id, unnest(@external_ids::text[]), unnest(@sizes::bigint[]),
       unnest(@modified_ats::timestamptz[]), @generation
ON CONFLICT (source_id, external_id) DO UPDATE SET
  size = EXCLUDED.size,
  modified_at = EXCLUDED.modified_at,
  seen_generation = EXCLUDED.seen_generation,
  state = 'present',
  updated_at = now();

-- name: MarkFilesDeleted :exec
UPDATE sources.files SET state = 'deleted', updated_at = now()
WHERE source_id = @source_id AND external_id = ANY(@external_ids::text[]);

-- name: DeleteFileRow :exec
DELETE FROM sources.files WHERE source_id = @source_id AND external_id = @external_id;

-- name: SetFileHash :exec
UPDATE sources.files SET content_hash = @content_hash, updated_at = now()
WHERE source_id = @source_id AND external_id = @external_id;

-- name: RecordFetchedFile :exec
-- A file can be fetched before any scan has listed it (the watcher is faster than the rescan), so the row
-- is created here too. seen_generation 0 means "not listed yet"; the next scan sets it.
INSERT INTO sources.files (source_id, external_id, size, modified_at, content_hash, seen_generation)
VALUES (@source_id, @external_id, @size, @modified_at, @content_hash, 0)
ON CONFLICT (source_id, external_id) DO UPDATE SET
  size = EXCLUDED.size,
  modified_at = EXCLUDED.modified_at,
  content_hash = EXCLUDED.content_hash,
  state = 'present',
  updated_at = now();

-- name: GetFileHash :one
SELECT content_hash FROM sources.files WHERE source_id = @source_id AND external_id = @external_id;

-- name: RecordScan :exec
INSERT INTO sources.scans (source_id, generation, status, discovered, changed, moved, deleted, held_deletions, error, started_at)
VALUES (@source_id, @generation, @status, @discovered, @changed, @moved, @deleted, @held_deletions, @error, @started_at);

-- name: FinishSourceScan :exec
UPDATE sources.sources SET last_scan_at = now(), last_error = @last_error WHERE id = @id;

-- name: RecordStep :exec
INSERT INTO sources.pipeline_steps (source_id, external_id, content_hash, step, status, detail)
VALUES (@source_id, @external_id, @content_hash, @step, @status, @detail);

-- name: UpsertQuarantine :exec
INSERT INTO sources.quarantine (source_id, external_id, reason, detail)
VALUES (@source_id, @external_id, @reason, @detail)
ON CONFLICT (source_id, external_id) DO UPDATE SET reason = EXCLUDED.reason, detail = EXCLUDED.detail, at = now();

-- name: ListQuarantine :many
SELECT id, source_id, external_id, reason, detail, at FROM sources.quarantine ORDER BY at DESC;

-- name: TakeQuarantined :one
DELETE FROM sources.quarantine WHERE id = @id RETURNING source_id, external_id;

-- name: GetSource :one
SELECT id, name, kind, root_path, status, last_scan_at, last_error, acl_set_id FROM sources.sources WHERE id = @id;

-- name: SetSourceStatus :execrows
UPDATE sources.sources SET status = @status WHERE id = @id;

-- name: ListScansPage :many
-- failed counts the files quarantined since the scan started: the scan itself only records what it listed.
SELECT s.id, s.source_id, s.started_at, s.finished_at, s.discovered, s.changed,
       (SELECT count(*) FROM sources.quarantine q WHERE q.source_id = s.source_id AND q.at >= s.started_at)::int AS failed
FROM sources.scans s WHERE s.id < @before ORDER BY s.id DESC LIMIT @page_size;

-- name: ListQuarantinePage :many
SELECT id, source_id, external_id, reason, detail, at FROM sources.quarantine
WHERE id < @before ORDER BY id DESC LIMIT @page_size;

-- name: FindUploadSource :one
SELECT id FROM sources.sources WHERE kind = 'upload' LIMIT 1;

-- name: CreateUploadSource :one
INSERT INTO sources.sources (name, kind, root_path) VALUES ('Uploads', 'upload', @root_path) RETURNING id;

-- name: CountQuarantine :one
SELECT count(*)::int FROM sources.quarantine;

-- name: SetSourceACLSet :execrows
UPDATE sources.sources SET acl_set_id = sqlc.narg(acl_set_id) WHERE id = @id;

-- name: GetSourceACLSet :one
SELECT acl_set_id FROM sources.sources WHERE id = @id;
