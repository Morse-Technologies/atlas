//go:build integration

package search

import (
	"context"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/models/llm"
)

// slotWait is how long a test waits for something that should happen at once.
const slotWait = 5 * time.Second

// blockingLLM writes one token and then waits until the request is cancelled, like a slow model.
type blockingLLM struct{ wrote chan struct{} }

func (blockingLLM) Model() string { return "test-model" }

func (b blockingLLM) Stream(ctx context.Context, _ llm.Request, onToken func(string) error) error {
	if err := onToken("Forty "); err != nil {
		return err
	}
	close(b.wrote)
	<-ctx.Done()
	return ctx.Err()
}

// askInBackground starts a question and returns when it has finished.
func askInBackground(ctx context.Context, w world, text string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- w.svc.Ask(ctx, Question{Who: w.who, Text: text}, &recordingSink{}) }()
	return done
}

func TestCancelledQuestionKeepsTextAndFreesTheSlot(t *testing.T) {
	gen := blockingLLM{wrote: make(chan struct{})}
	w := newWorld(t, Config{MaxGenerations: 1, Reranker: fixedReranker(0.9), LLM: gen})
	w.chunk("PNT-014", "Tightening torque of the spindle is 40 Nm", w.acl, true)
	ctx, cancel := context.WithCancel(w.ctx)
	done := askInBackground(ctx, w, "tightening torque?")
	<-gen.wrote
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(slotWait):
		t.Fatal("the cancelled question never finished: its generation slot is still held")
	}
	assertInterruptedAnswerIsStored(t, w)
	assertSlotIsFree(t, w)
}

func assertInterruptedAnswerIsStored(t *testing.T, w world) {
	t.Helper()
	list, _, err := w.svc.Conversations(w.ctx, w.who, ConversationPage{Size: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("conversations = %+v, %v; want the cancelled one", list, err)
	}
	_, answers, err := w.svc.Thread(w.ctx, w.who, list[0].ID)
	if err != nil || len(answers) != 1 {
		t.Fatalf("answers = %+v, %v", answers, err)
	}
	a := answers[0]
	if a.Outcome != OutcomeError || a.Text != "Forty " || len(a.Notes) == 0 || a.Notes[len(a.Notes)-1] != interruptedNote {
		t.Fatalf("answer = outcome %s, text %q, notes %v; want the partial text marked as interrupted", a.Outcome, a.Text, a.Notes)
	}
}

func assertSlotIsFree(t *testing.T, w world) {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.ctx, time.Second)
	defer cancel()
	if err := w.svc.slots.Acquire(ctx, 1); err != nil {
		t.Fatalf("the generation slot was not released: %v", err)
	}
	w.svc.slots.Release(1)
}
