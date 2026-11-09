//go:build integration

package documents

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/events"
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
