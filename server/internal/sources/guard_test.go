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
	_, err := checkHostileFile(candidate{staged: blobs.Staged{Path: bomb, Size: 1}, name: "bomb.docx"}, 1<<20)
	wantReason(t, err, errs.ReasonHostile)
}

// testdataFile is the path of a fixture shared by the server's tests.
func testdataFile(name string) string { return filepath.Join("..", "..", "testdata", name) }

func TestAPasswordProtectedPDFIsEncryptedNotCorrupt(t *testing.T) {
	_, err := checkHostileFile(candidate{staged: blobs.Staged{Path: testdataFile("encrypted.pdf"), Size: 1}, name: "plan.pdf"}, 1<<20)
	wantReason(t, err, errs.ReasonEncrypted)
}

func TestAPasswordProtectedOfficeFileIsEncryptedNotUnsupported(t *testing.T) {
	for _, name := range []string{"plan.docx", "plan.XLSX", "plan.pptx"} {
		_, err := checkHostileFile(candidate{staged: blobs.Staged{Path: testdataFile("encrypted.docx"), Size: 1}, name: name}, 1<<20)
		wantReason(t, err, errs.ReasonEncrypted)
	}
}

func TestAnOLEFileThatIsNotOOXMLIsNotCalledEncrypted(t *testing.T) {
	_, err := checkHostileFile(candidate{staged: blobs.Staged{Path: testdataFile("encrypted.docx"), Size: 1}, name: "old.doc"}, 1<<20)
	wantReason(t, err, errs.ReasonUnsupported) // this fixture has no Word streams; the point is that it is not "encrypted"
}

func TestAPDFWithoutEncryptionPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	encrypted, err := isEncrypted(candidate{staged: blobs.Staged{Path: path}, name: "plain.pdf"})
	if err != nil || encrypted {
		t.Fatalf("isEncrypted = %v, %v; want false", encrypted, err)
	}
}

func TestEncryptionNamedInTheLastKilobytesOfALargePDFIsFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.pdf")
	body := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 200<<10)...)
	if err := os.WriteFile(path, append(body, []byte("\ntrailer << /Encrypt 9 0 R >>\n%%EOF\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	encrypted, err := isEncrypted(candidate{staged: blobs.Staged{Path: path}, name: "big.pdf"})
	if err != nil || !encrypted {
		t.Fatalf("isEncrypted = %v, %v; want true", encrypted, err)
	}
}

func TestAnEmptyFileIsNotEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pdf")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	encrypted, err := isEncrypted(candidate{staged: blobs.Staged{Path: path}, name: "empty.pdf"})
	if err != nil || encrypted {
		t.Fatalf("isEncrypted = %v, %v; want false", encrypted, err)
	}
}
