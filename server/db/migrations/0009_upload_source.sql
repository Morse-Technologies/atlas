-- Files added through the API live in a folder Atlas manages, scanned like any other source (kind 'upload').

-- +goose Up
ALTER TABLE sources.sources DROP CONSTRAINT sources_kind_check;
ALTER TABLE sources.sources ADD CONSTRAINT sources_kind_check CHECK (kind IN ('folder', 'upload'));

-- +goose Down
ALTER TABLE sources.sources DROP CONSTRAINT sources_kind_check;
ALTER TABLE sources.sources ADD CONSTRAINT sources_kind_check CHECK (kind IN ('folder'));
