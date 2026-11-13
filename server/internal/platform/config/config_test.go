package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDatabaseURLFromSecretFile(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "db_url")
	if err := os.WriteFile(secret, []byte("postgres://atlas_app@db/atlas\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATLAS_DATABASE_URL", "")
	t.Setenv("ATLAS_DATABASE_URL_FILE", secret)
	cfg, err := Load[Database]()
	if err != nil || cfg.URL != "postgres://atlas_app@db/atlas" {
		t.Fatalf("Load() = %+v, %v", cfg, err)
	}
}

func TestMissingRequiredVariableFails(t *testing.T) {
	t.Setenv("ATLAS_DOCLING_URL", "")
	t.Setenv("ATLAS_GOTENBERG_URL", "http://gotenberg:3000")
	if _, err := Load[Models](); err == nil {
		t.Fatal("a role must not start without its required settings")
	}
}

func TestEmbeddingTimeoutIsReadAndDefaultsToTheClientsOwn(t *testing.T) {
	t.Setenv("ATLAS_EMBED_TIMEOUT", "")
	if cfg, err := Load[Embedding](); err != nil || cfg.Timeout != 0 {
		t.Fatalf("unset: Load() = %+v, %v; want zero, the client's own limit", cfg, err)
	}
	t.Setenv("ATLAS_EMBED_TIMEOUT", "45s")
	if cfg, err := Load[Embedding](); err != nil || cfg.Timeout != 45*time.Second {
		t.Fatalf("45s: Load() = %+v, %v", cfg, err)
	}
}
