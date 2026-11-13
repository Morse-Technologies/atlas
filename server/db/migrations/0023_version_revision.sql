-- The revision a document says it is ("Rev. 3" in its title block, or "rev3" in its file name), kept on the version
-- it was read from. Empty when nothing could be read: the screens then number versions in creation order.

-- +goose Up
ALTER TABLE documents.document_versions ADD COLUMN revision text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE documents.document_versions DROP COLUMN revision;
