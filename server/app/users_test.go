package app

import (
	"reflect"
	"testing"

	"github.com/morse/atlas/server/internal/access"
)

func TestRolesAreSplitOnCommasAndTrimmed(t *testing.T) {
	got := parseRoles(" viewer, curator ,,admin")
	want := []access.Role{"viewer", "curator", "admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
}

func TestOneArgumentIsRequired(t *testing.T) {
	if _, err := singleArg(nil, "usage"); err == nil {
		t.Fatal("no argument must be an error")
	}
	if v, err := singleArg([]string{"a@b.test"}, "usage"); err != nil || v != "a@b.test" {
		t.Fatalf("got %q, %v", v, err)
	}
}
