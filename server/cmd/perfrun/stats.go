package main

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"
)

const (
	p50   = 0.50
	p95   = 0.95
	p99   = 0.99
	tenth = 10 // sub-second times are shown to a tenth of a millisecond
)

// sample collects the durations of one kind of request from many workers.
type sample struct {
	mu     sync.Mutex
	values []time.Duration
	errors int
}

func (s *sample) add(d time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.errors++
		return
	}
	s.values = append(s.values, d)
}

// percentile of the durations collected, 0 if none.
func (s *sample) percentile(p float64) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.values) == 0 {
		return 0
	}
	sorted := slices.Clone(s.values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[min(int(p*float64(len(sorted))), len(sorted)-1)]
}

func (s *sample) count() int { return len(s.values) }

// result is one line of the report.
type result struct {
	Scenario string
	Users    int
	Duration time.Duration
	Samples  map[string]*sample // by kind of request
	Notes    []string
}

func (r result) print() {
	fmt.Printf("\n%s: %d users, %s\n", r.Scenario, r.Users, r.Duration.Round(time.Second))
	fmt.Printf("%-22s %7s %6s %8s %9s %9s %9s\n", "request", "ok", "errors", "per sec", "p50", "p95", "p99")
	names := make([]string, 0, len(r.Samples))
	for n := range r.Samples {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := r.Samples[n]
		fmt.Printf("%-22s %7d %6d %8.1f %9s %9s %9s\n", n, s.count(), s.errors, float64(s.count())/r.Duration.Seconds(),
			round(s.percentile(p50)), round(s.percentile(p95)), round(s.percentile(p99)))
	}
	for _, note := range r.Notes {
		fmt.Println(note)
	}
}

func round(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond / tenth)
	}
	return d.Round(time.Millisecond)
}
