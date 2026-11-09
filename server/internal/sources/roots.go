package sources

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/morse/atlas/server/internal/platform/errs"
)

// InsideRoots refuses a path that is not under one of the allowed roots. Symbolic links are resolved on both
// sides first, so a link inside a root cannot lead outside it. With no roots configured nothing is allowed.
func InsideRoots(path string, roots []string) error {
	real, err := resolve(path)
	if err != nil {
		return fmt.Errorf("%s is not a readable directory: %w", path, errs.ErrInvalid)
	}
	for _, root := range roots {
		if base, err := resolve(root); err == nil && within(base, real) {
			return nil
		}
	}
	return fmt.Errorf("%s is outside the folders Atlas may read: %w", path, errs.ErrInvalid)
}

func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// within reports whether path is base or inside it, comparing whole path elements ("/srv/ab" is not in "/srv/a").
func within(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
