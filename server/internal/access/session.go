package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	// SessionTTL is one work shift. The cookie max-age uses the same value.
	SessionTTL = 12 * time.Hour
	// idleLimit ends the session of a person who only asks questions, for shared tablets (ATLAS.md §13.2).
	idleLimit = 30 * time.Minute
	// touchEvery limits how often a busy session writes its last-seen time.
	touchEvery = time.Minute
)

// openSession ends the challenge and starts a session for user.
func (s *Service) openSession(ctx context.Context, q *accessdb.Queries, user accessdb.AccessUser) (SessionTicket, error) {
	secret, hash, err := newToken()
	if err != nil {
		return SessionTicket{}, err
	}
	now := s.Now()
	expires := now.Add(SessionTTL)
	if err := q.DeleteExpiredSessions(ctx, now); err != nil { // no background job yet; sessions are few
		return SessionTicket{}, fmt.Errorf("purge sessions: %w", err)
	}
	err = q.CreateSession(ctx, accessdb.CreateSessionParams{TenantID: user.TenantID, UserID: user.ID,
		TokenHash: hash, ExpiresAt: expires})
	if err != nil {
		return SessionTicket{}, fmt.Errorf("create session: %w", err)
	}
	if err := q.RecordLogin(ctx, accessdb.RecordLoginParams{ID: user.ID, Now: &now}); err != nil {
		return SessionTicket{}, fmt.Errorf("record login: %w", err)
	}
	return SessionTicket{Token: secret, ExpiresAt: expires}, nil
}

// Authenticate turns a session cookie into the principal it acts as. Every failure is ErrUnauthenticated.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	now := s.Now()
	session, err := accessdb.New(s.DB.Pool).GetSession(ctx, accessdb.GetSessionParams{TokenHash: hashToken(token), Now: now})
	if err != nil {
		return Principal{}, unauthenticated(err)
	}
	var principal Principal
	err = s.DB.InTenantTx(ctx, session.TenantID, func(tx pgx.Tx) error {
		var txErr error
		principal, txErr = s.principalFor(ctx, tx, session)
		return txErr
	})
	return principal, err
}

// unauthenticated hides "no such session" behind the one answer a client may see, but keeps real faults.
func unauthenticated(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnauthenticated
	}
	return fmt.Errorf("get session: %w", err)
}

func (s *Service) principalFor(ctx context.Context, tx pgx.Tx, session accessdb.AccessSession) (Principal, error) {
	q := accessdb.New(tx)
	user, err := q.GetUser(ctx, session.UserID)
	if err != nil {
		return Principal{}, unauthenticated(err)
	}
	if !user.IsActive {
		return Principal{}, errs.ErrUnauthenticated
	}
	roles, err := s.rolesOf(ctx, tx, []uuid.UUID{user.ID})
	if err != nil {
		return Principal{}, err
	}
	p := Principal{SessionID: session.ID, UserID: user.ID, TenantID: session.TenantID, Roles: roles[user.ID]}
	if s.idleTooLong(p, session) {
		return Principal{}, errs.ErrUnauthenticated
	}
	return p, s.touch(ctx, q, session)
}

// idleTooLong applies only to people whose single role is viewer.
func (s *Service) idleTooLong(p Principal, session accessdb.AccessSession) bool {
	askOnly := len(p.Roles) == 1 && p.Has(RoleViewer)
	return askOnly && s.Now().Sub(session.LastSeenAt) > idleLimit
}

func (s *Service) touch(ctx context.Context, q *accessdb.Queries, session accessdb.AccessSession) error {
	now := s.Now()
	err := q.TouchSession(ctx, accessdb.TouchSessionParams{ID: session.ID, Now: now, StaleBefore: now.Add(-touchEvery)})
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// Logout ends the principal's session.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	return s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := accessdb.New(tx).DeleteSession(ctx, p.SessionID); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Actor: p.UserID, Action: "user.logout", Subject: userSubject(p.UserID)})
	})
}
