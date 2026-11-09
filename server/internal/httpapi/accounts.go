package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// Accounts is what the HTTP layer needs from the access module. *access.Service implements it.
type Accounts interface {
	SetupDone(ctx context.Context) (bool, error)
	Setup(ctx context.Context, in access.SetupInput) (string, error)
	Login(ctx context.Context, email, password string) (access.Challenge, error)
	Verify(ctx context.Context, challengeID, code string) (access.SessionTicket, error)
	Authenticate(ctx context.Context, token string) (access.Principal, error)
	Logout(ctx context.Context, p access.Principal) error
	ChangePassword(ctx context.Context, p access.Principal, in access.PasswordChange) error
	GetUser(ctx context.Context, p access.Principal, id uuid.UUID) (access.User, error)
	ListUsers(ctx context.Context, p access.Principal, page access.Page) ([]access.User, uuid.UUID, error)
	UpdateRoles(ctx context.Context, p access.Principal, change access.RoleChange) (access.User, error)
	DeleteUser(ctx context.Context, p access.Principal, id uuid.UUID) error
	Settings(ctx context.Context, p access.Principal) (access.Settings, error)
	UpdateSettings(ctx context.Context, p access.Principal, patch access.SettingsPatch) (access.Settings, error)
}

// GetSetupStatus tells the UI whether /setup is still open.
func (s *Server) GetSetupStatus(ctx context.Context, _ GetSetupStatusRequestObject) (GetSetupStatusResponseObject, error) {
	done, err := s.Accounts.SetupDone(ctx)
	return GetSetupStatus200JSONResponse{SetupDone: done}, err
}

// Setup creates the organization and its first administrator.
func (s *Server) Setup(ctx context.Context, req SetupRequestObject) (SetupResponseObject, error) {
	b := req.Body
	uri, err := s.Accounts.Setup(ctx, access.SetupInput{OrgName: b.OrgName, Name: b.Name, Email: string(b.Email), Password: b.Password})
	if err != nil {
		return nil, err
	}
	return Setup201JSONResponse{TotpUri: uri}, nil
}

// Login is the first sign-in step: email and password.
func (s *Server) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	ch, err := s.Accounts.Login(ctx, req.Body.Email, req.Body.Password)
	if err != nil {
		return nil, err
	}
	return Login200JSONResponse{ChallengeId: ch.ID, NeedsCode: ch.NeedsCode}, nil
}

// VerifyLogin is the second step: the code from the authenticator app. It sets the session cookie.
func (s *Server) VerifyLogin(ctx context.Context, req VerifyLoginRequestObject) (VerifyLoginResponseObject, error) {
	code := ""
	if req.Body.Code != nil {
		code = *req.Body.Code
	}
	ticket, err := s.Accounts.Verify(ctx, req.Body.ChallengeId, code)
	if err != nil {
		return nil, err
	}
	cookie := sessionCookieValue(ticket)
	return VerifyLogin204Response{Headers: VerifyLogin204ResponseHeaders{SetCookie: &cookie}}, nil
}

// Logout ends the session and clears the cookie.
func (s *Server) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	if err := s.Accounts.Logout(ctx, principalFrom(ctx)); err != nil {
		return nil, err
	}
	cookie := clearedCookieValue()
	return Logout204Response{Headers: Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

// GetMe returns the signed-in person.
func (s *Server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	p := principalFrom(ctx)
	user, err := s.Accounts.GetUser(ctx, p, p.UserID)
	if err != nil {
		return nil, err
	}
	return GetMe200JSONResponse(toPerson(user)), nil
}

// DeleteMe erases the signed-in account.
func (s *Server) DeleteMe(ctx context.Context, _ DeleteMeRequestObject) (DeleteMeResponseObject, error) {
	p := principalFrom(ctx)
	if err := s.Accounts.DeleteUser(ctx, p, p.UserID); err != nil {
		return nil, err
	}
	return DeleteMe204Response{}, nil
}

// ListUsers returns one page of accounts.
func (s *Server) ListUsers(ctx context.Context, req ListUsersRequestObject) (ListUsersResponseObject, error) {
	page, err := pageFrom(req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	users, next, err := s.Accounts.ListUsers(ctx, principalFrom(ctx), page)
	if err != nil {
		return nil, err
	}
	people := make([]Person, len(users))
	for i, u := range users {
		people[i] = toPerson(u)
	}
	return ListUsers200JSONResponse{Body: people, Headers: ListUsers200ResponseHeaders{NextCursor: cursorOf(next)}}, nil
}

// DeleteUser erases an account.
func (s *Server) DeleteUser(ctx context.Context, req DeleteUserRequestObject) (DeleteUserResponseObject, error) {
	if err := s.Accounts.DeleteUser(ctx, principalFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return DeleteUser204Response{}, nil
}

// UpdateRoles replaces the roles of an account.
func (s *Server) UpdateRoles(ctx context.Context, req UpdateRolesRequestObject) (UpdateRolesResponseObject, error) {
	roles := make([]access.Role, len(req.Body.Roles))
	for i, r := range req.Body.Roles {
		roles[i] = access.Role(r)
	}
	user, err := s.Accounts.UpdateRoles(ctx, principalFrom(ctx), access.RoleChange{UserID: req.Id, Roles: roles})
	if err != nil {
		return nil, err
	}
	return UpdateRoles200JSONResponse(toPerson(user)), nil
}

func toPerson(u access.User) Person {
	roles := make([]Role, len(u.Roles))
	for i, r := range u.Roles {
		roles[i] = Role(r)
	}
	return Person{Id: u.ID, Name: u.Name, Email: u.Email, Department: u.Department, Roles: roles,
		IsActive: u.IsActive, LastActiveAt: u.LastActiveAt, Hue: u.Hue}
}

// pageSize clamps the requested page size to the contract.
func pageSize(limit *Limit) int32 {
	if limit == nil {
		return defaultPageSize
	}
	return int32(min(max(*limit, 1), maxPageSize))
}

// uuidCursor decodes a cursor that is the id of the last item seen (uuid.Nil: no cursor).
func uuidCursor(cursor *Cursor) (uuid.UUID, error) {
	if cursor == nil || *cursor == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(*cursor)
	if err != nil {
		return uuid.Nil, fmt.Errorf("cursor: %w", errs.ErrInvalid)
	}
	return id, nil
}

// pageFrom builds the users page: the cursor is the last id seen.
func pageFrom(limit *Limit, cursor *Cursor) (access.Page, error) {
	after, err := uuidCursor(cursor)
	return access.Page{After: after, Size: pageSize(limit)}, err
}

func cursorOf(next uuid.UUID) *string {
	if next == uuid.Nil {
		return nil
	}
	s := next.String()
	return &s
}

// ChangeMyPassword changes the signed-in person's password.
func (s *Server) ChangeMyPassword(ctx context.Context, req ChangeMyPasswordRequestObject) (ChangeMyPasswordResponseObject, error) {
	in := access.PasswordChange{Current: req.Body.CurrentPassword, New: req.Body.NewPassword}
	if err := s.Accounts.ChangePassword(ctx, principalFrom(ctx), in); err != nil {
		return nil, err
	}
	return ChangeMyPassword204Response{}, nil
}
