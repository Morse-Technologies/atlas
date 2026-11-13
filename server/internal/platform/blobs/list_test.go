package blobs

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ownedStore is a store seen from one tenant.
type ownedStore struct {
	*Store
	tenant uuid.UUID
}

func (o ownedStore) commit(t *testing.T, content string) string {
	t.Helper()
	staged, err := o.Stage(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Commit(o.tenant, staged); err != nil {
		t.Fatal(err)
	}
	return staged.Hash
}

func TestListBlobsListsOnlyTheTenantsOwnFiles(t *testing.T) {
	root := New(t.TempDir())
	mine, other := ownedStore{root, uuid.New()}, ownedStore{root, uuid.New()}
	want := mine.commit(t, "mine")
	other.commit(t, "theirs")

	listed, err := root.ListBlobs(mine.tenant)

	if err != nil || len(listed) != 1 || listed[0].Hash != want || listed[0].ModTime.IsZero() {
		t.Fatalf("ListBlobs = %+v, %v; want only %s", listed, err, want)
	}
}

func TestListBlobsOfATenantWithNoneIsEmpty(t *testing.T) {
	listed, err := New(t.TempDir()).ListBlobs(uuid.New())
	if err != nil || len(listed) != 0 {
		t.Fatalf("ListBlobs = %+v, %v; want empty", listed, err)
	}
}

func TestRemoveBlobDeletesItAndToleratesAMissingOne(t *testing.T) {
	s := ownedStore{New(t.TempDir()), uuid.New()}
	hash := s.commit(t, "x")

	if err := s.RemoveBlob(s.tenant, hash); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveBlob(s.tenant, hash); err != nil {
		t.Fatalf("removing a missing blob = %v, want nil", err)
	}
	if listed, _ := s.ListBlobs(s.tenant); len(listed) != 0 {
		t.Fatalf("%d blobs left", len(listed))
	}
}

func TestRemoveDerivedDeletesEveryStage(t *testing.T) {
	s := New(t.TempDir())
	for _, stage := range []string{"parse", "render"} {
		key := DerivedKey{Hash: "abc123", Stage: stage, Version: "v1"}
		if err := s.WriteDerived(key, func(w io.Writer) error { _, err := w.Write([]byte("x")); return err }); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.RemoveDerived("abc123"); err != nil {
		t.Fatal(err)
	}
	if has, _ := s.HasDerived(DerivedKey{Hash: "abc123", Stage: "parse", Version: "v1"}); has {
		t.Fatal("the derived artifact must be gone")
	}
}
