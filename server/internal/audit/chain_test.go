package audit

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var base = time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)

// chainOf links n events the way Record does.
func chainOf(t *testing.T, n int) []Event {
	t.Helper()
	prev, events := GenesisHash, make([]Event, n)
	for i := range events {
		actor := uuid.New()
		e := Event{ID: int64(i + 1), At: base.Add(time.Duration(i) * time.Second), ActorID: &actor,
			Action: "user.login", Subject: "user:x", Detail: "d", PrevHash: prev}
		hash, err := hashOf(prev, e)
		if err != nil {
			t.Fatal(err)
		}
		e.Hash = hash
		events[i], prev = e, hash
	}
	return events
}

func firstBroken(events []Event) int64 {
	prev := GenesisHash
	for _, e := range events {
		if !linkIsValid(prev, e) {
			return e.ID
		}
		prev = e.Hash
	}
	return 0
}

func TestUntouchedChainIsValid(t *testing.T) {
	if id := firstBroken(chainOf(t, 5)); id != 0 {
		t.Fatalf("chain broken at %d", id)
	}
}

func TestEditingAnyFieldBreaksTheChainAtThatEvent(t *testing.T) {
	edits := map[string]func(*Event){
		"action":  func(e *Event) { e.Action = "user.logout" },
		"subject": func(e *Event) { e.Subject = "user:y" },
		"detail":  func(e *Event) { e.Detail = "other" },
		"at":      func(e *Event) { e.At = e.At.Add(time.Second) },
		"actor":   func(e *Event) { other := uuid.New(); e.ActorID = &other },
	}
	for field, edit := range edits {
		events := chainOf(t, 5)
		edit(&events[2])
		if id := firstBroken(events); id != 3 {
			t.Errorf("editing %s: broken at %d, want 3", field, id)
		}
	}
}

func TestRemovingAnEventBreaksTheChain(t *testing.T) {
	events := chainOf(t, 5)
	events = append(events[:2], events[3:]...)
	if id := firstBroken(events); id != 4 {
		t.Fatalf("broken at %d, want 4 (the event after the gap)", id)
	}
}

func TestHashIsStableAcrossTimeZones(t *testing.T) {
	e := Event{ID: 1, At: base, Action: "a"}
	local := e
	local.At = base.In(time.FixedZone("CET", 3600))
	a, _ := hashOf(GenesisHash, e)
	b, _ := hashOf(GenesisHash, local)
	if a != b {
		t.Fatal("the same instant must hash the same in any zone")
	}
}
