package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/catalog/catalogdb"
	"github.com/morse/atlas/server/internal/platform/db"
)

// Bands group document types for the Documents screen (ATLAS_DESIGN.md §14).
const (
	BandProcedure = "procedure"
	BandManual    = "manual"
	BandNorm      = "norm"
	// BandERP is reserved for records that come from an ERP source (Pro); no Core document type maps to it.
	BandERP = "erp"

	displayLanguage = "en"
)

// bandOfCoreType maps the Core types to a band. Anything else is a manual.
var bandOfCoreType = map[string]string{
	"policy": BandProcedure, "procedure": BandProcedure, "instruction": BandProcedure, "form": BandProcedure,
	"standard": BandNorm,
}

// DocType is a document type of the tenant, with the band it is shown under.
type DocType struct {
	Key   string
	Label string
	Band  string
}

// EntityType is an entity type of the tenant.
type EntityType struct {
	Key   string
	Label string
}

// Snapshot is the tenant's taxonomy at one moment. Callers keep it for one request.
type Snapshot struct {
	DocTypes    []DocType
	EntityTypes []EntityType
}

// Load reads the taxonomy of the tenant that tx is scoped to.
func Load(ctx context.Context, tx pgx.Tx) (Snapshot, error) {
	q := catalogdb.New(tx)
	docs, err := q.ListDocTypes(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list doc types: %w", err)
	}
	entities, err := q.ListEntityTypes(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list entity types: %w", err)
	}
	var s Snapshot
	for _, d := range docs {
		s.DocTypes = append(s.DocTypes, DocType{Key: d.Key, Label: labelOf(d.Labels, d.Key), Band: bandFor(d.CoreType)})
	}
	for _, e := range entities {
		s.EntityTypes = append(s.EntityTypes, EntityType{Key: e.Key, Label: labelOf(e.Labels, e.Key)})
	}
	return s, nil
}

// Has reports whether key is a document type of the tenant.
func (s Snapshot) Has(key string) bool {
	for _, d := range s.DocTypes {
		if d.Key == key {
			return true
		}
	}
	return false
}

// BandOf returns the band of a document type; unknown types are manuals.
func (s Snapshot) BandOf(key string) string {
	for _, d := range s.DocTypes {
		if d.Key == key {
			return d.Band
		}
	}
	return BandManual
}

// KeysInBand lists the document types shown under band.
func (s Snapshot) KeysInBand(band string) []string {
	var keys []string
	for _, d := range s.DocTypes {
		if d.Band == band {
			keys = append(keys, d.Key)
		}
	}
	return keys
}

func bandFor(coreType string) string {
	if band, ok := bandOfCoreType[coreType]; ok {
		return band
	}
	return BandManual
}

// labelOf picks the display label, or the key if the labels are unreadable or lack the language.
func labelOf(raw []byte, fallback string) string {
	var labels map[string]string
	if err := json.Unmarshal(raw, &labels); err != nil || labels[displayLanguage] == "" {
		return fallback
	}
	return labels[displayLanguage]
}

// Service reads the taxonomy for the HTTP layer.
type Service struct{ DB *db.DB }

// Snapshot returns the taxonomy of one tenant.
func (s *Service) Snapshot(ctx context.Context, tenant uuid.UUID) (Snapshot, error) {
	var out Snapshot
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		var loadErr error
		out, loadErr = Load(ctx, tx)
		return loadErr
	})
	return out, err
}
