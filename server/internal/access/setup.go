package access

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	minPasswordLen = 12
	// setupLockKey is "ATLS" in ASCII: the advisory lock that serializes first-run setup.
	setupLockKey int64 = 0x41544c53
	totpIssuer         = "Atlas"
)

// SetupInput is the first administrator and the organization.
type SetupInput struct {
	OrgName  string
	Name     string
	Email    string
	Password string
}

// SetupDone reports whether the first administrator exists.
func (s *Service) SetupDone(ctx context.Context) (bool, error) {
	tenants, err := ListTenants(ctx, s.DB.Pool)
	if err != nil || len(tenants) == 0 {
		return false, err
	}
	var users int64
	err = s.DB.InTenantTx(ctx, tenants[0].ID, func(tx pgx.Tx) error {
		var countErr error
		users, countErr = accessdb.New(tx).CountUsers(ctx)
		return countErr
	})
	return users > 0, err
}

// Setup creates the organization and its first administrator, once. It returns the otpauth URI the
// administrator scans to enroll the second factor. A second call fails with errs.ErrConflict.
func (s *Service) Setup(ctx context.Context, in SetupInput) (string, error) {
	if err := validateSetup(in); err != nil {
		return "", err
	}
	admin, secret, err := s.prepareAdmin(in)
	if err != nil {
		return "", err
	}
	err = s.DB.InTx(ctx, func(tx pgx.Tx) error { return s.createFirstAdmin(ctx, tx, admin) })
	if err != nil {
		return "", err
	}
	return TOTPURI(totpIssuer, in.Email, secret), nil
}

// adminAccount is the first administrator with the secrets already derived, so nothing slow runs in the transaction.
type adminAccount struct {
	SetupInput
	hash string
	totp []byte
}

// prepareAdmin hashes the password and seals a new second-factor seed. It returns the seed in the clear
// once, for the enrollment URI.
func (s *Service) prepareAdmin(in SetupInput) (adminAccount, []byte, error) {
	hash, err := HashPassword(in.Password)
	if err != nil {
		return adminAccount{}, nil, err
	}
	secret, err := NewTOTPSecret()
	if err != nil {
		return adminAccount{}, nil, err
	}
	sealed, err := s.Box.Seal(secret)
	return adminAccount{SetupInput: in, hash: hash, totp: sealed}, secret, err
}

func validateSetup(in SetupInput) error {
	switch {
	case strings.TrimSpace(in.OrgName) == "" || strings.TrimSpace(in.Name) == "":
		return fmt.Errorf("organization and name are required: %w", errs.ErrInvalid)
	case !strings.Contains(in.Email, "@"):
		return fmt.Errorf("email is not valid: %w", errs.ErrInvalid)
	case len(in.Password) < minPasswordLen:
		return fmt.Errorf("password needs at least %d characters: %w", minPasswordLen, errs.ErrInvalid)
	}
	return nil
}

func (s *Service) createFirstAdmin(ctx context.Context, tx pgx.Tx, admin adminAccount) error {
	q := accessdb.New(tx)
	if err := q.LockSetup(ctx, setupLockKey); err != nil {
		return fmt.Errorf("lock setup: %w", err)
	}
	tenant, err := s.tenantForSetup(ctx, tx, admin.OrgName)
	if err != nil {
		return err
	}
	if err := db.SetTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if users, err := q.CountUsers(ctx); err != nil || users > 0 {
		return setupClosed(err)
	}
	return s.insertAdmin(ctx, tx, admin)
}

func (s *Service) insertAdmin(ctx context.Context, tx pgx.Tx, admin adminAccount) error {
	q := accessdb.New(tx)
	id, err := q.CreateUser(ctx, accessdb.CreateUserParams{Name: admin.Name, Email: admin.Email,
		PasswordHash: admin.hash, TotpSecretEnc: admin.totp, Hue: hueFor(admin.Email)})
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	if err := q.AddRole(ctx, accessdb.AddRoleParams{UserID: id, Role: string(RoleAdmin)}); err != nil {
		return fmt.Errorf("add admin role: %w", err)
	}
	return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Actor: id, Action: "setup.completed", Subject: userSubject(id)})
}

func setupClosed(err error) error {
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	return fmt.Errorf("setup already done: %w", errs.ErrConflict)
}

// tenantForSetup reuses the tenant `atlas setup` may have created, or creates it.
func (s *Service) tenantForSetup(ctx context.Context, tx pgx.Tx, orgName string) (uuid.UUID, error) {
	tenants, err := ListTenants(ctx, tx)
	if err != nil {
		return uuid.Nil, err
	}
	if len(tenants) > 0 {
		return tenants[0].ID, nil
	}
	return s.CreateTenant(ctx, tx, orgName)
}
