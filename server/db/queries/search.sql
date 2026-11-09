-- name: UseIterativeVectorScan :exec
-- Keeps the ACL filter exact while the HNSW index is walked (ATLAS.md §10.2).
SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true);

-- name: SearchVector :many
-- The cast and the model literal must match the partial index of migration 0011.
SELECT chunk_id, ((embedding::vector(1024)) <=> (@query_vector::text)::vector(1024))::float4 AS distance
FROM documents.v_searchable_vectors
WHERE embed_model = 'bge-m3' AND acl_set_id = ANY(@acl_sets::uuid[])
ORDER BY (embedding::vector(1024)) <=> (@query_vector::text)::vector(1024) LIMIT @top_k;

-- name: GetCandidateChunks :many
SELECT chunk_id, version_id, document_id, page, section, context_header, text, bbox, suspect_injection,
       document_title, document_code
FROM documents.v_searchable_chunks WHERE chunk_id = ANY(@ids::uuid[]);

-- name: CreateConversation :one
INSERT INTO search.conversations (user_id, title) VALUES (@user_id, @title) RETURNING id;

-- name: TouchConversation :exec
UPDATE search.conversations SET updated_at = now() WHERE id = @id;

-- name: GetOwnConversation :one
SELECT id, user_id, title, updated_at FROM search.conversations WHERE id = @id AND user_id = @user_id;

-- name: ListOwnConversations :many
SELECT id, user_id, title, updated_at,
       COALESCE((SELECT a.outcome FROM search.answers a WHERE a.conversation_id = c.id ORDER BY a.asked_at DESC LIMIT 1), 'answered')::text AS last_outcome
FROM search.conversations c WHERE c.user_id = @user_id AND c.id < @before ORDER BY c.id DESC LIMIT @page_size;

-- name: InsertAnswer :exec
INSERT INTO search.answers (id, conversation_id, user_id, question, asked_at, outcome, text, notes, nearby, latency_ms,
                            model, scope, prompt_version, embed_model, rerank_model, not_found_threshold, context_hash)
VALUES (@id, @conversation_id, @user_id, @question, @asked_at, @outcome, @text, @notes, @nearby, @latency_ms,
        @model, @scope, @prompt_version, @embed_model, @rerank_model, @not_found_threshold, @context_hash);

-- name: InsertCitations :exec
INSERT INTO search.citations (answer_id, n, document_id, version_id, chunk_id, label, page, section, quote, score, bbox)
SELECT @answer_id, unnest(@ns::int[]), unnest(@document_ids::uuid[]), unnest(@version_ids::uuid[]), unnest(@chunk_ids::uuid[]),
       unnest(@labels::text[]), unnest(@pages::int[]), unnest(@sections::text[]), unnest(@quotes::text[]),
       unnest(@scores::float4[]), unnest(@bboxes::jsonb[]);

-- name: InsertCandidates :exec
-- 0 means "not in that list" and -1 "not reranked"; both are stored as NULL.
INSERT INTO search.answer_candidates (answer_id, chunk_id, version_id, rank_lexical, rank_vector, rrf_score, rerank_score, sent_to_llm)
SELECT @answer_id, t.chunk_id, t.version_id, NULLIF(t.rank_lexical, 0), NULLIF(t.rank_vector, 0), t.rrf, NULLIF(t.rerank, -1), t.sent
FROM (SELECT unnest(@chunk_ids::uuid[]) AS chunk_id, unnest(@version_ids::uuid[]) AS version_id,
             unnest(@ranks_lexical::int[]) AS rank_lexical, unnest(@ranks_vector::int[]) AS rank_vector,
             unnest(@rrf_scores::float4[]) AS rrf, unnest(@rerank_scores::float4[]) AS rerank,
             unnest(@sent::boolean[]) AS sent) AS t;

-- name: ListAnswersOf :many
SELECT * FROM search.answers WHERE conversation_id = ANY(@conversation_ids::uuid[]) ORDER BY asked_at, id;

-- name: ListCitationsOf :many
SELECT answer_id, n, document_id, version_id, label, page, section, quote, score, bbox
FROM search.citations WHERE answer_id = ANY(@answer_ids::uuid[]) ORDER BY answer_id, n;

-- name: SetFeedback :execrows
UPDATE search.answers SET feedback = @feedback WHERE id = @id AND user_id = @user_id;

-- name: EraseUserConversations :execrows
-- Answers, citations and candidates go with their conversation (ON DELETE CASCADE).
DELETE FROM search.conversations WHERE user_id = @user_id;

-- name: CountAnswersByDay :many
SELECT (asked_at AT TIME ZONE 'UTC')::date AS day,
       count(*)::int AS questions,
       (count(*) FILTER (WHERE outcome = 'answered'))::int AS answered,
       (count(*) FILTER (WHERE outcome = 'not_found'))::int AS not_found
FROM search.answers WHERE asked_at >= @since GROUP BY 1 ORDER BY 1;

-- name: MedianLatency :one
SELECT COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY latency_ms), 0)::int AS median_ms
FROM search.answers WHERE asked_at >= @since AND outcome IN ('answered', 'not_found');

-- name: ListCommonLexemes :many
-- Words found in at least min_share of the passages, from the planner's statistics.
SELECT documents.common_lexemes(@min_share::real) AS lexeme;
