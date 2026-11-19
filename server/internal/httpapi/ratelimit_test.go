package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/errs"
)

type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time { return c.at }

func newTestLimiter() (*limiter, *fakeClock) {
	clock := &fakeClock{at: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}
	return newLimiter(clock.now), clock
}

// quota is a caller key and how many calls it may make in a window.
type quota struct {
	key     string
	allowed int
}

func takeN(l *limiter, q quota, calls int) (refused int) {
	for range calls {
		if _, ok := l.take(q.key, q.allowed); !ok {
			refused++
		}
	}
	return refused
}

func TestRateLimiterRefusesCallsPastTheLimitInsideTheWindow(t *testing.T) {
	l, _ := newTestLimiter()
	if refused := takeN(l, quota{"k", 3}, 5); refused != 2 {
		t.Fatalf("refused %d calls, want 2", refused)
	}
}

func TestRateLimiterReportsTheTimeLeftInTheWindow(t *testing.T) {
	l, clock := newTestLimiter()
	takeN(l, quota{"k", 1}, 1)
	clock.at = clock.at.Add(20 * time.Second)
	retryAfter, ok := l.take("k", 1)
	if ok || retryAfter != 40*time.Second {
		t.Fatalf("take = %v, %v; want refused with 40s left", retryAfter, ok)
	}
}

func TestRateLimiterOpensANewWindowWhenTheOldOneEnds(t *testing.T) {
	l, clock := newTestLimiter()
	takeN(l, quota{"k", 1}, 2)
	clock.at = clock.at.Add(limitWindow)
	if _, ok := l.take("k", 1); !ok {
		t.Fatal("a call after the window must be allowed")
	}
}

func TestRateLimiterCountsEachKeyApart(t *testing.T) {
	l, _ := newTestLimiter()
	takeN(l, quota{"a", 1}, 2)
	if _, ok := l.take("b", 1); !ok {
		t.Fatal("another caller must not share the count")
	}
}

func TestRateLimiterForgetsFinishedWindows(t *testing.T) {
	l, clock := newTestLimiter()
	takeN(l, quota{"old", 1}, 1)
	clock.at = clock.at.Add(2 * limitWindow)
	l.take("new", 1)
	if _, kept := l.windows["old"]; kept {
		t.Fatal("a finished window must be swept")
	}
}

func TestRateLimitClientAddressPrefersTheFirstForwardedValue(t *testing.T) {
	cases := map[string]struct{ forwarded, remote, want string }{
		"proxy sets it":        {"203.0.113.7, 10.0.0.1", "10.0.0.1:5555", "203.0.113.7"},
		"single value":         {" 203.0.113.8 ", "10.0.0.1:5555", "203.0.113.8"},
		"no header":            {"", "198.51.100.4:4321", "198.51.100.4"},
		"remote without port":  {"", "198.51.100.5", "198.51.100.5"},
		"empty forwarded list": {" , x", "198.51.100.6:1", "198.51.100.6"},
	}
	for name, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = c.remote
		if c.forwarded != "" {
			r.Header.Set("X-Forwarded-For", c.forwarded)
		}
		if got := clientAddress(r); got != c.want {
			t.Errorf("%s: clientAddress = %q, want %q", name, got, c.want)
		}
	}
}

func reachedHandler(reached *int) StrictHandlerFunc {
	return func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
		*reached++
		return nil, nil
	}
}

func TestRateLimitMiddlewareStopsAnAddressPastItsLimitBeforeTheHandler(t *testing.T) {
	l, _ := newTestLimiter()
	reached := 0
	call := l.middleware()(reachedHandler(&reached), "Login")
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	var err error
	for range attemptsPerWindow + 1 {
		_, err = call(context.Background(), nil, r, nil)
	}
	if reached != attemptsPerWindow || !isTooManyRequests(err) {
		t.Fatalf("handler ran %d times, last error %v", reached, err)
	}
}

func TestRateLimitMiddlewareCountsEachPersonApart(t *testing.T) {
	l, _ := newTestLimiter()
	reached := 0
	call := l.middleware()(reachedHandler(&reached), "UploadFiles")
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	for range uploadsPerWindow {
		_, _ = call(personContext(uuid.New()), nil, r, nil)
	}
	if _, err := call(personContext(uuid.New()), nil, r, nil); err != nil {
		t.Fatalf("a second person must have their own count: %v", err)
	}
}

func TestRateLimitMiddlewareLeavesUnlimitedOperationsAlone(t *testing.T) {
	l, _ := newTestLimiter()
	reached := 0
	call := l.middleware()(reachedHandler(&reached), "GetVersion")
	for range 100 {
		_, _ = call(context.Background(), nil, httptest.NewRequest(http.MethodGet, "/", nil), nil)
	}
	if reached != 100 {
		t.Fatalf("handler ran %d times, want 100", reached)
	}
}

func personContext(id uuid.UUID) context.Context {
	return context.WithValue(context.Background(), principalKey{}, access.Principal{UserID: id})
}

func TestRateLimitTooManyRequestsAnswers429WithRetryAfterAndDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	handleResponseError(rec, httptest.NewRequest(http.MethodPost, "/", nil), newTooManyRequests(1500*time.Millisecond))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if want := `"detail":"Too many attempts. Try again in 2 seconds."`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body %s lacks %s", rec.Body, want)
	}
}

func TestRateLimitOtherErrorsCarryNoRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	handleResponseError(rec, httptest.NewRequest(http.MethodGet, "/", nil), errors.Join(errs.ErrNotFound))
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("Retry-After belongs to 429 only")
	}
}

func TestRateLimitBothSignInStepsAreCountedPerClientAddress(t *testing.T) {
	for _, operation := range []string{"Login", "VerifyLogin"} {
		l, _ := newTestLimiter()
		reached := 0
		call := l.middleware()(reachedHandler(&reached), operation)
		attacker := requestFrom("203.0.113.7")
		for range attemptsPerWindow {
			_, _ = call(context.Background(), nil, attacker, nil)
		}
		if _, err := call(context.Background(), nil, attacker, nil); !isTooManyRequests(err) {
			t.Errorf("%s: the address past the limit was not refused: %v", operation, err)
		}
		if _, err := call(context.Background(), nil, requestFrom("203.0.113.8"), nil); err != nil {
			t.Errorf("%s: another address must have its own count: %v", operation, err)
		}
	}
}

func requestFrom(address string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-Forwarded-For", address)
	return r
}
