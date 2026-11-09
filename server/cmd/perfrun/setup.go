package main

import (
	"encoding/base32"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/morse/atlas/server/internal/access"
)

const (
	adminEmail    = "perf@acme.test"
	adminPassword = "a-long-password-for-perf"
	stateMode     = 0o600
)

// state is what `run` needs from `setup`.
type state struct {
	URL     string `json:"url"`
	UserID  string `json:"user_id"`
	Session string `json:"session"`
}

func readState(path string) (state, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the operator's own file
	if err != nil {
		return state{}, fmt.Errorf("%w (run `perfrun setup` first)", err)
	}
	var st state
	return st, json.Unmarshal(raw, &st)
}

func setupCommand(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	base := fs.String("url", "https://localhost", "address of Atlas")
	path := fs.String("state", "perf-state.json", "where to write the session")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := createAdmin(*base)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(st)
	return os.WriteFile(*path, raw, stateMode)
}

// createAdmin sets the organization up on an empty installation and signs the administrator in.
func createAdmin(base string) (state, error) {
	c, err := newClient(base, "")
	if err != nil {
		return state{}, err
	}
	var created struct {
		TOTPURI string `json:"totp_uri"`
	}
	body := map[string]string{"org_name": "Acme", "name": "Perf", "email": adminEmail, "password": adminPassword}
	if err := c.json(jsonCall{method: http.MethodPost, path: "/setup", body: body, out: &created}); err != nil {
		return state{}, err
	}
	secret, err := secretOf(created.TOTPURI)
	if err != nil {
		return state{}, err
	}
	return signIn(c, secret)
}

func signIn(c *client, secret []byte) (state, error) {
	var challenge struct {
		ChallengeID string `json:"challenge_id"`
	}
	if err := c.json(jsonCall{method: http.MethodPost, path: "/auth/login", body: map[string]string{"email": adminEmail, "password": adminPassword}, out: &challenge}); err != nil {
		return state{}, err
	}
	code := access.CodeAt(secret, time.Now())
	if err := c.json(jsonCall{method: http.MethodPost, path: "/auth/verify", body: map[string]string{"challenge_id": challenge.ChallengeID, "code": code}}); err != nil {
		return state{}, err
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := c.json(jsonCall{method: http.MethodGet, path: "/me", out: &me}); err != nil {
		return state{}, err
	}
	return state{URL: c.base, UserID: me.ID, Session: c.session()}, nil
}

// json sends a request, expects a success and decodes the body into out when it is given.
func (c *client) json(r jsonCall) error {
	status, data, _, err := c.call(r.method, r.path, r.body)
	if err != nil {
		return err
	}
	if status >= http.StatusBadRequest {
		return fmt.Errorf("%s %s: status %d: %s", r.method, r.path, status, data)
	}
	if r.out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, r.out)
}

// jsonCall is a request that must succeed, and where to put its answer.
type jsonCall struct {
	method, path string
	body, out    any
}

func (c *client) session() string {
	u, _ := url.Parse(c.base)
	for _, cookie := range c.http.Jar.Cookies(u) {
		if cookie.Name == "atlas_session" {
			return cookie.Value
		}
	}
	return ""
}

// secretOf reads the shared secret out of an otpauth:// address.
func secretOf(uri string) ([]byte, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(u.Query().Get("secret"))
}
