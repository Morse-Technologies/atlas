package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/morse/atlas/server/internal/fakemodel"
)

const (
	embedBatch  = 400 // vectors per INSERT: each is about 8 KB of text
	copyBatch   = 20000
	approxToken = 4 // characters per token, as the pipeline estimates without a tokenizer
	fileBytes   = 1 << 20
	readerEvery = 3 // one restricted source in this many lists the reader
	schema      = "documents"
)

var docTypes = [...]string{"procedure", "work_instruction", "manual", "standard", "form"}

// loader writes one corpus into a tenant.
type loader struct {
	pool   *pgxpool.Pool
	tenant uuid.UUID
	prof   profile
	vocab  vocabulary
	reader uuid.UUID    // the person who may read some of the restricted sources
	sets   []*uuid.UUID // the ACL set of each source (nil: everyone)
	open   uuid.UUID    // the default set: what the passages of an open source carry
	srcs   []uuid.UUID
}

type docRow struct {
	id, version, source uuid.UUID
	topic               int
	chunks              int
}

func (l *loader) run(ctx context.Context) error {
	steps := []func(context.Context) error{l.makeSources, l.dropVectorIndex, l.loadDocuments, l.rebuildVectorIndex, l.analyze}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (l *loader) exec(ctx context.Context, what, sql string) error {
	started := time.Now()
	if _, err := l.pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	fmt.Printf("%-28s %s\n", what, time.Since(started).Round(time.Millisecond))
	return nil
}

func (l *loader) dropVectorIndex(ctx context.Context) error {
	return l.exec(ctx, "drop vector index", "DROP INDEX IF EXISTS documents.chunk_embeddings_bge_m3")
}

// rebuildVectorIndex builds the HNSW index once, over all vectors: much faster than inserting into it.
func (l *loader) rebuildVectorIndex(ctx context.Context) error {
	if err := l.exec(ctx, "maintenance memory", "SET maintenance_work_mem = '1GB'"); err != nil {
		return err
	}
	return l.exec(ctx, "build vector index", `CREATE INDEX chunk_embeddings_bge_m3 ON documents.chunk_embeddings
		USING hnsw ((embedding::vector(1024)) vector_cosine_ops) WHERE embed_model = 'bge-m3'`)
}

func (l *loader) analyze(ctx context.Context) error {
	return l.exec(ctx, "analyze", "ANALYZE")
}

// makeSources creates the sources. The first ones are restricted: every third lists the reader, the others
// somebody else, so the reader's filter keeps a minority of the passages (the hard case for the vector index).
func (l *loader) makeSources(ctx context.Context) error {
	if err := l.pool.QueryRow(ctx, `INSERT INTO access.acl_sets (tenant_id, origin, is_default) VALUES ($1, 'core', true)
		ON CONFLICT (tenant_id) WHERE is_default DO UPDATE SET origin = 'core' RETURNING id`, l.tenant).Scan(&l.open); err != nil {
		return fmt.Errorf("default set: %w", err)
	}
	for i := range l.prof.Sources {
		id, err := l.makeSource(ctx, i)
		if err != nil {
			return err
		}
		l.srcs = append(l.srcs, id)
		set, err := l.restrictedSet(ctx, i)
		if err != nil {
			return err
		}
		l.sets = append(l.sets, set)
	}
	return nil
}

func (l *loader) makeSource(ctx context.Context, i int) (uuid.UUID, error) {
	var id uuid.UUID
	err := l.pool.QueryRow(ctx, `INSERT INTO sources.sources (tenant_id, name, kind, root_path, status) VALUES ($1, $2, 'folder', $3, 'paused') RETURNING id`,
		l.tenant, fmt.Sprintf("perf-%02d", i), fmt.Sprintf("/perf/%02d", i)).Scan(&id)
	return id, err
}

func (l *loader) restrictedSet(ctx context.Context, i int) (*uuid.UUID, error) {
	if i >= l.prof.Restricted {
		return nil, nil
	}
	reader := uuid.New()
	if i%readerEvery == 0 {
		reader = l.reader
	}
	var id uuid.UUID
	err := l.pool.QueryRow(ctx, `INSERT INTO access.acl_sets (tenant_id, origin, allow) VALUES ($1, 'source', $2) RETURNING id`, l.tenant, []uuid.UUID{reader}).Scan(&id)
	return &id, err
}

// loadDocuments writes documents, versions, passages and vectors, one batch of documents at a time.
func (l *loader) loadDocuments(ctx context.Context) error {
	started := time.Now()
	docs := l.plan()
	for from := 0; from < len(docs); from += copyBatch / l.prof.ChunksPerDoc {
		to := min(from+copyBatch/l.prof.ChunksPerDoc, len(docs))
		if err := l.loadBatch(ctx, docs[from:to]); err != nil {
			return err
		}
	}
	fmt.Printf("%-28s %s (%d documents, %d passages)\n", "load documents", time.Since(started).Round(time.Second), len(docs), l.prof.totalChunks())
	return nil
}

// plan decides every document: its topic, source and how many passages it has.
func (l *loader) plan() []docRow {
	docs := make([]docRow, 0, l.prof.Documents+l.prof.HugeDocs)
	for i := range l.prof.Documents + l.prof.HugeDocs {
		n := l.prof.ChunksPerDoc
		if i >= l.prof.Documents {
			n = l.prof.HugeChunks
		}
		docs = append(docs, docRow{id: uuid.Must(uuid.NewV7()), version: uuid.Must(uuid.NewV7()), source: l.srcs[i%len(l.srcs)], topic: i % topics, chunks: n})
	}
	return docs
}

func (l *loader) loadBatch(ctx context.Context, docs []docRow) error {
	return pgx.BeginFunc(ctx, l.pool, func(tx pgx.Tx) error {
		if err := l.copyDocuments(ctx, tx, docs); err != nil {
			return err
		}
		return l.copyPassages(ctx, tx, docs)
	})
}

func (l *loader) copyDocuments(ctx context.Context, tx pgx.Tx, docs []docRow) error {
	var documents, versions [][]any
	for i, d := range docs {
		code := fmt.Sprintf("PNT-%05d", d.topic*1000+i%1000)
		title := fmt.Sprintf("Procedimiento tema%03d numero %d", d.topic, i)
		documents = append(documents, []any{d.id, l.tenant, d.source, fmt.Sprintf("area%02d/%s.pdf", d.topic%20, code), title, code, docType(i), d.version, "current", l.sets[i%len(l.srcs)]})
		versions = append(versions, []any{d.version, l.tenant, d.id, d.version.String(), "application/pdf", int64(fileBytes), "indexed", chunkPages})
	}
	docCols := []string{"id", "tenant_id", "source_id", "external_id", "title", "code", "doc_type", "current_version_id", "status", "acl_set_id"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{schema, "documents"}, docCols, pgx.CopyFromRows(documents)); err != nil {
		return fmt.Errorf("copy documents: %w", err)
	}
	verCols := []string{"id", "tenant_id", "document_id", "content_hash", "mime", "size", "state", "pages"}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{schema, "document_versions"}, verCols, pgx.CopyFromRows(versions))
	return err
}

func docType(i int) string {
	return docTypes[i%len(docTypes)]
}

func (l *loader) copyPassages(ctx context.Context, tx pgx.Tx, docs []docRow) error {
	var rows [][]any
	var embeddings []embeddingRow
	for _, d := range docs {
		acl := l.setOf(d.source)
		for ord := range d.chunks {
			text := l.vocab.passage(d.topic)
			id := uuid.Must(uuid.NewV7())
			rows = append(rows, []any{id, l.tenant, d.version, d.id, ord, ord%chunkPages + 1, text, len(text) / approxToken, true, acl})
			embeddings = append(embeddings, embeddingRow{id: id, acl: acl, vector: vectorLiteral(fakemodel.Embed(text))})
		}
	}
	cols := []string{"id", "tenant_id", "version_id", "document_id", "ord", "page", "text", "tokens", "is_current", "acl_set_id"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{schema, "chunks"}, cols, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy chunks: %w", err)
	}
	return l.insertEmbeddings(ctx, tx, embeddings)
}

// setOf is the set the passages of a source carry: its own, or the default one for an open source.
func (l *loader) setOf(source uuid.UUID) *uuid.UUID {
	for i, s := range l.srcs {
		if s == source && l.sets[i] != nil {
			return l.sets[i]
		}
	}
	return &l.open
}

type embeddingRow struct {
	id     uuid.UUID
	acl    *uuid.UUID
	vector string
}

func (l *loader) insertEmbeddings(ctx context.Context, tx pgx.Tx, rows []embeddingRow) error {
	for from := 0; from < len(rows); from += embedBatch {
		batch := rows[from:min(from+embedBatch, len(rows))]
		ids, acls, vectors := make([]uuid.UUID, len(batch)), make([]*uuid.UUID, len(batch)), make([]string, len(batch))
		for i, r := range batch {
			ids[i], acls[i], vectors[i] = r.id, r.acl, r.vector
		}
		_, err := tx.Exec(ctx, `INSERT INTO documents.chunk_embeddings (tenant_id, chunk_id, embed_model, embedding, acl_set_id, is_current)
			SELECT $1, c, 'bge-m3', v::vector, a, true FROM unnest($2::uuid[], $3::text[], $4::uuid[]) AS t (c, v, a)`, l.tenant, ids, vectors, acls)
		if err != nil {
			return fmt.Errorf("insert embeddings: %w", err)
		}
	}
	return nil
}

func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%.5g", x)
	}
	b.WriteByte(']')
	return b.String()
}

// newLoader is a loader with a seeded random source, so a corpus is the same every time.
// target is where a corpus goes and who may read its restricted part.
type target struct {
	tenant, reader uuid.UUID
}

func newLoader(pool *pgxpool.Pool, t target, p profile) *loader {
	return &loader{pool: pool, tenant: t.tenant, reader: t.reader, prof: p, vocab: vocabulary{rng: rand.New(rand.NewPCG(1, 2))}}
}
