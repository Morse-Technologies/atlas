package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/riverqueue/river"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/graph"
	"github.com/morse/atlas/server/internal/httpapi"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/overview"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/config"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/idempotency"
	"github.com/morse/atlas/server/internal/platform/license"
	"github.com/morse/atlas/server/internal/platform/models/docling"
	"github.com/morse/atlas/server/internal/platform/models/gotenberg"
	"github.com/morse/atlas/server/internal/platform/schema"
	"github.com/morse/atlas/server/internal/search"
	"github.com/morse/atlas/server/internal/sources"
)

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 2 * time.Minute
	// No WriteTimeout: /ask streams SSE for as long as the model writes; per-route deadlines go in handlers.
	// shutdownGrace lets in-flight requests and jobs finish after SIGTERM.
	shutdownGrace = 30 * time.Second
)

// uploadsDirName is the folder inside the blob volume where uploaded files are kept.
const uploadsDirName = "uploads"

// deps are the clients shared by the api and worker roles.
type deps struct {
	db        *db.DB
	blobs     *blobs.Store
	blobDir   string
	docling   *docling.Client
	gotenberg *gotenberg.Client
	schema    int64
}

func loadDeps(ctx context.Context) (deps, error) {
	blobCfg, err := config.Load[config.Blobs]()
	if err != nil {
		return deps{}, err
	}
	modelCfg, err := config.Load[config.Models]()
	if err != nil {
		return deps{}, err
	}
	d, err := openDB(ctx)
	if err != nil {
		return deps{}, err
	}
	version, err := schema.Require(ctx, d)
	if err != nil {
		d.Pool.Close()
		return deps{}, err
	}
	return deps{db: d, blobs: blobs.New(blobCfg.Dir), blobDir: blobCfg.Dir, docling: docling.New(modelCfg.DoclingURL),
		gotenberg: gotenberg.New(modelCfg.GotenbergURL), schema: version}, nil
}

// runAPI serves the HTTP contract until SIGTERM.
func runAPI(ctx context.Context, _ []string) error {
	if _, err := newLogger(); err != nil {
		return err
	}
	httpCfg, err := config.Load[config.HTTP]()
	if err != nil {
		return err
	}
	d, err := loadDeps(ctx)
	if err != nil {
		return err
	}
	defer d.db.Pool.Close()
	server, err := buildAPI(d, httpCfg.Addr)
	if err != nil {
		return err
	}
	return serveUntilDone(ctx, server)
}

// buildAPI assembles the HTTP server: the services, and a queue client so handlers can queue jobs.
func buildAPI(d deps, addr string) (*http.Server, error) {
	parts, err := newAPIParts(d)
	if err != nil {
		return nil, err
	}
	queue, err := jobs.NewClient(d.db.Pool, jobs.Setup{}) // insert-only: the API queues work, workers run it
	if err != nil {
		return nil, err
	}
	return newHTTPServer(addr, apiServer(d, parts), queue), nil
}

func newHTTPServer(addr string, api *httpapi.Server, queue *jobs.Client) *http.Server {
	return &http.Server{Addr: addr, Handler: withJobs(queue, httpapi.NewHandler(api)),
		ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: readTimeout, IdleTimeout: idleTimeout}
}

// newAccounts builds the access service with the secret key from the environment.
func newAccounts(d *db.DB) (*access.Service, error) {
	cfg, err := config.Load[config.Auth]()
	if err != nil {
		return nil, err
	}
	key, err := access.ParseKey(cfg.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("ATLAS_SECRET_KEY: %w", err)
	}
	box, err := access.NewBox(key)
	if err != nil {
		return nil, err
	}
	return access.NewService(d, box, createTenantWithCatalog), nil
}

// apiParts are the pieces of the API that are built before the server.
type apiParts struct {
	accounts httpapi.Accounts
	roots    []string
	admin    *sources.Admin
	search   *search.Service
	licenses *license.Holder
	deploy   httpapi.Deployment
	checks   []httpapi.Check
}

func newAPIParts(d deps) (apiParts, error) {
	accounts, err := newAccounts(d.db)
	if err != nil {
		return apiParts{}, err
	}
	roots, err := config.Load[config.Sources]()
	if err != nil {
		return apiParts{}, err
	}
	answers, checks, err := newSearch(d.db)
	if err != nil {
		return apiParts{}, err
	}
	licenses, err := newLicenses()
	if err != nil {
		return apiParts{}, err
	}
	deploy, err := newDeployment()
	admin := sources.NewAdmin(d.db, filepath.Join(d.blobDir, uploadsDirName), roots.Roots)
	admin.ApplyAccess = documents.ApplyAccess
	return apiParts{accounts: accounts, roots: roots.Roots, admin: admin, search: answers, checks: checks, licenses: licenses, deploy: deploy}, err
}

func apiServer(d deps, parts apiParts) *httpapi.Server {
	return &httpapi.Server{
		Version: Version, SchemaVersion: d.schema, Checks: append(coreChecks(d), parts.checks...),
		Accounts: parts.accounts, Audit: &audit.Service{DB: d.db}, Answers: parts.search,
		Sources:     parts.admin,
		Documents:   documents.NewLibrary(d.db, d.blobs),
		Taxonomy:    &catalog.Service{DB: d.db},
		Idempotency: idempotency.NewStore(d.db), Licenses: parts.licenses, Graph: graph.NewService(d.db), Panel: newPanel(d, parts), Deployment: parts.deploy,
	}
}

// coreChecks are the dependencies the API cannot work without.
func coreChecks(d deps) []httpapi.Check {
	return []httpapi.Check{
		{Name: "postgres", Probe: d.db.Pool.Ping},
		{Name: "blobs", Probe: func(context.Context) error { return d.blobs.CheckWritable() }},
		{Name: "docling", Probe: d.docling.Health},
		{Name: "gotenberg", Probe: d.gotenberg.Health},
	}
}

func serveUntilDone(ctx context.Context, server *http.Server) error {
	errc := make(chan error, 1)
	go func() { errc <- server.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// runWorker runs the pipeline jobs until SIGTERM.
func runWorker(ctx context.Context, _ []string) error {
	logger, err := newLogger()
	if err != nil {
		return err
	}
	d, err := loadDeps(ctx)
	if err != nil {
		return err
	}
	defer d.db.Pool.Close()
	workers, err := registerWorkers(d, logger)
	if err != nil {
		return err
	}
	client, err := jobs.NewClient(d.db.Pool, jobs.Setup{Workers: workers, Periodic: append(sources.PeriodicJobs(), graph.PeriodicJobs()...), Logger: logger})
	if err != nil {
		return err
	}
	return runClientUntilDone(ctx, client)
}

func runClientUntilDone(ctx context.Context, client *jobs.Client) error {
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	return client.Stop(stopCtx)
}

// runWatch turns file-system events on local folder sources into fetch jobs until SIGTERM.
func runWatch(ctx context.Context, _ []string) error {
	logger, err := newLogger()
	if err != nil {
		return err
	}
	d, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer d.Pool.Close()
	client, err := jobs.NewClient(d.Pool, jobs.Setup{Logger: logger})
	if err != nil {
		return err
	}
	m := &sources.Module{DB: d, Logger: logger}
	return watchWithReload(jobs.WithClient(ctx, client), m)
}

// watchReload is how often the watcher re-reads the sources, so new or paused sources take effect.
const watchReload = 5 * time.Minute

// watchWithReload watches the current folder sources for one reload period at a time until ctx ends.
// Events pending when a period ends are dropped; the periodic rescan picks those files up.
func watchWithReload(ctx context.Context, m *sources.Module) error {
	for ctx.Err() == nil {
		targets, err := m.WatchTargets(ctx)
		if err != nil {
			return err
		}
		m.Logger.Info("watching folders", "count", len(targets))
		period, cancel := context.WithTimeout(ctx, watchReload)
		err = m.Watch(period, targets)
		<-period.Done() // Watch returns at once when there is nothing to watch
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func registerWorkers(d deps, logger *slog.Logger) (*river.Workers, error) {
	workers := river.NewWorkers()
	(&sources.Module{DB: d.db, Blobs: d.blobs, Logger: logger}).Register(workers)
	docs := &documents.Module{DB: d.db, Blobs: d.blobs, Docling: d.docling, Gotenberg: d.gotenberg, Logger: logger}
	if err := useEmbedder(docs); err != nil {
		return nil, err
	}
	docs.Register(workers)
	(&search.Service{Config: search.Config{DB: d.db}}).Register(workers)
	graph.NewService(d.db).Register(workers)
	return workers, nil
}

// useEmbedder gives the documents module the embedding client when ATLAS_EMBED_URL is set.
func useEmbedder(m *documents.Module) error {
	cfg, err := config.Load[config.Embedding]()
	if err != nil || cfg.URL == "" {
		return err
	}
	e, err := newEmbedding(cfg)
	if err != nil {
		return err
	}
	if e.tokens != nil {
		m.Tokens = e.tokens
	}
	m.Embedder, m.EmbedModel = e.embedder, cfg.Model
	return nil
}

// withJobs lets handlers queue jobs in the transaction of the change that needs them (ATLAS.md §7.1).
func withJobs(queue *jobs.Client, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(jobs.WithClient(r.Context(), queue)))
	})
}

// newLicenses reads the license settings. With a file but no public key the server refuses to start: it could
// never verify the file, and silently running as Core would hide a configuration mistake.
func newLicenses() (*license.Holder, error) {
	cfg, err := config.Load[config.License]()
	if err != nil {
		return nil, err
	}
	if cfg.File != "" && cfg.PublicKey == "" {
		return nil, errors.New("ATLAS_LICENSE_FILE is set but ATLAS_LICENSE_PUBLIC_KEY is not")
	}
	key, err := base64.StdEncoding.DecodeString(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("ATLAS_LICENSE_PUBLIC_KEY: %w", err)
	}
	return license.NewHolder(cfg.File, key), nil
}

// newDeployment reads what the screen shows but an administrator cannot change.
func newDeployment() (httpapi.Deployment, error) {
	embed, err := config.Load[config.Embedding]()
	if err != nil {
		return httpapi.Deployment{}, err
	}
	answering, err := config.Load[config.Answering]()
	if err != nil {
		return httpapi.Deployment{}, err
	}
	network, err := config.Load[config.Network]()
	return httpapi.Deployment{ChatModel: answering.LLMModel, EmbedModel: embed.Model, RerankModel: answering.RerankModel,
		NetworkIsolated: network.Isolated}, err
}

// newPanel wires the overview from the modules that hold the numbers.
func newPanel(d deps, parts apiParts) *overview.Service {
	return &overview.Service{Questions: parts.search, Library: documents.NewLibrary(d.db, d.blobs), Quarantine: parts.admin,
		Alerts: graph.NewService(d.db), Taxonomy: &catalog.Service{DB: d.db}, Now: time.Now}
}
