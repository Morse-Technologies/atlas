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
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	maxFailedLogins      = 5
	lockDuration         = 15 * time.Minute
	challengeTTL         = 5 * time.Minute
	maxChallengeAttempts = 5
)

// errBadCredentials is the only answer to a failed sign-in: it never says whether the email exists,
// the password was wrong, the code was wrong or the account is locked.
var errBadCredentials = fmt.Errorf("invalid credentials: %w", errs.ErrUnauthenticated)

// dummyHash is verified when the email is unknown, so unknown and known emails take the same time.
var dummyHash, _ = HashPassword("not-a-real-password")

type credentials struct{ email, password string }

// Challenge is the first step of sign-in: the secret to send back with the second factor.
type Challenge struct {
	ID        string
	NeedsCode bool
}

// Login checks email and password. On success it returns a challenge to complete with Verify.
func (s *Service) Login(ctx context.Context, email, password string) (Challenge, error) {
	tenant, err := s.tenantID(ctx)
	if err != nil {
		return Challenge{}, errBadCredentials
	}
	var out Challenge
	var passed bool
	err = s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		var txErr error
		out, passed, txErr = s.checkPassword(ctx, tx, credentials{email, password})
		return txErr
	})
	if err != nil {
		return Challenge{}, err
	}
	if !passed { // the failed-attempt counter was committed above
		return Challenge{}, errBadCredentials
	}
	return out, nil
}

// checkPassword verifies the password and, if right, stores a challenge. A wrong password is not an error
// here: returning one would roll back the failed-attempt counter.
func (s *Service) checkPassword(ctx context.Context, tx pgx.Tx, c credentials) (Challenge, bool, error) {
	q := accessdb.New(tx)
	user, err := q.GetUserByEmail(ctx, c.email)
	if errors.Is(err, pgx.ErrNoRows) {
		VerifyPassword(dummyHash, c.password)
		return Challenge{}, false, nil
	}
	if err != nil {
		return Challenge{}, false, fmt.Errorf("get user: %w", err)
	}
	if !usable(user, s.Now()) || !VerifyPassword(user.PasswordHash, c.password) {
		return Challenge{}, false, s.recordFailure(ctx, tx, user.ID)
	}
	id, err := s.storeChallenge(ctx, q, user)
	return Challenge{ID: id, NeedsCode: user.TotpSecretEnc != nil}, err == nil, err
}

// usable is true when the account may start a session at now: it is active and not locked.
func usable(user accessdb.AccessUser, now time.Time) bool {
	locked := user.LockedUntil != nil && user.LockedUntil.After(now)
	return user.IsActive && !locked
}

// recordFailure counts a failed attempt (locking the account at the limit) and leaves a trace in the audit log.
func (s *Service) recordFailure(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	err := accessdb.New(tx).RecordFailedLogin(ctx, accessdb.RecordFailedLoginParams{
		ID: userID, MaxFailures: maxFailedLogins, LockedUntil: s.Now().Add(lockDuration)})
	if err != nil {
		return fmt.Errorf("record failed login: %w", err)
	}
	return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Actor: userID, Action: "user.login_failed", Subject: userSubject(userID)})
}

func (s *Service) storeChallenge(ctx context.Context, q *accessdb.Queries, user accessdb.AccessUser) (string, error) {
	secret, hash, err := newToken()
	if err != nil {
		return "", err
	}
	err = q.CreateChallenge(ctx, accessdb.CreateChallengeParams{TenantID: user.TenantID, UserID: user.ID,
		TokenHash: hash, ExpiresAt: s.Now().Add(challengeTTL)})
	if err != nil {
		return "", fmt.Errorf("create challenge: %w", err)
	}
	return secret, nil
}

// attempt is one try at the second factor: the challenge, the code, and (once loaded) the account.
type attempt struct {
	tx        pgx.Tx
	challenge accessdb.AccessLoginChallenge
	user      accessdb.AccessUser
	code      string
}

func (a attempt) withUser(u accessdb.AccessUser) attempt {
	a.user = u
	return a
}

// SessionTicket is a new session: the cookie value and when it stops working.
type SessionTicket struct {
	Token     string
	ExpiresAt time.Time
}

// Verify completes sign-in with the second factor (if the account has one) and opens a session.
func (s *Service) Verify(ctx context.Context, challengeID, code string) (SessionTicket, error) {
	challenge, err := accessdb.New(s.DB.Pool).GetChallenge(ctx,
		accessdb.GetChallengeParams{TokenHash: hashToken(challengeID), Now: s.Now()})
	if err != nil {
		return SessionTicket{}, errBadCredentials
	}
	var ticket SessionTicket
	var passed bool
	err = s.DB.InTenantTx(ctx, challenge.TenantID, func(tx pgx.Tx) error {
		var txErr error
		ticket, passed, txErr = s.completeChallenge(ctx, tx, attempt{tx: tx, challenge: challenge, code: code})
		return txErr
	})
	if err != nil {
		return SessionTicket{}, err
	}
	if !passed {
		return SessionTicket{}, errBadCredentials
	}
	return ticket, nil
}

func (s *Service) completeChallenge(ctx context.Context, tx pgx.Tx, a attempt) (SessionTicket, bool, error) {
	q := accessdb.New(tx)
	user, err := q.GetUser(ctx, a.challenge.UserID)
	if err != nil {
		return SessionTicket{}, false, db.NotFoundIfNoRows(err, "user")
	}
	if !usable(user, s.Now()) { // locked or deactivated since the password step
		return SessionTicket{}, false, nil
	}
	ok, err := s.secondFactorPassed(ctx, a.withUser(user))
	if err != nil || !ok {
		return SessionTicket{}, false, err
	}
	ticket, err := s.openSession(ctx, q, user)
	if err != nil {
		return SessionTicket{}, false, err
	}
	entry := audit.Entry{At: s.Now(), Actor: user.ID, Action: "user.login", Subject: userSubject(user.ID)}
	return ticket, true, audit.Record(ctx, tx, entry)
}

// secondFactorPassed counts the attempt before checking it, so guessing codes is bounded.
func (s *Service) secondFactorPassed(ctx context.Context, a attempt) (bool, error) {
	user, q := a.user, accessdb.New(a.tx)
	if user.TotpSecretEnc == nil {
		return true, nil
	}
	attempts, err := q.BumpChallengeAttempts(ctx, a.challenge.ID)
	if err != nil {
		return false, fmt.Errorf("bump attempts: %w", err)
	}
	accepted, err := s.acceptCode(ctx, a, attempts <= maxChallengeAttempts)
	if err != nil || accepted {
		return accepted, err
	}
	return false, s.recordFailure(ctx, a.tx, user.ID)
}

// acceptCode is true when the code is right and its time step was not used before; it then keeps the step.
func (s *Service) acceptCode(ctx context.Context, a attempt, withinAttempts bool) (bool, error) {
	secret, err := s.Box.Open(a.user.TotpSecretEnc)
	if err != nil {
		return false, err
	}
	step, matches := MatchTOTP(secret, a.code, s.Now())
	if !withinAttempts || !matches {
		return false, nil
	}
	taken, err := accessdb.New(a.tx).SetTOTPStep(ctx, accessdb.SetTOTPStepParams{ID: a.user.ID, Step: step})
	if err != nil {
		return false, fmt.Errorf("keep totp step: %w", err)
	}
	return taken == 1, nil
}
