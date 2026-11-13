//go:build integration

package access_test

import (
	"context"
	"encoding/base32"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	adminPassword = "a-long-enough-password"
	totpStep      = 30 * time.Second
)

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
	f.now = f.now.Add(totpStep) // a code works once: every sign-in uses a step of its own
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

// addViewer creates a viewer without a second factor and returns the principal of that person.
func (f *fixture) addViewer(admin access.Principal, name, email string) access.Principal {
	f.t.Helper()
	creds, err := f.svc.CreateUser(f.ctx, by(admin), access.NewUser{Name: name, Email: email, Roles: []access.Role{access.RoleViewer}, WithoutTOTP: true})
	if err != nil {
		f.t.Fatal(err)
	}
	return access.Principal{TenantID: admin.TenantID, UserID: creds.UserID, Roles: []access.Role{access.RoleViewer}}
}

func namesOf(people []access.PersonName) (names []string) {
	for _, p := range people {
		names = append(names, p.Name)
	}
	return names
}

func TestPeopleNamesListOnlyActiveAccounts(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	bea := f.addViewer(admin, "Bea", "bea@acme.test")
	f.addViewer(admin, "Cy", "cy@acme.test")
	f.exec("UPDATE access.users SET is_active = false WHERE email = 'cy@acme.test'")
	people, _, err := f.svc.ListPeople(f.ctx, bea, access.Page{Size: 50})
	if got := namesOf(people); err != nil || len(got) != 2 || !slices.Contains(got, "Ada") || !slices.Contains(got, "Bea") {
		t.Fatalf("ListPeople = %v, %v; want Ada and Bea, not the deactivated Cy", got, err)
	}
}

func TestPeopleNamesArePagedById(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	f.addViewer(admin, "Bea", "bea@acme.test")
	first, cursor, err := f.svc.ListPeople(f.ctx, admin, access.Page{Size: 1})
	if err != nil || len(first) != 1 || cursor != first[0].ID {
		t.Fatalf("first page = %v, cursor %v, %v; want one person and its id as cursor", first, cursor, err)
	}
	second, end, err := f.svc.ListPeople(f.ctx, admin, access.Page{After: cursor, Size: 1})
	if err != nil || len(second) != 1 || second[0].ID == first[0].ID || end != uuid.Nil {
		t.Fatalf("second page = %v, cursor %v, %v; want the other person and no cursor", second, end, err)
	}
}

func TestAnotherTenantSeesNoPeopleNames(t *testing.T) {
	f := newFixture(t)
	f.setup()
	other := access.Principal{TenantID: dbtest.NewTenant(t, f.svc.DB), UserID: uuid.New()}
	people, _, err := f.svc.ListPeople(f.ctx, other, access.Page{Size: 50})
	if err != nil || len(people) != 0 {
		t.Fatalf("ListPeople = %v, %v; RLS must hide the other tenant", people, err)
	}
}

func TestAnAccountCreatedFromTheConsoleSignsInWithItsPassword(t *testing.T) {
	f := newFixture(t)
	f.setup()
	p, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	creds, err := f.svc.CreateUser(f.ctx, by(p), access.NewUser{Name: "Bea", Email: "bea@acme.test", Roles: []access.Role{access.RoleViewer}, WithoutTOTP: true})
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
	if _, err := f.svc.CreateUser(f.ctx, by(p), access.NewUser{Name: "Bea", Email: "BEA@acme.test", Roles: []access.Role{access.RoleViewer}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("a second account with the same e-mail = %v, want ErrConflict", err)
	}
}

func by(p access.Principal) access.By {
	return access.By{TenantID: p.TenantID, ActorID: p.UserID}
}

func TestResettingAPasswordUnlocksTheAccountAndEndsItsSessions(t *testing.T) {
	f := newFixture(t)
	f.setup()
	ticket := f.signIn()
	p, _ := f.svc.Authenticate(f.ctx, ticket.Token)
	for range 5 {
		_, _ = f.svc.Login(f.ctx, "ada@acme.test", "wrong-password-here")
	}
	creds, err := f.svc.ResetPassword(f.ctx, by(p), p.UserID)
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
	creds, err := f.svc.ResetTOTP(f.ctx, by(p), p.UserID)
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
	if _, err := f.svc.ResetTOTP(f.ctx, by(p), uuid.New()); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("second factor: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.ResetPassword(f.ctx, by(p), uuid.New()); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// signInAgain opens a second session for the same account, as a second device would.
func (f *fixture) signInAgain() access.SessionTicket {
	f.t.Helper()
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

// createViewer makes Bea, a viewer without a second factor, and returns her one-time password.
func (f *fixture) createViewer(p access.Principal) access.Credentials {
	f.t.Helper()
	creds, err := f.svc.CreateUser(f.ctx, by(p), access.NewUser{Name: "Bea", Email: "bea@acme.test",
		Roles: []access.Role{access.RoleViewer}, WithoutTOTP: true})
	if err != nil {
		f.t.Fatal(err)
	}
	return creds
}

func (f *fixture) signInWithoutCode(email, password string) access.SessionTicket {
	f.t.Helper()
	ch, err := f.svc.Login(f.ctx, email, password)
	if err != nil {
		f.t.Fatal(err)
	}
	ticket, err := f.svc.Verify(f.ctx, ch.ID, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return ticket
}

func TestCreatedAccountMustChangeItsPassword(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	creds := f.createViewer(admin)
	bea, err := f.svc.Authenticate(f.ctx, f.signInWithoutCode("bea@acme.test", creds.Password).Token)
	if err != nil || !bea.MustChangePassword {
		t.Fatalf("principal %+v, %v; a new account must be told to change its password", bea, err)
	}
	if err := f.svc.ChangePassword(f.ctx, bea, access.PasswordChange{Current: creds.Password, New: "my-own-long-password"}); err != nil {
		t.Fatal(err)
	}
	bea, _ = f.svc.Authenticate(f.ctx, f.signInWithoutCode("bea@acme.test", "my-own-long-password").Token)
	if bea.MustChangePassword {
		t.Fatal("after choosing a password the mark must be gone")
	}
}

func TestCreatedAccountEventNamesTheAdmin(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	creds := f.createViewer(admin)
	events, _, err := (&audit.Service{DB: f.svc.DB}).List(f.ctx, admin.TenantID, audit.Page{Size: 1})
	if err != nil || events[0].Action != "user.created" || events[0].ActorID == nil || *events[0].ActorID != admin.UserID {
		t.Fatalf("events %+v, %v; want user.created by the admin", events, err)
	}
	if strings.Contains(events[0].Detail+events[0].Subject, creds.Password) || strings.Contains(events[0].Subject, "bea@") {
		t.Fatalf("the event leaks a secret or an e-mail: %+v", events[0])
	}
}

func TestTOTPIsRequiredForAdminAuditorCurator(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	for _, role := range []access.Role{access.RoleAdmin, access.RoleAuditor, access.RoleCurator} {
		_, err := f.svc.CreateUser(f.ctx, by(admin), access.NewUser{Name: "X", Email: "x@acme.test", Roles: []access.Role{role}, WithoutTOTP: true})
		if !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("%s without a second factor = %v, want ErrInvalid", role, err)
		}
	}
}

func TestResetPasswordEndsSessionsAndRequiresChange(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	beaCreds := f.createViewer(admin)
	beaTicket := f.signInWithoutCode("bea@acme.test", beaCreds.Password)
	bea, _ := f.svc.Authenticate(f.ctx, beaTicket.Token)
	_ = f.svc.ChangePassword(f.ctx, bea, access.PasswordChange{Current: beaCreds.Password, New: "my-own-long-password"})

	reset, err := f.svc.ResetPassword(f.ctx, by(admin), bea.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(f.ctx, beaTicket.Token); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("her old session = %v, want it ended", err)
	}
	again, _ := f.svc.Authenticate(f.ctx, f.signInWithoutCode("bea@acme.test", reset.Password).Token)
	if !again.MustChangePassword {
		t.Fatal("after a reset the person must choose a new password")
	}
}

func TestResetTOTPEndsSessions(t *testing.T) {
	f := newFixture(t)
	f.setup()
	ticket := f.signIn()
	p, _ := f.svc.Authenticate(f.ctx, ticket.Token)
	if _, err := f.svc.ResetTOTP(f.ctx, by(p), p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(f.ctx, ticket.Token); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("session after a second-factor reset = %v, want it ended", err)
	}
}

func TestResetEventsNameTheAdmin(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	bea, _ := f.svc.Authenticate(f.ctx, f.signInWithoutCode("bea@acme.test", f.createViewer(admin).Password).Token)
	log := &audit.Service{DB: f.svc.DB}
	for _, reset := range []func() (access.Credentials, error){
		func() (access.Credentials, error) { return f.svc.ResetPassword(f.ctx, by(admin), bea.UserID) },
		func() (access.Credentials, error) { return f.svc.ResetTOTP(f.ctx, by(admin), bea.UserID) },
	} {
		creds, err := reset()
		events, _, listErr := log.List(f.ctx, admin.TenantID, audit.Page{Size: 1})
		if err != nil || listErr != nil || events[0].ActorID == nil || *events[0].ActorID != admin.UserID {
			t.Fatalf("event %+v (%v, %v); want the admin as actor", events, err, listErr)
		}
		if strings.Contains(events[0].Detail, creds.Password+creds.TOTPURI) || events[0].Detail != "" {
			t.Fatalf("detail %q: an administrator's event carries no free text", events[0].Detail)
		}
	}
}

func TestUserIDByEmailIgnoresCase(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	id, err := f.svc.UserIDByEmail(f.ctx, admin.TenantID, "ADA@acme.test")
	if err != nil || id != admin.UserID {
		t.Fatalf("id %s, %v; want the administrator", id, err)
	}
	if _, err := f.svc.UserIDByEmail(f.ctx, admin.TenantID, "nobody@acme.test"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("unknown e-mail = %v, want ErrNotFound", err)
	}
}

func TestTOTPCodeCannotBeReused(t *testing.T) {
	f := newFixture(t)
	f.setup()
	f.signIn()
	used := access.CodeAt(f.secret, f.now)

	again, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Verify(f.ctx, again.ID, used); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("Verify with a code already used = %v, want unauthenticated", err)
	}
	// The next step brings a code the server has not seen.
	f.now = f.now.Add(totpStep)
	next, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Verify(f.ctx, next.ID, access.CodeAt(f.secret, f.now)); err != nil {
		t.Fatalf("Verify with the code of the next step = %v", err)
	}
}

func TestTOTPCodeOfAnOlderStepIsRefusedAfterANewerOneWasUsed(t *testing.T) {
	f := newFixture(t)
	f.setup()
	older := access.CodeAt(f.secret, f.now)
	f.now = f.now.Add(totpStep)
	f.signIn() // uses the step after `older`, which the server still accepts as drift
	f.now = f.now.Add(-totpStep)
	ch, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Verify(f.ctx, ch.ID, older); err == nil {
		t.Fatal("a code older than the last one accepted must be refused")
	}
}

// challengeAfterPassword passes the password step and returns the challenge, with its code ready for the next step.
func (f *fixture) challengeAfterPassword() (access.Challenge, string) {
	f.t.Helper()
	ch, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword)
	if err != nil {
		f.t.Fatal(err)
	}
	return ch, access.CodeAt(f.secret, f.now)
}

// tenantID is the only tenant of the fixture.
func (f *fixture) tenantID() uuid.UUID {
	f.t.Helper()
	tenants, err := access.ListTenants(f.ctx, f.svc.DB.Pool)
	if err != nil || len(tenants) != 1 {
		f.t.Fatalf("tenants = %v, %v", tenants, err)
	}
	return tenants[0].ID
}

// exec runs SQL inside the tenant, as the application would, so row-level security applies.
func (f *fixture) exec(sql string) {
	f.t.Helper()
	err := f.svc.DB.InTenantTx(f.ctx, f.tenantID(), func(tx pgx.Tx) error {
		_, execErr := tx.Exec(f.ctx, sql)
		return execErr
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// count returns how many rows a table has.
func (f *fixture) count(table string) (n int) {
	f.t.Helper()
	err := f.svc.DB.InTenantTx(f.ctx, f.tenantID(), func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestVerifyRefusesALockedAccount(t *testing.T) {
	f := newFixture(t)
	f.setup()
	ch, code := f.challengeAfterPassword()
	f.exec("UPDATE access.users SET locked_until = '2026-11-07 10:00:00+00'") // after the fixture's 09:00
	if _, err := f.svc.Verify(f.ctx, ch.ID, code); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("Verify on a locked account = %v, want unauthenticated", err)
	}
}

func TestVerifyRefusesADeactivatedAccount(t *testing.T) {
	f := newFixture(t)
	f.setup()
	ch, code := f.challengeAfterPassword()
	f.exec("UPDATE access.users SET is_active = false")
	if _, err := f.svc.Verify(f.ctx, ch.ID, code); !errors.Is(err, errs.ErrUnauthenticated) {
		t.Fatalf("Verify on a deactivated account = %v, want unauthenticated", err)
	}
}

func TestExpiredSessionsArePurged(t *testing.T) {
	f := newFixture(t)
	f.setup()
	f.signIn()
	if _, err := f.svc.Login(f.ctx, "ada@acme.test", adminPassword); err != nil { // a challenge nobody completes
		t.Fatal(err)
	}
	if err := f.svc.PurgeExpired(f.ctx, f.tenantID(), f.now); err != nil {
		t.Fatal(err)
	}
	if sessions, challenges := f.count("access.sessions"), f.count("access.login_challenges"); sessions != 1 || challenges == 0 {
		t.Fatalf("before they expire: %d sessions, %d challenges; want them kept", sessions, challenges)
	}

	later := f.now.Add(access.SessionTTL + time.Minute)
	if err := f.svc.PurgeExpired(f.ctx, f.tenantID(), later); err != nil {
		t.Fatal(err)
	}
	if sessions, challenges := f.count("access.sessions"), f.count("access.login_challenges"); sessions != 0 || challenges != 0 {
		t.Fatalf("after they expire: %d sessions, %d challenges; want none", sessions, challenges)
	}
}

func TestOrgNameIsTheCallersOwn(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	other := access.Principal{TenantID: dbtest.NewTenant(t, f.svc.DB), UserID: uuid.New()}

	mine, errMine := f.svc.OrgName(f.ctx, admin)
	theirs, errTheirs := f.svc.OrgName(f.ctx, other)
	if errMine != nil || errTheirs != nil {
		t.Fatalf("OrgName: %v, %v", errMine, errTheirs)
	}
	if mine != "Acme" || theirs == "Acme" || theirs == "" {
		t.Fatalf("OrgName = %q and %q; each person must get only their own organization", mine, theirs)
	}
}

func TestOrgNameFollowsSettings(t *testing.T) {
	f := newFixture(t)
	f.setup()
	admin, _ := f.svc.Authenticate(f.ctx, f.signIn().Token)
	renamed := "Acme Industrial"
	if _, err := f.svc.UpdateSettings(f.ctx, admin, access.SettingsPatch{OrgName: &renamed}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.OrgName(f.ctx, admin); err != nil || got != renamed {
		t.Fatalf("OrgName = %q, %v; want %q", got, err, renamed)
	}
}
