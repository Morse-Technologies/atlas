package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		path := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestWalkSkipsNoiseAndUsesSlashIDs(t *testing.T) {
	root := writeTree(t, "Calidad/PNT-014.pdf", "Calidad/~$PNT-014.docx", ".DS_Store")
	seen, err := folder{root: root}.walk(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].ExternalID != "Calidad/PNT-014.pdf" {
		t.Fatalf("walk() = %+v", seen)
	}
}

func TestOpenRefusesPathsOutsideRoot(t *testing.T) {
	root := writeTree(t, "inside.pdf")
	if _, err := (folder{root: root}).open("../outside.pdf"); err == nil {
		t.Fatal("open escaped the source root")
	}
}

func TestWalkOfMissingRootIsIncomplete(t *testing.T) {
	_, err := folder{root: filepath.Join(t.TempDir(), "unmounted")}.walk(context.Background())
	if err == nil {
		t.Fatal("a missing root must make the walk incomplete, or the scan would delete everything")
	}
}
