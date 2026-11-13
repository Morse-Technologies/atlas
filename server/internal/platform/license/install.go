package license

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	licenseDirMode  = 0o750
	licenseFileMode = 0o640
)

// Install checks a license file and, only if it is good, makes it the one in force: it is written where every
// process reads it, and takes effect at once. A refused file leaves the current license as it was. The error of a
// refusal carries a fixed reason that is safe to show (errs.WithPublicDetail), never the content of the file.
func (h *Holder) Install(raw []byte) (License, error) {
	lic, err := Parse(raw, h.Key, h.Now())
	if err == nil && lic.Plan == PlanPro && hasRunOut(lic.ValidUntil, h.Now()) {
		err = &Rejected{Reason: ReasonExpired}
	}
	if err != nil {
		return License{}, refusal(err)
	}
	if err := h.store(raw, lic); err != nil {
		return License{}, err
	}
	return lic, nil
}

func refusal(err error) error {
	return errs.WithPublicDetail(fmt.Errorf("%w: %w", err, errs.ErrInvalid), reasonText[ReasonOf(err)])
}

// store writes the file by renaming a finished temporary one, so a reader never sees half a license.
func (h *Holder) store(raw []byte, lic License) error {
	if h.Path == "" {
		return errors.New("license: this installation has no place to keep a license file")
	}
	if err := os.MkdirAll(filepath.Dir(h.Path), licenseDirMode); err != nil {
		return fmt.Errorf("license: create folder: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.Path), ".license-*")
	if err != nil {
		return fmt.Errorf("license: write: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone after the rename; cleans up after a failure
	if err := writeAndClose(tmp, raw); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), h.Path); err != nil {
		return fmt.Errorf("license: replace: %w", err)
	}
	return h.remember(lic)
}

func writeAndClose(f *os.File, raw []byte) error {
	_, writeErr := f.Write(raw)
	chmodErr := f.Chmod(licenseFileMode)
	if closeErr := f.Close(); writeErr != nil || chmodErr != nil || closeErr != nil {
		return fmt.Errorf("license: write: %w", firstError(writeErr, chmodErr, closeErr))
	}
	return nil
}

func firstError(failures ...error) error {
	for _, err := range failures {
		if err != nil {
			return err
		}
	}
	return nil
}

// remember makes lic the current license without waiting for the next read of the file.
func (h *Holder) remember(lic License) error {
	info, err := os.Stat(h.Path)
	if err != nil {
		return fmt.Errorf("license: check: %w", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.current, h.loaded = lic, info.ModTime()
	return nil
}
