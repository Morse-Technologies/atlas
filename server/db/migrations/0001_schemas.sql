-- One schema per module (ATLAS.md §5.1). Runs as the migration role; the app role gets data access only.

-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'atlas_app') THEN
    -- Login and password are set by the deployment (compose init script or test helper), never here.
    CREATE ROLE atlas_app NOLOGIN NOBYPASSRLS;
  END IF;
END $$;
-- +goose StatementEnd

CREATE SCHEMA sources;
CREATE SCHEMA documents;
CREATE SCHEMA catalog;
CREATE SCHEMA search;
CREATE SCHEMA access;
CREATE SCHEMA graph;
CREATE SCHEMA audit;
CREATE SCHEMA platform;
CREATE SCHEMA river;

GRANT USAGE ON SCHEMA sources, documents, catalog, search, access, graph, audit, platform, river TO atlas_app;

-- Tables created later by this role are readable and writable by the app; RLS policies do the tenant filtering.
ALTER DEFAULT PRIVILEGES IN SCHEMA sources, documents, catalog, search, access, graph, platform, river
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO atlas_app;
-- Audit is append-only for the app (ATLAS.md §15).
ALTER DEFAULT PRIVILEGES IN SCHEMA audit GRANT SELECT, INSERT ON TABLES TO atlas_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA river GRANT USAGE, SELECT ON SEQUENCES TO atlas_app;

-- +goose Down
DROP SCHEMA river, platform, audit, graph, access, search, catalog, documents, sources CASCADE;
