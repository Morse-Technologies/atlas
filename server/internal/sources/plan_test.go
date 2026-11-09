package sources

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

func file(id string, size int64, hash string) FileState {
	return FileState{ExternalID: id, Size: size, ModifiedAt: t0, Hash: hash}
}

// manyFiles returns n known files a0..a(n-1) with distinct hashes.
func manyFiles(n int) []FileState {
	out := make([]FileState, n)
	for i := range out {
		out[i] = file(fmt.Sprintf("a%d", i), 1, fmt.Sprintf("h%d", i))
	}
	return out
}

type planCase struct {
	name string
	in   ScanInput
	want ScanPlan
}

var planCases = []planCase{
	{
		name: "new and changed files are fetched",
		in:   ScanInput{Known: []FileState{file("a", 1, "ha")}, Seen: []FileState{file("a", 2, ""), file("b", 1, "hb")}, WalkComplete: true},
		want: ScanPlan{Fetch: []string{"a", "b"}},
	},
	{
		name: "unchanged files are left alone",
		in:   ScanInput{Known: []FileState{file("a", 1, "ha")}, Seen: []FileState{file("a", 1, "")}, WalkComplete: true},
		want: ScanPlan{},
	},
	{
		name: "incomplete walk deletes nothing and waits for new files",
		in:   ScanInput{Known: []FileState{file("a", 1, "ha"), file("gone", 1, "hg")}, Seen: []FileState{file("a", 2, ""), file("new", 1, "hn")}},
		want: ScanPlan{Fetch: []string{"a"}},
	},
	{
		name: "same content at a new path is a move",
		in:   ScanInput{Known: []FileState{file("old/a.pdf", 1, "h1")}, Seen: []FileState{file("new/a.pdf", 1, "h1")}, WalkComplete: true},
		want: ScanPlan{Moves: []Move{{From: "old/a.pdf", To: "new/a.pdf", Hash: "h1"}}},
	},
	{
		name: "a gone file without a twin is deleted",
		in:   ScanInput{Known: []FileState{file("a", 1, "ha"), file("b", 1, "hb")}, Seen: []FileState{file("a", 1, "")}, WalkComplete: true},
		want: ScanPlan{Delete: []string{"b"}},
	},
	{
		name: "a gone file never fetched has no hash and is not matched as a move",
		in:   ScanInput{Known: []FileState{file("old.pdf", 1, "")}, Seen: []FileState{file("new.pdf", 1, "")}, WalkComplete: true},
		want: ScanPlan{Fetch: []string{"new.pdf"}, Delete: []string{"old.pdf"}},
	},
	{
		name: "two gone copies and one new path: one move, one delete",
		in:   ScanInput{Known: []FileState{file("a/x.pdf", 1, "h"), file("b/x.pdf", 1, "h")}, Seen: []FileState{file("c/x.pdf", 1, "h")}, WalkComplete: true},
		want: ScanPlan{Moves: []Move{{From: "a/x.pdf", To: "c/x.pdf", Hash: "h"}}, Delete: []string{"b/x.pdf"}},
	},
	{
		name: "losing most of a large source is held",
		in:   ScanInput{Known: manyFiles(20), Seen: manyFiles(5), WalkComplete: true},
		want: ScanPlan{HeldDeletions: 15},
	},
	{
		name: "mass deletion goes through when allowed",
		in:   ScanInput{Known: manyFiles(12), Seen: manyFiles(1), WalkComplete: true, AllowMassDeletion: true},
		want: ScanPlan{Delete: externalIDs(manyFiles(12)[1:])},
	},
}

func TestPlanScan(t *testing.T) {
	for _, tc := range planCases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanScan(tc.in)
			if !samePlan(got, tc.want) {
				t.Fatalf("PlanScan() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func samePlan(a, b ScanPlan) bool {
	return slices.Equal(sorted(a.Fetch), sorted(b.Fetch)) && slices.Equal(a.Moves, b.Moves) &&
		slices.Equal(sorted(a.Delete), sorted(b.Delete)) && a.HeldDeletions == b.HeldDeletions
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
