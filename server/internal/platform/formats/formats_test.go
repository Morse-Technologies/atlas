package formats

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
)

func TestDetectReadsContentNotExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "looks-like.pdf")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho not a pdf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Detect(path)
	if errs.PermanentReason(err) != errs.ReasonUnsupported {
		t.Fatalf("Detect() error = %v, want permanent unsupported", err)
	}
}

func TestDetectRecognizesPDF(t *testing.T) {
	f, err := Detect(filepath.Join("..", "..", "..", "testdata", "golden", "PNT-014-rev3.pdf"))
	if err != nil || f.MIME != "application/pdf" || f.RenderToPDF {
		t.Fatalf("Detect() = %+v, %v", f, err)
	}
}

func TestEveryFormatHasAnExtension(t *testing.T) {
	for _, f := range supported {
		if f.Ext == "" || f.UploadName() == "document" {
			t.Errorf("%s has no extension: the converters would not know how to read it", f.MIME)
		}
	}
}
