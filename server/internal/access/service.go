package access

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Role is a permission set a person holds. Values match the access.user_roles check and the API.
type Role string

// Roles (ATLAS.md §13.3).
const (
	RoleViewer            Role = "viewer"
	RoleEditorMetadata    Role = "editor_metadata"
	RoleCurator           Role = "curator"
	RoleOrganizerApprover Role = "organizer_approver"
	RoleAdmin             Role = "admin"
	RoleAuditor           Role = "auditor"
)

var knownRoles = map[Role]bool{
	RoleViewer: true, RoleEditorMetadata: true, RoleCurator: true,
	RoleOrganizerApprover: true, RoleAdmin: true, RoleAuditor: true,
}

// User is a person with an account.
type User struct {
	ID           uuid.UUID
	Name         string
	Email        string
	Department   string
	Roles        []Role
	IsActive     bool
	LastActiveAt *time.Time
	Hue          int
}

// Principal is who a request acts as. It is rebuilt from the database on every request, so a role change
// or a deleted account takes effect at once.
type Principal struct {
	SessionID uuid.UUID
	UserID    uuid.UUID
	TenantID  uuid.UUID
	Roles     []Role
}

// Has reports whether the principal holds role.
func (p Principal) Has(role Role) bool {
	return hasRole(p.Roles, role)
}

// CreateTenantFunc creates the organization and its Core catalog inside tx. It is injected by the
// composition root so access does not import catalog.
type CreateTenantFunc func(ctx context.Context, tx pgx.Tx, orgName string) (uuid.UUID, error)

// Service holds accounts, sessions and roles.
type Service struct {
	DB           *db.DB
	Box          *Box
	CreateTenant CreateTenantFunc
	Now          func() time.Time
}

// NewService returns a Service that reads the real clock.
func NewService(d *db.DB, box *Box, createTenant CreateTenantFunc) *Service {
	return &Service{DB: d, Box: box, CreateTenant: createTenant, Now: time.Now}
}

// tenantID returns the tenant of this installation.
// shortcut: on-prem has exactly one tenant; a multi-tenant (cloud) login needs a tenant hint in the request.
func (s *Service) tenantID(ctx context.Context) (uuid.UUID, error) {
	tenants, err := ListTenants(ctx, s.DB.Pool)
	if err != nil {
		return uuid.Nil, err
	}
	if len(tenants) == 0 {
		return uuid.Nil, fmt.Errorf("no organization yet: %w", errs.ErrUnauthenticated)
	}
	return tenants[0].ID, nil
}

func (s *Service) rolesOf(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID][]Role, error) {
	rows, err := accessdb.New(tx).ListRolesOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	roles := make(map[uuid.UUID][]Role, len(ids))
	for _, r := range rows {
		roles[r.UserID] = append(roles[r.UserID], Role(r.Role))
	}
	return roles, nil
}

func toUser(row accessdb.AccessUser, roles []Role) User {
	return User{ID: row.ID, Name: row.Name, Email: row.Email, Department: row.Department, Roles: roles,
		IsActive: row.IsActive, LastActiveAt: row.LastActiveAt, Hue: int(row.Hue)}
}

const hueDegrees = 360

// hueFor gives a person a stable avatar color from their email.
func hueFor(email string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(email))
	return int32(h.Sum32() % hueDegrees)
}

// userSubject names an account in audit events by id, never by email: the log cannot be edited.
func userSubject(id uuid.UUID) string { return "user:" + id.String() }
