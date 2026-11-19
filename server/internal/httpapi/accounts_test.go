package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// recordingAccounts remembers what the handlers asked of the access module.
type recordingAccounts struct {
	Accounts
	by          access.By
	newUser     access.NewUser
	deleted     uuid.UUID
	roleChange  access.RoleChange
	nextCursor  uuid.UUID
	credentials access.Credentials
	failWith    error
}

func (r *recordingAccounts) CreateUser(_ context.Context, by access.By, in access.NewUser) (access.Credentials, error) {
	r.by, r.newUser = by, in
	return r.credentials, r.failWith
}

func (r *recordingAccounts) GetUser(_ context.Context, _ access.Principal, id uuid.UUID) (access.User, error) {
	return access.User{ID: id, Name: "Grace", Roles: r.newUser.Roles}, nil
}

func (r *recordingAccounts) DeleteUser(_ context.Context, _ access.Principal, id uuid.UUID) error {
	r.deleted = id
	return r.failWith
}

func (r *recordingAccounts) UpdateRoles(_ context.Context, _ access.Principal, change access.RoleChange) (access.User, error) {
	r.roleChange = change
	return access.User{ID: change.UserID, Roles: change.Roles}, r.failWith
}

func (r *recordingAccounts) ListUsers(context.Context, access.Principal, access.Page) ([]access.User, uuid.UUID, error) {
	return []access.User{{ID: uuid.New(), Name: "Ada"}}, r.nextCursor, nil
}

func (r *recordingAccounts) ResetPassword(_ context.Context, by access.By, _ uuid.UUID) (access.Credentials, error) {
	r.by = by
	return r.credentials, r.failWith
}

func (r *recordingAccounts) ResetTOTP(context.Context, access.By, uuid.UUID) (access.Credentials, error) {
	return r.credentials, r.failWith
}

func adminServer(accounts *recordingAccounts) (*Server, context.Context, access.Principal) {
	admin := access.Principal{UserID: uuid.New(), TenantID: uuid.New(), Roles: []access.Role{access.RoleAdmin}}
	return &Server{Accounts: accounts}, personContextOf(admin), admin
}

func personContextOf(p access.Principal) context.Context {
	return context.WithValue(context.Background(), principalKey{}, p)
}

func TestCreateUserNamesTheAdminAndShowsTheSecretsOnce(t *testing.T) {
	accounts := &recordingAccounts{credentials: access.Credentials{UserID: uuid.New(), Password: "one-time", TOTPURI: "otpauth://x"}}
	server, ctx, admin := adminServer(accounts)
	body := &UserCreate{Name: "Grace", Email: "g@x.org", Roles: []Role{"viewer"}, SecondFactor: false}

	res, err := server.CreateUser(ctx, CreateUserRequestObject{Body: body})

	created := res.(CreateUser201JSONResponse)
	if err != nil || created.Password != "one-time" || *created.TotpUri != "otpauth://x" {
		t.Fatalf("res=%+v err=%v", created, err)
	}
	if accounts.by != (access.By{TenantID: admin.TenantID, ActorID: admin.UserID}) {
		t.Errorf("the audit trail must name the admin: %+v", accounts.by)
	}
	if !accounts.newUser.WithoutTOTP || accounts.newUser.Roles[0] != access.RoleViewer {
		t.Errorf("new user not translated: %+v", accounts.newUser)
	}
}

func TestCreateUserWithoutASecondFactorHasNoAuthenticatorLink(t *testing.T) {
	server, ctx, _ := adminServer(&recordingAccounts{credentials: access.Credentials{Password: "p"}})

	res, _ := server.CreateUser(ctx, CreateUserRequestObject{Body: &UserCreate{Roles: []Role{"viewer"}}})

	if res.(CreateUser201JSONResponse).TotpUri != nil {
		t.Error("an account without second factor must not get an authenticator link")
	}
}

func TestCreateUserPassesOnTheError(t *testing.T) {
	server, ctx, _ := adminServer(&recordingAccounts{failWith: errs.ErrConflict})

	_, err := server.CreateUser(ctx, CreateUserRequestObject{Body: &UserCreate{}})

	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateRolesReplacesTheRolesOfThatAccount(t *testing.T) {
	accounts := &recordingAccounts{}
	server, ctx, _ := adminServer(accounts)
	target := uuid.New()

	res, err := server.UpdateRoles(ctx, UpdateRolesRequestObject{Id: target, Body: &RolesUpdate{Roles: []Role{"auditor"}}})

	person := Person(res.(UpdateRoles200JSONResponse))
	if err != nil || accounts.roleChange.UserID != target || person.Roles[0] != "auditor" {
		t.Fatalf("change=%+v person=%+v err=%v", accounts.roleChange, person, err)
	}
}

func TestDeleteUserAndDeleteMeEraseTheRightAccount(t *testing.T) {
	accounts := &recordingAccounts{}
	server, ctx, admin := adminServer(accounts)
	other := uuid.New()

	if _, err := server.DeleteUser(ctx, DeleteUserRequestObject{Id: other}); err != nil || accounts.deleted != other {
		t.Fatalf("DeleteUser erased %v, err %v", accounts.deleted, err)
	}
	if _, err := server.DeleteMe(ctx, DeleteMeRequestObject{}); err != nil || accounts.deleted != admin.UserID {
		t.Fatalf("DeleteMe erased %v, err %v", accounts.deleted, err)
	}
}

func TestResetsReturnOnlyTheSecretTheyChanged(t *testing.T) {
	accounts := &recordingAccounts{credentials: access.Credentials{Password: "new-pass", TOTPURI: "otpauth://new"}}
	server, ctx, _ := adminServer(accounts)

	password, _ := server.ResetUserPassword(ctx, ResetUserPasswordRequestObject{Id: uuid.New()})
	totp, _ := server.ResetUserTotp(ctx, ResetUserTotpRequestObject{Id: uuid.New()})

	if password.(ResetUserPassword200JSONResponse).Password != "new-pass" ||
		totp.(ResetUserTotp200JSONResponse).TotpUri != "otpauth://new" {
		t.Fatalf("password=%+v totp=%+v", password, totp)
	}
}

func TestListUsersGivesTheNextCursorOnlyWhenThereIsAnotherPage(t *testing.T) {
	accounts := &recordingAccounts{}
	server, ctx, _ := adminServer(accounts)

	last, _ := server.ListUsers(ctx, ListUsersRequestObject{})
	accounts.nextCursor = uuid.New()
	more, _ := server.ListUsers(ctx, ListUsersRequestObject{})

	if last.(ListUsers200JSONResponse).Headers.NextCursor != nil {
		t.Error("the last page must not carry a cursor")
	}
	if got := more.(ListUsers200JSONResponse).Headers.NextCursor; got == nil || *got != accounts.nextCursor.String() {
		t.Errorf("cursor = %v, want %v", got, accounts.nextCursor)
	}
}

func TestListUsersRefusesACursorThatIsNotAnId(t *testing.T) {
	server, ctx, _ := adminServer(&recordingAccounts{})
	bad := "not-a-uuid"

	_, err := server.ListUsers(ctx, ListUsersRequestObject{Params: ListUsersParams{Cursor: &bad}})

	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestPageSizeIsClampedToTheAllowedRange(t *testing.T) {
	tooSmall, tooBig, ok := -5, 10_000, 30
	cases := map[string]struct {
		limit *Limit
		want  int32
	}{
		"missing": {nil, defaultPageSize}, "below one": {&tooSmall, 1}, "above max": {&tooBig, maxPageSize}, "inside": {&ok, 30},
	}
	for name, c := range cases {
		if got := pageSize(c.limit); got != c.want {
			t.Errorf("%s: pageSize = %d, want %d", name, got, c.want)
		}
	}
}

func TestEmptyCursorMeansTheFirstPage(t *testing.T) {
	empty := ""
	if id, err := uuidCursor(&empty); id != uuid.Nil || err != nil {
		t.Fatalf("id=%v err=%v", id, err)
	}
}
