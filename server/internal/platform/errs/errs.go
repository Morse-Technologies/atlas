// Package errs defines the three error classes (ATLAS.md §16.2): domain errors, transient errors (retry)
// and permanent errors (quarantine). Adapters translate library errors into these; callers use Classify.
package errs

import "errors"

// Domain errors. HTTP maps them to 401, 404, 403, 409 and 422.
var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrNotFound        = errors.New("not found")
	ErrForbidden       = errors.New("forbidden")
	ErrConflict        = errors.New("conflict")
	ErrInvalid         = errors.New("invalid")
)

// Reason says why a file is quarantined. Values match the quarantine.reason column and the frontend type.
type Reason string

// Quarantine reasons.
const (
	ReasonEncrypted   Reason = "encrypted"
	ReasonCorrupt     Reason = "corrupt"
	ReasonUnsupported Reason = "unsupported"
	ReasonTooLarge    Reason = "too_large"
	ReasonHostile     Reason = "hostile"
	// ReasonRetriesExhausted is a file whose step kept failing for a cause that might pass (a model that was down)
	// until River gave up.
	ReasonRetriesExhausted Reason = "retries_exhausted"
)

// Transient marks a failure worth retrying: timeouts, 429, 503, a service that is down.
type Transient struct{ Err error }

func (e *Transient) Error() string { return "transient: " + e.Err.Error() }
func (e *Transient) Unwrap() error { return e.Err }

// Permanent marks a failure that retrying cannot fix. The file goes to quarantine with Reason.
type Permanent struct {
	Reason Reason
	Err    error
}

func (e *Permanent) Error() string { return "permanent (" + string(e.Reason) + "): " + e.Err.Error() }
func (e *Permanent) Unwrap() error { return e.Err }

// NewTransient wraps err as retryable.
func NewTransient(err error) error { return &Transient{Err: err} }

// NewPermanent wraps err as not retryable, with the quarantine reason.
func NewPermanent(reason Reason, err error) error { return &Permanent{Reason: reason, Err: err} }

// Class is the handling an error needs.
type Class int

// Error classes. Unknown errors are retried, like transient ones, because most are I/O.
const (
	ClassUnknown Class = iota
	ClassDomain
	ClassTransient
	ClassPermanent
)

// Classify returns the class of err.
func Classify(err error) Class {
	var transient *Transient
	var permanent *Permanent
	switch {
	case errors.As(err, &permanent):
		return ClassPermanent
	case errors.As(err, &transient):
		return ClassTransient
	case isDomain(err):
		return ClassDomain
	default:
		return ClassUnknown
	}
}

func isDomain(err error) bool {
	return errors.Is(err, ErrUnauthenticated) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid)
}

// PermanentReason returns the quarantine reason of a permanent error, or ReasonCorrupt if it has none.
func PermanentReason(err error) Reason {
	var permanent *Permanent
	if errors.As(err, &permanent) {
		return permanent.Reason
	}
	return ReasonCorrupt
}

// Public is an error with a reason that is safe to show to the person who caused it: the module writes it, from a
// fixed list, and never from internal text. The HTTP layer sends it as the detail of the problem.
type Public struct {
	Err    error
	Detail string
}

func (e *Public) Error() string { return e.Err.Error() }
func (e *Public) Unwrap() error { return e.Err }

// WithPublicDetail attaches a reason that may be shown to the caller. The error keeps its class.
func WithPublicDetail(err error, detail string) error { return &Public{Err: err, Detail: detail} }

// PublicDetail returns the reason attached by WithPublicDetail, or "" if there is none.
func PublicDetail(err error) string {
	var public *Public
	if errors.As(err, &public) {
		return public.Detail
	}
	return ""
}
