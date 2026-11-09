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
