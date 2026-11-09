#!/usr/bin/env bash
# Prints the query plans (EXPLAIN ANALYZE, BUFFERS) of the hot queries, as the app role with row-level security
# for the first person of the first organization. Used by perf.sh after the corpus is loaded.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root/deploy/compose"
app_password="$(sed -n 's/^ATLAS_APP_PASSWORD=//p' .env.dev)"
psql_app() { docker compose --env-file .env.dev -f compose.yaml -f perf.override.yaml exec -T -e PGPASSWORD="$app_password" postgres psql -h 127.0.0.1 -U atlas_app -d atlas "$@"; }
tenant="$(docker compose --env-file .env.dev exec -T postgres psql -U atlas_owner -d atlas -Atc 'select id from access.tenants limit 1')"
person="$(docker compose --env-file .env.dev exec -T postgres psql -U atlas_owner -d atlas -Atc 'select id from access.users limit 1')"

psql_app <<SQL
\\set QUIET on
BEGIN;
SELECT set_config('atlas.tenant_id', '$tenant', true), set_config('atlas.user_id', '$person', true) \\gset
\\echo '-- word search: three topic words and two very common ones'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON)
SELECT chunk_id, ts_rank_cd(tsv, to_tsquery('simple', 'tema017p01 | tema017p02 | tema017p03 | comun000 | comun001'))::float4 AS rank
FROM documents.v_searchable_chunks
WHERE tsv @@ to_tsquery('simple', 'tema017p01 | tema017p02 | tema017p03 | comun000 | comun001')
  AND acl_set_id = ANY (ARRAY(SELECT access.readable_acl_sets('$person'::uuid)))
ORDER BY rank DESC, chunk_id LIMIT 50;

\\echo '-- vector search'
SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true) \\gset
SELECT embedding::text AS probe FROM documents.chunk_embeddings OFFSET 1000 LIMIT 1 \\gset
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON)
SELECT chunk_id, ((embedding::vector(1024)) <=> (:'probe'::text)::vector(1024))::float4 AS distance
FROM documents.v_searchable_vectors
WHERE embed_model = 'bge-m3' AND acl_set_id = ANY (ARRAY(SELECT access.readable_acl_sets('$person'::uuid)))
ORDER BY (embedding::vector(1024)) <=> (:'probe'::text)::vector(1024) LIMIT 50;

\\echo '-- first page of documents'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON)
SELECT * FROM documents.documents d WHERE d.deleted_at IS NULL AND d.current_version_id IS NOT NULL AND d.id > '00000000-0000-0000-0000-000000000000'
ORDER BY d.id LIMIT 51;

\\echo '-- documents filtered by a word of the title'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON)
SELECT * FROM documents.documents d WHERE d.deleted_at IS NULL AND d.current_version_id IS NOT NULL AND d.id > '00000000-0000-0000-0000-000000000000'
  AND (d.title ILIKE '%tema017%' OR d.code ILIKE '%tema017%' OR d.external_id ILIKE '%tema017%')
ORDER BY d.id LIMIT 51;

\\echo '-- documents counted by type (the panel)'
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, SUMMARY ON)
SELECT doc_type, count(*) FROM documents.documents WHERE deleted_at IS NULL AND current_version_id IS NOT NULL GROUP BY doc_type;
ROLLBACK;
SQL
