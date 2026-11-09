// Package events is the contract between modules: the arguments of every River job one module enqueues
// for another (ATLAS.md §5.2, §7.3). Kinds carry a version suffix; an incompatible change adds .v2.
// Producers and consumers import this leaf package, never each other's internals.
package events

import (
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// Queues, one per resource that needs a concurrency limit (ATLAS.md §7.2).
const (
	QueueIngest  = "ingest"
	QueueConvert = "convert"
	QueueEnrich  = "enrich"
	QueueEmbed   = "embed"
	QueueIndex   = "index"
	QueueGraph   = "graph"
	QueueMaint   = "maint"
)

// dedupWindow collapses repeated saves of the same file into one job (ATLAS.md §6).
const dedupWindow = time.Minute

// dedupedOptsIn queues a job once per window for identical arguments.
func dedupedOptsIn(queue string) river.InsertOpts {
	return river.InsertOpts{Queue: queue, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: dedupWindow}}
}

// optsIn queues a job with no deduplication.
func optsIn(queue string) river.InsertOpts {
	return river.InsertOpts{Queue: queue}
}

// SourceRescan walks a source and turns differences into jobs.
type SourceRescan struct {
	TenantID          uuid.UUID `json:"tenant_id"`
	SourceID          uuid.UUID `json:"source_id"`
	AllowMassDeletion bool      `json:"allow_mass_deletion"`
	// Manual marks a scan a person asked for. It is a different job from the same scan found by the watcher or the
	// timer a moment ago, so pressing "Sync" is never swallowed by the one-minute deduplication.
	Manual bool `json:"manual"`
}

func (SourceRescan) Kind() string                 { return "source.rescan.v1" }
func (SourceRescan) InsertOpts() river.InsertOpts { return dedupedOptsIn(QueueMaint) }
func (a SourceRescan) Tenant() uuid.UUID          { return a.TenantID }

// RescanDueSources is the periodic tick that enqueues SourceRescan for every source due. It spans tenants.
type RescanDueSources struct{}

func (RescanDueSources) Kind() string                 { return "source.rescan_due.v1" }
func (RescanDueSources) InsertOpts() river.InsertOpts { return optsIn(QueueMaint) }

// VersionFetch reads one file from its source, guards it and stores the blob.
type VersionFetch struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	SourceID   uuid.UUID `json:"source_id"`
	ExternalID string    `json:"external_id"`
}

func (VersionFetch) Kind() string                 { return "version.fetch.v1" }
func (VersionFetch) InsertOpts() river.InsertOpts { return dedupedOptsIn(QueueIngest) }
func (a VersionFetch) Tenant() uuid.UUID          { return a.TenantID }

// VersionStored says a file's new content is in the blob store. Produced by sources, consumed by documents.
type VersionStored struct {
	TenantID    uuid.UUID `json:"tenant_id"`
	SourceID    uuid.UUID `json:"source_id"`
	ExternalID  string    `json:"external_id"`
	ContentHash string    `json:"content_hash"`
	MIME        string    `json:"mime"`
	Size        int64     `json:"size"`
}

func (VersionStored) Kind() string                 { return "version.stored.v1" }
func (VersionStored) InsertOpts() river.InsertOpts { return optsIn(QueueIngest) }
func (a VersionStored) Tenant() uuid.UUID          { return a.TenantID }

// VersionConvert renders (if needed) and parses one version.
type VersionConvert struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	VersionID uuid.UUID `json:"version_id"`
}

func (VersionConvert) Kind() string                 { return "version.convert.v1" }
func (VersionConvert) InsertOpts() river.InsertOpts { return optsIn(QueueConvert) }
func (a VersionConvert) Tenant() uuid.UUID          { return a.TenantID }

// VersionEnrich turns the parsed document into blocks (later also chunks, metadata and entities).
type VersionEnrich struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	VersionID uuid.UUID `json:"version_id"`
}

func (VersionEnrich) Kind() string                 { return "version.enrich.v1" }
func (VersionEnrich) InsertOpts() river.InsertOpts { return optsIn(QueueEnrich) }
func (a VersionEnrich) Tenant() uuid.UUID          { return a.TenantID }

// VersionChunk cuts the blocks of a version into passages.
type VersionChunk struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	VersionID uuid.UUID `json:"version_id"`
}

func (VersionChunk) Kind() string                 { return "version.chunk.v1" }
func (VersionChunk) InsertOpts() river.InsertOpts { return optsIn(QueueEnrich) }
func (a VersionChunk) Tenant() uuid.UUID          { return a.TenantID }

// VersionEmbed computes the vectors of a version's passages.
type VersionEmbed struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	VersionID uuid.UUID `json:"version_id"`
}

func (VersionEmbed) Kind() string                 { return "version.embed.v1" }
func (VersionEmbed) InsertOpts() river.InsertOpts { return optsIn(QueueEmbed) }
func (a VersionEmbed) Tenant() uuid.UUID          { return a.TenantID }

// VersionIndex makes a version's passages the ones search finds. The old version stops being found in the same transaction.
type VersionIndex struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	VersionID uuid.UUID `json:"version_id"`
}

func (VersionIndex) Kind() string                 { return "version.index.v1" }
func (VersionIndex) InsertOpts() river.InsertOpts { return optsIn(QueueIndex) }
func (a VersionIndex) Tenant() uuid.UUID          { return a.TenantID }

// DocumentDeleted says a file is gone from its source after a complete scan.
type DocumentDeleted struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	SourceID   uuid.UUID `json:"source_id"`
	ExternalID string    `json:"external_id"`
}

func (DocumentDeleted) Kind() string                 { return "document.deleted.v1" }
func (DocumentDeleted) InsertOpts() river.InsertOpts { return optsIn(QueueIngest) }
func (a DocumentDeleted) Tenant() uuid.UUID          { return a.TenantID }

// DocumentMoved says a file changed path with the same content: keep the document and its history.
type DocumentMoved struct {
	TenantID      uuid.UUID `json:"tenant_id"`
	SourceID      uuid.UUID `json:"source_id"`
	OldExternalID string    `json:"old_external_id"`
	NewExternalID string    `json:"new_external_id"`
}

func (DocumentMoved) Kind() string                 { return "document.moved.v1" }
func (DocumentMoved) InsertOpts() river.InsertOpts { return optsIn(QueueIngest) }
func (a DocumentMoved) Tenant() uuid.UUID          { return a.TenantID }

// UserDeleted says an account was erased: whatever else holds the person's data must erase it too (GDPR art. 17).
// Produced by access, consumed by search.
type UserDeleted struct {
	TenantID uuid.UUID `json:"tenant_id"`
	UserID   uuid.UUID `json:"user_id"`
}

func (UserDeleted) Kind() string                 { return "user.deleted.v1" }
func (UserDeleted) InsertOpts() river.InsertOpts { return optsIn(QueueMaint) }
func (a UserDeleted) Tenant() uuid.UUID          { return a.TenantID }

// VersionIndexed says a version's passages are now the ones search finds. Produced by documents, consumed by graph.
type VersionIndexed struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	VersionID  uuid.UUID `json:"version_id"`
	DocumentID uuid.UUID `json:"document_id"`
}

func (VersionIndexed) Kind() string                 { return "version.indexed.v1" }
func (VersionIndexed) InsertOpts() river.InsertOpts { return optsIn(QueueGraph) }
func (a VersionIndexed) Tenant() uuid.UUID          { return a.TenantID }

// DocumentChanged says a person corrected a document's metadata, so the rules about it must be checked again.
type DocumentChanged struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	DocumentID uuid.UUID `json:"document_id"`
}

func (DocumentChanged) Kind() string                 { return "document.changed.v1" }
func (DocumentChanged) InsertOpts() river.InsertOpts { return dedupedOptsIn(QueueGraph) }
func (a DocumentChanged) Tenant() uuid.UUID          { return a.TenantID }

// AlertsEvaluate checks every alert rule over a whole tenant.
type AlertsEvaluate struct {
	TenantID uuid.UUID `json:"tenant_id"`
}

func (AlertsEvaluate) Kind() string                 { return "alerts.evaluate.v1" }
func (AlertsEvaluate) InsertOpts() river.InsertOpts { return dedupedOptsIn(QueueGraph) }
func (a AlertsEvaluate) Tenant() uuid.UUID          { return a.TenantID }

// AlertsNightly is the periodic tick that queues AlertsEvaluate for every tenant. It spans tenants.
type AlertsNightly struct{}

func (AlertsNightly) Kind() string                 { return "alerts.nightly.v1" }
func (AlertsNightly) InsertOpts() river.InsertOpts { return optsIn(QueueGraph) }
