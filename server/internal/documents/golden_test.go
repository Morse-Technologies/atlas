//go:build golden

package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/morse/atlas/server/internal/platform/models/docling"
	"github.com/morse/atlas/server/internal/platform/models/gotenberg"
)

// Images pinned by digest, the same as deploy/compose/compose.yaml.
const (
	doclingImage   = "quay.io/docling-project/docling-serve-cpu:v1.38.2@sha256:7a480a1604fa3b42efeddc7db9527690d9f9bc903226136013e2bfe6b1a4dfb0"
	gotenbergImage = "gotenberg/gotenberg:8.39.0@sha256:13672f317b443c793e972b1bd2155ec62a3e7b6891b411e8196442cc053ee5f5"
	goldenDir      = "../../testdata/golden"
	// startupDeadline: docling-serve loads its layout and OCR models on start.
	startupDeadline = 5 * time.Minute
	parseDeadline   = 10 * time.Minute
)

var update = flag.Bool("update", false, "rewrite the expected .blocks.json files")

type service struct {
	image string
	port  string
}

func startService(t *testing.T, s service) string {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.Run(ctx, s.image, testcontainers.WithExposedPorts(s.port+"/tcp"),
		testcontainers.WithWaitStrategyAndDeadline(startupDeadline, wait.ForHTTP("/health").WithPort(s.port+"/tcp")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	endpoint, err := c.PortEndpoint(ctx, s.port+"/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

// TestGolden parses every supported file in testdata/golden and compares its blocks with <file>.blocks.json.
func TestGolden(t *testing.T) {
	tools := Tools{
		Docling:   docling.New(startService(t, service{image: doclingImage, port: "5001"})),
		Gotenberg: gotenberg.New(startService(t, service{image: gotenbergImage, port: "3000"})),
	}
	files, err := SupportedFiles(goldenDir)
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden inputs in %s: %v", goldenDir, err)
	}
	for _, in := range files {
		t.Run(filepath.Base(in.Path), func(t *testing.T) { checkGolden(t, tools, in) })
	}
}

func checkGolden(t *testing.T, tools Tools, in LocalFile) {
	ctx, cancel := context.WithTimeout(context.Background(), parseDeadline)
	defer cancel()
	parsed, err := ParseFile(ctx, tools, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	compareOrUpdate(t, in.Path+".blocks.json", got)
}

func compareOrUpdate(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(bytes.TrimSpace(want), bytes.TrimSpace(got)) {
		t.Fatalf("blocks differ from %s; review the change and run with -update if it is intended", path)
	}
}
