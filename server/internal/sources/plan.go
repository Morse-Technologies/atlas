package sources

// Rules that keep a rescan from emptying the index (ATLAS.md §6.1). Pure functions, tested without a database.

const (
	// massDeletionRatio: losing more than this share of a source in one scan looks like an unmounted share.
	massDeletionRatio = 0.20
	// massDeletionMinFiles keeps small folders usable: deleting 1 of 3 files is not suspicious.
	massDeletionMinFiles = 10
)

// ScanInput is what the planner needs: the files known before, the files seen now, and how the walk went.
type ScanInput struct {
	Known             []FileState // present files from the last scans, with the hash of their fetched content
	Seen              []FileState // files listed now; Hash is set only for files not in Known
	WalkComplete      bool
	AllowMassDeletion bool
}

// Move is a file that changed path but kept its content.
type Move struct {
	From, To string
	Hash     string
}

// ScanPlan is what the rescan must do.
type ScanPlan struct {
	Fetch         []string // new or changed files
	Moves         []Move
	Delete        []string
	HeldDeletions int // deletions not applied because they looked like a share outage
}

// PlanScan decides fetches, moves and deletions. An incomplete walk only refetches changed known files:
// new files wait for a complete walk, because one of them may be the new path of a file not reached yet.
func PlanScan(in ScanInput) ScanPlan {
	known := byExternalID(in.Known)
	changed, added := splitSeen(in.Seen, known)
	if !in.WalkComplete {
		return ScanPlan{Fetch: externalIDs(changed)}
	}
	gone := goneFiles(in.Known, in.Seen)
	moves, gone, added := matchMoves(gone, added)
	plan := ScanPlan{Fetch: append(externalIDs(changed), externalIDs(added)...), Moves: moves}
	plan.Delete, plan.HeldDeletions = applyMassDeletionHold(externalIDs(gone), len(in.Known), in.AllowMassDeletion)
	return plan
}

func byExternalID(files []FileState) map[string]FileState {
	out := make(map[string]FileState, len(files))
	for _, f := range files {
		out[f.ExternalID] = f
	}
	return out
}

// splitSeen separates known files whose size or mtime changed from files never seen before.
func splitSeen(seen []FileState, known map[string]FileState) (changed, added []FileState) {
	for _, f := range seen {
		before, ok := known[f.ExternalID]
		switch {
		case !ok:
			added = append(added, f)
		case before.Size != f.Size || !before.ModifiedAt.Equal(f.ModifiedAt):
			changed = append(changed, f)
		}
	}
	return changed, added
}

func goneFiles(known, seen []FileState) []FileState {
	seenIDs := byExternalID(seen)
	var gone []FileState
	for _, f := range known {
		if _, ok := seenIDs[f.ExternalID]; !ok {
			gone = append(gone, f)
		}
	}
	return gone
}

// matchMoves pairs a gone file with a new file of the same content hash. Each new file matches once.
func matchMoves(gone, added []FileState) ([]Move, []FileState, []FileState) {
	addedByHash := map[string][]FileState{}
	for _, f := range added {
		addedByHash[f.Hash] = append(addedByHash[f.Hash], f)
	}
	var moves []Move
	var stillGone []FileState
	for _, g := range gone {
		candidates := addedByHash[g.Hash]
		if g.Hash == "" || len(candidates) == 0 {
			stillGone = append(stillGone, g)
			continue
		}
		moves = append(moves, Move{From: g.ExternalID, To: candidates[0].ExternalID, Hash: g.Hash})
		addedByHash[g.Hash] = candidates[1:]
	}
	return moves, stillGone, remainingAdded(added, moves)
}

func remainingAdded(added []FileState, moves []Move) []FileState {
	movedTo := map[string]bool{}
	for _, m := range moves {
		movedTo[m.To] = true
	}
	var out []FileState
	for _, f := range added {
		if !movedTo[f.ExternalID] {
			out = append(out, f)
		}
	}
	return out
}

// applyMassDeletionHold returns the deletions to apply and how many were held back.
func applyMassDeletionHold(deletes []string, knownCount int, allow bool) ([]string, int) {
	if allow || !looksLikeOutage(len(deletes), knownCount) {
		return deletes, 0
	}
	return nil, len(deletes)
}

func looksLikeOutage(deleteCount, knownCount int) bool {
	return deleteCount >= massDeletionMinFiles && float64(deleteCount) > massDeletionRatio*float64(knownCount)
}

func externalIDs(files []FileState) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.ExternalID
	}
	return out
}
