// Package migrations embeds the goose SQL migrations so the binary carries its own schema.
package migrations

import "embed"

// FS holds every *.sql migration, applied in file-name order.
//
//go:embed *.sql
var FS embed.FS
