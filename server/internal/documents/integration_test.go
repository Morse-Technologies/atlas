//go:build integration

package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/models/docling"
	"github.com/morse/atlas/server/internal/sources"
)

// fakeDocling answers docling-serve's async API with one fixed task status and the small fixture document.
func fakeDocling(t *testing.T, taskStatus string) *docling.Client {
	t.Helper()
	doc, err := os.ReadFile("testdata/docling_small.json")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/convert/file/async", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"task_id":"t"}`) })
	mux.HandleFunc("GET /v1/status/poll/t", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"task_id":"t","task_status":%q}`, taskStatus)
	})
	mux.HandleFunc("GET /v1/result/t", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"status":"success","document":{"json_content":%s}}`, doc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return docling.New(srv.URL)
}

// pipeline is one tenant's document "Calidad/PNT-014.pdf" pushed through stored → convert → enrich.
type pipeline struct {
	t        *testing.T
	ctx      context.Context
	m        *Module
	tenantID uuid.UUID
	sourceID uuid.UUID
}

const externalID = "Calidad/PNT-014.pdf"

func newPipeline(t *testing.T, taskStatus string) pipeline {
	t.Helper()
	d := dbtest.Start(t)
	m := &Module{DB: d, Blobs: blobs.New(t.TempDir()), Docling: fakeDocling(t, taskStatus), Logger: slog.Default()}
	p := pipeline{t: t, ctx: dbtest.WithJobs(t, d), m: m, tenantID: dbtest.NewTenant(t, d)}
	p.must(d.InTenantTx(p.ctx, p.tenantID, func(tx pgx.Tx) (err error) {
		p.sourceID, err = sources.AddFolder(p.ctx, tx, sources.NewFolder{TenantID: p.tenantID, Name: "docs", Path: t.TempDir()})
		return err
	}))
	return p
}

// ingest stores content as a new blob and runs the three jobs; it returns the version id.
func (p pipeline) ingest(content string) uuid.UUID {
	p.t.Helper()
	return p.ingestAs(externalID, content)
}

// ingestAs is ingest for a file with another path.
func (p pipeline) ingestAs(path, content string) uuid.UUID {
	p.t.Helper()
	hash := p.writeBlob(content)
	stored := events.VersionStored{TenantID: p.tenantID, SourceID: p.sourceID, ExternalID: path, ContentHash: hash, MIME: pdfMIME, Size: int64(len(content))}
	p.must(p.m.OnVersionStored(p.ctx, stored))
	versionID := p.versionWithHash(hash)
	p.must(p.m.Convert(p.ctx, events.VersionConvert{TenantID: p.tenantID, VersionID: versionID}))
	p.must(p.m.Enrich(p.ctx, events.VersionEnrich{TenantID: p.tenantID, VersionID: versionID}))
	return versionID
}

func (p pipeline) writeBlob(content string) string {
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	path := p.m.Blobs.BlobPath(p.tenantID, hash)
	p.must(os.MkdirAll(filepath.Dir(path), 0o750))
	p.must(os.WriteFile(path, []byte(content), 0o600))
	return hash
}

func (p pipeline) query(sql string, args []any, dest ...any) {
	p.t.Helper()
	p.must(p.m.DB.InTenantTx(p.ctx, p.tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(p.ctx, sql, args...).Scan(dest...)
	}))
}

func (p pipeline) versionWithHash(hash string) uuid.UUID {
	var id uuid.UUID
	p.query("SELECT id FROM documents.document_versions WHERE content_hash = $1", []any{hash}, &id)
	return id
}

func (p pipeline) current() (uuid.UUID, int) {
	var id uuid.UUID
	var blocks int
	p.query(`SELECT d.current_version_id, (SELECT count(*) FROM documents.blocks b WHERE b.version_id = d.current_version_id)
	         FROM documents.documents d WHERE d.external_id = $1`, []any{externalID}, &id, &blocks)
	return id, blocks
}

func (p pipeline) must(err error) {
	p.t.Helper()
	if err != nil {
		p.t.Fatal(err)
	}
}

func TestNewContentBecomesCurrentAndARevertSwitchesBack(t *testing.T) {
	p := newPipeline(t, "success")
	first := p.ingest("revision 1")
	second := p.ingest("revision 2")
	if cur, blocks := p.current(); cur != second || blocks != len(wantSmallBlocks) {
		t.Fatalf("after revision 2: current %s with %d blocks, want %s with %d", cur, blocks, second, len(wantSmallBlocks))
	}
	if again := p.ingest("revision 1"); again != first {
		t.Fatalf("reverting to known content created version %s instead of reusing %s", again, first)
	}
	if cur, _ := p.current(); cur != first {
		t.Fatalf("after the revert the current version is %s, want %s", cur, first)
	}
}

func TestUnparseableVersionIsQuarantinedNotRetried(t *testing.T) {
	p := newPipeline(t, "failure")
	hash := p.writeBlob("broken pdf")
	stored := events.VersionStored{TenantID: p.tenantID, SourceID: p.sourceID, ExternalID: externalID, ContentHash: hash, MIME: pdfMIME, Size: 1}
	p.must(p.m.OnVersionStored(p.ctx, stored))
	if err := p.m.Convert(p.ctx, events.VersionConvert{TenantID: p.tenantID, VersionID: p.versionWithHash(hash)}); err != nil {
		t.Fatalf("a permanent failure must end the job cleanly, got %v", err)
	}
	var state, reason string
	p.query(`SELECT v.state, q.reason FROM documents.document_versions v, sources.quarantine q WHERE v.content_hash = $1`, []any{hash}, &state, &reason)
	if state != "quarantined" || reason != "corrupt" {
		t.Fatalf("version %s, quarantine reason %q", state, reason)
	}
}

// downDocling is a docling-serve that answers 503 to everything, like one that is starting or out of memory.
func downDocling(t *testing.T) *docling.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	t.Cleanup(srv.Close)
	return docling.New(srv.URL)
}

func (p pipeline) failedConvert(hash string, exhausted bool) {
	p.t.Helper()
	args := events.VersionConvert{TenantID: p.tenantID, VersionID: p.versionWithHash(hash)}
	err := p.m.Convert(p.ctx, args)
	if errs.Classify(err) == errs.ClassPermanent || err == nil {
		p.t.Fatalf("Convert with the model down = %v, want an error River retries", err)
	}
	hook := onStageFailed(p.m, StepConvert, func(a events.VersionConvert) uuid.UUID { return a.VersionID })
	p.must(hook(p.ctx, jobs.Failed[events.VersionConvert]{Args: args, Err: err, Exhausted: exhausted}))
}

func TestExhaustedRetriesQuarantineTheVersion(t *testing.T) {
	p := newPipeline(t, "success")
	p.m.Docling = downDocling(t)
	hash := p.writeBlob("a pdf the model never reads")
	p.must(p.m.OnVersionStored(p.ctx, events.VersionStored{TenantID: p.tenantID, SourceID: p.sourceID, ExternalID: externalID, ContentHash: hash, MIME: pdfMIME, Size: 1}))

	p.failedConvert(hash, false)
	var state string
	p.query(`SELECT state FROM documents.document_versions WHERE content_hash = $1`, []any{hash}, &state)
	if state != "retrying" {
		t.Fatalf("after a failed attempt the version is %q, want retrying", state)
	}

	p.failedConvert(hash, true)
	var reason string
	p.query(`SELECT v.state, q.reason FROM documents.document_versions v, sources.quarantine q WHERE v.content_hash = $1`, []any{hash}, &state, &reason)
	if state != "quarantined" || reason != string(errs.ReasonRetriesExhausted) {
		t.Fatalf("after the last attempt: version %q, quarantine reason %q; want quarantined for retries_exhausted", state, reason)
	}
}

func TestARetryThatSucceedsLeavesRetrying(t *testing.T) {
	p := newPipeline(t, "success")
	hash := p.writeBlob("a pdf the model reads on the second try")
	p.must(p.m.OnVersionStored(p.ctx, events.VersionStored{TenantID: p.tenantID, SourceID: p.sourceID, ExternalID: externalID, ContentHash: hash, MIME: pdfMIME, Size: 1}))
	versionID := p.versionWithHash(hash)
	p.must(p.m.DB.InTenantTx(p.ctx, p.tenantID, func(tx pgx.Tx) error {
		return documentsdb.New(tx).MarkVersionRetrying(p.ctx, versionID)
	}))

	p.must(p.m.Convert(p.ctx, events.VersionConvert{TenantID: p.tenantID, VersionID: versionID}))

	var state string
	p.query(`SELECT state FROM documents.document_versions WHERE id = $1`, []any{versionID}, &state)
	if state != "processing" {
		t.Fatalf("after the retry worked the version is %q, want processing", state)
	}
}

// documentNamed reads the document of the pipeline at path through the library, as a person would see it.
func (p pipeline) documentNamed(path string) Document {
	p.t.Helper()
	lib := &Library{DB: p.m.DB, Blobs: p.m.Blobs, Now: time.Now}
	who := Actor{TenantID: p.tenantID, UserID: uuid.New()}
	docs, _, err := lib.List(p.ctx, who, Listing{Query: path, Status: "all", Size: 10})
	if err != nil || len(docs) != 1 {
		p.t.Fatalf("documents named %q: %d, %v", path, len(docs), err)
	}
	return docs[0]
}

func provenanceOf(doc Document, field string) Provenance {
	for _, p := range doc.Provenance {
		if p.Field == field {
			return p
		}
	}
	return Provenance{}
}

func TestTheRulesFillTitleCodeAndRevisionWithWhereTheyWereRead(t *testing.T) {
	p := newPipeline(t, "success")
	p.ingestAs("Calidad/PNT-014-rev3.pdf", "revision 3")
	doc := p.documentNamed("PNT-014-rev3.pdf")
	if doc.Title != "Montaje del husillo" || doc.Code != "PNT-014" || doc.Versions[0].Revision != "3" {
		t.Fatalf("document = title %q, code %q, revision %q", doc.Title, doc.Code, doc.Versions[0].Revision)
	}
	if title, code := provenanceOf(doc, "title"), provenanceOf(doc, "code"); title.Source != "titleblock" || code.Source != "path" || code.Value != "PNT-014" {
		t.Fatalf("provenance title %+v, code %+v", title, code)
	}
	dbtest.RequireJobs(t, p.m.DB, events.DocumentChanged{TenantID: p.tenantID, DocumentID: doc.ID})
}

func TestExtractedValuesNeverOverrideManual(t *testing.T) {
	p := newPipeline(t, "success")
	p.ingest("revision 1")
	lib := &Library{DB: p.m.DB, Blobs: p.m.Blobs, Now: time.Now}
	who := Actor{TenantID: p.tenantID, UserID: uuid.New()}
	id := p.documentNamed(externalID).ID
	_, err := lib.Correct(p.ctx, who, Correction{ID: id, Patch: MetadataPatch{Title: ptr("Mi título"), Code: ptr("MAN-001")}})
	p.must(err)
	p.ingest("revision 2")
	doc := p.documentNamed(externalID)
	if doc.Title != "Mi título" || doc.Code != "MAN-001" || provenanceOf(doc, "title").Source != "manual" || provenanceOf(doc, "code").Source != "manual" {
		t.Fatalf("after a new version: title %q (%s), code %q (%s); a person's correction must stay",
			doc.Title, provenanceOf(doc, "title").Source, doc.Code, provenanceOf(doc, "code").Source)
	}
}
