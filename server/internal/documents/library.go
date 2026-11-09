package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/db"
)

// Actor is who asks: the tenant to scope to and the person to put in the audit log.
type Actor struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
}

// Library is what the HTTP layer calls to read and correct documents.
type Library struct {
	DB    *db.DB
	Blobs *blobs.Store
	Now   func() time.Time
}

// NewLibrary returns a Library that reads the real clock.
func NewLibrary(d *db.DB, store *blobs.Store) *Library {
	return &Library{DB: d, Blobs: store, Now: time.Now}
}

// Version is one revision of a document.
type Version struct {
	ID        uuid.UUID
	Revision  string
	State     string
	IndexedAt time.Time
	Pages     int
}

// Provenance says where a metadata value came from.
type Provenance struct {
	Field      string
	Value      string
	Source     string
	Confidence float32
}

// Document is a document with its versions and the origin of its metadata.
type Document struct {
	ID               uuid.UUID
	SourceID         uuid.UUID
	CurrentVersionID uuid.UUID
	Code             string
	Title            string
	DocType          string
	Band             string
	Status           string
	Department       string
	Path             string
	Language         string
	Summary          string
	OwnerID          *uuid.UUID
	ApprovedAt       *time.Time
	ReviewDueAt      *time.Time
	Attributes       map[string]string
	Versions         []Version
	Provenance       []Provenance
}

// Listing selects documents. Band and Status accept "all" or "" for no filter. After is the id of the last
// document of the previous page (uuid.Nil: first page).
type Listing struct {
	Query    string
	Band     string
	Status   string
	SourceID *uuid.UUID
	After    uuid.UUID
	Size     int32
}

const filterAll = "all"

// List returns one page of documents in id order, and the cursor of the next page (uuid.Nil: none).
func (l *Library) List(ctx context.Context, who Actor, in Listing) ([]Document, uuid.UUID, error) {
	var docs []Document
	err := l.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var listErr error
		docs, listErr = l.page(ctx, tx, in)
		return listErr
	})
	if err != nil || int32(len(docs)) <= in.Size {
		return docs, uuid.Nil, err
	}
	return docs[:in.Size], docs[in.Size-1].ID, nil
}

// page reads one row more than asked, so the caller can tell whether another page exists.
func (l *Library) page(ctx context.Context, tx pgx.Tx, in Listing) ([]Document, error) {
	tax, err := catalog.Load(ctx, tx)
	if err != nil {
		return nil, err
	}
	params := listParams(in, tax)
	rows, err := documentsdb.New(tx).ListDocuments(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return l.assemble(ctx, tx, assembly{rows: rows, tax: tax})
}

func listParams(in Listing, tax catalog.Snapshot) documentsdb.ListDocumentsParams {
	p := documentsdb.ListDocumentsParams{After: in.After, PageSize: in.Size + 1, SourceID: in.SourceID, Q: likeEscape(in.Query)}
	if in.Status != filterAll {
		p.Status = in.Status
	}
	if in.Band != "" && in.Band != filterAll {
		p.FilterTypes, p.DocTypes = true, tax.KeysInBand(in.Band)
	}
	return p
}

// likeEscape stops %, _ and \ typed by a person from acting as wildcards.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Get returns one document.
func (l *Library) Get(ctx context.Context, who Actor, id uuid.UUID) (Document, error) {
	var doc Document
	err := l.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var getErr error
		doc, getErr = l.load(ctx, tx, id)
		return getErr
	})
	return doc, err
}

func (l *Library) load(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Document, error) {
	row, err := documentsdb.New(tx).GetDocument(ctx, id)
	if err != nil {
		return Document{}, db.NotFoundIfNoRows(err, "document")
	}
	tax, err := catalog.Load(ctx, tx)
	if err != nil {
		return Document{}, err
	}
	docs, err := l.assemble(ctx, tx, assembly{rows: []documentsdb.DocumentsDocument{row}, tax: tax})
	if err != nil {
		return Document{}, err
	}
	return docs[0], nil
}

// assembly is a set of document rows to complete, and the taxonomy that gives each its band.
type assembly struct {
	rows []documentsdb.DocumentsDocument
	tax  catalog.Snapshot
}

// assemble fills versions and provenance for a set of rows with two queries, not two per document.
func (l *Library) assemble(ctx context.Context, tx pgx.Tx, in assembly) ([]Document, error) {
	ids := make([]uuid.UUID, len(in.rows))
	for i, r := range in.rows {
		ids[i] = r.ID
	}
	q := documentsdb.New(tx)
	versions, err := q.ListVersionsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	prov, err := q.ListProvenanceOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list provenance: %w", err)
	}
	docs := make([]Document, len(in.rows))
	for i, r := range in.rows {
		docs[i] = toDocument(r, in.tax)
	}
	attach(docs, versions, prov)
	return docs, nil
}

func attach(docs []Document, versions []documentsdb.ListVersionsOfRow, prov []documentsdb.ListProvenanceOfRow) {
	index := make(map[uuid.UUID]int, len(docs))
	for i, d := range docs {
		index[d.ID] = i
	}
	counted := map[uuid.UUID]int{}
	for _, v := range versions {
		counted[v.DocumentID]++
		d := &docs[index[v.DocumentID]]
		d.Versions = append(d.Versions, toVersion(v, counted[v.DocumentID]))
	}
	for _, p := range prov {
		d := &docs[index[p.DocumentID]]
		d.Provenance = append(d.Provenance, Provenance{Field: p.Field, Value: p.Value, Source: p.Source, Confidence: p.Confidence})
	}
}

// versionStates maps the pipeline's states to the ones the API shows. A parsed version is not searchable yet, so
// it still shows as processing.
var versionStates = map[string]string{"processing": "processing", "parsed": "processing", "indexed": "indexed", "superseded": "superseded", "quarantined": "quarantined"}

// toVersion numbers revisions in creation order until metadata extraction reads the real revision (part 2).
func toVersion(v documentsdb.ListVersionsOfRow, ordinal int) Version {
	at := v.CreatedAt
	if v.ParsedAt != nil {
		at = *v.ParsedAt
	}
	pages := 0
	if v.Pages != nil {
		pages = int(*v.Pages)
	}
	return Version{ID: v.ID, Revision: fmt.Sprintf("v%d", ordinal), State: versionStates[v.State], IndexedAt: at, Pages: pages}
}

func toDocument(r documentsdb.DocumentsDocument, tax catalog.Snapshot) Document {
	attrs := map[string]string{}
	_ = json.Unmarshal(r.Attributes, &attrs) // the column is written only by this package as a string map
	current := uuid.Nil
	if r.CurrentVersionID != nil {
		current = *r.CurrentVersionID
	}
	return Document{ID: r.ID, SourceID: r.SourceID, CurrentVersionID: current, Code: r.Code, Title: r.Title,
		DocType: r.DocType, Band: tax.BandOf(r.DocType), Status: r.Status, Department: r.Department, Path: r.ExternalID,
		Language: r.Language, Summary: r.Summary, OwnerID: r.OwnerID, ApprovedAt: r.ApprovedAt,
		ReviewDueAt: r.ReviewDueAt, Attributes: attrs}
}

// Counts is how many documents there are, for the panel.
type Counts struct {
	ByType     map[string]int // current documents per document type
	Processing int            // versions still being read
}

// Counts returns the number of current documents per type and the versions still processing.
func (l *Library) Counts(ctx context.Context, scope db.Scope) (Counts, error) {
	out := Counts{ByType: map[string]int{}}
	err := l.DB.InScopeTx(ctx, scope, func(tx pgx.Tx) error {
		q := documentsdb.New(tx)
		rows, err := q.CountCurrentByType(ctx)
		if err != nil {
			return fmt.Errorf("count documents: %w", err)
		}
		for _, r := range rows {
			out.ByType[r.DocType] = int(r.Total)
		}
		processing, err := q.CountProcessing(ctx)
		out.Processing = int(processing)
		return err
	})
	return out, err
}
