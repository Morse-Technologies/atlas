package documents

import (
	"time"

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
	jobs.AddTenantJob(workers, jobs.Job[events.VersionConvert]{Handle: m.Convert, Timeout: convertTimeout})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionEnrich]{Handle: m.Enrich})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionChunk]{Handle: m.Chunk})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionEmbed]{Handle: m.Embed, Timeout: embedTimeout})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionIndex]{Handle: m.Index})
	jobs.AddTenantJob(workers, jobs.Job[events.DocumentDeleted]{Handle: m.OnDocumentDeleted})
	jobs.AddTenantJob(workers, jobs.Job[events.DocumentMoved]{Handle: m.OnDocumentMoved})
}
