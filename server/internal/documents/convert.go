package documents

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/formats"
	"github.com/morse/atlas/server/internal/platform/models"
	"github.com/morse/atlas/server/internal/sources"
)

// Stage names and versions. Bump a version when its output changes, so cached artifacts are rebuilt.
const (
	StepConvert   = "convert"
	StepEnrich    = "enrich"
	renderStage   = "render"
	renderVersion = "1"
	parseStage    = "parse"
	parserVersion = "1"
	// renderedPDFName is the upload name of an Office file after its PDF rendering.
	renderedPDFName = "document.pdf"
	// maxPages bounds what one document may cost (ATLAS.md §8.1).
	maxPages = 2000
)

// Convert renders the version if needed and parses it; a permanent failure quarantines the version.
func (m *Module) Convert(ctx context.Context, args events.VersionConvert) error {
	v, err := m.loadVersion(ctx, args.TenantID, args.VersionID)
	if err != nil {
		return err
	}
	if err := m.renderAndParse(ctx, v); err != nil {
		return m.quarantineIfPermanent(ctx, failedVersion{v: v, stage: StepConvert, err: err})
	}
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		if err := sources.RecordStep(ctx, tx, v.step(StepConvert, sources.StepDone)); err != nil {
			return err
		}
		return jobs.Enqueue(ctx, tx, events.VersionEnrich{TenantID: args.TenantID, VersionID: args.VersionID})
	})
}

func (m *Module) renderAndParse(ctx context.Context, v version) error {
	input, err := m.parserInput(ctx, v)
	if err != nil {
		return err
	}
	raw, err := m.parse(ctx, parseRequest{v: v, input: input})
	if err != nil {
		return err
	}
	parsed, err := Normalize(raw)
	if err != nil {
		return err
	}
	if parsed.Pages > maxPages {
		return errs.NewPermanent(errs.ReasonTooLarge, fmt.Errorf("%d pages, the limit is %d", parsed.Pages, maxPages))
	}
	return nil
}

// parserInput is the original blob, or its PDF rendering for Office formats.
func (m *Module) parserInput(ctx context.Context, v version) (models.File, error) {
	format, ok := formats.ByMIME(v.row.Mime)
	if !ok {
		return models.File{}, errs.NewPermanent(errs.ReasonUnsupported, fmt.Errorf("content type %s is no longer ingested", v.row.Mime))
	}
	original := models.File{Name: format.UploadName(), Path: m.Blobs.BlobPath(v.tenantID, v.row.ContentHash)}
	if !format.RenderToPDF {
		return original, nil
	}
	key := blobs.DerivedKey{Hash: v.row.ContentHash, Stage: renderStage, Version: renderVersion}
	err := m.buildOnce(key, func(w io.Writer) error { return m.Gotenberg.OfficeToPDF(ctx, original, w) })
	return models.File{Name: renderedPDFName, Path: m.Blobs.DerivedPath(key)}, err
}

type parseRequest struct {
	v     version
	input models.File
}

// parse returns the DoclingDocument JSON, from cache when this content was parsed before.
func (m *Module) parse(ctx context.Context, req parseRequest) ([]byte, error) {
	key := parseKey(req.v.row.ContentHash)
	err := m.buildOnce(key, func(w io.Writer) error {
		raw, err := m.Docling.Convert(ctx, req.input)
		if err != nil {
			return err
		}
		_, err = w.Write(raw)
		return err
	})
	if err != nil {
		return nil, err
	}
	return os.ReadFile(m.Blobs.DerivedPath(key))
}

func parseKey(contentHash string) blobs.DerivedKey {
	return blobs.DerivedKey{Hash: contentHash, Stage: parseStage, Version: parserVersion}
}

// buildOnce writes a derived artifact unless it is already cached.
func (m *Module) buildOnce(key blobs.DerivedKey, write func(io.Writer) error) error {
	cached, err := m.Blobs.HasDerived(key)
	if err != nil || cached {
		return err
	}
	return m.Blobs.WriteDerived(key, write)
}

type failedVersion struct {
	v     version
	stage string
	err   error
}

func (m *Module) quarantineIfPermanent(ctx context.Context, f failedVersion) error {
	if errs.Classify(f.err) != errs.ClassPermanent {
		return f.err
	}
	return m.DB.InTenantTx(ctx, f.v.tenantID, func(tx pgx.Tx) error {
		if err := documentsdb.New(tx).MarkVersionQuarantined(ctx, f.v.row.ID); err != nil {
			return err
		}
		return sources.Quarantine(ctx, tx, sources.Failure{
			SourceID: f.v.row.SourceID, ExternalID: f.v.row.ExternalID, ContentHash: f.v.row.ContentHash,
			Stage: f.stage, Err: f.err,
		})
	})
}

func (v version) step(name, status string) sources.Step {
	return sources.Step{SourceID: v.row.SourceID, ExternalID: v.row.ExternalID, ContentHash: v.row.ContentHash, Name: name, Status: status}
}
