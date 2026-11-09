package app

import (
	"testing"

	"github.com/google/uuid"
)

func TestSingleIDArgRejectsMissingExtraAndMalformedIDs(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"not-a-uuid"}} {
		if _, err := singleIDArg(args, "usage"); err == nil {
			t.Errorf("singleIDArg(%q) accepted bad input", args)
		}
	}
	id := uuid.New()
	if got, err := singleIDArg([]string{id.String()}, "usage"); err != nil || got != id {
		t.Fatalf("singleIDArg() = %s, %v", got, err)
	}
}
