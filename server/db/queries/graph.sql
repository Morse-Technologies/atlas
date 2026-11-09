-- name: ListCurrentTexts :many
SELECT page, text FROM documents.v_searchable_chunks WHERE document_id = @document_id ORDER BY ord;

-- name: GetDocFacts :many
SELECT id, title, code, doc_type, department FROM documents.v_documents WHERE id = ANY(@ids::uuid[]);

-- name: ListDocsByCode :many
SELECT id, code FROM documents.v_documents WHERE code = ANY(@codes::text[]) AND id <> @self;

-- name: ListDocsWithSameCode :many
SELECT id FROM documents.v_documents WHERE code = @code AND code <> '' AND id <> @self;

-- name: GetDocCode :one
SELECT code FROM documents.v_documents WHERE id = @id;

-- name: DeleteMentions :exec
DELETE FROM graph.mentions WHERE document_id = @document_id;

-- name: InsertMentions :exec
INSERT INTO graph.mentions (document_id, entity_type, label, count)
SELECT @document_id, unnest(@entity_types::text[]), unnest(@labels::text[]), unnest(@counts::int[]);

-- name: ListDocsMentioningCode :many
SELECT document_id FROM graph.mentions WHERE entity_type = 'code' AND label = @code AND document_id <> @self;

-- name: ListMentionsOf :many
SELECT document_id, entity_type, label, count FROM graph.mentions
WHERE document_id = ANY(@document_ids::uuid[]) AND entity_type <> 'code' ORDER BY document_id, entity_type, label;

-- name: ListDocsMentioning :many
SELECT document_id FROM graph.mentions WHERE entity_type = @entity_type AND label = @label;

-- name: DeleteOwnEdges :exec
-- Rejected edges stay, so a person's decision is not undone by the next scan.
DELETE FROM graph.edges WHERE src = @document_id AND produced_by = @produced_by AND status <> 'rejected';

-- name: DeleteDuplicateEdges :exec
DELETE FROM graph.edges WHERE (src = @document_id OR dst = @document_id) AND produced_by = 'equal-code' AND status <> 'rejected';

-- name: InsertEdges :exec
INSERT INTO graph.edges (src, dst, kind, layer, produced_by)
SELECT unnest(@srcs::uuid[]), unnest(@dsts::uuid[]), @kind, @layer, @produced_by
ON CONFLICT DO NOTHING;

-- name: ListEdgesOf :many
SELECT id, src, dst, kind, layer, status FROM graph.edges
WHERE (src = ANY(@ids::uuid[]) OR dst = ANY(@ids::uuid[])) AND status <> 'rejected' ORDER BY id;

-- name: ListHubs :many
SELECT doc AS id FROM (
  SELECT src AS doc FROM graph.edges WHERE status <> 'rejected' UNION ALL SELECT dst FROM graph.edges WHERE status <> 'rejected'
) e GROUP BY doc ORDER BY count(*) DESC, doc LIMIT @top;

-- name: SetEdgeStatus :execrows
UPDATE graph.edges SET status = @status WHERE id = @id;

-- name: UpsertAlert :exec
-- The same problem found again refreshes the alert; one that was resolved and came back opens again. A dismissed
-- alert stays dismissed.
INSERT INTO graph.alerts (rule, severity, title, detail, document_ids, entity_type, entity_label, fingerprint, evidence, first_seen_at, last_seen_at)
VALUES (@rule, @severity, @title, @detail, @document_ids, @entity_type, @entity_label, @fingerprint, @evidence, @seen_at, @seen_at)
ON CONFLICT (tenant_id, fingerprint) DO UPDATE SET
  title = EXCLUDED.title, detail = EXCLUDED.detail, evidence = EXCLUDED.evidence, severity = EXCLUDED.severity,
  last_seen_at = EXCLUDED.last_seen_at,
  status = CASE WHEN graph.alerts.status = 'resolved' THEN 'open' ELSE graph.alerts.status END;

-- name: ResolveUnseen :exec
-- Closes the alerts of a rule that this run did not find again. With a document, only the alerts about it.
UPDATE graph.alerts SET status = 'resolved'
WHERE rule = @rule AND status IN ('open', 'ack') AND last_seen_at < @run_started
  AND (sqlc.narg(document_id)::uuid IS NULL OR document_ids @> ARRAY[sqlc.narg(document_id)::uuid]);

-- name: RuleNoOwner :many
SELECT id, title, code FROM documents.v_documents
WHERE owner_id IS NULL AND status <> 'obsolete' AND (sqlc.narg(document_id)::uuid IS NULL OR id = sqlc.narg(document_id));

-- name: RuleReviewOverdue :many
SELECT id, title, code, review_due_at FROM documents.v_documents
WHERE review_due_at < @today AND status <> 'obsolete' AND (sqlc.narg(document_id)::uuid IS NULL OR id = sqlc.narg(document_id));

-- name: RuleSuspect :many
SELECT p.document_id, min(p.page)::int AS page, min(left(p.text, 300))::text AS quote
FROM documents.v_suspect_passages p WHERE (sqlc.narg(document_id)::uuid IS NULL OR p.document_id = sqlc.narg(document_id))
GROUP BY p.document_id;

-- name: RuleDuplicates :many
SELECT src, dst FROM graph.edges
WHERE kind = 'duplicates' AND status <> 'rejected' AND (sqlc.narg(document_id)::uuid IS NULL OR src = sqlc.narg(document_id) OR dst = sqlc.narg(document_id));

-- name: ListAlertsPage :many
SELECT * FROM graph.alerts WHERE id < @before ORDER BY id DESC LIMIT @page_size;

-- name: LockAlert :one
SELECT * FROM graph.alerts WHERE id = @id FOR UPDATE;

-- name: SaveAlert :exec
UPDATE graph.alerts SET status = @status, assignee_id = @assignee_id, resolution_note = @resolution_note WHERE id = @id;

-- name: CountOpenAlerts :many
SELECT severity, count(*)::int AS total FROM graph.alerts WHERE status = 'open' GROUP BY severity;

-- name: ListDocsInDepartment :many
SELECT id FROM documents.v_documents WHERE department = @department ORDER BY id LIMIT @top;
