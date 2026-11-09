package graph

import (
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/platform/db"
)

// Actor is who asks: the tenant to scope to and the person to put in the audit log.
type Actor struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
}

// Service builds the graph and keeps the alerts.
type Service struct {
	DB  *db.DB
	Now func() time.Time
}

// NewService returns a Service that reads the real clock.
func NewService(d *db.DB) *Service { return &Service{DB: d, Now: time.Now} }

// Edge producers, stored with each edge so a rescan only replaces what it made.
const (
	producerCodeRegex = "regex-code"
	producerEqualCode = "equal-code"
	edgeReferences    = "references"
	edgeDuplicates    = "duplicates"
	layerPatterns     = 1
	layerEqualCodes   = 2
)
