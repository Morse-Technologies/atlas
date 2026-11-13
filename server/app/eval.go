package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/search"
)

const (
	percentFactor = 100
	pathMatchSize = 10 // documents read to resolve one path; more than one match is an error anyway
)

var errNoQuestions = errors.New("the file has no questions")

// evalOptions are the flags of `atlas eval`.
type evalOptions struct {
	file, as string
	asJSON   bool
}

// runEval: atlas eval --file QUESTIONS.jsonl --as EMAIL [--json]. It asks every question as that person, without
// storing anything, prints how well the answers are backed and fails (exit 1) below the bar.
func runEval(ctx context.Context, args []string) error {
	opts, err := parseEvalOptions(args)
	if err != nil {
		return err
	}
	report, err := evaluateFile(ctx, opts)
	if err != nil {
		return err
	}
	if err := printEvalReport(report, opts.asJSON); err != nil {
		return err
	}
	return belowTheBar(report)
}

func evaluateFile(ctx context.Context, opts evalOptions) (search.EvalReport, error) {
	cases, err := readEvalCases(opts.file)
	if err != nil {
		return search.EvalReport{}, err
	}
	ctx, session, err := openCLISession(ctx)
	if err != nil {
		return search.EvalReport{}, err
	}
	defer session.db.Pool.Close()
	return evaluate(ctx, session, evalRun{as: opts.as, cases: cases})
}

func parseEvalOptions(args []string) (evalOptions, error) {
	flags := flag.NewFlagSet("eval", flag.ContinueOnError)
	file := flags.String("file", "", "JSON lines: {\"question\": \"...\", \"expected\": {\"code\": \"PNT-014\", \"page\": 4} or null}")
	as := flags.String("as", "", "e-mail of the person who asks; only what they may read can be cited")
	asJSON := flags.Bool("json", false, "print the report as JSON")
	if err := flags.Parse(args); err != nil || *file == "" || *as == "" {
		return evalOptions{}, errors.New("usage: atlas eval --file QUESTIONS.jsonl --as EMAIL [--json]")
	}
	return evalOptions{file: *file, as: *as, asJSON: *asJSON}, nil
}

func readEvalCases(path string) ([]search.EvalCase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read questions: %w", err)
	}
	defer func() { _ = file.Close() }()
	return parseEvalCases(file)
}

// parseEvalCases reads one JSON object per line; blank lines are skipped and an unknown field is an error.
func parseEvalCases(r io.Reader) ([]search.EvalCase, error) {
	var cases []search.EvalCase
	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		c, err := parseEvalCase(text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		cases = append(cases, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read questions: %w", err)
	}
	if len(cases) == 0 {
		return nil, errNoQuestions
	}
	return cases, nil
}

func parseEvalCase(text string) (search.EvalCase, error) {
	var c search.EvalCase
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("not a question: %w", err)
	}
	return c, checkEvalCase(c)
}

func checkEvalCase(c search.EvalCase) error {
	if strings.TrimSpace(c.Question) == "" {
		return errors.New("the question is empty")
	}
	e := c.Expected
	switch {
	case e == nil:
		return nil
	case (e.Code == "") == (e.Path == ""):
		return errors.New(`"expected" needs exactly one of "code" or "path"`)
	case e.Page < 0:
		return errors.New(`"page" cannot be negative`)
	}
	return nil
}

// evalRun is what one evaluation needs besides the installation: who asks and what.
type evalRun struct {
	as    string
	cases []search.EvalCase
}

func evaluate(ctx context.Context, s cliSession, run evalRun) (search.EvalReport, error) {
	who, err := evalActor(ctx, s, run.as)
	if err != nil {
		return search.EvalReport{}, err
	}
	service, _, err := newSearch(s.db)
	if err != nil {
		return search.EvalReport{}, err
	}
	e := evaluator{search: service, documents: documents.NewLibrary(s.db, nil), who: who}
	if err := e.resolvePaths(ctx, run.cases); err != nil {
		return search.EvalReport{}, err
	}
	trials, err := e.tryAll(ctx, run.cases)
	if err != nil {
		return search.EvalReport{}, err
	}
	return search.Score(run.cases, trials), nil
}

func evalActor(ctx context.Context, s cliSession, email string) (search.Actor, error) {
	accounts, err := newAccounts(s.db)
	if err != nil {
		return search.Actor{}, err
	}
	id, err := accounts.UserIDByEmail(ctx, s.tenantID, email)
	return search.Actor{TenantID: s.tenantID, UserID: id}, err
}

// evaluator asks the questions as one person.
type evaluator struct {
	search    *search.Service
	documents *documents.Library
	who       search.Actor
}

func (e evaluator) tryAll(ctx context.Context, cases []search.EvalCase) ([]search.Trial, error) {
	trials := make([]search.Trial, len(cases))
	for i, c := range cases {
		trial, err := e.search.Trial(ctx, search.Question{Who: e.who, Text: c.Question})
		if err != nil {
			return nil, fmt.Errorf("question %q: %w", c.Question, err)
		}
		trials[i] = trial
	}
	return trials, nil
}

// resolvePaths finds the document each expected path names, among those the person may read.
func (e evaluator) resolvePaths(ctx context.Context, cases []search.EvalCase) error {
	for _, c := range cases {
		if c.Expected == nil || c.Expected.Path == "" {
			continue
		}
		id, err := e.documentAt(ctx, c.Expected.Path)
		if err != nil {
			return err
		}
		c.Expected.DocumentID = id
	}
	return nil
}

// documentAt finds the one document whose path is, or ends with, path.
func (e evaluator) documentAt(ctx context.Context, path string) (uuid.UUID, error) {
	who := documents.Actor{TenantID: e.who.TenantID, UserID: e.who.UserID}
	found, _, err := e.documents.List(ctx, who, documents.Listing{Query: path, Status: "all", Size: pathMatchSize})
	if err != nil {
		return uuid.Nil, fmt.Errorf("look for %q: %w", path, err)
	}
	var matches []uuid.UUID
	for _, d := range found {
		if d.Path == path || strings.HasSuffix(d.Path, "/"+path) {
			matches = append(matches, d.ID)
		}
	}
	if len(matches) != 1 {
		return uuid.Nil, fmt.Errorf("the path %q matches %d documents the person can read; it must match exactly one", path, len(matches))
	}
	return matches[0], nil
}

func printEvalReport(r search.EvalReport, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(r)
	}
	rows := make([][]string, len(r.Rows))
	for i, row := range r.Rows {
		rows[i] = []string{row.Question, string(row.Verdict), fmt.Sprintf("%.2f", row.TopScore)}
	}
	if err := printTable([]string{"QUESTION", "VERDICT", "TOP SCORE"}, rows); err != nil {
		return err
	}
	fmt.Printf("\nAnswers with the right citation: %s (needs %s)\n", percent(r.Precision), percent(search.MinPrecision))
	fmt.Printf("Answers without a source:        %s (must stay under %s)\n", percent(r.Unsupported), percent(search.MaxUnsupported))
	fmt.Println(proposedThresholdLine(r.ProposedThreshold))
	return nil
}

func percent(share float64) string { return fmt.Sprintf("%.1f%%", share*percentFactor) }

func proposedThresholdLine(threshold float64) string {
	if threshold == 0 {
		return "Proposed not-found threshold: none (no reranker took part)"
	}
	return fmt.Sprintf("Proposed not-found threshold: %.2f (set it in Settings)", threshold)
}

// belowTheBar is the exit status of an evaluation: an error when the answers are not good enough to trust.
func belowTheBar(r search.EvalReport) error {
	if r.Passes() {
		return nil
	}
	return fmt.Errorf("the answers are below the bar: %s with the right citation (needs %s) and %s without a source (must stay under %s)",
		percent(r.Precision), percent(search.MinPrecision), percent(r.Unsupported), percent(search.MaxUnsupported))
}
