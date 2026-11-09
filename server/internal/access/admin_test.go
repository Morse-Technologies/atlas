package access

import (
	"errors"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
)

func TestAnAccountNeedsANameAndAnEmail(t *testing.T) {
	for _, in := range []NewUser{{Email: "a@b.test", Roles: []Role{RoleViewer}}, {Name: "Ada", Email: "nobody", Roles: []Role{RoleViewer}}} {
		if err := validateNewUser(in); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("%+v: err = %v, want ErrInvalid", in, err)
		}
	}
}

func TestOnlyPeopleWhoJustAskCanSkipTheSecondFactor(t *testing.T) {
	skip := func(roles ...Role) error {
		return validateNewUser(NewUser{Name: "Ada", Email: "a@b.test", Roles: roles, WithoutTOTP: true})
	}
	if err := skip(RoleViewer); err != nil {
		t.Fatalf("a viewer may skip it: %v", err)
	}
	for _, r := range []Role{RoleAdmin, RoleAuditor, RoleCurator} {
		if err := skip(RoleViewer, r); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("%s must not skip it: %v", r, err)
		}
	}
}

func TestUnknownOrMissingRolesAreRefused(t *testing.T) {
	for _, roles := range [][]Role{nil, {"root"}} {
		if err := validateNewUser(NewUser{Name: "Ada", Email: "a@b.test", Roles: roles}); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("roles %v: err = %v", roles, err)
		}
	}
}

func TestGeneratedPasswordsAreLongRandomAndVerifiable(t *testing.T) {
	a, hashA, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := newPassword()
	if len(a) < minPasswordLen || a == b || !VerifyPassword(hashA, a) {
		t.Fatalf("password %q (len %d): too short, repeated, or not matching its hash", a, len(a))
	}
}
