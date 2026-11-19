package log

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestNewHonoursTheLevelAndInstallsTheDefaultLogger(t *testing.T) {
	logger, err := New("warn")
	if err != nil {
		t.Fatal(err)
	}
	if logger != slog.Default() {
		t.Error("the logger must become the slog default")
	}
	if logger.Enabled(context.Background(), slog.LevelInfo) || !logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("level warn must hide info and show warn")
	}
}

func TestNewRefusesAnUnknownLevelNamingIt(t *testing.T) {
	_, err := New("loud")
	if err == nil || !strings.HasPrefix(err.Error(), `log level "loud"`) {
		t.Fatalf("want an error naming the level, got %v", err)
	}
}

func TestErrIsTheErrorAttribute(t *testing.T) {
	cause := errors.New("boom")
	attr := Err(cause)
	if attr.Key != "error" || attr.Value.Any() != cause {
		t.Fatalf("attr = %v", attr)
	}
}
