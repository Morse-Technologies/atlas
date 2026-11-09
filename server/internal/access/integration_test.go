//go:build integration

package access_test

import (
	"context"
	"encoding/base32"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const adminPassword = "a-long-enough-password"

type fixture struct {
	t      *testing.T
	ctx    context.Context
	svc    *access.Service
	now    time.Time
	secret []byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	box, err := access.NewBox(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, ctx: context.Background(), now: time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)}
	createTenant := func(ctx context.Context, tx pgx.Tx, org string) (uuid.UUID, error) {
		return access.CreateTenant(ctx, tx, org)
	}
	f.svc = access.NewService(dbtest.Start(t), box, createTenant)
	f.svc.Now = func() time.Time { return f.now }
	return f
}

func (f *fixture) setup() {
	f.t.Helper()
	uri, err := f.svc.Setup(f.ctx, access.SetupInput{OrgName: "Acme", Name: "Ada", Email: "ada@acme.test", Password: adminPassword})
	if err != nil {
		f.t.Fatal(err)
	}
	parsed, _ := url.Parse(uri)
	f.secret, _ = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(parsed.Query().Get("secret"))
}

func (f *fixture) signIn() access.SessionTicket {
	f.t.Helper()
	ch, err := f.svc.Login(f.ctx, "ADA@acme.test", adminPassword)
	if err != nil || !ch.NeedsCode {
		f.t.Fatalf("Login = %+v, %v", ch, err)
	}
	ticket, err := f.svc.Verify(f.ctx, ch.ID, access.CodeAt(f.secret, f.now))
	if err != nil {
		f.t.Fatal(err)
	}
	return ticket
}

func TestSetupRunsOnce(t *testing.T) {
	f := newFixture(t)
	if done, _ := f.svc.SetupDone(f.ctx); done {
		t.Fatal("setup must be open on an empty installation")
	}
	f.setup()
	if done, _ := f.svc.SetupDone(f.ctx); !done {
		t.Fatal("setup must be closed once the admin exists")
	}
	_, err := f.svc.Setup(f.ctx, access.SetupInput{OrgName: "Evil", Name: "X", Email: "x@evil.test", Password: adminPassword})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("second setup = %v, want ErrConflict", err)
	}
}

func TestSignInThenLogout(t *testing.T) {
	f := newFixture(t)
	f.setup()
	ticket := f.signIn()
	p, err := f.svc.Authenticate(f.ctx, ticket.Token)
	if err != nil || !p.Has(access.RoleAdmin) {
		t.Fatalf("Authenticate = %+v, %v", p, err)
	}
	if err := f.svc.Logout(f.ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(f.ctx, ticket.Token); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("after logout = %v, want ErrUnauthenticated", err)
	}
}

func TestSignInLeavesAVerifiableTrail(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	_ = f.svc.Logout(f.ctx, p)
	log := &audit.Service{DB: f.svc.DB}
	events, _, err := log.List(f.ctx, p.TenantID, audit.Page{Size: 50})
	if err != nil || len(events) != 3 || events[0].Action != "user.logout" || events[2].Action != "setup.completed" {
		t.Fatalf("events = %+v, %v; want setup.completed, user.login, user.logout (newest first)", events, err)
	}
	if v, err := log.Verify(f.ctx, p.TenantID); err != nil || !v.OK || v.Checked != 3 {
		t.Fatalf("Verify = %+v, %v", v, err)
	}
}

func TestSessionExpiresAfterTheShift(t *testing.T) {
	f := newFixture(t)
	f.setup()
	ticket := f.signIn()
	f.now = f.now.Add(access.SessionTTL + time.Second)
	if _, err := f.svc.Authenticate(f.ctx, ticket.Token); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("expired session = %v, want ErrUnauthenticated", err)
	}
}

func TestWrongCodeAndWrongPasswordGiveTheSameAnswer(t *testing.T) {
	f := newFixture(t)
	f.setup()
	_, errPassword := f.svc.Login(f.ctx, "ada@acme.test", "wrong-password-here")
	_, errUnknown := f.svc.Login(f.ctx, "nobody@acme.test", adminPassword)
	ch, _ := f.svc.Login(f.ctx, "ada@acme.test", adminPassword)
	_, errCode := f.svc.Verify(f.ctx, ch.ID, "000000")
	for _, err := range []error{errPassword, errUnknown, errCode} {
		if err == nil || err.Error() != errPassword.Error() {
			t.Fatalf("answers differ: %v vs %v", err, errPassword)
		}
	}
}

func TestAccountLocksAfterRepeatedFailures(t *testing.T) {
	f := newFixture(t)
	f.setup()
	for range 5 {
		_, _ = f.svc.Login(f.ctx, "ada@acme.test", "wrong-password-here")
	}
	if _, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword); err == nil {
		t.Fatal("the right password must not pass while the account is locked")
	}
	f.now = f.now.Add(16 * time.Minute)
	if _, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword); err != nil {
		t.Fatalf("after the lock expires: %v", err)
	}
}

func TestLastAdminCannotBeDeletedOrDemoted(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	if err := f.svc.DeleteUser(f.ctx, p, p.UserID); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("delete last admin = %v, want ErrConflict", err)
	}
	_, err := f.svc.UpdateRoles(f.ctx, p, access.RoleChange{UserID: p.UserID, Roles: []access.Role{access.RoleViewer}})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("demote last admin = %v, want ErrConflict", err)
	}
}

func TestUnknownRoleIsRejected(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	_, err := f.svc.UpdateRoles(f.ctx, p, access.RoleChange{UserID: p.UserID, Roles: []access.Role{"root"}})
	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("unknown role = %v, want ErrInvalid", err)
	}
}

func TestAnotherTenantSeesNoUsers(t *testing.T) {
	f := newFixture(t)
	f.setup()
	other := access.Principal{TenantID: dbtest.NewTenant(t, f.svc.DB)}
	users, _, err := f.svc.ListUsers(f.ctx, other, access.Page{Size: 50})
	if err != nil || len(users) != 0 {
		t.Fatalf("ListUsers = %v, %v; RLS must hide the other tenant", users, err)
	}
}

func TestAnAccountCreatedFromTheConsoleSignsInWithItsPassword(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	creds, err := f.svc.CreateUser(f.ctx, p.TenantID, access.NewUser{Name: "Bea", Email: "bea@acme.test", Roles: []access.Role{access.RoleViewer}, WithoutTOTP: true})
	if err != nil || creds.Password == "" || creds.TOTPURI != "" {
		t.Fatalf("creds %+v, %v", creds, err)
	}
	ch, err := f.svc.Login(f.ctx, "bea@acme.test", creds.Password)
	if err != nil || ch.NeedsCode {
		t.Fatalf("login %+v, %v; a viewer without a second factor signs in with the password alone", ch, err)
	}
	if _, err := f.svc.Verify(f.ctx, ch.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateUser(f.ctx, p.TenantID, access.NewUser{Name: "Bea", Email: "BEA@acme.test", Roles: []access.Role{access.RoleViewer}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("a second account with the same e-mail = %v, want ErrConflict", err)
	}
}

func TestResettingAPasswordUnlocksTheAccountAndEndsItsSessions(t *testing.T) {
	f := newFixture(t)
	f.setup()
	ticket := f.signIn()
	p, _ := f.svc.Authenticate(f.ctx, ticket.Token)
	for range 5 {
		_, _ = f.svc.Login(f.ctx, "ada@acme.test", "wrong-password-here")
	}
	creds, err := f.svc.ResetPassword(f.ctx, p.TenantID, "ada@acme.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(f.ctx, ticket.Token); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("the old session = %v, want it ended", err)
	}
	if _, err := f.svc.Login(f.ctx, "ada@acme.test", creds.Password); err != nil {
		t.Fatalf("the new password must work at once: %v", err)
	}
}

func TestResettingTheSecondFactorInvalidatesTheOldCodes(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	oldSecret := f.secret
	creds, err := f.svc.ResetTOTP(f.ctx, p.TenantID, "ada@acme.test")
	if err != nil || creds.TOTPURI == "" {
		t.Fatalf("creds %+v, %v", creds, err)
	}
	ch, _ := f.svc.Login(f.ctx, "ada@acme.test", adminPassword)
	if _, err := f.svc.Verify(f.ctx, ch.ID, access.CodeAt(oldSecret, f.now)); err == nil {
		t.Fatal("a code from the old authenticator must not work")
	}
}

func TestResettingAnUnknownAccountIsNotFound(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	if _, err := f.svc.ResetPassword(f.ctx, p.TenantID, "nobody@acme.test"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// signInAgain opens a second session for the same account, as a second device would.
func (f *fixture) signInAgain() access.SessionTicket {
	f.t.Helper()
	f.now = f.now.Add(time.Minute) // a code is accepted once per time step
	return f.signIn()
}

func TestChangingThePasswordKeepsThisSessionAndEndsTheOthers(t *testing.T) {
	f := newFixture(t)
	f.setup()
	here, there := f.signIn(), f.signInAgain()
	p, _ := f.svc.Authenticate(f.ctx, here.Token)
	if err := f.svc.ChangePassword(f.ctx, p, access.PasswordChange{Current: adminPassword, New: "another-long-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(f.ctx, here.Token); err != nil {
		t.Fatalf("this session must stay: %v", err)
	}
	if _, err := f.svc.Authenticate(f.ctx, there.Token); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("the other session = %v, want ended", err)
	}
	if _, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("the old password still works: %v", err)
	}
	if _, err := f.svc.Login(f.ctx, "ada@acme.test", "another-long-password"); err != nil {
		t.Fatalf("the new password must work: %v", err)
	}
}

func TestAWrongCurrentPasswordChangesNothingAndCountsAsAFailure(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	for range 5 { // the account locks at the same limit as sign-in
		err := f.svc.ChangePassword(f.ctx, p, access.PasswordChange{Current: "not-the-password", New: "another-long-password"})
		if !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("err = %v, want ErrInvalid (and not an authentication error, which would end the session)", err)
		}
	}
	if _, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("after five wrong guesses the account must be locked, even for the right password: %v", err)
	}
}

func TestANewPasswordThatIsTooShortIsRefused(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	err := f.svc.ChangePassword(f.ctx, p, access.PasswordChange{Current: adminPassword, New: "short"})
	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
