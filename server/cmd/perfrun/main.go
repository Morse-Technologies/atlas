// Command perfrun measures a running Atlas under load: many people asking questions, listing and filtering
// documents, at once. Run `setup` once on an empty installation: it creates the administrator and `-people` viewers
// (50 by default, one per simulated user) and signs each in, one every few seconds because sign-in is limited.
// Then `run` as often as needed: person i uses account i, and asks no faster than the server's per-person limit
// allows. `readers` prints the ids of everyone, for `perfseed -readers`. Times are as a browser sees them, through Caddy.
//
//	perfrun setup -url https://localhost -state perf-state.json -people 50
//	perfrun readers -state perf-state.json
//	perfrun run -url https://localhost -state perf-state.json -questions perf-questions.json -scenario ask -users 20 -duration 2m
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

const defaultUsers = 20

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "perfrun:", err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: perfrun setup|readers|run [flags]")
	}
	switch args[0] {
	case "setup":
		return setupCommand(args[1:])
	case "readers":
		return readersCommand(args[1:])
	case "run":
		return runCommand(args[1:])
	}
	return fmt.Errorf("unknown command %q", args[0])
}

type runFlags struct {
	url, state, questions, scenario string
	users                           int
	duration                        time.Duration
}

func parseRun(args []string) (runFlags, error) {
	var f runFlags
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.StringVar(&f.url, "url", "https://localhost", "address of Atlas")
	fs.StringVar(&f.state, "state", "perf-state.json", "file written by setup")
	fs.StringVar(&f.questions, "questions", "perf-questions.json", "file written by perfseed")
	fs.StringVar(&f.scenario, "scenario", "ask", "ask, documents, mixed")
	fs.IntVar(&f.users, "users", defaultUsers, "people at once")
	fs.DurationVar(&f.duration, "duration", time.Minute, "how long")
	return f, fs.Parse(args)
}

// readersCommand prints the ids of the administrator and of every person, separated by commas.
func readersCommand(args []string) error {
	fs := flag.NewFlagSet("readers", flag.ContinueOnError)
	path := fs.String("state", "perf-state.json", "file written by setup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := readState(*path)
	if err != nil {
		return err
	}
	fmt.Println(st.readerIDs())
	return nil
}

func runCommand(args []string) error {
	f, err := parseRun(args)
	if err != nil {
		return err
	}
	l, err := f.prepare()
	if err != nil {
		return err
	}
	res, err := l.scenario(f.scenario)
	if err != nil {
		return err
	}
	res.Notes = l.notes()
	res.print()
	if refused := l.rateLimited.Load(); refused > 0 {
		return fmt.Errorf("%d %s", refused, refusedWith429)
	}
	return nil
}

// prepare takes the saved session of each simulated person and loads the questions.
func (f runFlags) prepare() (*load, error) {
	st, err := readState(f.state)
	if err != nil {
		return nil, err
	}
	st.URL = f.url // the address given to run wins over the one setup saved
	clients, err := clientsFor(st, f.users)
	if err != nil {
		return nil, err
	}
	questions, err := readQuestions(f.questions)
	if err != nil {
		return nil, err
	}
	l := newLoad(clients, questions)
	l.users, l.duration = f.users, f.duration
	return l, nil
}

// clientsFor makes one client per simulated person, each with the session of a different account.
func clientsFor(st state, users int) ([]*client, error) {
	if users > len(st.People) {
		return nil, fmt.Errorf("setup created %d people; -users %d needs one each", len(st.People), users)
	}
	clients := make([]*client, users)
	for i := range clients {
		c, err := newClient(st.URL, st.People[i].Session)
		if err != nil {
			return nil, err
		}
		clients[i] = c
	}
	return clients, nil
}

// scenario runs one of the named loads.
func (l *load) scenario(name string) (result, error) {
	steps := map[string]func(*visitor){"ask": l.ask, "mixed": l.mixed, "documents": l.browse}
	step, ok := steps[name]
	if !ok {
		return result{}, fmt.Errorf("unknown scenario %q (ask, documents, mixed)", name)
	}
	res := l.run(step)
	res.Scenario = name
	return res, nil
}
