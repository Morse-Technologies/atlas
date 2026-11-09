package blobs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func tempFiles(t *testing.T, s *Store) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.root, tmpDirName))
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestCommittingTheSameContentTwiceKeepsOneBlob(t *testing.T) {
	s, tenant := New(t.TempDir()), uuid.New()
	for range 2 {
		staged, err := s.Stage(strings.NewReader("same bytes"))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Commit(tenant, staged); err != nil {
			t.Fatal(err)
		}
	}
	if left := tempFiles(t, s); len(left) != 0 {
		t.Fatalf("temp area not cleaned: %d files", len(left))
	}
}

func TestFailedDerivedWriteLeavesNothingBehind(t *testing.T) {
	s := New(t.TempDir())
	key := DerivedKey{Hash: "abc", Stage: "parse", Version: "1"}
	err := s.WriteDerived(key, func(io.Writer) error { return errors.New("docling died") })
	cached, statErr := s.HasDerived(key)
	if err == nil || cached || statErr != nil || len(tempFiles(t, s)) != 0 {
		t.Fatalf("err=%v cached=%v statErr=%v: a failed write must not look cached", err, cached, statErr)
	}
}
