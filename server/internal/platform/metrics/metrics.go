// Package metrics counts what an operator watches (requests, model calls, queued jobs) and writes it in the
// Prometheus text format, by hand: the format is a few lines and a client library is a dependency for no more.
package metrics

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	contentType = "text/plain; version=0.0.4; charset=utf-8"
	kindCounter = "counter"
	kindGauge   = "gauge"
	readTimeout = 5 * time.Second
)

// Default is the registry of the process. shortcut: the model clients are built in several places and share it
// instead of receiving one; give each its own registry if a process ever needs two sets of numbers.
var Default = NewRegistry()

// Labels are the dimensions of one series, e.g. {"operation": "Ask"}.
type Labels map[string]string

// Sample is one value of a gauge.
type Sample struct {
	Labels Labels
	Value  float64
}

// Gauge is a value read when the metrics are scraped, such as the jobs waiting in the queue.
type Gauge struct {
	Name string
	Read func(context.Context) ([]Sample, error)
}

type series struct{ name, labels string }

// Registry holds counters and the gauges read at scrape time. It is safe for concurrent use.
type Registry struct {
	mu       sync.Mutex
	counters map[series]float64
	gauges   []Gauge
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{counters: map[series]float64{}}
}

// Inc adds one to the counter name under labels.
func (r *Registry) Inc(name string, labels Labels) {
	r.add(series{name, renderLabels(labels)}, 1)
}

// Observe records one duration as name_sum (seconds) and name_count, from which an average follows.
func (r *Registry) Observe(name string, labels Labels, took time.Duration) {
	rendered := renderLabels(labels)
	r.add(series{name + "_sum", rendered}, took.Seconds())
	r.add(series{name + "_count", rendered}, 1)
}

// AddGauge registers a value that is read on every scrape.
func (r *Registry) AddGauge(g Gauge) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, g)
}

func (r *Registry) add(s series, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[s] += value
}

// Handler serves the registry; a gauge that cannot be read is logged and left out, the rest are still served.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", contentType)
		ctx, cancel := context.WithTimeout(req.Context(), readTimeout)
		defer cancel()
		r.write(ctx, w)
	})
}

func (r *Registry) write(ctx context.Context, w io.Writer) {
	r.writeCounters(w)
	for _, g := range r.snapshotGauges() {
		samples, err := g.Read(ctx)
		if err != nil {
			slog.Warn("metrics: gauge not read", "gauge", g.Name, "error", err)
			continue
		}
		writeGauge(w, g.Name, samples)
	}
}

func (r *Registry) writeCounters(w io.Writer) {
	r.mu.Lock()
	snapshot := maps.Clone(r.counters)
	r.mu.Unlock()
	for _, s := range slices.SortedFunc(maps.Keys(snapshot), compareSeries) {
		fmt.Fprintf(w, "# TYPE %s %s\n%s%s %g\n", s.name, kindCounter, s.name, s.labels, snapshot[s])
	}
}

func (r *Registry) snapshotGauges() []Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.gauges)
}

func writeGauge(w io.Writer, name string, samples []Sample) {
	fmt.Fprintf(w, "# TYPE %s %s\n", name, kindGauge)
	for _, s := range samples {
		fmt.Fprintf(w, "%s%s %g\n", name, renderLabels(s.Labels), s.Value)
	}
}

func compareSeries(a, b series) int {
	if c := strings.Compare(a.name, b.name); c != 0 {
		return c
	}
	return strings.Compare(a.labels, b.labels)
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// renderLabels gives {a="1",b="2"} with the names sorted, so one label set is always one key.
func renderLabels(labels Labels) string {
	if len(labels) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(labels))
	for _, name := range slices.Sorted(maps.Keys(labels)) {
		pairs = append(pairs, name+`="`+labelEscaper.Replace(labels[name])+`"`)
	}
	return "{" + strings.Join(pairs, ",") + "}"
}
