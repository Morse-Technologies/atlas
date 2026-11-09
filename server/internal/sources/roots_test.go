package sources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsInsideARootPass(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "docs")
	_ = os.Mkdir(sub, 0o750)
	for _, path := range []string{root, sub} {
		if err := InsideRoots(path, []string{root}); err != nil {
			t.Errorf("%s should pass: %v", path, err)
		}
	}
}

func TestASiblingWithTheSamePrefixIsOutside(t *testing.T) {
	parent := t.TempDir()
	root, sibling := filepath.Join(parent, "docs"), filepath.Join(parent, "docs-private")
	_ = os.Mkdir(root, 0o750)
	_ = os.Mkdir(sibling, 0o750)
	if InsideRoots(sibling, []string{root}) == nil {
		t.Fatal("docs-private is not inside docs")
	}
}

func TestALinkInsideARootCannotLeadOutside(t *testing.T) {
	root, secret := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("symlinks not available")
	}
	if InsideRoots(link, []string{root}) == nil {
		t.Fatal("a link out of the root must be refused")
	}
}

func TestNoRootsMeansNothingIsAllowed(t *testing.T) {
	if InsideRoots(t.TempDir(), nil) == nil {
		t.Fatal("with no roots configured nothing may be registered")
	}
}
