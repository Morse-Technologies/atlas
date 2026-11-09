-- Who may read a source's documents (ATLAS.md §10.2). A source has an optional ACL set: no set means every
-- signed-in person (the default set). The documents carry the set of their source, and row-level security
-- hides the documents a person may not read from every query made for them, so search, lists, files, the map
-- and the counts cannot reveal that a hidden document exists. Jobs and the console set no user and see all.

-- +goose Up
ALTER TABLE sources.sources ADD COLUMN acl_set_id uuid;
ALTER TABLE documents.documents ADD COLUMN acl_set_id uuid;

-- The sets a person may read: the default one and every set that lists them.
CREATE FUNCTION access.readable_acl_sets(person uuid) RETURNS SETOF uuid
LANGUAGE sql STABLE AS $$
  SELECT id FROM access.acl_sets WHERE is_default OR person = ANY (allow)
$$;
GRANT EXECUTE ON FUNCTION access.readable_acl_sets(uuid) TO atlas_app;

-- NULLIF(current_setting('atlas.user_id'), '') is null for jobs and the console: no restriction there.
DROP POLICY tenant_isolation ON documents.documents;
CREATE POLICY tenant_isolation ON documents.documents USING (
  tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid
  AND (
    NULLIF(current_setting('atlas.user_id', true), '') IS NULL
    OR acl_set_id IS NULL
    OR acl_set_id IN (SELECT access.readable_acl_sets(NULLIF(current_setting('atlas.user_id', true), '')::uuid))
  )
);

-- An alert about documents a person cannot read is hidden whole: its title or evidence would name them.
DROP POLICY tenant_isolation ON graph.alerts;
CREATE POLICY tenant_isolation ON graph.alerts USING (
  tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid
  AND (
    NULLIF(current_setting('atlas.user_id', true), '') IS NULL
    OR NOT EXISTS (SELECT 1 FROM unnest(document_ids) AS d (id) WHERE NOT EXISTS (SELECT 1 FROM documents.documents x WHERE x.id = d.id))
  )
);

-- +goose Down
DROP POLICY tenant_isolation ON graph.alerts;
CREATE POLICY tenant_isolation ON graph.alerts USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
DROP POLICY tenant_isolation ON documents.documents;
CREATE POLICY tenant_isolation ON documents.documents USING (tenant_id = NULLIF(current_setting('atlas.tenant_id', true), '')::uuid);
DROP FUNCTION access.readable_acl_sets(uuid);
ALTER TABLE documents.documents DROP COLUMN acl_set_id;
ALTER TABLE sources.sources DROP COLUMN acl_set_id;
