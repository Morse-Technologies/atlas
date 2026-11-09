package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	sessionCookie = "atlas_session"
	// roleAnySession is the x-atlas-role of operations any signed-in person may call.
	roleAnySession = "session"
	uploadTimeout  = 15 * time.Minute
)

type principalKey struct{}

// principalFrom returns who the request acts as. Handlers behind authenticate always have one.
func principalFrom(ctx context.Context) access.Principal {
	p, _ := ctx.Value(principalKey{}).(access.Principal)
	return p
}

// requirement is what an operation needs, read from the contract: security [] is public, and
// x-atlas-role is the minimum role.
type requirement struct {
	public  bool
	role    access.Role
	feature string // Pro feature the operation needs (x-atlas-feature); empty: none
}

// allows applies the role rule. Everyone signed in may ask (viewer). An administrator may do anything except
// read the audit log, which only auditors may (separation of duties).
func (r requirement) allows(p access.Principal) bool {
	switch {
	case r.role == "" || r.role == access.RoleViewer:
		return true
	case p.Has(r.role):
		return true
	default:
		return p.Has(access.RoleAdmin) && r.role != access.RoleAuditor
	}
}

// requirementsFromSpec reads every operation's requirement from the embedded OpenAPI document, so the
// contract stays the only place that says who may call what.
func requirementsFromSpec() (map[string]requirement, error) {
	spec, err := GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	reqs := map[string]requirement{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			role, hasRole := op.Extensions["x-atlas-role"].(string)
			public := op.Security != nil && len(*op.Security) == 0
			if !public && !hasRole {
				return nil, fmt.Errorf("%s %s has no security: [] and no x-atlas-role", method, path)
			}
			feature, _ := op.Extensions["x-atlas-feature"].(string)
			reqs[op.OperationID] = requirement{public: public, role: roleFor(role), feature: feature}
		}
	}
	return reqs, nil
}

func roleFor(name string) access.Role {
	if name == roleAnySession {
		return ""
	}
	return access.Role(name)
}

// authenticate rejects calls without a valid session or without the role the contract asks for.
// An operation missing from the contract is refused: the default is deny.
func (s *Server) authenticate(reqs map[string]requirement) StrictMiddlewareFunc {
	return func(next StrictHandlerFunc, operationID string) StrictHandlerFunc {
		req, known := reqs[operationID]
		if known && req.public {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			extendForUpload(w, operationID)
			p, err := s.principalOf(ctx, r)
			if err != nil {
				return nil, err
			}
			if !known || !req.allows(p) || !s.hasFeature(req.feature) {
				return nil, errs.ErrForbidden
			}
			ctx = context.WithValue(context.WithValue(ctx, principalKey{}, p), requestKey{}, r)
			return next(ctx, w, r, request)
		}
	}
}

// extendForUpload gives an upload the time a slow link needs; the server-wide read timeout is for small requests.
func extendForUpload(w http.ResponseWriter, operationID string) {
	if operationID == "uploadFiles" {
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(uploadTimeout))
	}
}

func (s *Server) principalOf(ctx context.Context, r *http.Request) (access.Principal, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || s.Accounts == nil {
		return access.Principal{}, errs.ErrUnauthenticated
	}
	return s.Accounts.Authenticate(ctx, cookie.Value)
}

// sameOrigin is the CSRF check for cookie sessions (with SameSite=Lax): a write that names an Origin must
// name this host. Requests without Origin (curl, server to server) carry no browser cookie risk.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeMethod(r.Method) && !originMatches(r) {
			writeProblem(w, ProblemFor(errs.ErrForbidden))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func originMatches(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

// sessionCookieValue is the Set-Cookie header that starts a session. Secure is always on: Atlas is served
// over TLS by Caddy, and browsers accept Secure cookies on http://localhost too.
func sessionCookieValue(ticket access.SessionTicket) string {
	c := http.Cookie{Name: sessionCookie, Value: ticket.Token, Path: "/", Expires: ticket.ExpiresAt,
		MaxAge: int(access.SessionTTL / time.Second), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	return c.String()
}

// clearedCookieValue is the Set-Cookie header that ends the session in the browser.
func clearedCookieValue() string {
	c := http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	return c.String()
}

// hasFeature reports whether the license switches on feature. An operation with no feature is always allowed;
// without a license source nothing Pro is.
func (s *Server) hasFeature(feature string) bool {
	if feature == "" {
		return true
	}
	return s.Licenses != nil && s.Licenses.Current().Has(feature)
}
