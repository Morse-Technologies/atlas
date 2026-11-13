-- An administrator can stop reading a folder: the source is kept, marked, and no longer listed, scanned or
-- watched, and its documents are retired by a job. The folder itself is never touched.

-- +goose Up
ALTER TABLE sources.sources ADD COLUMN removed_at timestamptz;

-- +goose Down
ALTER TABLE sources.sources DROP COLUMN removed_at;
