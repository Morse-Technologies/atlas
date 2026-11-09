package app

import (
	"context"

	"github.com/morse/atlas/server/internal/platform/config"
	"github.com/morse/atlas/server/internal/platform/schema"
)

// runMigrate applies all migrations as the schema owner (ATLAS.md §4.3).
func runMigrate(ctx context.Context, _ []string) error {
	cfg, err := config.Load[config.Migrate]()
	if err != nil {
		return err
	}
	return schema.Migrate(ctx, cfg.URL)
}
