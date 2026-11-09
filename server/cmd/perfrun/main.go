// Command perfrun measures a running Atlas under load: many people asking questions, listing and filtering
// documents, at once. Run `setup` once on an empty installation (it creates the administrator and signs in), then
// `run` as often as needed. Times are as a browser sees them, through Caddy.
//
//	perfrun setup -url https://localhost -state perf-state.json
//	perfrun run -url https://localhost -state perf-state.json -questions perf-questions.json -scenario ask -users 20 -duration 2m
package main

import (
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
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
		return errors.New("usage: perfrun setup|run [flags]")
	}
	switch args[0] {
	case "setup":
		return setupCommand(args[1:])
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
	return nil
}

// prepare signs in with the saved session and loads the questions.
func (f runFlags) prepare() (*load, error) {
	st, err := readState(f.state)
	if err != nil {
		return nil, err
	}
	c, err := newClient(f.url, st.Session)
	if err != nil {
		return nil, err
	}
	questions, err := readQuestions(f.questions)
	if err != nil {
		return nil, err
	}
	l := newLoad(c, questions)
	l.users, l.duration = f.users, f.duration
	return l, nil
}

// scenario runs one of the named loads.
func (l *load) scenario(name string) (result, error) {
	steps := map[string]func(*rand.Rand){"ask": l.ask, "mixed": l.mixed, "documents": l.browse}
	step, ok := steps[name]
	if !ok {
		return result{}, fmt.Errorf("unknown scenario %q (ask, documents, mixed)", name)
	}
	res := l.run(step)
	res.Scenario = name
	return res, nil
}
