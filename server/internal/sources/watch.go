package sources

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/log"
)

// settleDelay waits for editors that save a file several times in a row (ATLAS.md §6).
const settleDelay = 2 * time.Second

// WatchTarget is one folder source to watch.
type WatchTarget struct {
	TenantID uuid.UUID
	SourceID uuid.UUID
	Root     string
}

// WatchTargets lists the active folder sources of every tenant on this host.
func (m *Module) WatchTargets(ctx context.Context) ([]WatchTarget, error) {
	tenants, err := access.ListTenants(ctx, m.DB.Pool)
	if err != nil {
		return nil, err
	}
	var targets []WatchTarget
	for _, t := range tenants {
		found, err := m.tenantWatchTargets(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		targets = append(targets, found...)
	}
	return targets, nil
}

func (m *Module) tenantWatchTargets(ctx context.Context, tenantID uuid.UUID) ([]WatchTarget, error) {
	var out []WatchTarget
	err := m.DB.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		list, err := List(ctx, tx)
		for _, s := range list {
			if s.Status == StatusActive {
				out = append(out, WatchTarget{TenantID: tenantID, SourceID: s.ID, Root: s.Root})
			}
		}
		return err
	})
	return out, err
}

// Watch enqueues a fetch for every file created or written under the targets until ctx ends.
// Deletions and renames are left to the rescan, which deletes safely and detects moves.
func (m *Module) Watch(ctx context.Context, targets []WatchTarget) error {
	group, ctx := errgroup.WithContext(ctx)
	for _, t := range targets {
		group.Go(func() error { return m.watchFolder(ctx, t) })
	}
	return group.Wait()
}

func (m *Module) watchFolder(ctx context.Context, t WatchTarget) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch %s: %w", t.Root, err)
	}
	defer w.Close()
	if err := addTree(w, t.Root); err != nil {
		return err
	}
	settle := newDebouncer(settleDelay, func(path string) { m.enqueueFetch(ctx, t, path) })
	return m.processEvents(ctx, watchLoop{watcher: w, settle: settle, root: t.Root})
}

type watchLoop struct {
	watcher *fsnotify.Watcher
	settle  *debouncer
	root    string
}

// processEvents handles watcher events until ctx ends or the watcher closes its channels.
func (m *Module) processEvents(ctx context.Context, l watchLoop) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, open := <-l.watcher.Events:
			if !open {
				return errWatcherClosed
			}
			m.handleEvent(l.watcher, l.settle, ev)
		case err, open := <-l.watcher.Errors:
			if !open {
				return errWatcherClosed
			}
			m.Logger.Warn("watch error", "root", l.root, log.Err(err))
		}
	}
}

var errWatcherClosed = errors.New("file-system watcher closed unexpectedly")

func (m *Module) handleEvent(w *fsnotify.Watcher, settle *debouncer, ev fsnotify.Event) {
	if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Write) {
		return
	}
	if isDir(ev.Name) {
		if err := addTree(w, ev.Name); err != nil {
			m.Logger.Warn("watch new directory", "path", ev.Name, log.Err(err))
		}
		return
	}
	if !isIgnorableName(filepath.Base(ev.Name)) {
		settle.touch(ev.Name)
	}
}

// addTree watches a directory and all its subdirectories (fsnotify is not recursive).
func addTree(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return w.Add(path)
	})
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (m *Module) enqueueFetch(ctx context.Context, t WatchTarget, path string) {
	rel, err := filepath.Rel(t.Root, path)
	if err != nil {
		m.Logger.Warn("watched path outside its source", "path", path, "root", t.Root, log.Err(err))
		return
	}
	job := events.VersionFetch{TenantID: t.TenantID, SourceID: t.SourceID, ExternalID: filepath.ToSlash(rel)}
	err = m.DB.InTenantTx(ctx, t.TenantID, func(tx pgx.Tx) error { return jobs.Enqueue(ctx, tx, job) })
	if err != nil {
		m.Logger.Warn("enqueue fetch from watcher", "path", path, log.Err(err))
	}
}

// debouncer calls fire once per path after the path has been quiet for delay.
// Libraries such as bep/debounce debounce one function, not one timer per key, so this stays local.
type debouncer struct {
	mu     sync.Mutex
	delay  time.Duration
	timers map[string]*time.Timer
	fire   func(path string)
}

func newDebouncer(delay time.Duration, fire func(path string)) *debouncer {
	return &debouncer{delay: delay, timers: map[string]*time.Timer{}, fire: fire}
}

func (d *debouncer) touch(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[path]; ok {
		t.Reset(d.delay)
		return
	}
	d.timers[path] = time.AfterFunc(d.delay, func() { d.settle(path) })
}

func (d *debouncer) settle(path string) {
	d.mu.Lock()
	delete(d.timers, path)
	d.mu.Unlock()
	d.fire(path)
}
