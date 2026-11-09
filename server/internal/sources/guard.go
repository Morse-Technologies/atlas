package sources

import (
	"archive/zip"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/formats"
)

// Limits for files that are ZIP containers (DOCX, XLSX, PPTX, ODF), ATLAS.md §8.1.
const (
	maxArchiveUncompressedBytes = 1 << 30 // 1 GB
	maxArchiveRatio             = 100     // uncompressed / compressed
	maxArchiveEntries           = 10_000
)

var (
	errTooManyEntries = errors.New("too many entries in container")
	errUnsafePath     = errors.New("container entry escapes its root")
	errTooBig         = errors.New("container expands beyond the size limit")
	errRatio          = errors.New("container compression ratio is suspicious")
)

// checkHostileFile runs the checks of ATLAS.md §8.1 on a staged file and returns its format.
// Every refusal is a permanent error whose reason ends up in quarantine.
func checkHostileFile(staged blobs.Staged, maxBytes int64) (formats.Format, error) {
	if err := checkSize(staged.Size, maxBytes); err != nil {
		return formats.Format{}, err
	}
	format, err := formats.Detect(staged.Path)
	if err != nil {
		return formats.Format{}, err
	}
	if format.ZipContainer {
		err = checkArchive(staged.Path)
	}
	return format, err
}

// checkSize refuses files over the source's limit. The reader was capped at limit+1 bytes, so a staged
// size above the limit means the file is too large without reading it all.
func checkSize(size, limit int64) error {
	if size > limit {
		return errs.NewPermanent(errs.ReasonTooLarge, fmt.Errorf("file exceeds the %d-byte limit of this source", limit))
	}
	return nil
}

// checkArchive inspects a ZIP container without extracting it.
func checkArchive(filePath string) error {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return errs.NewPermanent(errs.ReasonCorrupt, fmt.Errorf("unreadable container: %w", err))
	}
	defer r.Close()
	return checkArchiveEntries(r.File)
}

func checkArchiveEntries(files []*zip.File) error {
	if len(files) > maxArchiveEntries {
		return hostileFile(errTooManyEntries)
	}
	var compressed, uncompressed uint64
	for _, f := range files {
		if isUnsafeEntryName(f.Name) {
			return hostileFile(errUnsafePath)
		}
		compressed += f.CompressedSize64
		uncompressed += f.UncompressedSize64
	}
	return checkExpansion(compressed, uncompressed)
}

func checkExpansion(compressed, uncompressed uint64) error {
	if uncompressed > maxArchiveUncompressedBytes {
		return hostileFile(errTooBig)
	}
	if compressed > 0 && uncompressed/compressed > maxArchiveRatio {
		return hostileFile(errRatio)
	}
	return nil
}

func isUnsafeEntryName(name string) bool {
	clean := path.Clean(strings.ReplaceAll(name, `\`, "/"))
	return path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../")
}

func hostileFile(err error) error {
	return errs.NewPermanent(errs.ReasonHostile, err)
}
