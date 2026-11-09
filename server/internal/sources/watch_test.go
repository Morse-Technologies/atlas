package sources

import (
	"sync/atomic"
	"testing"
	"time"
)

const testSettle = 20 * time.Millisecond

func TestDebouncerFiresOncePerBurstAndPath(t *testing.T) {
	var fired atomic.Int32
	d := newDebouncer(testSettle, func(string) { fired.Add(1) })
	for range 5 {
		d.touch("a.pdf") // an editor saving the same file several times
	}
	d.touch("b.pdf")
	time.Sleep(10 * testSettle)
	if got := fired.Load(); got != 2 {
		t.Fatalf("fired %d times, want one per path", got)
	}
}
