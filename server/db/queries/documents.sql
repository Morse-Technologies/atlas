-- name: UpsertDocument :one
INSERT INTO documents.documents (source_id, external_id, title, acl_set_id)
VALUES (@source_id, @external_id, @title, sqlc.narg(acl_set_id))
ON CONFLICT (source_id, external_id) DO UPDATE SET deleted_at = NULL, updated_at = now()
RETURNING id;

-- name: UpsertVersion :one
-- Content seen before (a revert) reuses its version; derived artifacts are cached, so reprocessing is cheap.
INSERT INTO documents.document_versions (document_id, content_hash, mime, size)
VALUES (@document_id, @content_hash, @mime, @size)
ON CONFLICT (document_id, content_hash) DO UPDATE SET state = 'processing'
RETURNING id;

-- name: GetVersion :one
SELECT v.id, v.document_id, v.content_hash, v.mime, d.source_id, d.external_id
FROM documents.document_versions v JOIN documents.documents d ON d.id = v.document_id
WHERE v.id = @id;

-- name: MarkVersionQuarantined :exec
UPDATE documents.document_versions SET state = 'quarantined' WHERE id = @id;

-- name: MarkVersionRetrying :exec
-- Only a version still on its way through the pipeline can be retrying; an indexed or replaced one is left as it is.
UPDATE documents.document_versions SET state = 'retrying' WHERE id = @id AND state IN ('processing', 'parsed');

-- name: ClearVersionRetrying :exec
UPDATE documents.document_versions SET state = 'processing' WHERE id = @id AND state = 'retrying';

-- name: MarkVersionParsed :exec
UPDATE documents.document_versions SET state = 'parsed', pages = @pages, parsed_at = now() WHERE id = @id;

-- name: MarkVersionIndexed :exec
UPDATE documents.document_versions SET state = 'indexed' WHERE id = @id;

-- name: SupersedeOtherVersions :exec
UPDATE documents.document_versions SET state = 'superseded'
WHERE document_id = @document_id AND id <> @current_id AND state IN ('parsed', 'indexed');

-- name: SetCurrentVersion :exec
UPDATE documents.documents SET current_version_id = @version_id, updated_at = now() WHERE id = @document_id;

-- name: DeleteBlocks :exec
DELETE FROM documents.blocks WHERE version_id = @version_id;

-- name: InsertBlocks :exec
-- COPY is not allowed on tables with RLS, so bulk insert goes through unnest.
INSERT INTO documents.blocks (version_id, ord, kind, page, bbox, text)
SELECT @version_id, unnest(@ords::int[]), unnest(@kinds::text[]), unnest(@pages::int[]),
       unnest(@bboxes::jsonb[]), unnest(@texts::text[]);

-- name: MarkDocumentDeleted :exec
UPDATE documents.documents SET deleted_at = now(), updated_at = now()
WHERE source_id = @source_id AND external_id = @external_id;

-- name: MoveDocument :exec
-- A title a person corrected stays; the path-derived title only replaces a title nobody edited.
UPDATE documents.documents d SET external_id = @new_external_id, updated_at = now(),
  title = CASE WHEN EXISTS (SELECT 1 FROM documents.field_provenance p
                            WHERE p.document_id = d.id AND p.field = 'title' AND p.source = 'manual')
               THEN d.title ELSE @title END
WHERE d.source_id = @source_id AND d.external_id = @old_external_id;

-- name: ListDocuments :many
-- A document is listed once it has a current version. doc_types is the allowed set when filter_types is true.
SELECT * FROM documents.documents d
WHERE d.deleted_at IS NULL AND d.current_version_id IS NOT NULL AND d.id > @after
  AND (@filter_types::boolean = false OR d.doc_type = ANY(@doc_types::text[]))
  AND (@status::text = '' OR d.status = @status)
  AND (sqlc.narg(source_id)::uuid IS NULL OR d.source_id = sqlc.narg(source_id))
  AND (@q::text = '' OR d.title ILIKE '%' || @q || '%' OR d.code ILIKE '%' || @q || '%' OR d.external_id ILIKE '%' || @q || '%')
ORDER BY d.id LIMIT @page_size;

-- name: GetDocument :one
SELECT * FROM documents.documents WHERE id = @id AND deleted_at IS NULL;

-- name: ListVersionsOf :many
SELECT id, document_id, state, pages, mime, created_at, parsed_at, revision FROM documents.document_versions
WHERE document_id = ANY(@document_ids::uuid[]) ORDER BY created_at, id;

-- name: ListProvenanceOf :many
SELECT document_id, field, value, source, confidence FROM documents.field_provenance
WHERE document_id = ANY(@document_ids::uuid[]) ORDER BY field;

-- name: UpdateMetadata :exec
UPDATE documents.documents SET title = @title, code = @code, doc_type = @doc_type, status = @status,
  department = @department, owner_id = @owner_id, approved_at = @approved_at, review_due_at = @review_due_at,
  language = @language, attributes = @attributes, updated_at = now()
WHERE id = @id;

-- name: SetManualProvenance :exec
INSERT INTO documents.field_provenance (document_id, field, value, source, confidence)
VALUES (@document_id, @field, @value, 'manual', 1)
ON CONFLICT (document_id, field) DO UPDATE SET value = EXCLUDED.value, source = 'manual', confidence = 1, updated_at = now();

-- name: SetVersionRevision :exec
UPDATE documents.document_versions SET revision = @revision WHERE id = @id;

-- name: SetExtractedProvenance :execrows
-- A field a person corrected (source "manual") is never replaced: zero rows tell the caller to leave the value alone.
INSERT INTO documents.field_provenance (document_id, field, value, source, confidence)
VALUES (@document_id, @field, @value, @source, @confidence)
ON CONFLICT (document_id, field) DO UPDATE SET value = EXCLUDED.value, source = EXCLUDED.source,
  confidence = EXCLUDED.confidence, updated_at = now()
WHERE documents.field_provenance.source <> 'manual';

-- name: GetVersionForViewer :one
SELECT v.id, v.document_id, v.content_hash, v.mime, d.external_id
FROM documents.document_versions v JOIN documents.documents d ON d.id = v.document_id
WHERE v.id = @id AND v.document_id = @document_id AND d.deleted_at IS NULL AND v.state <> 'quarantined';

-- name: ListBlocks :many
SELECT ord, kind, page, bbox, text FROM documents.blocks WHERE version_id = @version_id ORDER BY ord;

-- name: GetVersionTitle :one
SELECT d.id AS document_id, d.title FROM documents.document_versions v
JOIN documents.documents d ON d.id = v.document_id WHERE v.id = @version_id;

-- name: DeleteChunks :exec
DELETE FROM documents.chunks WHERE version_id = @version_id;

-- name: InsertChunks :exec
-- New passages are indexed with the language the organization chose for word search.
INSERT INTO documents.chunks (id, version_id, document_id, ord, page, section, context_header, text, codes, tokens, bbox, suspect_injection, lang)
SELECT unnest(@ids::uuid[]), @version_id, @document_id, unnest(@ords::int[]), unnest(@pages::int[]),
       unnest(@sections::text[]), unnest(@headers::text[]), unnest(@texts::text[]), unnest(@codes::text[]),
       unnest(@token_counts::int[]), unnest(@bboxes::jsonb[]), unnest(@suspects::boolean[]), documents.tenant_regconfig();

-- name: ListChunksToEmbed :many
SELECT c.id, c.context_header, c.text FROM documents.chunks c
WHERE c.version_id = @version_id
  AND NOT EXISTS (SELECT 1 FROM documents.chunk_embeddings e WHERE e.chunk_id = c.id AND e.embed_model = @embed_model)
ORDER BY c.ord;

-- name: InsertEmbeddings :exec
INSERT INTO documents.chunk_embeddings (chunk_id, embed_model, embedding)
SELECT unnest(@chunk_ids::uuid[]), @embed_model, unnest(@vectors::text[])::vector
ON CONFLICT DO NOTHING;

-- name: IsCurrentVersion :one
SELECT current_version_id = @version_id::uuid AS is_current FROM documents.documents WHERE id = @document_id;

-- name: RetireOtherChunks :exec
UPDATE documents.chunks SET is_current = false WHERE document_id = @document_id AND version_id <> @version_id AND is_current;

-- name: RetireOtherEmbeddings :exec
UPDATE documents.chunk_embeddings e SET is_current = false FROM documents.chunks c
WHERE e.chunk_id = c.id AND c.document_id = @document_id AND c.version_id <> @version_id AND e.is_current;

-- name: PublishChunks :exec
UPDATE documents.chunks SET is_current = true, acl_set_id = @acl_set_id WHERE version_id = @version_id;

-- name: PublishEmbeddings :exec
UPDATE documents.chunk_embeddings e SET is_current = true, acl_set_id = @acl_set_id FROM documents.chunks c
WHERE e.chunk_id = c.id AND c.version_id = @version_id;

-- name: RetireDocumentChunks :exec
UPDATE documents.chunks SET is_current = false
WHERE document_id IN (SELECT id FROM documents.documents WHERE source_id = @source_id AND external_id = @external_id);

-- name: RetireDocumentEmbeddings :exec
UPDATE documents.chunk_embeddings e SET is_current = false FROM documents.chunks c
WHERE e.chunk_id = c.id AND c.document_id IN (SELECT id FROM documents.documents WHERE source_id = @source_id AND external_id = @external_id);

-- name: CountCurrentByType :many
SELECT doc_type, count(*)::int AS total FROM documents.documents
WHERE deleted_at IS NULL AND current_version_id IS NOT NULL GROUP BY doc_type;

-- name: CountProcessing :one
-- A parsed version is still being made searchable.
SELECT count(*)::int FROM documents.document_versions WHERE state IN ('processing', 'parsed');

-- name: CountRetrying :one
SELECT count(*)::int FROM documents.document_versions WHERE state = 'retrying';

-- name: GetDocumentACLSet :one
SELECT acl_set_id FROM documents.documents WHERE id = @id;

-- name: SetSourceDocumentsACL :exec
UPDATE documents.documents SET acl_set_id = sqlc.narg(acl_set_id) WHERE source_id = @source_id;

-- name: SetSourceChunksACL :exec
UPDATE documents.chunks c SET acl_set_id = @acl_set_id FROM documents.documents d
WHERE c.document_id = d.id AND d.source_id = @source_id;

-- name: SetSourceEmbeddingsACL :exec
UPDATE documents.chunk_embeddings e SET acl_set_id = @acl_set_id FROM documents.chunks c
JOIN documents.documents d ON d.id = c.document_id
WHERE e.chunk_id = c.id AND d.source_id = @source_id;

-- name: RetireSourceChunks :exec
UPDATE documents.chunks c SET is_current = false FROM documents.documents d
WHERE c.document_id = d.id AND d.source_id = @source_id;

-- name: RetireSourceEmbeddings :exec
UPDATE documents.chunk_embeddings e SET is_current = false FROM documents.chunks c
JOIN documents.documents d ON d.id = c.document_id
WHERE e.chunk_id = c.id AND d.source_id = @source_id;

-- name: MarkSourceDocumentsDeleted :exec
UPDATE documents.documents SET deleted_at = now(), updated_at = now()
WHERE source_id = @source_id AND deleted_at IS NULL;

-- name: DeleteBlocksOfDocumentsDeletedBefore :execrows
DELETE FROM documents.blocks b USING documents.document_versions v, documents.documents d
WHERE b.version_id = v.id AND v.document_id = d.id AND d.deleted_at < @before;

-- name: DeleteChunksOfDocumentsDeletedBefore :execrows
-- Vectors go with their chunk (ON DELETE CASCADE).
DELETE FROM documents.chunks c USING documents.documents d
WHERE c.document_id = d.id AND d.deleted_at < @before;

-- name: ListLiveContentHashes :many
-- Every version of a document that is not deleted keeps its original, the superseded ones too.
SELECT DISTINCT v.content_hash FROM documents.document_versions v
JOIN documents.documents d ON d.id = v.document_id WHERE d.deleted_at IS NULL;

-- name: GetSearchLanguage :one
SELECT language FROM documents.search_language;

-- name: SetSearchLanguage :exec
INSERT INTO documents.search_language (language) VALUES (@language)
ON CONFLICT (tenant_id) DO UPDATE SET language = EXCLUDED.language, updated_at = now();

-- name: RelabelChunks :execrows
-- Gives up to batch passages the word-search language of the organization; their word index follows by itself.
UPDATE documents.chunks SET lang = documents.tenant_regconfig()
WHERE id IN (SELECT id FROM documents.chunks WHERE lang <> documents.tenant_regconfig() LIMIT @batch);
