//go:build integration

package documents

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/models/tei"
)

// fakeTEI answers /tokenize with one token per word and /embed with a constant vector of the right size.
func fakeTEI(t *testing.T) *tei.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tokenize", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Inputs []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		out := make([][]map[string]bool, len(in.Inputs))
		for i, text := range in.Inputs {
			out[i] = make([]map[string]bool, len(strings.Fields(text)))
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /embed", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Inputs []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		out := make([][]float32, len(in.Inputs))
		for i := range out {
			out[i] = make([]float32, tei.Dimension)
			out[i][0] = 1
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return tei.New(srv.URL)
}

// publish runs chunk, embed and index for a version already enriched.
func (p pipeline) publish(versionID uuid.UUID) {
	p.t.Helper()
	p.must(p.m.Chunk(p.ctx, events.VersionChunk{TenantID: p.tenantID, VersionID: versionID}))
	p.must(p.m.Embed(p.ctx, events.VersionEmbed{TenantID: p.tenantID, VersionID: versionID}))
	p.must(p.m.Index(p.ctx, events.VersionIndex{TenantID: p.tenantID, VersionID: versionID}))
}

func (p pipeline) countCurrent(versionID uuid.UUID) (chunks, vectors int) {
	p.query(`SELECT count(*) FILTER (WHERE is_current AND acl_set_id IS NOT NULL) FROM documents.chunks WHERE version_id = $1`, []any{versionID}, &chunks)
	p.query(`SELECT count(*) FILTER (WHERE e.is_current AND e.acl_set_id IS NOT NULL)
	         FROM documents.chunk_embeddings e JOIN documents.chunks c ON c.id = e.chunk_id WHERE c.version_id = $1`, []any{versionID}, &vectors)
	return chunks, vectors
}

func (p pipeline) versionState(id uuid.UUID) string {
	var state string
	p.query("SELECT state FROM documents.document_versions WHERE id = $1", []any{id}, &state)
	return state
}

// Until the passages are published a document must not claim to be indexed: a question asked in that window finds nothing.
func TestAVersionIsParsedUntilItsPassagesAreSearchableAndThenIndexed(t *testing.T) {
	p := newPipeline(t, "success")
	version := p.ingest("revision 1")
	if got := p.versionState(version); got != "parsed" {
		t.Fatalf("after enrich the state is %q, want parsed", got)
	}
	p.publish(version)
	if got := p.versionState(version); got != "indexed" {
		t.Fatalf("after indexing the state is %q, want indexed", got)
	}
}

func TestAVersionThatIsNoLongerCurrentIsNotMarkedIndexed(t *testing.T) {
	p := newPipeline(t, "success")
	first := p.ingest("revision 1")
	p.ingest("revision 2") // revision 2 is now the current one
	p.must(p.m.Chunk(p.ctx, events.VersionChunk{TenantID: p.tenantID, VersionID: first}))
	p.must(p.m.Embed(p.ctx, events.VersionEmbed{TenantID: p.tenantID, VersionID: first}))
	p.must(p.m.Index(p.ctx, events.VersionIndex{TenantID: p.tenantID, VersionID: first}))
	if got := p.versionState(first); got == "indexed" {
		t.Fatal("a superseded version finishing late must not become indexed or searchable")
	}
}

func TestAVersionBecomesSearchableWithVectorsAndAnACLSet(t *testing.T) {
	p := newPipeline(t, "success")
	client := fakeTEI(t)
	p.m.Tokens, p.m.Embedder, p.m.EmbedModel = client, client, "bge-m3"
	version := p.ingest("revision 1")
	p.publish(version)
	chunks, vectors := p.countCurrent(version)
	if chunks == 0 || vectors != chunks {
		t.Fatalf("current chunks %d, vectors %d; every passage needs both", chunks, vectors)
	}
}

func TestANewVersionRetiresTheOldPassagesInTheSameStep(t *testing.T) {
	p := newPipeline(t, "success")
	client := fakeTEI(t)
	p.m.Tokens, p.m.Embedder, p.m.EmbedModel = client, client, "bge-m3"
	first := p.ingest("revision 1")
	p.publish(first)
	second := p.ingest("revision 2")
	p.publish(second)
	if old, _ := p.countCurrent(first); old != 0 {
		t.Fatalf("%d passages of the old version are still searchable", old)
	}
	if now, _ := p.countCurrent(second); now == 0 {
		t.Fatal("the new version is not searchable")
	}
}

func TestWithoutAnEmbedderPassagesAreStillSearchableByWords(t *testing.T) {
	p := newPipeline(t, "success")
	version := p.ingest("revision 1")
	p.publish(version)
	chunks, vectors := p.countCurrent(version)
	if chunks == 0 || vectors != 0 {
		t.Fatalf("chunks %d vectors %d; want passages with no vectors", chunks, vectors)
	}
}

func TestRunningChunkTwiceKeepsTheSameIDs(t *testing.T) {
	p := newPipeline(t, "success")
	version := p.ingest("revision 1")
	p.must(p.m.Chunk(p.ctx, events.VersionChunk{TenantID: p.tenantID, VersionID: version}))
	var before, after uuid.UUID
	p.query("SELECT id FROM documents.chunks WHERE version_id = $1 AND ord = 0", []any{version}, &before)
	p.must(p.m.Chunk(p.ctx, events.VersionChunk{TenantID: p.tenantID, VersionID: version}))
	p.query("SELECT id FROM documents.chunks WHERE version_id = $1 AND ord = 0", []any{version}, &after)
	if before != after {
		t.Fatal("a retried job must produce the same passage ids")
	}
}

func TestADeletedDocumentStopsBeingFound(t *testing.T) {
	p := newPipeline(t, "success")
	version := p.ingest("revision 1")
	p.publish(version)
	p.must(p.m.OnDocumentDeleted(p.ctx, events.DocumentDeleted{TenantID: p.tenantID, SourceID: p.sourceID, ExternalID: externalID}))
	if chunks, _ := p.countCurrent(version); chunks != 0 {
		t.Fatalf("%d passages of a deleted document are still searchable", chunks)
	}
}

func TestRemovedSourceDocumentsAreNotSearchable(t *testing.T) {
	p := newPipeline(t, "success")
	version := p.ingest("revision 1")
	p.publish(version)
	p.must(p.m.OnSourceRemoved(p.ctx, events.SourceRemoved{TenantID: p.tenantID, SourceID: p.sourceID}))
	chunks, vectors := p.countCurrent(version)
	var listed int
	p.query("SELECT count(*) FROM documents.v_documents", nil, &listed)
	if chunks != 0 || vectors != 0 || listed != 0 {
		t.Fatalf("%d passages, %d vectors and %d listed documents remain after the source was removed", chunks, vectors, listed)
	}
}

// chunksNotIn counts the passages whose word index was not built with the configuration (e.g. "documents.atlas_es").
func (p pipeline) chunksNotIn(config string) int {
	var n int
	p.query(`SELECT count(*) FROM documents.chunks WHERE lang <> $1::regconfig`, []any{config}, &n)
	return n
}

func (p pipeline) exec(sql string, args ...any) {
	p.t.Helper()
	p.must(p.m.DB.InTenantTx(p.ctx, p.tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(p.ctx, sql, args...)
		return err
	}))
}

func (p pipeline) library() *Library {
	return &Library{DB: p.m.DB, Blobs: p.m.Blobs, Now: time.Now}
}

func (p pipeline) chooseSearchLanguage(language string) {
	p.t.Helper()
	p.must(p.library().SetSearchLanguage(p.ctx, Actor{TenantID: p.tenantID, UserID: uuid.New()}, language))
}

func TestPassagesAreIndexedWithTheLanguageOfTheOrganization(t *testing.T) {
	p := newPipeline(t, "success")
	p.chooseSearchLanguage("en")
	p.publish(p.ingest("revision 1"))
	if wrong := p.chunksNotIn("documents.atlas_en"); wrong != 0 {
		t.Fatalf("%d new passages were not indexed in English", wrong)
	}
}

func TestChangingLanguageRelabelsExistingChunks(t *testing.T) {
	p := newPipeline(t, "success")
	p.publish(p.ingest("revision 1"))
	if wrong := p.chunksNotIn("documents.atlas_none"); wrong != 0 {
		t.Fatalf("%d passages start outside the default configuration", wrong)
	}
	p.chooseSearchLanguage("es")
	p.must(p.m.OnSearchLanguageChanged(p.ctx, events.SearchLanguageChanged{TenantID: p.tenantID}))
	if wrong := p.chunksNotIn("documents.atlas_es"); wrong != 0 {
		t.Fatalf("%d passages are still in the old configuration", wrong)
	}
	if got, err := p.library().SearchLanguage(p.ctx, Actor{TenantID: p.tenantID, UserID: uuid.New()}); err != nil || got != "es" {
		t.Fatalf("SearchLanguage() = %q, %v", got, err)
	}
}

func TestAnUnknownSearchLanguageIsRefusedAndNothingChanges(t *testing.T) {
	p := newPipeline(t, "success")
	err := p.library().SetSearchLanguage(p.ctx, Actor{TenantID: p.tenantID, UserID: uuid.New()}, "klingon")
	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("SetSearchLanguage() = %v, want errs.ErrInvalid", err)
	}
	dbtest.RequireNoJob(t, p.m.DB, events.SearchLanguageChanged{TenantID: p.tenantID})
}

// A corpus larger than one batch is re-labelled by a chain of jobs: a full batch queues the next one.
func TestAFullBatchQueuesTheNextOne(t *testing.T) {
	p := newPipeline(t, "success")
	version := p.ingest("revision 1")
	p.publish(version)
	p.exec(`INSERT INTO documents.chunks (id, version_id, document_id, ord, page, text, tokens)
	        SELECT gen_random_uuid(), c.version_id, c.document_id, 1000 + g, 1, 'x', 1
	        FROM (SELECT version_id, document_id FROM documents.chunks LIMIT 1) c, generate_series(1, $1) g`, relangBatch)
	p.chooseSearchLanguage("es")
	args := events.SearchLanguageChanged{TenantID: p.tenantID}
	p.must(p.m.OnSearchLanguageChanged(p.ctx, args))
	if left := p.chunksNotIn("documents.atlas_es"); left == 0 {
		t.Fatal("one run re-labelled more than a batch")
	}
	p.must(p.m.OnSearchLanguageChanged(p.ctx, args))
	if left := p.chunksNotIn("documents.atlas_es"); left != 0 {
		t.Fatalf("%d passages left after the second run", left)
	}
}
