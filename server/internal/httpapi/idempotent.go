package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/morse/atlas/server/internal/platform/idempotency"
)

const (
	idempotencyHeader = "Idempotency-Key"
	replayedHeader    = "Idempotent-Replayed"
	// maxIdempotentBody bounds what is read to fingerprint a request, and what is kept as the answer.
	maxIdempotentBody = 1 << 20
	askPath           = "/api/v1/ask"
)

// IdempotencyStore is what the HTTP layer needs from platform/idempotency. *idempotency.Store implements it.
type IdempotencyStore interface {
	Begin(ctx context.Context, r idempotency.Request) (*idempotency.Outcome, error)
	Finish(ctx context.Context, r idempotency.Request, out idempotency.Outcome) error
	Release(ctx context.Context, r idempotency.Request) error
}

// idempotent replays the first answer to a write that is retried with the same Idempotency-Key.
// It does nothing for reads, for requests without a key, for uploads (the body is a file stream) and for /ask
// (the answer is a stream, and every question is its own audit record).
func (s *Server) idempotent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Idempotency == nil || !wantsIdempotency(r) {
			next.ServeHTTP(w, r)
			return
		}
		p, err := s.principalOf(r.Context(), r)
		if err != nil { // not signed in: the normal path answers 401
			next.ServeHTTP(w, r)
			return
		}
		s.serveOnce(w, r, once{next: next, req: idempotency.Request{Tenant: p.TenantID, User: p.UserID, Key: r.Header.Get(idempotencyHeader)}})
	})
}

func wantsIdempotency(r *http.Request) bool {
	return r.Header.Get(idempotencyHeader) != "" && !isSafeMethod(r.Method) && r.URL.Path != askPath &&
		!strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/")
}

// once is the handler to run at most once for a key.
type once struct {
	next http.Handler
	req  idempotency.Request
}

func (s *Server) serveOnce(w http.ResponseWriter, r *http.Request, o once) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIdempotentBody))
	if err != nil {
		writeProblem(w, Problem{Type: "about:blank", Title: http.StatusText(http.StatusRequestEntityTooLarge), Status: http.StatusRequestEntityTooLarge})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	o.req.Hash = fingerprint(r, body)
	replay, err := s.Idempotency.Begin(r.Context(), o.req)
	if err != nil {
		writeProblem(w, ProblemFor(err))
		return
	}
	if replay != nil {
		replayTo(w, replay)
		return
	}
	rec := &recorder{ResponseWriter: w, status: http.StatusOK}
	o.next.ServeHTTP(rec, r)
	s.settle(r.Context(), o.req, rec)
}

// settle keeps the answer of a request that completed, and frees the key after a server failure so a retry runs again.
func (s *Server) settle(ctx context.Context, req idempotency.Request, rec *recorder) {
	ctx = context.WithoutCancel(ctx) // the client may be gone; the key must still be settled
	if rec.status >= http.StatusInternalServerError || rec.overflow {
		_ = s.Idempotency.Release(ctx, req)
		return
	}
	out := idempotency.Outcome{Status: rec.status, ContentType: rec.Header().Get("Content-Type"), Body: rec.body.Bytes()}
	_ = s.Idempotency.Finish(ctx, req, out)
}

func fingerprint(r *http.Request, body []byte) string {
	h := sha256.New()
	h.Write([]byte(r.Method + "\n" + r.URL.RequestURI() + "\n"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func replayTo(w http.ResponseWriter, out *idempotency.Outcome) {
	if out.ContentType != "" {
		w.Header().Set("Content-Type", out.ContentType)
	}
	w.Header().Set(replayedHeader, "true")
	w.WriteHeader(out.Status)
	_, _ = w.Write(out.Body)
}

// recorder passes the answer through and keeps a copy, up to maxIdempotentBody.
type recorder struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	overflow bool
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.body.Len()+len(p) > maxIdempotentBody {
		r.overflow = true
	} else {
		r.body.Write(p)
	}
	return r.ResponseWriter.Write(p)
}
