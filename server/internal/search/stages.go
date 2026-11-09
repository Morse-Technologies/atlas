package search

import (
	"context"
	"log/slog"
	"time"
)

// stageClock times the steps of finding passages, to see where a slow question spends its time. It writes one
// debug line per question (ATLAS_LOG_LEVEL=debug), so it costs nothing in normal running.
type stageClock struct {
	last  time.Time
	attrs []any
}

func newStageClock() *stageClock { return &stageClock{last: time.Now()} }

// lap records the time since the previous lap, in milliseconds, under name.
func (c *stageClock) lap(name string) {
	now := time.Now()
	c.attrs = append(c.attrs, name+"_ms", float64(now.Sub(c.last).Microseconds())/microsPerMilli)
	c.last = now
}

const microsPerMilli = 1000

func (c *stageClock) log(ctx context.Context, candidates int) {
	c.logAs(ctx, "retrieved", candidates)
}

func (c *stageClock) logAs(ctx context.Context, what string, found int) {
	slog.DebugContext(ctx, what, append(c.attrs, "found", found)...)
}
