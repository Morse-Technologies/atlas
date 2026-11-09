package documents

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/morse/atlas/server/internal/platform/formats"
	"github.com/morse/atlas/server/internal/platform/models"
	"github.com/morse/atlas/server/internal/platform/models/docling"
	"github.com/morse/atlas/server/internal/platform/models/gotenberg"
)

// p95 is the percentile reported per document.
const p95 = 0.95

// Tools are the conversion services, for running the parse steps outside the pipeline.
type Tools struct {
	Docling   *docling.Client
	Gotenberg *gotenberg.Client
}

// BenchResult summarizes parsing a directory (ATLAS.md §24.3, item 1).
type BenchResult struct {
	Documents      int
	Failed         int
	Pages          int
	Total          time.Duration
	P95PerDoc      time.Duration
	PagesPerMinute float64
}

// LocalFile is a file on this host with its detected format.
type LocalFile struct {
	Path   string
	Format formats.Format
}

// BenchParse renders and parses every supported file under dir, without touching the database.
func BenchParse(ctx context.Context, tools Tools, dir string) (BenchResult, error) {
	files, err := SupportedFiles(dir)
	if err != nil {
		return BenchResult{}, err
	}
	var res BenchResult
	var durations []time.Duration
	for _, f := range files {
		start := time.Now()
		parsed, err := ParseFile(ctx, tools, f)
		if err != nil {
			res.Failed++
			continue
		}
		durations = append(durations, time.Since(start))
		res.Documents, res.Pages = res.Documents+1, res.Pages+parsed.Pages
	}
	return summarize(res, durations), nil
}

// SupportedFiles lists the files under dir that the pipeline converts. Unsupported files are skipped.
func SupportedFiles(dir string) ([]LocalFile, error) {
	var out []LocalFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if format, err := formats.Detect(path); err == nil {
			out = append(out, LocalFile{Path: path, Format: format})
		}
		return nil
	})
	return out, err
}

// ParseFile renders (if needed), parses and normalizes one file: the same steps as the convert and enrich
// jobs, without database or cache. Used by the benchmark and the golden tests.
func ParseFile(ctx context.Context, tools Tools, f LocalFile) (Parsed, error) {
	input := models.File{Name: f.Format.UploadName(), Path: f.Path}
	if f.Format.RenderToPDF {
		pdf, err := renderToTemp(ctx, tools.Gotenberg, input)
		if err != nil {
			return Parsed{}, err
		}
		defer os.Remove(pdf.Path)
		input = pdf
	}
	raw, err := tools.Docling.Convert(ctx, input)
	if err != nil {
		return Parsed{}, err
	}
	return Normalize(raw)
}

func renderToTemp(ctx context.Context, g *gotenberg.Client, input models.File) (models.File, error) {
	tmp, err := os.CreateTemp("", "atlas-render-*.pdf")
	if err != nil {
		return models.File{}, err
	}
	renderErr := g.OfficeToPDF(ctx, input, tmp)
	if err := closeOrRemove(tmp, renderErr); err != nil {
		return models.File{}, err
	}
	return models.File{Name: renderedPDFName, Path: tmp.Name()}, nil
}

// closeOrRemove closes a written temp file and deletes it if writing or closing failed.
func closeOrRemove(f *os.File, writeErr error) error {
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(f.Name())
		return errors.Join(writeErr, closeErr)
	}
	return nil
}

func summarize(res BenchResult, durations []time.Duration) BenchResult {
	if len(durations) == 0 {
		return res
	}
	slices.Sort(durations)
	for _, d := range durations {
		res.Total += d
	}
	res.P95PerDoc = durations[int(float64(len(durations)-1)*p95)]
	res.PagesPerMinute = float64(res.Pages) / res.Total.Minutes()
	return res
}
