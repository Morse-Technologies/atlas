package sources

import (
	"bytes"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
)

type upload struct{ name, content string }

// body builds a multipart body with one "files" part per upload.
func body(t *testing.T, uploads ...upload) *multipart.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, u := range uploads {
		part, err := w.CreateFormFile(uploadFormField, u.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(u.content))
	}
	_ = w.Close()
	return multipart.NewReader(&buf, w.Boundary())
}

func store(t *testing.T, uploads ...upload) (UploadResult, string) {
	t.Helper()
	admin := &Admin{UploadRoot: t.TempDir()}
	dir := filepath.Join(admin.UploadRoot, "tenant")
	result, err := admin.store(t.Context(), dir, body(t, uploads...))
	if err != nil {
		t.Fatal(err)
	}
	return result, dir
}

func TestAcceptedFilesLandInTheFolder(t *testing.T) {
	result, dir := store(t, upload{"PNT-014.pdf", "pdf bytes"})
	data, err := os.ReadFile(filepath.Join(dir, "PNT-014.pdf"))
	if result.Accepted != 1 || err != nil || string(data) != "pdf bytes" {
		t.Fatalf("result %+v, data %q, err %v", result, data, err)
	}
}

func TestPathsInNamesCannotClimbOut(t *testing.T) {
	result, dir := store(t, upload{"../../etc/evil.pdf", "x"}, upload{`..\..\win.pdf`, "x"})
	for _, name := range []string{"evil.pdf", "win.pdf"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should be saved inside the folder: %v", name, err)
		}
	}
	if result.Accepted != 2 {
		t.Fatalf("accepted %d, want 2", result.Accepted)
	}
}

func TestHiddenAndEmptyFilesAreRefusedWithAReason(t *testing.T) {
	result, _ := store(t, upload{".secret.pdf", "x"}, upload{"~$lock.docx", "x"}, upload{"empty.pdf", ""})
	if result.Accepted != 0 || len(result.Rejected) != 3 {
		t.Fatalf("result %+v; want 3 refused", result)
	}
	for _, r := range result.Rejected {
		if r.Reason == "" {
			t.Errorf("%s refused without a reason", r.Name)
		}
	}
}

func TestSameNameReplacesTheFileSoThePipelineSeesANewVersion(t *testing.T) {
	_, dir := store(t, upload{"a.pdf", "one"}, upload{"a.pdf", "two"})
	if data, _ := os.ReadFile(filepath.Join(dir, "a.pdf")); string(data) != "two" {
		t.Fatalf("content %q, want the last upload", data)
	}
}

func TestNoTemporaryFileIsLeftBehind(t *testing.T) {
	admin := &Admin{UploadRoot: t.TempDir()}
	_, _ = admin.store(t.Context(), filepath.Join(admin.UploadRoot, "t"), body(t, upload{"a.pdf", "x"}, upload{".bad", "x"}))
	left, _ := os.ReadDir(filepath.Join(admin.UploadRoot, tempDirName))
	if len(left) != 0 {
		t.Fatalf("%d temporary files left", len(left))
	}
}

func TestTooManyFilesIsRefused(t *testing.T) {
	uploads := make([]upload, maxUploadFiles+1)
	for i := range uploads {
		uploads[i] = upload{name: string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".pdf", content: "x"}
	}
	admin := &Admin{UploadRoot: t.TempDir()}
	if _, err := admin.store(t.Context(), filepath.Join(admin.UploadRoot, "t"), body(t, uploads...)); err == nil {
		t.Fatal("more files than the limit must be refused")
	}
}
