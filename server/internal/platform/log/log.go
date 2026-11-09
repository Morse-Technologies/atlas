// Package log builds the process logger: JSON to stdout, never document content at info level (ATLAS.md §16.3).
package log

import (
	"fmt"
	"log/slog"
	"os"
)

// New returns a JSON logger at the given level and installs it as the slog default.
func New(level string) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("log level %q: %w", level, err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(logger)
	return logger, nil
}

// Err is the one attribute name every log line uses for an error.
func Err(err error) slog.Attr {
	return slog.Any("error", err)
}
