package sources

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
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

// candidate is a staged file and the name it has in its source, which tells an encrypted Office file apart.
type candidate struct {
	staged blobs.Staged
	name   string
}

// checkHostileFile runs the checks of ATLAS.md §8.1 on a staged file and returns its format.
// Every refusal is a permanent error whose reason ends up in quarantine.
func checkHostileFile(c candidate, maxBytes int64) (formats.Format, error) {
	staged := c.staged
	if err := checkSize(staged.Size, maxBytes); err != nil {
		return formats.Format{}, err
	}
	if err := checkNotEncrypted(c); err != nil {
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

// checkNotEncrypted refuses a password-protected file, so the administrator is told to remove the password
// instead of being told the file is corrupt. A PDF names its encryption dictionary in the trailer or, when
// linearized, near the start. A password-protected Office file is not the usual ZIP but an OLE container.
func checkNotEncrypted(c candidate) error {
	encrypted, err := isEncrypted(c)
	if err != nil {
		return fmt.Errorf("check encryption: %w", err)
	}
	if encrypted {
		return errs.NewPermanent(errs.ReasonEncrypted, errors.New("the file is protected by a password"))
	}
	return nil
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

const (
	pdfHeadBytes = 1 << 10  // a linearized PDF declares its encryption in the first kilobyte
	pdfTailBytes = 64 << 10 // any other keeps its trailer at the end
	pdfMagic     = "%PDF-"
	pdfEncrypt   = "/Encrypt"
)

// oleSignature opens every OLE container, the wrapper Office puts around a password-protected OOXML file.
var oleSignature = []byte{0xD0, 0xCF, 0x11, 0xE0}

// ooxmlExtensions are the Office formats that are normally ZIP files; the OLE legacy ones (.doc, .ppt) are not.
var ooxmlExtensions = map[string]bool{".docx": true, ".xlsx": true, ".pptx": true}

func isEncrypted(c candidate) (bool, error) {
	head, tail, err := readEnds(c.staged.Path)
	if err != nil {
		return false, err
	}
	if bytes.HasPrefix(head, []byte(pdfMagic)) {
		return bytes.Contains(head[:min(len(head), pdfHeadBytes)], []byte(pdfEncrypt)) ||
			bytes.Contains(tail, []byte(pdfEncrypt)), nil
	}
	return ooxmlExtensions[strings.ToLower(path.Ext(c.name))] && bytes.HasPrefix(head, oleSignature), nil
}

// readEnds returns the start and the end of a file; for a small file they overlap.
func readEnds(filePath string) (head, tail []byte, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	head = make([]byte, min(info.Size(), pdfHeadBytes))
	if _, err = f.ReadAt(head, 0); err != nil {
		return nil, nil, err
	}
	tail = make([]byte, min(info.Size(), pdfTailBytes))
	_, err = f.ReadAt(tail, info.Size()-int64(len(tail)))
	return head, tail, err
}
