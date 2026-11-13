package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	percent      = 100
	recallEvery  = 10 // one answer in this many is checked against its topic
	askShare     = 70 // percent of a mixed load that asks questions
	listShare    = 90 // asks and lists together, in percent
	listPageSize = 50 // what the app asks for
	deepPages    = 5  // pages followed by the deep listing
	httpOK       = http.StatusOK
	listKinds    = 3
	// The server admits 20 questions a minute per person (asksPerWindow in server/internal/httpapi/ratelimit.go).
	// One every 3.5 s is about 17 a minute: the fastest pace a person is allowed, with room for the window edge.
	askPause = 3500 * time.Millisecond
	// What a run says, and fails with, when the server refused questions: its figures do not measure Atlas.
	refusedWith429 = "questions were refused with 429: this run is not valid"
)

type question struct {
	Text  string `json:"text"`
	Topic string `json:"topic"`
}

func readQuestions(path string) ([]question, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the operator's own file
	if err != nil {
		return nil, err
	}
	var out []question
	return out, json.Unmarshal(raw, &out)
}

// load is one scenario running: who, for how long, and what has been measured.
type load struct {
	clients     []*client // one per simulated person, each signed in as a different account
	questions   []question
	users       int
	duration    time.Duration
	samples     map[string]*sample
	checked     atomic.Int64
	hits        atomic.Int64
	rateLimited atomic.Int64
}

// visitor is one simulated person: their own session, their own random choices and the moment they may ask next.
type visitor struct {
	c       *client
	rng     *rand.Rand
	nextAsk time.Time
}

func newLoad(clients []*client, questions []question) *load {
	l := &load{clients: clients, questions: questions, samples: map[string]*sample{}}
	for _, kind := range []string{"ask first sources", "ask first word", "ask complete", "list documents", "filter documents", "page deep", "overview"} {
		l.samples[kind] = &sample{}
	}
	return l
}

// run starts the users and lets each repeat step until the time is up.
func (l *load) run(step func(*visitor)) result {
	var wg sync.WaitGroup
	started := time.Now()
	deadline := started.Add(l.duration)
	for i := range l.users {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			v := &visitor{c: l.clients[seed-1], rng: rand.New(rand.NewPCG(seed, seed+1))}
			for time.Now().Before(deadline) {
				step(v)
			}
		}(uint64(i + 1))
	}
	wg.Wait()
	return result{Users: l.users, Duration: time.Since(started), Samples: l.nonEmpty()}
}

func (l *load) nonEmpty() map[string]*sample {
	out := map[string]*sample{}
	for k, s := range l.samples {
		if s.count()+s.errors > 0 {
			out[k] = s
		}
	}
	return out
}

func (l *load) ask(v *visitor) {
	v.waitForTurn()
	q := l.questions[v.rng.IntN(len(l.questions))]
	t, err := v.c.ask(q.Text)
	if errors.Is(err, errRateLimited) {
		l.rateLimited.Add(1)
	}
	l.samples["ask first sources"].add(t.Meta, err)
	l.samples["ask first word"].add(t.FirstToken, err)
	l.samples["ask complete"].add(t.Done, err)
	if err == nil && t.Outcome == "answered" && v.rng.IntN(recallEvery) == 0 {
		l.checkTopic(v, q, t)
	}
}

// waitForTurn holds a person back until they may ask again, so none asks faster than the server allows.
func (v *visitor) waitForTurn() {
	time.Sleep(time.Until(v.nextAsk))
	v.nextAsk = time.Now().Add(askPause)
}

// checkTopic asks for the title of the first cited document: it must be about the question's topic.
func (l *load) checkTopic(v *visitor, q question, t timings) {
	status, body, _, err := v.c.call(http.MethodGet, "/documents/"+t.Cited[0], nil)
	if err != nil || status != httpOK {
		return
	}
	l.checked.Add(1)
	if strings.Contains(string(body), q.Topic) {
		l.hits.Add(1)
	}
}

func (l *load) listFirstPage(v *visitor) {
	status, _, d, err := v.c.call(http.MethodGet, fmt.Sprintf("/documents?limit=%d", listPageSize), nil)
	l.samples["list documents"].add(d, statusErr(status, err))
}

func (l *load) filter(v *visitor) {
	word := fmt.Sprintf("tema%03d", v.rng.IntN(topics))
	status, _, d, err := v.c.call(http.MethodGet, fmt.Sprintf("/documents?limit=%d&q=%s", listPageSize, word), nil)
	l.samples["filter documents"].add(d, statusErr(status, err))
}

const topics = 200

func (l *load) deepPage(v *visitor) {
	cursor := ""
	started := time.Now()
	var err error
	for range deepPages {
		cursor, err = v.c.nextPage(cursor)
		if err != nil || cursor == "" {
			break
		}
	}
	l.samples["page deep"].add(time.Since(started), err)
}

func (l *load) overview(v *visitor) {
	status, _, d, err := v.c.call(http.MethodGet, "/stats/overview", nil)
	l.samples["overview"].add(d, statusErr(status, err))
}

func statusErr(status int, err error) error {
	if err == nil && status != httpOK {
		return fmt.Errorf("status %d", status)
	}
	return err
}

// mixed is what a team does: mostly asks, some browsing, the odd look at the panel.
func (l *load) mixed(v *visitor) {
	switch roll := v.rng.IntN(percent); {
	case roll < askShare:
		l.ask(v)
	case roll < listShare:
		if v.rng.IntN(2) == 0 {
			l.listFirstPage(v)
		} else {
			l.filter(v)
		}
	default:
		l.overview(v)
	}
}

func (l *load) notes() []string {
	notes := []string{fmt.Sprintf("each person asks at most once every %s (the server allows 20 a minute)", askPause)}
	if refused := l.rateLimited.Load(); refused > 0 {
		notes = append(notes, fmt.Sprintf("%d %s", refused, refusedWith429))
	}
	if l.checked.Load() > 0 {
		notes = append(notes, fmt.Sprintf("recall of the first cited document: %d of %d checked answers were about the question's topic",
			l.hits.Load(), l.checked.Load()))
	}
	return notes
}

// browse is a person going through the documents: the first page, a filter, and now and then far down the list.
func (l *load) browse(v *visitor) {
	switch v.rng.IntN(listKinds) {
	case 0:
		l.listFirstPage(v)
	case 1:
		l.filter(v)
	default:
		l.deepPage(v)
	}
}
