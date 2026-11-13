-- name: CreateFolderSource :one
INSERT INTO sources.sources (name, kind, root_path) VALUES (@name, 'folder', @root_path) RETURNING id;

-- name: ListSources :many
SELECT id, name, kind, root_path, status, last_scan_at, last_error, acl_set_id FROM sources.sources WHERE removed_at IS NULL ORDER BY created_at;

-- name: ListDueSources :many
SELECT id FROM sources.sources
WHERE removed_at IS NULL AND status IN ('active', 'error') AND (last_scan_at IS NULL OR last_scan_at + rescan_interval <= now());

-- name: GetSourceRoot :one
SELECT root_path, max_file_bytes FROM sources.sources WHERE id = @id AND removed_at IS NULL;

-- name: StartScan :one
-- Bumps the generation and records the scan as running in one statement; FinishScan closes it.
WITH bumped AS (
  UPDATE sources.sources src SET scan_generation = src.scan_generation + 1 WHERE src.id = @id AND src.removed_at IS NULL
  RETURNING src.id, src.scan_generation, src.root_path
), started AS (
  INSERT INTO sources.scans (source_id, generation, status, discovered, changed, moved, deleted, held_deletions, started_at)
  SELECT id, scan_generation, 'running', 0, 0, 0, 0, 0, @started_at FROM bumped
  RETURNING id
)
SELECT bumped.scan_generation, bumped.root_path, started.id AS scan_id FROM bumped, started;

-- name: FailStaleScans :exec
-- A scan still running long after the job timeout was interrupted (the process died): close it as failed.
UPDATE sources.scans SET status = 'failed', error = 'interrupted', finished_at = now()
WHERE source_id = @source_id AND status = 'running' AND started_at < @older_than;

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

-- name: FinishScan :exec
UPDATE sources.scans SET status = @status, discovered = @discovered, changed = @changed, moved = @moved,
  deleted = @deleted, held_deletions = @held_deletions, error = @error, finished_at = now()
WHERE id = @id;

-- name: FinishSourceScan :exec
-- A failing scan puts the source in error (unless an administrator paused it); the next good scan clears it.
UPDATE sources.sources SET last_scan_at = now(), last_error = sqlc.narg('last_error')::text,
  status = CASE
    WHEN sqlc.narg('last_error')::text IS NOT NULL AND status <> 'paused' THEN 'error'
    WHEN sqlc.narg('last_error')::text IS NULL AND status = 'error' THEN 'active'
    ELSE status
  END
WHERE id = @id;

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
SELECT id, name, kind, root_path, status, last_scan_at, last_error, acl_set_id FROM sources.sources WHERE id = @id AND removed_at IS NULL;

-- name: SetSourceStatus :execrows
UPDATE sources.sources SET status = @status WHERE id = @id AND removed_at IS NULL;

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

-- name: RemoveSource :execrows
-- The source stops being read: it leaves every list, is paused for good measure, and its files count as deleted.
WITH removed AS (
  UPDATE sources.sources SET removed_at = @removed_at, status = 'paused' WHERE id = @id AND removed_at IS NULL RETURNING id
), gone AS (
  UPDATE sources.files SET state = 'deleted', updated_at = now() WHERE source_id IN (SELECT id FROM removed)
)
SELECT count(*) FROM removed;

-- name: GetSourceKinds :many
SELECT id, kind FROM sources.sources WHERE id = ANY(@ids::uuid[]);
