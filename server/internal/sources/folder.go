package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileState is what a scan knows about one file. ExternalID is the slash-separated path under the root.
type FileState struct {
	ExternalID string
	Size       int64
	ModifiedAt time.Time
	Hash       string
}

// folder reads a directory tree mounted on this host (a local folder or a CIFS mount, ATLAS.md §6.1).
// It is the only connector so far, so there is no Connector interface yet.
type folder struct {
	root string
}

// walk lists every regular file under the root. On error it returns what it saw so far and the error:
// the caller must then treat the listing as incomplete and delete nothing.
func (f folder) walk(ctx context.Context) ([]FileState, error) {
	var seen []FileState
	err := filepath.WalkDir(f.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		state, ok, err := f.fileState(path, d)
		if ok {
			seen = append(seen, state)
		}
		return err
	})
	return seen, err
}

// fileState describes a walked entry; ok is false for directories, symlinks and editor/system noise.
func (f folder) fileState(path string, d fs.DirEntry) (FileState, bool, error) {
	if !d.Type().IsRegular() || isIgnorableName(d.Name()) {
		return FileState{}, false, nil
	}
	info, err := d.Info()
	if err != nil {
		return FileState{}, false, err
	}
	rel, err := filepath.Rel(f.root, path)
	if err != nil {
		return FileState{}, false, err
	}
	return FileState{ExternalID: filepath.ToSlash(rel), Size: info.Size(), ModifiedAt: info.ModTime().UTC()}, true, nil
}

// isIgnorableName skips hidden files and Office lock files (~$report.docx), which are never documents.
func isIgnorableName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$")
}

// open opens a file by external id. os.Root refuses paths that escape the source root.
func (f folder) open(externalID string) (*os.File, error) {
	root, err := os.OpenRoot(f.root)
	if err != nil {
		return nil, fmt.Errorf("open source root: %w", err)
	}
	defer root.Close()
	return root.Open(filepath.FromSlash(externalID))
}

// hash returns the sha256 of a file's content, used to recognize moved files.
func (f folder) hash(externalID string) (string, error) {
	file, err := f.open(externalID)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", externalID, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
