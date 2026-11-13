package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestReadersFlagTakesSeveralIds(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	readers, err := parseReaders(first.String() + "," + second.String())
	if err != nil || len(readers) != 2 || readers[0] != first || readers[1] != second {
		t.Fatalf("parseReaders = %v, %v; want both ids in order", readers, err)
	}
}

func TestReadersFlagRejectsABadId(t *testing.T) {
	for _, list := range []string{"", "not-an-id", uuid.NewString() + ",oops"} {
		if _, err := parseReaders(list); err == nil || !strings.Contains(err.Error(), "reader id") {
			t.Errorf("parseReaders(%q) = %v, want an error naming the id", list, err)
		}
	}
}
