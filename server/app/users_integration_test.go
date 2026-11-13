//go:build integration

package app

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/dbtest"
)

// consoleFixture is an installation with one administrator and the secret key the console reads from the environment.
func consoleFixture(t *testing.T) (cliSession, *access.Service) {
	t.Helper()
	key := make([]byte, 32)
	t.Setenv("ATLAS_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	box, _ := access.NewBox(key)
	d := dbtest.Start(t)
	svc := access.NewService(d, box, func(ctx context.Context, tx pgx.Tx, org string) (uuid.UUID, error) {
		return access.CreateTenant(ctx, tx, org)
	})
	if _, err := svc.Setup(context.Background(), access.SetupInput{OrgName: "Acme", Name: "Ada", Email: "ada@acme.test", Password: "a-long-enough-password"}); err != nil {
		t.Fatal(err)
	}
	tenants, err := access.ListTenants(context.Background(), d.Pool)
	if err != nil {
		t.Fatal(err)
	}
	return cliSession{db: d, tenantID: tenants[0].ID}, svc
}

func TestTheConsoleResetsAnAccountByEmail(t *testing.T) {
	session, svc := consoleFixture(t)
	ctx := context.Background()
	if err := userResetPassword(ctx, session, []string{"ADA@acme.test"}); err != nil {
		t.Fatalf("reset-password by e-mail: %v", err)
	}
	if err := userResetTOTP(ctx, session, []string{"ada@acme.test"}); err != nil {
		t.Fatalf("reset-totp by e-mail: %v", err)
	}
	id, err := svc.UserIDByEmail(ctx, session.tenantID, "ada@acme.test")
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.GetUser(ctx, access.Principal{TenantID: session.tenantID}, id)
	if err != nil || !user.MustChangePassword {
		t.Fatalf("user %+v, %v; a console reset must also make the person choose a password", user, err)
	}
	if err := userResetPassword(ctx, session, []string{"nobody@acme.test"}); err == nil {
		t.Fatal("an unknown e-mail must fail")
	}
}
