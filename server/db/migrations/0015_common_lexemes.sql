-- Search functions. First, the words that are in a large share of the passages (the planner already counts them for each column). A word
-- search that includes them matches nearly every passage, so every passage is ranked: the cost grows with the
-- corpus. The search leaves them out when a rarer word is there (ATLAS.md §11). The function reads the planner's
-- statistics, which the app role cannot see when row-level security is on, so it runs as its owner.

-- +goose Up
CREATE FUNCTION documents.common_lexemes(min_share real) RETURNS SETOF text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT u.lexeme
  FROM pg_stats s,
       -- the frequencies have three more values than the words (least, most, null share): those pair with no word
       LATERAL unnest(s.most_common_elems::text::text[], s.most_common_elem_freqs) AS u (lexeme, share)
  WHERE s.schemaname = 'documents' AND s.tablename = 'chunks' AND s.attname = 'tsv' AND u.lexeme IS NOT NULL AND u.share >= min_share
$$;
REVOKE ALL ON FUNCTION documents.common_lexemes(real) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION documents.common_lexemes(real) TO atlas_app;

-- The word search. Under row-level security the planner may not use the word index for a user's condition
-- (`@@` is not leakproof, so it could leak rows a policy hides), and reads every passage: 300 ms at 300,000
-- passages instead of 3. A definer function applies the tenant itself, from the same setting the policies use,
-- and the sets a person may read, which the caller computes (the passages carry their source's set).
CREATE FUNCTION documents.search_words(query text, acl_sets uuid[], top_k int) RETURNS TABLE (chunk_id uuid, rank real)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
  SELECT c.id, ts_rank_cd(c.tsv, q)::real
  FROM documents.chunks c, to_tsquery('simple', query) AS q
  WHERE c.tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid
    AND c.is_current AND c.tsv @@ q AND c.acl_set_id = ANY (acl_sets)
  ORDER BY 2 DESC, c.id LIMIT top_k
$$;
REVOKE ALL ON FUNCTION documents.search_words(text, uuid[], int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION documents.search_words(text, uuid[], int) TO atlas_app;

-- +goose Down
DROP FUNCTION documents.search_words(text, uuid[], int);
DROP FUNCTION documents.common_lexemes(real);
