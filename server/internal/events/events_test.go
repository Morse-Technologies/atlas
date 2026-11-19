package events

import (
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

var versionedKind = regexp.MustCompile(`\.v[0-9]+$`)

func allJobs() []river.JobArgs {
	return []river.JobArgs{
		SourceRescan{}, SourceRemoved{}, RescanDueSources{}, VersionFetch{}, VersionStored{}, VersionConvert{},
		VersionEnrich{}, VersionChunk{}, VersionEmbed{}, VersionIndex{}, DocumentDeleted{}, DocumentMoved{},
		UserDeleted{}, VersionIndexed{}, DocumentChanged{}, SearchLanguageChanged{}, AlertsEvaluate{},
		AlertsNightly{}, MaintDue{}, MaintTenant{},
	}
}

func TestEveryKindIsVersionedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, job := range allJobs() {
		kind := job.Kind()
		if !versionedKind.MatchString(kind) {
			t.Errorf("kind %q has no version suffix", kind)
		}
		if seen[kind] {
			t.Errorf("kind %q is used twice", kind)
		}
		seen[kind] = true
	}
}

func TestEveryJobIsQueuedOnAKnownQueue(t *testing.T) {
	known := map[string]bool{QueueIngest: true, QueueConvert: true, QueueEnrich: true, QueueEmbed: true,
		QueueIndex: true, QueueGraph: true, QueueMaint: true}
	for _, job := range allJobs() {
		if queue := job.(interface{ InsertOpts() river.InsertOpts }).InsertOpts().Queue; !known[queue] {
			t.Errorf("%s uses unknown queue %q", job.Kind(), queue)
		}
	}
}

func TestTenantJobsReportTheirTenant(t *testing.T) {
	id := uuid.New()
	tenanted := []interface{ Tenant() uuid.UUID }{
		SourceRescan{TenantID: id}, VersionFetch{TenantID: id}, VersionConvert{TenantID: id}, DocumentDeleted{TenantID: id},
		UserDeleted{TenantID: id}, AlertsEvaluate{TenantID: id}, MaintTenant{TenantID: id},
	}
	for _, job := range tenanted {
		if job.Tenant() != id {
			t.Errorf("%T does not return its TenantID", job)
		}
	}
}

func TestOnlyRepeatedSavesAreDeduplicated(t *testing.T) {
	deduped := SourceRescan{}.InsertOpts().UniqueOpts
	if !deduped.ByArgs || deduped.ByPeriod != dedupWindow {
		t.Errorf("SourceRescan must collapse identical jobs inside %v: %+v", dedupWindow, deduped)
	}
	if (VersionStored{}).InsertOpts().UniqueOpts.ByArgs {
		t.Error("VersionStored must not be deduplicated")
	}
}
