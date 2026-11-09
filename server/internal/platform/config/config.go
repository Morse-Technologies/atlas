// Package config loads role-specific settings from ATLAS_* environment variables (ATLAS.md §16.1).
// Each role loads only the structs it needs, so a missing variable fails only the role that uses it.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Database is the app-role connection, subject to RLS.
type Database struct {
	URL string `env:"ATLAS_DATABASE_URL,required,notEmpty"`
	// MaxConns bounds the pool of each process. A question uses a few connections at once, so it must be well
	// above the number of people asking together, or they queue for one.
	MaxConns int32 `env:"ATLAS_DB_MAX_CONNS" envDefault:"24"`
}

// Migrate is the schema-owner connection, used only by `atlas migrate`.
type Migrate struct {
	URL string `env:"ATLAS_MIGRATE_DATABASE_URL,required,notEmpty"`
}

// Blobs is where original files and derived artifacts live.
type Blobs struct {
	Dir string `env:"ATLAS_BLOB_DIR" envDefault:"/data"`
}

// Models holds the URLs of the services that open customer files.
type Models struct {
	DoclingURL   string `env:"ATLAS_DOCLING_URL,required,notEmpty"`
	GotenbergURL string `env:"ATLAS_GOTENBERG_URL,required,notEmpty"`
}

// HTTP is the API listener.
type HTTP struct {
	Addr string `env:"ATLAS_HTTP_ADDR" envDefault:":8080"`
}

// Sources lists the folders an administrator may register as sources (ATLAS_SOURCE_ROOTS, comma separated).
type Sources struct {
	Roots []string `env:"ATLAS_SOURCE_ROOTS" envSeparator:"," envDefault:"/sources"`
}

// Embedding is the TEI instance that embeds and tokenizes. Empty: passages are found by words only.
type Embedding struct {
	URL   string `env:"ATLAS_EMBED_URL"`
	Model string `env:"ATLAS_EMBED_MODEL" envDefault:"bge-m3"`
	// API is the kind of server at URL: tei (default) or ollama, the light option for trials.
	API string `env:"ATLAS_EMBED_API" envDefault:"tei"`
}

// Answering holds the services that rank and write answers. Each may be empty: search then degrades
// (no reranker: fused ranking; no LLM: fragments without generated text), see ATLAS.md §11.3.
type Answering struct {
	RerankURL   string `env:"ATLAS_RERANK_URL"`
	RerankModel string `env:"ATLAS_RERANK_MODEL" envDefault:"bge-reranker-v2-m3"`
	// RerankTimeout is how long one ranking may take; raise it only for trials on slow CPUs.
	RerankTimeout  time.Duration `env:"ATLAS_RERANK_TIMEOUT" envDefault:"5s"`
	LLMURL         string        `env:"ATLAS_LLM_URL"`
	LLMModel       string        `env:"ATLAS_LLM_MODEL"`
	MaxGenerations int64         `env:"ATLAS_LLM_MAX_CONCURRENT" envDefault:"2"`
	// CommonShare is the share of passages above which a word is left out of the word search when a rarer one is
	// there. 0 searches with every word.
	CommonShare float64 `env:"ATLAS_SEARCH_COMMON_SHARE" envDefault:"0.02"`
}

// Auth holds the key that encrypts second-factor seeds: 32 random bytes, base64 encoded (ATLAS_SECRET_KEY_FILE
// in production). Losing it locks every account with a second factor, so it is backed up apart from the data.
type Auth struct {
	SecretKey string `env:"ATLAS_SECRET_KEY,required,notEmpty"`
}

// Network says whether the server may reach the internet. On-prem installs are air-gapped by default.
type Network struct {
	Isolated bool `env:"ATLAS_NETWORK_ISOLATED" envDefault:"true"`
}

// License locates the signed license file and the public key that checks it (base64, 32 bytes). Both may be empty:
// the installation then runs as Core.
type License struct {
	File      string `env:"ATLAS_LICENSE_FILE"`
	PublicKey string `env:"ATLAS_LICENSE_PUBLIC_KEY"`
}

// Log sets the slog level: debug, info, warn or error.
type Log struct {
	Level string `env:"ATLAS_LOG_LEVEL" envDefault:"info"`
}

// secretVars may be given as NAME_FILE pointing to a file (Docker secrets) instead of NAME.
var secretVars = []string{"ATLAS_DATABASE_URL", "ATLAS_MIGRATE_DATABASE_URL", "ATLAS_SECRET_KEY"}

// Load reads environment variables into T after resolving *_FILE secrets.
func Load[T any]() (T, error) {
	var cfg T
	if err := loadSecretFiles(); err != nil {
		return cfg, err
	}
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

func loadSecretFiles() error {
	for _, name := range secretVars {
		if err := loadSecretFile(name); err != nil {
			return err
		}
	}
	return nil
}

// loadSecretFile copies the content of NAME_FILE into NAME when NAME itself is not set.
func loadSecretFile(name string) error {
	path := os.Getenv(name + "_FILE")
	if path == "" || os.Getenv(name) != "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s_FILE: %w", name, err)
	}
	return os.Setenv(name, strings.TrimSpace(string(content)))
}
