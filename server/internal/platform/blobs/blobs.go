// Package blobs stores original files by content hash and derived artifacts on local disk (ATLAS.md §10.3).
// Writes are atomic: temp file, fsync, rename. Disk only; an S3 store comes with the cloud, not before.
package blobs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const (
	dirPerm        = 0o750
	hashPrefixLen  = 2 // blobs/{tenant}/{sha[0:2]}/{sha}: keeps directories small
	tmpDirName     = "tmp"
	blobsDirName   = "blobs"
	derivedDirName = "derived"
)

// Store is a directory tree under one root.
type Store struct {
	root string
}

// Staged is a file written to the temp area and hashed, not yet committed.
type Staged struct {
	Path string
	Hash string
	Size int64
}

// DerivedKey names an artifact made from a blob by a pipeline stage at a given stage version.
type DerivedKey struct {
	Hash    string
	Stage   string
	Version string
}

// New returns a store rooted at dir. Directories are created on first write.
func New(dir string) *Store {
	return &Store{root: dir}
}

// Stage copies r into a temp file while hashing it. The caller commits or discards the result.
func (s *Store) Stage(r io.Reader) (Staged, error) {
	tmp, err := s.createTemp()
	if err != nil {
		return Staged{}, err
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, hasher), r)
	if err := closeSynced(tmp, copyErr); err != nil {
		_ = os.Remove(tmp.Name())
		return Staged{}, fmt.Errorf("stage blob: %w", err)
	}
	return Staged{Path: tmp.Name(), Hash: hex.EncodeToString(hasher.Sum(nil)), Size: size}, nil
}

// Commit moves a staged file to its content-addressed place. Committing the same content twice is a no-op.
func (s *Store) Commit(tenantID uuid.UUID, staged Staged) error {
	return moveInto(staged.Path, s.BlobPath(tenantID, staged.Hash))
}

// Discard removes a staged file that will not be committed.
func (s *Store) Discard(staged Staged) {
	if err := os.Remove(staged.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("discard staged blob", "path", staged.Path, "err", err)
	}
}

// BlobPath is where a tenant's blob with this hash lives.
func (s *Store) BlobPath(tenantID uuid.UUID, hash string) string {
	return filepath.Join(s.root, blobsDirName, tenantID.String(), hash[:hashPrefixLen], hash)
}

// DerivedPath is where a derived artifact lives.
func (s *Store) DerivedPath(key DerivedKey) string {
	return filepath.Join(s.root, derivedDirName, key.Hash, key.Stage+"-"+key.Version)
}

// WriteDerived stores an artifact produced by write, atomically.
func (s *Store) WriteDerived(key DerivedKey, write func(io.Writer) error) error {
	tmp, err := s.createTemp()
	if err != nil {
		return err
	}
	if err := closeSynced(tmp, write(tmp)); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write derived %s: %w", key.Stage, err)
	}
	return moveInto(tmp.Name(), s.DerivedPath(key))
}

// HasDerived reports whether the artifact already exists (a cache hit).
func (s *Store) HasDerived(key DerivedKey) (bool, error) {
	_, err := os.Stat(s.DerivedPath(key))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// CheckWritable is the readiness probe for the blob volume.
func (s *Store) CheckWritable() error {
	tmp, err := s.createTemp()
	if err != nil {
		return err
	}
	_ = tmp.Close()
	return os.Remove(tmp.Name())
}

func (s *Store) createTemp() (*os.File, error) {
	dir := filepath.Join(s.root, tmpDirName)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("blob temp dir: %w", err)
	}
	return os.CreateTemp(dir, "stage-*")
}

// closeSynced flushes and closes f, returning the first error including one from the earlier write.
func closeSynced(f *os.File, writeErr error) error {
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func moveInto(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return fmt.Errorf("blob dir: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("blob commit: %w", err)
	}
	return nil
}
