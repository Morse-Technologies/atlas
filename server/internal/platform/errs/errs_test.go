package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassifySeesThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("convert: %w", NewPermanent(ReasonHostile, errors.New("zip bomb")))
	if Classify(wrapped) != ClassPermanent || PermanentReason(wrapped) != ReasonHostile {
		t.Fatalf("Classify(%v) lost the permanent reason", wrapped)
	}
	if Classify(fmt.Errorf("load: %w", ErrNotFound)) != ClassDomain {
		t.Fatal("wrapped domain errors must stay domain errors")
	}
}

func TestUnknownErrorsDefaultToRetry(t *testing.T) {
	if Classify(errors.New("connection reset")) != ClassUnknown || PermanentReason(errors.New("x")) != ReasonCorrupt {
		t.Fatal("unclassified errors are retried; a permanent error without reason is reported as corrupt")
	}
}

func TestAPublicDetailSurvivesWrappingAndKeepsTheClass(t *testing.T) {
	err := fmt.Errorf("install: %w", WithPublicDetail(fmt.Errorf("signature: %w", ErrInvalid), "the signature does not match"))
	if PublicDetail(err) != "the signature does not match" || Classify(err) != ClassDomain {
		t.Fatalf("detail %q, class %v", PublicDetail(err), Classify(err))
	}
}

func TestAnErrorWithoutAPublicDetailHasNone(t *testing.T) {
	if PublicDetail(fmt.Errorf("db: %w", errors.New("password=secret"))) != "" {
		t.Fatal("internal text must never become a public detail")
	}
}
