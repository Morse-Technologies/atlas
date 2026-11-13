package documents

import (
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
)

// convertTimeout covers OCR of long scanned documents (ATLAS.md §7.4).
const convertTimeout = 30 * time.Minute

// embedTimeout covers embedding a long document in batches.
const embedTimeout = 10 * time.Minute

// Register adds the documents jobs to a worker process.
func (m *Module) Register(workers *river.Workers) {
	jobs.AddTenantJob(workers, jobs.Job[events.VersionStored]{Handle: m.OnVersionStored})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionConvert]{Handle: m.Convert, Timeout: convertTimeout,
		OnFailed: onStageFailed(m, StepConvert, func(a events.VersionConvert) uuid.UUID { return a.VersionID })})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionEnrich]{Handle: m.Enrich,
		OnFailed: onStageFailed(m, StepEnrich, func(a events.VersionEnrich) uuid.UUID { return a.VersionID })})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionChunk]{Handle: m.Chunk,
		OnFailed: onStageFailed(m, StepChunk, func(a events.VersionChunk) uuid.UUID { return a.VersionID })})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionEmbed]{Handle: m.Embed, Timeout: embedTimeout,
		OnFailed: onStageFailed(m, StepEmbed, func(a events.VersionEmbed) uuid.UUID { return a.VersionID })})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionIndex]{Handle: m.Index,
		OnFailed: onStageFailed(m, StepIndex, func(a events.VersionIndex) uuid.UUID { return a.VersionID })})
	jobs.AddTenantJob(workers, jobs.Job[events.DocumentDeleted]{Handle: m.OnDocumentDeleted})
	jobs.AddTenantJob(workers, jobs.Job[events.DocumentMoved]{Handle: m.OnDocumentMoved})
	jobs.AddTenantJob(workers, jobs.Job[events.SourceRemoved]{Handle: m.OnSourceRemoved})
	jobs.AddTenantJob(workers, jobs.Job[events.SearchLanguageChanged]{Handle: m.OnSearchLanguageChanged})
}
