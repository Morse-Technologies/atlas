package docling

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/models"
)

// fakeServe answers like docling-serve with a fixed task status and result body.
type fakeServe struct {
	taskStatus string
	result     string
}

func (f fakeServe) start(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+submitPath, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"task_id":"t1","task_status":"pending"}`)
	})
	mux.HandleFunc("GET "+pollPath+"t1", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"task_id":"t1","task_status":%q}`, f.taskStatus)
	})
	mux.HandleFunc("GET "+resultPath+"t1", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, f.result) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func sampleFile(t *testing.T) models.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7"), 0o600); err != nil {
		t.Fatal(err)
	}
	return models.File{Name: "document.pdf", Path: path}
}

func TestConvertReturnsTheDoclingDocument(t *testing.T) {
	c := fakeServe{taskStatus: statusSuccess, result: `{"status":"success","document":{"json_content":{"name":"x"}}}`}.start(t)
	got, err := c.Convert(context.Background(), sampleFile(t))
	if err != nil || string(got) != `{"name":"x"}` {
		t.Fatalf("Convert() = %s, %v", got, err)
	}
}

func TestFailedTaskIsPermanent(t *testing.T) {
	c := fakeServe{taskStatus: statusFailure}.start(t)
	_, err := c.Convert(context.Background(), sampleFile(t))
	if errs.PermanentReason(err) != errs.ReasonCorrupt {
		t.Fatalf("got %v, want permanent corrupt", err)
	}
}

func TestEmptyResultIsPermanent(t *testing.T) {
	c := fakeServe{taskStatus: statusSuccess, result: `{"status":"success","document":{}}`}.start(t)
	_, err := c.Convert(context.Background(), sampleFile(t))
	if errs.Classify(err) != errs.ClassPermanent {
		t.Fatalf("got %v, want permanent", err)
	}
}

func TestJobTimeoutWhileWaitingIsTransient(t *testing.T) {
	c := fakeServe{taskStatus: "started"}.start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Convert(ctx, sampleFile(t))
	if errs.Classify(err) != errs.ClassTransient {
		t.Fatalf("got %v, want transient so the job is retried", err)
	}
}
