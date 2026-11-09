package sources

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// writeZip builds a ZIP file whose entries hold the given contents.
func writeZip(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.docx")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(content)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantReason(t *testing.T, err error, reason errs.Reason) {
	t.Helper()
	if errs.Classify(err) != errs.ClassPermanent || errs.PermanentReason(err) != reason {
		t.Fatalf("got %v, want permanent %s", err, reason)
	}
}

func TestCheckArchiveRejectsZipBomb(t *testing.T) {
	path := writeZip(t, map[string][]byte{"word/document.xml": bytes.Repeat([]byte{0}, 50<<20)})
	wantReason(t, checkArchive(path), errs.ReasonHostile)
}

func TestCheckArchiveRejectsPathTraversal(t *testing.T) {
	path := writeZip(t, map[string][]byte{"../../etc/passwd": []byte("x")})
	wantReason(t, checkArchive(path), errs.ReasonHostile)
}

func TestCheckArchiveAcceptsNormalContainer(t *testing.T) {
	path := writeZip(t, map[string][]byte{"word/document.xml": []byte("<w:document>hola</w:document>")})
	if err := checkArchive(path); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSize(t *testing.T) {
	wantReason(t, checkSize(11, 10), errs.ReasonTooLarge)
	if err := checkSize(10, 10); err != nil {
		t.Fatal(err)
	}
}

func TestCheckHostileFileChecksArchivesOnlyForContainers(t *testing.T) {
	bomb := writeZip(t, map[string][]byte{"word/document.xml": bytes.Repeat([]byte{0}, 50<<20)})
	_, err := checkHostileFile(blobs.Staged{Path: bomb, Size: 1}, 1<<20)
	wantReason(t, err, errs.ReasonHostile)
}
