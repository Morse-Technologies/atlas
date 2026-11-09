-- name: InsertEntityTypes :exec
INSERT INTO catalog.entity_types (key, labels, contradiction_anchor)
SELECT unnest(@keys::text[]), unnest(@labels::jsonb[]), unnest(@anchors::boolean[])
ON CONFLICT DO NOTHING;

-- name: InsertDocTypes :exec
INSERT INTO catalog.doc_types (key, core_type, labels)
-- Core types are their own core_type; packs map theirs to one of these.
SELECT unnest(@keys::text[]), unnest(@keys::text[]), unnest(@labels::jsonb[])
ON CONFLICT DO NOTHING;

-- name: ListDocTypes :many
SELECT key, core_type, labels FROM catalog.doc_types ORDER BY key;

-- name: ListEntityTypes :many
SELECT key, labels FROM catalog.entity_types ORDER BY key;
