package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/morse/atlas/server/internal/platform/metrics"
)

const (
	requestsTotal  = "atlas_http_requests_total"
	requestSeconds = "atlas_http_request_seconds"
	unknownOp      = "unknown"
)

type operationKey struct{}

// operationName is where the strict middleware tells the measuring handler which operation served the call,
// because the routing happens inside and the outer handler only sees the path.
type operationName struct{ id string }

// measured counts every call by operation and status, and times it. The status is what the handler wrote.
func measured(reg *metrics.Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op := &operationName{id: unknownOp}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), operationKey{}, op)))
		reg.Inc(requestsTotal, metrics.Labels{"operation": op.id, "status": strconv.Itoa(rec.status)})
		reg.Observe(requestSeconds, metrics.Labels{"operation": op.id}, time.Since(start))
	})
}

// recordOperation is the innermost-to-outermost strict middleware that names the operation for measured.
func recordOperation(next StrictHandlerFunc, operationID string) StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		if op, ok := ctx.Value(operationKey{}).(*operationName); ok {
			op.id = operationID
		}
		return next(ctx, w, r, request)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush keeps the answer stream of /ask flowing through the recorder.
func (s *statusRecorder) Flush() {
	http.NewResponseController(s.ResponseWriter).Flush() //nolint:errcheck // a writer that cannot flush just buffers
}

// Unwrap lets http.ResponseController reach the real writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
