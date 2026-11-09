-- A version is "parsed" when its blocks exist and "indexed" when search can find its passages. Until now parsed was
-- shown as indexed, so a document looked ready while chunks and vectors were still being made, and a question
-- asked in that window found nothing.

-- +goose Up
ALTER TABLE documents.document_versions DROP CONSTRAINT document_versions_state_check;
ALTER TABLE documents.document_versions ADD CONSTRAINT document_versions_state_check
  CHECK (state IN ('processing', 'parsed', 'indexed', 'superseded', 'quarantined'));
-- Versions that were current before this migration already have published passages when they were indexed.
UPDATE documents.document_versions v SET state = 'indexed'
WHERE v.state = 'parsed' AND EXISTS (SELECT 1 FROM documents.chunks c WHERE c.version_id = v.id AND c.is_current);

-- +goose Down
UPDATE documents.document_versions SET state = 'parsed' WHERE state = 'indexed';
ALTER TABLE documents.document_versions DROP CONSTRAINT document_versions_state_check;
ALTER TABLE documents.document_versions ADD CONSTRAINT document_versions_state_check
  CHECK (state IN ('processing', 'parsed', 'superseded', 'quarantined'));
