package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	limitWindow       = time.Minute
	attemptsPerWindow = 10
	asksPerWindow     = 20
	uploadsPerWindow  = 10
)

// limit allows `allowed` calls of one operation per limitWindow, counted per caller address or per signed-in person.
type limit struct {
	allowed  int
	byPerson bool
}

// limits are the operations that can be hammered: password and code guessing, the first setup, the model and uploads.
var limits = map[string]limit{
	"Login":       {allowed: attemptsPerWindow},
	"VerifyLogin": {allowed: attemptsPerWindow},
	"Setup":       {allowed: attemptsPerWindow},
	"Ask":         {allowed: asksPerWindow, byPerson: true},
	"UploadFiles": {allowed: uploadsPerWindow, byPerson: true},
}

// tooManyRequests is the error of a caller past its limit; it maps to 429 with a Retry-After header.
type tooManyRequests struct{ RetryAfter time.Duration }

func (e *tooManyRequests) Error() string { return "too many requests" }

func newTooManyRequests(retryAfter time.Duration) error {
	seconds := retryAfterSeconds(retryAfter)
	detail := fmt.Sprintf("Too many attempts. Try again in %d seconds.", seconds)
	return errs.WithPublicDetail(&tooManyRequests{RetryAfter: retryAfter}, detail)
}

// retryAfterSeconds rounds up, so a client that waits that long is never refused again by the same window.
func retryAfterSeconds(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

type window struct {
	start time.Time
	count int
}

// limiter is a fixed-window counter in memory. shortcut: state is per process and lost on restart, and the window
// edge allows a burst of twice the limit; move to a shared store if Atlas ever runs several api replicas.
type limiter struct {
	now       func() time.Time
	mu        sync.Mutex
	windows   map[string]window
	lastSweep time.Time
}

func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, windows: map[string]window{}}
}

// take counts one call under key. When the limit is spent it returns how long until the window ends.
func (l *limiter) take(key string, allowed int) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	w := l.windows[key]
	if now.Sub(w.start) >= limitWindow {
		w = window{start: now}
	}
	if w.count >= allowed {
		return w.start.Add(limitWindow).Sub(now), false
	}
	w.count++
	l.windows[key] = w
	return 0, true
}

// sweep forgets finished windows, at most once per window, so the map does not grow without bound.
func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < limitWindow {
		return
	}
	l.lastSweep = now
	for key, w := range l.windows {
		if now.Sub(w.start) >= limitWindow {
			delete(l.windows, key)
		}
	}
}

// middleware refuses a call past its operation's limit before the handler runs, so a refused attempt reaches
// nothing downstream and leaves no audit event. It runs after authenticate, which provides the person.
func (l *limiter) middleware() StrictMiddlewareFunc {
	return func(next StrictHandlerFunc, operationID string) StrictHandlerFunc {
		lim, limited := limits[operationID]
		if !limited {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			key := operationID + "|" + callerKey(ctx, r, lim)
			if retryAfter, ok := l.take(key, lim.allowed); !ok {
				return nil, newTooManyRequests(retryAfter)
			}
			return next(ctx, w, r, request)
		}
	}
}

func callerKey(ctx context.Context, r *http.Request, lim limit) string {
	if lim.byPerson {
		return principalFrom(ctx).UserID.String()
	}
	return clientAddress(r)
}

// clientAddress is the first X-Forwarded-For value, which the Caddy in front of the API sets, or the peer address.
func clientAddress(r *http.Request) string {
	if forwarded, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ","); strings.TrimSpace(forwarded) != "" {
		return strings.TrimSpace(forwarded)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// setRetryAfter adds the Retry-After header to a 429 answer.
func setRetryAfter(w http.ResponseWriter, err error) {
	var tooMany *tooManyRequests
	if errors.As(err, &tooMany) {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(tooMany.RetryAfter)))
	}
}

func isTooManyRequests(err error) bool {
	var tooMany *tooManyRequests
	return errors.As(err, &tooMany)
}
