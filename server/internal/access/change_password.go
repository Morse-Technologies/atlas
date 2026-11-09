package access

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// errWrongPassword says the typed current password is not the account's. The person is already signed in, so
// naming the problem leaks nothing; it is not an authentication error (that would end the session).
var errWrongPassword = fmt.Errorf("the current password is not right: %w", errs.ErrInvalid)

// PasswordChange is a person changing their own password.
type PasswordChange struct {
	Current string
	New     string
}

// ChangePassword replaces the principal's password and ends their other sessions; this session stays. A wrong
// current password counts as a failed sign-in, so a stolen session cannot be used to guess the password.
func (s *Service) ChangePassword(ctx context.Context, p Principal, in PasswordChange) error {
	if len(in.New) < minPasswordLen {
		return fmt.Errorf("password needs at least %d characters: %w", minPasswordLen, errs.ErrInvalid)
	}
	right := false
	err := s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var txErr error
		right, txErr = s.replacePassword(ctx, tx, passwordChanging{who: p, in: in})
		return txErr
	})
	if err != nil {
		return err
	}
	if !right { // the failed-attempt counter was committed above
		return errWrongPassword
	}
	return nil
}

// passwordChanging is who is changing their password, and to what.
type passwordChanging struct {
	who Principal
	in  PasswordChange
}

func (s *Service) replacePassword(ctx context.Context, tx pgx.Tx, c passwordChanging) (bool, error) {
	user, err := accessdb.New(tx).GetUser(ctx, c.who.UserID)
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	if user.LockedUntil != nil && user.LockedUntil.After(s.Now()) {
		return false, nil // locked: no guess is checked
	}
	if !VerifyPassword(user.PasswordHash, c.in.Current) {
		return false, s.recordFailure(ctx, tx, user.ID)
	}
	return true, s.storeNewPassword(ctx, tx, c)
}

func (s *Service) storeNewPassword(ctx context.Context, tx pgx.Tx, c passwordChanging) error {
	q := accessdb.New(tx)
	hash, err := HashPassword(c.in.New)
	if err != nil {
		return err
	}
	if _, err := q.SetPasswordHash(ctx, accessdb.SetPasswordHashParams{ID: c.who.UserID, PasswordHash: hash}); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if err := q.DeleteOtherSessionsOf(ctx, accessdb.DeleteOtherSessionsOfParams{UserID: c.who.UserID, Keep: c.who.SessionID}); err != nil {
		return fmt.Errorf("end other sessions: %w", err)
	}
	entry := audit.Entry{At: s.Now(), Actor: c.who.UserID, Action: "user.password_changed", Subject: userSubject(c.who.UserID)}
	return audit.Record(ctx, tx, entry)
}
