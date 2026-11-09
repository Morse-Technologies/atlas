package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
)

// Console administration of accounts and of the audit log. There is no e-mail recovery on-prem
// (ATLAS.md §13.1): whoever runs the server creates accounts and recovers access here.

const listLimit = 500

var userCommands = map[string]func(context.Context, cliSession, []string) error{
	"add":            userAdd,
	"list":           userList,
	"reset-password": userResetPassword,
	"reset-totp":     userResetTOTP,
}

// runUser: atlas user add --name N --email E --roles a,b [--no-totp] | list | reset-password EMAIL | reset-totp EMAIL
func runUser(ctx context.Context, args []string) error {
	return runSubcommand(ctx, subcommandCall{args: args, table: userCommands})
}

var auditCommands = map[string]func(context.Context, cliSession, []string) error{
	"verify": auditVerify,
}

// runAudit: atlas audit verify
func runAudit(ctx context.Context, args []string) error {
	return runSubcommand(ctx, subcommandCall{args: args, table: auditCommands})
}

func userAdd(ctx context.Context, s cliSession, args []string) error {
	flags := flag.NewFlagSet("user add", flag.ContinueOnError)
	name := flags.String("name", "", "full name")
	email := flags.String("email", "", "e-mail address, used to sign in")
	roles := flags.String("roles", string(access.RoleViewer), "comma separated roles")
	noTOTP := flags.Bool("no-totp", false, "no second factor (only for people who just ask questions)")
	if err := flags.Parse(args); err != nil || *name == "" || *email == "" {
		return errors.New("usage: atlas user add --name NAME --email EMAIL [--roles viewer,curator] [--no-totp]")
	}
	accounts, err := newAccounts(s.db)
	if err != nil {
		return err
	}
	creds, err := accounts.CreateUser(ctx, s.tenantID, access.NewUser{Name: *name, Email: *email, Roles: parseRoles(*roles), WithoutTOTP: *noTOTP})
	if err != nil {
		return err
	}
	printCredentials(creds)
	return nil
}

func parseRoles(list string) []access.Role {
	var roles []access.Role
	for _, name := range strings.Split(list, ",") {
		if name = strings.TrimSpace(name); name != "" {
			roles = append(roles, access.Role(name))
		}
	}
	return roles
}

func userResetPassword(ctx context.Context, s cliSession, args []string) error {
	email, err := singleArg(args, "usage: atlas user reset-password EMAIL")
	if err != nil {
		return err
	}
	accounts, err := newAccounts(s.db)
	if err != nil {
		return err
	}
	creds, err := accounts.ResetPassword(ctx, s.tenantID, email)
	if err == nil {
		printCredentials(creds)
	}
	return err
}

func userResetTOTP(ctx context.Context, s cliSession, args []string) error {
	email, err := singleArg(args, "usage: atlas user reset-totp EMAIL")
	if err != nil {
		return err
	}
	accounts, err := newAccounts(s.db)
	if err != nil {
		return err
	}
	creds, err := accounts.ResetTOTP(ctx, s.tenantID, email)
	if err == nil {
		printCredentials(creds)
	}
	return err
}

func userList(ctx context.Context, s cliSession, _ []string) error {
	accounts, err := newAccounts(s.db)
	if err != nil {
		return err
	}
	users, _, err := accounts.ListUsers(ctx, access.Principal{TenantID: s.tenantID}, access.Page{Size: listLimit})
	if err != nil {
		return err
	}
	rows := make([][]string, len(users))
	for i, u := range users {
		rows[i] = []string{u.Email, u.Name, joinedRoles(u.Roles), timeOrDash(u.LastActiveAt)}
	}
	return printTable([]string{"EMAIL", "NAME", "ROLES", "LAST ACTIVE"}, rows)
}

func joinedRoles(roles []access.Role) string {
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = string(r)
	}
	return strings.Join(names, ",")
}

// printCredentials shows secrets once, on the console of whoever runs the command.
func printCredentials(c access.Credentials) {
	if c.Password != "" {
		fmt.Println("Password (shown once):", c.Password)
	}
	if c.TOTPURI != "" {
		fmt.Println("Second factor (add it to an authenticator app):", c.TOTPURI)
	}
}

func singleArg(args []string, usage string) (string, error) {
	if len(args) != 1 {
		return "", errors.New(usage)
	}
	return args[0], nil
}

// auditVerify walks the hash chain and fails if it is broken, so it can run from a scheduler.
func auditVerify(ctx context.Context, s cliSession, _ []string) error {
	result, err := (&audit.Service{DB: s.db}).Verify(ctx, s.tenantID)
	if err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("the audit chain is broken at event %d (%d events checked before it)", *result.BrokenAt, result.Checked)
	}
	fmt.Printf("audit chain intact: %d events checked\n", result.Checked)
	return nil
}
