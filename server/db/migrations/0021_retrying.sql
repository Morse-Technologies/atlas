-- A version whose step failed and will be tried again shows as "retrying"; one whose tries ran out goes to
-- quarantine with its own reason, so nobody is left looking at a document that is "processing" forever.

-- +goose Up
ALTER TABLE documents.document_versions DROP CONSTRAINT document_versions_state_check;
ALTER TABLE documents.document_versions ADD CONSTRAINT document_versions_state_check
  CHECK (state IN ('processing', 'retrying', 'parsed', 'indexed', 'superseded', 'quarantined'));
ALTER TABLE sources.quarantine DROP CONSTRAINT quarantine_reason_check;
ALTER TABLE sources.quarantine ADD CONSTRAINT quarantine_reason_check
  CHECK (reason IN ('encrypted', 'corrupt', 'unsupported', 'too_large', 'hostile', 'retries_exhausted'));

-- +goose Down
DELETE FROM sources.quarantine WHERE reason = 'retries_exhausted';
UPDATE documents.document_versions SET state = 'processing' WHERE state = 'retrying';
ALTER TABLE sources.quarantine DROP CONSTRAINT quarantine_reason_check;
ALTER TABLE sources.quarantine ADD CONSTRAINT quarantine_reason_check
  CHECK (reason IN ('encrypted', 'corrupt', 'unsupported', 'too_large', 'hostile'));
ALTER TABLE documents.document_versions DROP CONSTRAINT document_versions_state_check;
ALTER TABLE documents.document_versions ADD CONSTRAINT document_versions_state_check
  CHECK (state IN ('processing', 'parsed', 'indexed', 'superseded', 'quarantined'));
