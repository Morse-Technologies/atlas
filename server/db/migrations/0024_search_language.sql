-- Word search that ignores accents and knows the stems of the organization's language ("husillos" finds "husillo").
-- One text-search configuration per choice; the organization's choice is one row; chunks carry the configuration
-- their word index (tsv) was built with, and the search reads the same one. Default: no stems, accents ignored.

-- +goose Up
CREATE TEXT SEARCH CONFIGURATION documents.atlas_es (COPY = pg_catalog.spanish);
ALTER TEXT SEARCH CONFIGURATION documents.atlas_es
  ALTER MAPPING FOR asciiword, word, numword, asciihword, hword, numhword, hword_asciipart, hword_part, hword_numpart WITH unaccent, spanish_stem;
CREATE TEXT SEARCH CONFIGURATION documents.atlas_en (COPY = pg_catalog.english);
ALTER TEXT SEARCH CONFIGURATION documents.atlas_en
  ALTER MAPPING FOR asciiword, word, numword, asciihword, hword, numhword, hword_asciipart, hword_part, hword_numpart WITH unaccent, english_stem;
CREATE TEXT SEARCH CONFIGURATION documents.atlas_none (COPY = pg_catalog.simple);
ALTER TEXT SEARCH CONFIGURATION documents.atlas_none
  ALTER MAPPING FOR asciiword, word, numword, asciihword, hword, numhword, hword_asciipart, hword_part, hword_numpart WITH unaccent, simple;

CREATE TABLE documents.search_language (
  tenant_id  uuid PRIMARY KEY DEFAULT NULLIF(current_setting('atlas.tenant_id', true), '')::uuid,
  language   text NOT NULL DEFAULT 'none' CHECK (language IN ('es', 'en', 'none')),
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE documents.search_language ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON documents.search_language USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);

-- The configuration of the current tenant. It runs as its owner so the search function below can use it under
-- row-level security, and it reads the same setting the policies use.
CREATE FUNCTION documents.tenant_regconfig() RETURNS regconfig
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT COALESCE(
    (SELECT CASE s.language WHEN 'es' THEN 'documents.atlas_es' WHEN 'en' THEN 'documents.atlas_en' END::regconfig
     FROM documents.search_language s WHERE s.tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid),
    'documents.atlas_none'::regconfig)
$$;
REVOKE ALL ON FUNCTION documents.tenant_regconfig() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION documents.tenant_regconfig() TO atlas_app;

-- Existing passages are re-labelled here; changing the language later is done in batches by a job (documents).
ALTER TABLE documents.chunks ALTER COLUMN lang SET DEFAULT 'documents.atlas_none'::regconfig;
UPDATE documents.chunks SET lang = 'documents.atlas_none'::regconfig;

CREATE OR REPLACE FUNCTION documents.search_words(query text, acl_sets uuid[], top_k int) RETURNS TABLE (chunk_id uuid, rank real)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT c.id, ts_rank_cd(c.tsv, q)::real
  FROM documents.chunks c, to_tsquery(documents.tenant_regconfig(), query) AS q
  WHERE c.tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid
    AND c.is_current AND c.tsv @@ q AND c.acl_set_id = ANY (acl_sets)
  ORDER BY 2 DESC, c.id LIMIT top_k
$$;

-- +goose Down
CREATE OR REPLACE FUNCTION documents.search_words(query text, acl_sets uuid[], top_k int) RETURNS TABLE (chunk_id uuid, rank real)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT c.id, ts_rank_cd(c.tsv, q)::real
  FROM documents.chunks c, to_tsquery('simple', query) AS q
  WHERE c.tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid
    AND c.is_current AND c.tsv @@ q AND c.acl_set_id = ANY (acl_sets)
  ORDER BY 2 DESC, c.id LIMIT top_k
$$;
ALTER TABLE documents.chunks ALTER COLUMN lang SET DEFAULT 'simple';
UPDATE documents.chunks SET lang = 'simple';
DROP FUNCTION documents.tenant_regconfig();
DROP TABLE documents.search_language;
DROP TEXT SEARCH CONFIGURATION documents.atlas_none;
DROP TEXT SEARCH CONFIGURATION documents.atlas_en;
DROP TEXT SEARCH CONFIGURATION documents.atlas_es;
