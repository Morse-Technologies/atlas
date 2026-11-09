package db

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/errs"
)

func TestNoErrorStaysNoError(t *testing.T) {
	if err := NotFoundIfNoRows(nil, "thing"); err != nil {
		t.Fatalf("a successful query must not become an error, got %v", err)
	}
}

func TestAMissingRowIsNotFoundAndOtherErrorsKeepTheirCause(t *testing.T) {
	if err := NotFoundIfNoRows(pgx.ErrNoRows, "thing"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	cause := errors.New("connection reset")
	if err := NotFoundIfNoRows(cause, "thing"); !errors.Is(err, cause) || errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v, want the original cause and not ErrNotFound", err)
	}
}
