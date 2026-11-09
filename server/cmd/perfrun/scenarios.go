package main

import (
	"encoding/json"
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
	c         *client
	questions []question
	users     int
	duration  time.Duration
	samples   map[string]*sample
	checked   atomic.Int64
	hits      atomic.Int64
}

func newLoad(c *client, questions []question) *load {
	l := &load{c: c, questions: questions, samples: map[string]*sample{}}
	for _, kind := range []string{"ask first sources", "ask first word", "ask complete", "list documents", "filter documents", "page deep", "overview"} {
		l.samples[kind] = &sample{}
	}
	return l
}

// run starts the users and lets each repeat step until the time is up.
func (l *load) run(step func(*rand.Rand)) result {
	var wg sync.WaitGroup
	started := time.Now()
	deadline := started.Add(l.duration)
	for i := range l.users {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(seed, seed+1))
			for time.Now().Before(deadline) {
				step(rng)
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

func (l *load) ask(rng *rand.Rand) {
	q := l.questions[rng.IntN(len(l.questions))]
	t, err := l.c.ask(q.Text)
	l.samples["ask first sources"].add(t.Meta, err)
	l.samples["ask first word"].add(t.FirstToken, err)
	l.samples["ask complete"].add(t.Done, err)
	if err == nil && t.Outcome == "answered" && rng.IntN(recallEvery) == 0 {
		l.checkTopic(q, t)
	}
}

// checkTopic asks for the title of the first cited document: it must be about the question's topic.
func (l *load) checkTopic(q question, t timings) {
	status, body, _, err := l.c.call(http.MethodGet, "/documents/"+t.Cited[0], nil)
	if err != nil || status != httpOK {
		return
	}
	l.checked.Add(1)
	if strings.Contains(string(body), q.Topic) {
		l.hits.Add(1)
	}
}

func (l *load) listFirstPage(_ *rand.Rand) {
	status, _, d, err := l.c.call(http.MethodGet, fmt.Sprintf("/documents?limit=%d", listPageSize), nil)
	l.samples["list documents"].add(d, statusErr(status, err))
}

func (l *load) filter(rng *rand.Rand) {
	word := fmt.Sprintf("tema%03d", rng.IntN(topics))
	status, _, d, err := l.c.call(http.MethodGet, fmt.Sprintf("/documents?limit=%d&q=%s", listPageSize, word), nil)
	l.samples["filter documents"].add(d, statusErr(status, err))
}

const topics = 200

func (l *load) deepPage(_ *rand.Rand) {
	cursor := ""
	started := time.Now()
	var err error
	for range deepPages {
		cursor, err = l.nextPage(cursor)
		if err != nil || cursor == "" {
			break
		}
	}
	l.samples["page deep"].add(time.Since(started), err)
}

func (l *load) overview(_ *rand.Rand) {
	status, _, d, err := l.c.call(http.MethodGet, "/stats/overview", nil)
	l.samples["overview"].add(d, statusErr(status, err))
}

func statusErr(status int, err error) error {
	if err == nil && status != httpOK {
		return fmt.Errorf("status %d", status)
	}
	return err
}

// mixed is what a team does: mostly asks, some browsing, the odd look at the panel.
func (l *load) mixed(rng *rand.Rand) {
	switch roll := rng.IntN(percent); {
	case roll < askShare:
		l.ask(rng)
	case roll < listShare:
		if rng.IntN(2) == 0 {
			l.listFirstPage(rng)
		} else {
			l.filter(rng)
		}
	default:
		l.overview(rng)
	}
}

func (l *load) notes() []string {
	if l.checked.Load() == 0 {
		return nil
	}
	return []string{fmt.Sprintf("recall of the first cited document: %d of %d checked answers were about the question's topic",
		l.hits.Load(), l.checked.Load())}
}

// browse is a person going through the documents: the first page, a filter, and now and then far down the list.
func (l *load) browse(rng *rand.Rand) {
	switch rng.IntN(listKinds) {
	case 0:
		l.listFirstPage(rng)
	case 1:
		l.filter(rng)
	default:
		l.deepPage(rng)
	}
}
