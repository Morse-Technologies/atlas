//go:build integration

package documents

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources"
)

// restricted is one source with one document, and two people: Ana, who may read it once it is restricted, and Ben.
type restricted struct {
	libraryFixture
	d     *db.DB
	admin *sources.Admin
	ana   Actor
	ben   Actor
	src   uuid.UUID
	doc   uuid.UUID
}

func newRestricted(t *testing.T) restricted {
	t.Helper()
	d := dbtest.Start(t)
	f := libraryFixture{t: t, ctx: dbtest.WithJobs(t, d), lib: NewLibrary(d, blobs.New(t.TempDir())),
		who: Actor{TenantID: dbtest.NewTenant(t, d)}, docs: map[string]uuid.UUID{}}
	r := restricted{libraryFixture: f, d: d, admin: sources.NewAdmin(d, t.TempDir(), nil)}
	r.admin.ApplyAccess = ApplyAccess
	r.ana, r.ben = r.person("ana@acme.test"), r.person("ben@acme.test")
	must(t, d.InTenantTx(f.ctx, f.who.TenantID, func(tx pgx.Tx) (err error) {
		r.src, err = sources.AddFolder(f.ctx, tx, sources.NewFolder{TenantID: f.who.TenantID, Name: "quality", Path: t.TempDir()})
		if err != nil {
			return err
		}
		r.doc, err = f.insert(tx, r.src, "quality/PNT-014.pdf", "procedure")
		return err
	}))
	return r
}

func (r restricted) person(email string) Actor {
	var id uuid.UUID
	must(r.t, r.d.InTenantTx(r.ctx, r.who.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.ctx, `INSERT INTO access.users (name, email, password_hash, hue) VALUES ($1, $1, 'x', 0) RETURNING id`, email).Scan(&id)
	}))
	return Actor{TenantID: r.who.TenantID, UserID: id}
}

func (r restricted) restrictTo(readers []uuid.UUID) {
	r.t.Helper()
	_, err := r.admin.SetReaders(r.ctx, sources.Actor{TenantID: r.who.TenantID, UserID: r.ana.UserID}, sources.ReadersChange{ID: r.src, Readers: readers})
	must(r.t, err)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (r restricted) sees(who Actor) int {
	r.t.Helper()
	docs, _, err := r.lib.List(r.ctx, who, Listing{Size: 50, Status: filterAll})
	must(r.t, err)
	return len(docs)
}

func TestARestrictedSourceIsInvisibleToEveryoneButItsReaders(t *testing.T) {
	r := newRestricted(t)
	if r.sees(r.ben) != 1 {
		t.Fatal("a new source is open to every signed-in person")
	}
	r.restrictTo([]uuid.UUID{r.ana.UserID})
	if r.sees(r.ana) != 1 || r.sees(r.ben) != 0 {
		t.Fatalf("Ana sees %d, Ben sees %d; want 1 and 0", r.sees(r.ana), r.sees(r.ben))
	}
	if _, err := r.lib.Get(r.ctx, r.ben, r.doc); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("Ben reading the document = %v, want not found (never forbidden: it must not confirm it exists)", err)
	}
	counts, err := r.lib.Counts(r.ctx, db.Scope{Tenant: r.ben.TenantID, User: r.ben.UserID})
	if err != nil || len(counts.ByType) != 0 {
		t.Fatalf("Ben's counts %+v, %v; hidden documents must not be counted", counts, err)
	}
}

func TestAHiddenDocumentsFileIsNotFoundForTheOthers(t *testing.T) {
	r := newRestricted(t)
	var version uuid.UUID
	must(t, r.d.InTenantTx(r.ctx, r.who.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.ctx, "SELECT current_version_id FROM documents.documents WHERE id = $1", r.doc).Scan(&version)
	}))
	r.restrictTo([]uuid.UUID{r.ana.UserID})
	_, err := r.lib.OpenFile(r.ctx, r.ben, FileRequest{DocumentID: r.doc, VersionID: version})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("Ben opening the file = %v, want not found", err)
	}
}

func TestOpeningASourceAgainShowsItsDocumentsToEveryone(t *testing.T) {
	r := newRestricted(t)
	r.restrictTo([]uuid.UUID{r.ana.UserID})
	r.restrictTo(nil)
	if r.sees(r.ben) != 1 {
		t.Fatal("a source opened to everyone must show its documents again")
	}
}

func TestADocumentThatArrivesLaterInARestrictedSourceIsRestrictedToo(t *testing.T) {
	r := newRestricted(t)
	r.restrictTo([]uuid.UUID{r.ana.UserID})
	m := &Module{DB: r.d, Blobs: blobs.New(t.TempDir())}
	stored := events.VersionStored{TenantID: r.who.TenantID, SourceID: r.src, ExternalID: "quality/new.pdf", ContentHash: "h", MIME: pdfMIME, Size: 1}
	must(t, m.OnVersionStored(r.ctx, stored))
	var hidden bool
	must(t, r.d.InTenantTx(r.ctx, r.who.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.ctx, "SELECT acl_set_id IS NOT NULL FROM documents.documents WHERE external_id = 'quality/new.pdf'").Scan(&hidden)
	}))
	if !hidden {
		t.Fatal("the new document must carry the source's set from the start")
	}
	if r.sees(r.ben) != 0 {
		t.Fatal("Ben must not see the new document either")
	}
}

func TestASourceCannotBeRestrictedToNobodyOrToSomeoneWhoDoesNotExist(t *testing.T) {
	r := newRestricted(t)
	who := sources.Actor{TenantID: r.who.TenantID, UserID: r.ana.UserID}
	for _, readers := range [][]uuid.UUID{{}, {uuid.New()}} {
		_, err := r.admin.SetReaders(r.ctx, who, sources.ReadersChange{ID: r.src, Readers: readers})
		if !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("readers %v: err = %v, want invalid", readers, err)
		}
	}
}
