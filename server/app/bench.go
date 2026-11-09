package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/platform/config"
	"github.com/morse/atlas/server/internal/platform/models/docling"
	"github.com/morse/atlas/server/internal/platform/models/gotenberg"
)

// runBench: atlas bench parse DIR. Prints the capacity figures of ATLAS.md §24.3 item 1.
func runBench(ctx context.Context, args []string) error {
	if len(args) != 2 || args[0] != "parse" {
		return errors.New("usage: atlas bench parse DIR")
	}
	cfg, err := config.Load[config.Models]()
	if err != nil {
		return err
	}
	tools := documents.Tools{Docling: docling.New(cfg.DoclingURL), Gotenberg: gotenberg.New(cfg.GotenbergURL)}
	res, err := documents.BenchParse(ctx, tools, args[1])
	if err != nil {
		return err
	}
	fmt.Printf("documents %d (failed %d), pages %d, total %s, p95 per document %s, %.1f pages/min\n",
		res.Documents, res.Failed, res.Pages, res.Total, res.P95PerDoc, res.PagesPerMinute)
	return nil
}
