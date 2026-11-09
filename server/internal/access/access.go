// Package access owns tenants and, later, users, sessions, roles and ACLs (ATLAS.md §13).
// Part 1 needs only the tenant registry: creating the tenant and finding it from processes without a session.
package access

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/access/accessdb"
)

// Tenant is one organization served by this installation.
type Tenant struct {
	ID      uuid.UUID
	OrgName string
}

// CreateTenant registers an organization and returns its id.
func CreateTenant(ctx context.Context, conn accessdb.DBTX, orgName string) (uuid.UUID, error) {
	id, err := accessdb.New(conn).CreateTenant(ctx, orgName)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create tenant: %w", err)
	}
	return id, nil
}

// ListTenants returns every tenant. The registry has no RLS, so this works without a tenant scope.
func ListTenants(ctx context.Context, conn accessdb.DBTX) ([]Tenant, error) {
	rows, err := accessdb.New(conn).ListTenants(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	out := make([]Tenant, len(rows))
	for i, r := range rows {
		out[i] = Tenant{ID: r.ID, OrgName: r.OrgName}
	}
	return out, nil
}
