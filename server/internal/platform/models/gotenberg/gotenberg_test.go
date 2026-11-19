package gotenberg

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/models"
)

func officeFile(t *testing.T) models.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "offer.docx")
	if err := os.WriteFile(path, []byte("docx bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	return models.File{Name: "offer.docx", Path: path}
}

func TestOfficeToPDFSendsTheFileAndStreamsThePDFBack(t *testing.T) {
	var uploaded string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile(fileField)
		if err != nil || r.URL.Path != libreOfficePath {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(file)
		uploaded = string(raw)
		_, _ = w.Write([]byte("%PDF-1.7"))
	}))
	defer server.Close()
	var pdf bytes.Buffer

	err := New(server.URL).OfficeToPDF(context.Background(), officeFile(t), &pdf)

	if err != nil || uploaded != "docx bytes" || pdf.String() != "%PDF-1.7" {
		t.Fatalf("err=%v uploaded=%q pdf=%q", err, uploaded, pdf.String())
	}
}

func TestOfficeToPDFAServerErrorIsTransient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := New(server.URL).OfficeToPDF(context.Background(), officeFile(t), io.Discard)

	var transient *errs.Transient
	if !errors.As(err, &transient) {
		t.Fatalf("want a transient error, got %v", err)
	}
}

func TestOfficeToPDFARefusedFileIsPermanent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	err := New(server.URL).OfficeToPDF(context.Background(), officeFile(t), io.Discard)

	if errs.PermanentReason(err) == "" {
		t.Fatalf("want a permanent error, got %v", err)
	}
}

func TestHealthFollowsTheStatusOfTheHealthRoute(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != healthPath {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	client := New(server.URL)

	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("healthy server: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := client.Health(context.Background()); err == nil {
		t.Fatal("an unhealthy server must be reported")
	}
}
