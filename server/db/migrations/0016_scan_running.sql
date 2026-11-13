-- A scan is recorded when it starts and closed when it ends, so the Sources screen can show one in progress.
-- Until it ends a scan has no finish time and the status 'running'.

-- +goose Up
ALTER TABLE sources.scans ALTER COLUMN finished_at DROP NOT NULL;
ALTER TABLE sources.scans ALTER COLUMN finished_at DROP DEFAULT;
ALTER TABLE sources.scans DROP CONSTRAINT scans_status_check;
ALTER TABLE sources.scans ADD CONSTRAINT scans_status_check CHECK (status IN ('running', 'completed', 'failed', 'held'));

-- +goose Down
UPDATE sources.scans SET status = 'failed', finished_at = now(), error = 'interrupted' WHERE status = 'running';
ALTER TABLE sources.scans DROP CONSTRAINT scans_status_check;
ALTER TABLE sources.scans ADD CONSTRAINT scans_status_check CHECK (status IN ('completed', 'failed', 'held'));
ALTER TABLE sources.scans ALTER COLUMN finished_at SET DEFAULT now();
ALTER TABLE sources.scans ALTER COLUMN finished_at SET NOT NULL;
