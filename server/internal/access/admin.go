package access

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Account administration from the server console. There is no e-mail recovery on-prem (ATLAS.md §13.1): the
// person who runs the server creates accounts and recovers access with these. Events are recorded as the system.

const passwordBytes = 15 // 20 characters once encoded

// totpRequiredFor are the roles that must sign in with a second factor; for the others it is optional.
var totpRequiredFor = map[Role]bool{RoleAdmin: true, RoleAuditor: true, RoleCurator: true}

// NewUser is an account to create. WithoutTOTP is allowed only when no role requires a second factor.
type NewUser struct {
	Name        string
	Email       string
	Roles       []Role
	WithoutTOTP bool
}

// Credentials are shown once to whoever runs the command. The password is never stored in the clear.
type Credentials struct {
	UserID   uuid.UUID
	Password string // empty when only the second factor was reset
	TOTPURI  string // empty for an account without a second factor
}

// CreateUser makes an account with a random password and, unless allowed to skip it, a second factor.
func (s *Service) CreateUser(ctx context.Context, tenant uuid.UUID, in NewUser) (Credentials, error) {
	if err := validateNewUser(in); err != nil {
		return Credentials{}, err
	}
	var out Credentials
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		var createErr error
		out, createErr = s.insertUser(ctx, tx, in)
		return createErr
	})
	return out, err
}

func validateNewUser(in NewUser) error {
	if strings.TrimSpace(in.Name) == "" || !strings.Contains(in.Email, "@") {
		return fmt.Errorf("a name and an e-mail address are required: %w", errs.ErrInvalid)
	}
	if err := validateRoles(in.Roles); err != nil {
		return err
	}
	if in.WithoutTOTP && needsTOTP(in.Roles) {
		return fmt.Errorf("a second factor is required for these roles: %w", errs.ErrInvalid)
	}
	return nil
}

func needsTOTP(roles []Role) bool {
	for _, r := range roles {
		if totpRequiredFor[r] {
			return true
		}
	}
	return false
}

func (s *Service) insertUser(ctx context.Context, tx pgx.Tx, in NewUser) (Credentials, error) {
	q := accessdb.New(tx)
	if err := emailMustBeFree(ctx, q, in.Email); err != nil {
		return Credentials{}, err
	}
	password, hash, err := newPassword()
	if err != nil {
		return Credentials{}, err
	}
	seed, err := s.newSecondFactor(in)
	if err != nil {
		return Credentials{}, err
	}
	id, err := storeUser(ctx, q, storing{in: in, hash: hash, sealed: seed.sealed})
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{UserID: id, Password: password, TOTPURI: seed.uri(in.Email)}, s.recordSystem(ctx, tx, systemEvent{action: "user.created", user: id})
}

func emailMustBeFree(ctx context.Context, q *accessdb.Queries, email string) error {
	_, err := q.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
		return fmt.Errorf("an account with this e-mail exists: %w", errs.ErrConflict)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("check e-mail: %w", err)
	}
	return nil
}

// storing is an account ready to be written: its hash and its sealed second-factor seed.
type storing struct {
	in     NewUser
	hash   string
	sealed []byte
}

func storeUser(ctx context.Context, q *accessdb.Queries, w storing) (uuid.UUID, error) {
	id, err := q.CreateUser(ctx, accessdb.CreateUserParams{Name: w.in.Name, Email: w.in.Email, PasswordHash: w.hash,
		TotpSecretEnc: w.sealed, Hue: hueFor(w.in.Email)})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create user: %w", err)
	}
	for _, r := range w.in.Roles {
		if err := q.AddRole(ctx, accessdb.AddRoleParams{UserID: id, Role: string(r)}); err != nil {
			return uuid.Nil, fmt.Errorf("add role: %w", err)
		}
	}
	return id, nil
}

// secondFactor is a fresh TOTP seed, sealed for storage, or none.
type secondFactor struct {
	raw    []byte
	sealed []byte
}

func (f secondFactor) uri(email string) string {
	if f.raw == nil {
		return ""
	}
	return TOTPURI(totpIssuer, email, f.raw)
}

func (s *Service) newSecondFactor(in NewUser) (secondFactor, error) {
	if in.WithoutTOTP {
		return secondFactor{}, nil
	}
	return s.sealNewSeed()
}

func (s *Service) sealNewSeed() (secondFactor, error) {
	raw, err := NewTOTPSecret()
	if err != nil {
		return secondFactor{}, err
	}
	sealed, err := s.Box.Seal(raw)
	return secondFactor{raw: raw, sealed: sealed}, err
}

// newPassword returns a random password and its hash.
func newPassword() (password, hash string, err error) {
	raw := make([]byte, passwordBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("new password: %w", err)
	}
	password = base64.RawURLEncoding.EncodeToString(raw)
	hash, err = HashPassword(password)
	return password, hash, err
}

// ResetPassword gives an account a new random password, lifts any lock and ends its sessions.
func (s *Service) ResetPassword(ctx context.Context, tenant uuid.UUID, email string) (Credentials, error) {
	var out Credentials
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		q := accessdb.New(tx)
		user, err := q.GetUserByEmail(ctx, email)
		if err != nil {
			return notFoundUser(err)
		}
		password, hash, err := newPassword()
		if err != nil {
			return err
		}
		if _, err := q.SetPasswordHash(ctx, accessdb.SetPasswordHashParams{ID: user.ID, PasswordHash: hash}); err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		out = Credentials{UserID: user.ID, Password: password}
		return s.endSessionsAndRecord(ctx, tx, systemEvent{action: "user.password_reset", user: user.ID})
	})
	return out, err
}

// ResetTOTP gives an account a new second factor and ends its sessions. Use it when someone loses their phone.
func (s *Service) ResetTOTP(ctx context.Context, tenant uuid.UUID, email string) (Credentials, error) {
	var out Credentials
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		q := accessdb.New(tx)
		user, err := q.GetUserByEmail(ctx, email)
		if err != nil {
			return notFoundUser(err)
		}
		seed, err := s.sealNewSeed()
		if err != nil {
			return err
		}
		if _, err := q.SetTOTPSecret(ctx, accessdb.SetTOTPSecretParams{ID: user.ID, TotpSecretEnc: seed.sealed}); err != nil {
			return fmt.Errorf("set second factor: %w", err)
		}
		out = Credentials{UserID: user.ID, TOTPURI: seed.uri(user.Email)}
		return s.endSessionsAndRecord(ctx, tx, systemEvent{action: "user.totp_reset", user: user.ID})
	})
	return out, err
}

func notFoundUser(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no account with this e-mail: %w", errs.ErrNotFound)
	}
	return fmt.Errorf("get user: %w", err)
}

// systemEvent is something done to an account from the console.
type systemEvent struct {
	action string
	user   uuid.UUID
}

func (s *Service) endSessionsAndRecord(ctx context.Context, tx pgx.Tx, e systemEvent) error {
	q := accessdb.New(tx)
	if err := q.DeleteSessionsOf(ctx, e.user); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	if err := q.DeleteChallengesOf(ctx, e.user); err != nil {
		return fmt.Errorf("end challenges: %w", err)
	}
	return s.recordSystem(ctx, tx, e)
}

func (s *Service) recordSystem(ctx context.Context, tx pgx.Tx, e systemEvent) error {
	return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Action: e.action, Subject: userSubject(e.user), Detail: "from the server console"})
}
