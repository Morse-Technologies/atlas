package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/log"
)

const problemContentType = "application/problem+json"

// ErrNotImplemented answers operations of the contract that later parts of the plan build.
var ErrNotImplemented = errors.New("not implemented yet")

// errNothingToChange answers a PATCH that names no field.
var errNothingToChange = errors.Join(errs.ErrInvalid, errors.New("no field to change"))

// ProblemFor translates an error into RFC 9457 Problem Details. It is the only place that does so;
// internal error text is never sent, only the status title.
func ProblemFor(err error) Problem {
	status := statusFor(err)
	return Problem{Type: "about:blank", Title: http.StatusText(status), Status: status}
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrNotImplemented):
		return http.StatusNotImplemented
	case errors.Is(err, errs.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, errs.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, errs.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, errs.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, errs.ErrInvalid):
		return http.StatusUnprocessableEntity
	case errs.Classify(err) == errs.ClassTransient:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// writeProblem writes a Problem Details response.
func writeProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// handleResponseError is used for errors returned by handlers.
func handleResponseError(w http.ResponseWriter, r *http.Request, err error) {
	p := ProblemFor(err)
	forgetRejectedSession(w, r, err)
	if p.Status >= http.StatusInternalServerError && p.Status != http.StatusNotImplemented {
		slog.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, log.Err(err))
	}
	writeProblem(w, p)
}

// handleRequestError is used when the request does not match the contract (bad JSON, bad parameter).
// The detail only describes the caller's own input, so it is safe to return.
func handleRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	detail := err.Error()
	writeProblem(w, Problem{Type: "about:blank", Title: http.StatusText(http.StatusBadRequest), Status: http.StatusBadRequest, Detail: &detail})
}

// forgetRejectedSession clears the session cookie when the server refuses it. The cookie is HttpOnly, so the
// browser's code cannot remove it, and a stale one would bounce the person between the login page (which sends
// signed-in people home) and the screens (which send them back because the API answers 401).
func forgetRejectedSession(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, errs.ErrUnauthenticated) {
		return
	}
	if _, sent := r.Cookie(sessionCookie); sent == nil {
		w.Header().Add("Set-Cookie", clearedCookieValue())
	}
}
