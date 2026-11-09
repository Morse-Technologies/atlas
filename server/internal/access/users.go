package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Page selects users after a cursor. After is the id of the last user of the previous page (uuid.Nil: first page).
type Page struct {
	After uuid.UUID
	Size  int32
}

// GetUser returns one account.
func (s *Service) GetUser(ctx context.Context, p Principal, id uuid.UUID) (User, error) {
	var out User
	err := s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		row, err := accessdb.New(tx).GetUser(ctx, id)
		if err != nil {
			return db.NotFoundIfNoRows(err, "user")
		}
		roles, err := s.rolesOf(ctx, tx, []uuid.UUID{id})
		out = toUser(row, roles[id])
		return err
	})
	return out, err
}

// ListUsers returns one page of accounts in id order, and the cursor of the next page ("" on the last one).
func (s *Service) ListUsers(ctx context.Context, p Principal, page Page) ([]User, uuid.UUID, error) {
	var users []User
	err := s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var txErr error
		users, txErr = s.usersAfter(ctx, tx, page)
		return txErr
	})
	if err != nil || int32(len(users)) <= page.Size {
		return users, uuid.Nil, err
	}
	return users[:page.Size], users[page.Size-1].ID, nil
}

// usersAfter reads one row more than the page, so the caller can tell whether another page exists.
func (s *Service) usersAfter(ctx context.Context, tx pgx.Tx, page Page) ([]User, error) {
	rows, err := accessdb.New(tx).ListUsers(ctx, accessdb.ListUsersParams{After: page.After, PageSize: page.Size + 1})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	roles, err := s.rolesOf(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	users := make([]User, len(rows))
	for i, r := range rows {
		users[i] = toUser(r, roles[r.ID])
	}
	return users, nil
}

// RoleChange sets the complete list of roles of one account.
type RoleChange struct {
	UserID uuid.UUID
	Roles  []Role
}

// UpdateRoles replaces the roles of an account. The last administrator cannot lose the admin role.
func (s *Service) UpdateRoles(ctx context.Context, p Principal, change RoleChange) (User, error) {
	if err := validateRoles(change.Roles); err != nil {
		return User{}, err
	}
	err := s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		q := accessdb.New(tx)
		if !hasRole(change.Roles, RoleAdmin) {
			if err := s.guardLastAdmin(ctx, q, change.UserID); err != nil {
				return err
			}
		}
		if err := replaceRoles(ctx, q, change); err != nil {
			return err
		}
		return s.record(ctx, tx, auditChange{by: p.UserID, action: "user.roles_changed", target: change.UserID, detail: "roles=" + joinRoles(change.Roles)})
	})
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, p, change.UserID)
}

func validateRoles(roles []Role) error {
	if len(roles) == 0 {
		return fmt.Errorf("at least one role is required: %w", errs.ErrInvalid)
	}
	for _, r := range roles {
		if !knownRoles[r] {
			return fmt.Errorf("unknown role %q: %w", r, errs.ErrInvalid)
		}
	}
	return nil
}

func hasRole(roles []Role, want Role) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func replaceRoles(ctx context.Context, q *accessdb.Queries, change RoleChange) error {
	if _, err := q.GetUser(ctx, change.UserID); err != nil {
		return db.NotFoundIfNoRows(err, "user")
	}
	if err := q.ClearRoles(ctx, change.UserID); err != nil {
		return fmt.Errorf("clear roles: %w", err)
	}
	for _, r := range change.Roles {
		if err := q.AddRole(ctx, accessdb.AddRoleParams{UserID: change.UserID, Role: string(r)}); err != nil {
			return fmt.Errorf("add role: %w", err)
		}
	}
	return nil
}

// DeleteUser erases an account and, by cascade, its sessions and roles. The last administrator cannot be deleted.
func (s *Service) DeleteUser(ctx context.Context, p Principal, id uuid.UUID) error {
	return s.DB.InTenantTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		q := accessdb.New(tx)
		if err := s.guardLastAdmin(ctx, q, id); err != nil {
			return err
		}
		n, err := q.DeleteUser(ctx, id)
		if err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("user: %w", errs.ErrNotFound)
		}
		if err := s.record(ctx, tx, auditChange{by: p.UserID, action: "user.deleted", target: id}); err != nil {
			return err
		}
		return jobs.Enqueue(ctx, tx, events.UserDeleted{TenantID: p.TenantID, UserID: id})
	})
}

// guardLastAdmin fails with ErrConflict when userID is the only administrator and is about to lose that role.
// It locks the admin rows first, so two concurrent requests cannot both pass.
func (s *Service) guardLastAdmin(ctx context.Context, q *accessdb.Queries, userID uuid.UUID) error {
	admins, err := q.LockAdmins(ctx)
	if err != nil {
		return fmt.Errorf("lock admins: %w", err)
	}
	isAdmin := false
	for _, a := range admins {
		isAdmin = isAdmin || a == userID
	}
	if isAdmin && len(admins) <= 1 {
		return fmt.Errorf("the last administrator stays: %w", errs.ErrConflict)
	}
	return nil
}

// auditChange is what an administrative action reports about the account it touched.
type auditChange struct {
	by     uuid.UUID
	action string
	target uuid.UUID
	detail string
}

func (s *Service) record(ctx context.Context, tx pgx.Tx, c auditChange) error {
	return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Actor: c.by, Action: c.action,
		Subject: userSubject(c.target), Detail: c.detail})
}

func joinRoles(roles []Role) string {
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = string(r)
	}
	return strings.Join(names, ",")
}

// UserExists reports whether id is an account of the tenant that tx is scoped to. Other modules use it to
// check references to people (a document owner) without reading access tables.
func UserExists(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	_, err := accessdb.New(tx).GetUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	return true, nil
}

// DefaultACLSet returns the set that the chunks of every source belong to, creating it on first use.
func DefaultACLSet(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	q := accessdb.New(tx)
	id, err := q.GetDefaultACLSet(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = q.CreateDefaultACLSet(ctx)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("default acl set: %w", err)
	}
	return id, nil
}

// AllowedACLSets returns the sets whose chunks a person may be shown: the default set, and every set of a
// restricted source that lists them. Search filters on them before ranking.
func AllowedACLSets(ctx context.Context, tx pgx.Tx, person uuid.UUID) ([]uuid.UUID, error) {
	ids, err := accessdb.New(tx).ListReadableACLSets(ctx, person)
	if err != nil {
		return nil, fmt.Errorf("readable acl sets: %w", err)
	}
	if len(ids) == 0 { // no default set yet: make it, every person may read it
		id, err := DefaultACLSet(ctx, tx)
		return []uuid.UUID{id}, err
	}
	return ids, nil
}
