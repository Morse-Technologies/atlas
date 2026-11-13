package main

import (
	"encoding/base32"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/morse/atlas/server/internal/access"
)

const (
	adminEmail     = "perf@acme.test"
	adminPassword  = "a-long-password-for-perf"
	personPassword = "a-long-password-for-perf-people"
	stateMode      = 0o600
	// An account without a second factor takes any code at the second sign-in step, as the browser journey does.
	noSecondFactorCode = "000000"
	// Sign-in is limited to 10 a minute per address (loginsPerWindow in server/internal/httpapi/ratelimit.go): one
	// sign-in every 6.5 s stays under it, which is why setup takes about six minutes for 50 people.
	signInGap = 6500 * time.Millisecond
	// The most people any scenario of perf.sh uses at once (-users of the biggest one).
	defaultPeople = 50
)

// state is what `run` needs from `setup`: the administrator and one signed-in person per simulated user.
type state struct {
	URL     string   `json:"url"`
	UserID  string   `json:"user_id"`
	Session string   `json:"session"`
	People  []person `json:"people"`
}

// person is a viewer created by setup, with the session of their own.
type person struct {
	UserID  string `json:"user_id"`
	Session string `json:"session"`
}

// readerIDs lists the ids of everyone who may read: the administrator and every person, separated by commas.
func (st state) readerIDs() string {
	ids := []string{st.UserID}
	for _, p := range st.People {
		ids = append(ids, p.UserID)
	}
	return strings.Join(ids, ",")
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
	path := fs.String("state", "perf-state.json", "where to write the sessions")
	people := fs.Int("people", defaultPeople, "viewers to create and sign in, one per simulated user")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := createAdmin(*base)
	if err != nil {
		return err
	}
	admin, err := newClient(st.URL, st.Session)
	if err != nil {
		return err
	}
	if st.People, err = createPeople(admin, *people); err != nil {
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
	if err := c.signInWith(credentials{adminEmail, adminPassword, access.CodeAt(secret, time.Now())}); err != nil {
		return state{}, err
	}
	id, err := c.myID()
	return state{URL: c.base, UserID: id, Session: c.session()}, err
}

// credentials are the three things a sign-in asks for.
type credentials struct{ email, password, code string }

// signInWith runs both sign-in steps; the client keeps the session cookie.
func (c *client) signInWith(cred credentials) error {
	var challenge struct {
		ChallengeID string `json:"challenge_id"`
	}
	login := map[string]string{"email": cred.email, "password": cred.password}
	if err := c.json(jsonCall{method: http.MethodPost, path: "/auth/login", body: login, out: &challenge}); err != nil {
		return err
	}
	verify := map[string]string{"challenge_id": challenge.ChallengeID, "code": cred.code}
	return c.json(jsonCall{method: http.MethodPost, path: "/auth/verify", body: verify})
}

func (c *client) myID() (string, error) {
	var me struct {
		ID string `json:"id"`
	}
	err := c.json(jsonCall{method: http.MethodGet, path: "/me", out: &me})
	return me.ID, err
}

// createPeople adds n viewers as the administrator and signs each in, with a pause between sign-ins.
func createPeople(admin *client, n int) ([]person, error) {
	people := make([]person, 0, n)
	for i := 1; i <= n; i++ {
		p, err := admin.addPerson(i)
		if err != nil {
			return nil, fmt.Errorf("person %d of %d: %w", i, n, err)
		}
		people = append(people, p)
	}
	return people, nil
}

// addPerson creates viewer number n, who has no second factor, and signs them in with a client of their own.
func (c *client) addPerson(n int) (person, error) {
	email := fmt.Sprintf("perf-%02d@acme.test", n)
	body := map[string]any{"name": fmt.Sprintf("Perf Person %02d", n), "email": email, "roles": []string{"viewer"}, "second_factor": false}
	var created struct {
		Password string `json:"password"`
	}
	if err := c.json(jsonCall{method: http.MethodPost, path: "/users", body: body, out: &created}); err != nil {
		return person{}, err
	}
	time.Sleep(signInGap)
	return signInPerson(c.base, credentials{email, created.Password, noSecondFactorCode})
}

// signInPerson signs a new person in and changes the one-time password, which a first sign-in demands.
func signInPerson(base string, cred credentials) (person, error) {
	own, err := newClient(base, "")
	if err != nil {
		return person{}, err
	}
	if err := own.signInWith(cred); err != nil {
		return person{}, err
	}
	change := map[string]string{"current_password": cred.password, "new_password": personPassword}
	if err := own.json(jsonCall{method: http.MethodPut, path: "/me/password", body: change}); err != nil {
		return person{}, err
	}
	id, err := own.myID()
	return person{UserID: id, Session: own.session()}, err
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
