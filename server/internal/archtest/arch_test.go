// Package archtest checks architecture rules that the compiler does not (ATLAS.md §5.2, §5.4, §4.3).
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	modulePath  = "github.com/morse/atlas/server/internal/"
	internalDir = ".."
)

// modules are the business modules; each may import another only through its root package (the facade).
var modules = map[string]bool{
	"sources": true, "documents": true, "catalog": true, "access": true,
	"search": true, "graph": true, "audit": true,
}

// sectorTerms must not appear in Core: sector vocabulary arrives as catalog data from domain packs.
var sectorTerms = regexp.MustCompile(`(?i)\b(machines?|máquinas?|maquinas?|plantas?|plants?|cnc)\b`)

var versionedKind = regexp.MustCompile(`Kind\(\) string\s*\{\s*return "([^"]+)"`)

func goFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(internalDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".gen.go") {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// moduleOf returns the module a file or import path belongs to, and the rest of the path inside it.
func moduleOf(rel string) (string, string) {
	module, rest, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return module, rest
}

func TestModulesImportOnlyFacades(t *testing.T) {
	for _, path := range goFiles(t) {
		own, _ := moduleOf(strings.TrimPrefix(filepath.ToSlash(path), "../"))
		for _, imp := range imports(t, path) {
			checkImport(t, importCheck{file: path, own: own, imp: imp})
		}
	}
}

type importCheck struct {
	file, own, imp string
}

func checkImport(t *testing.T, c importCheck) {
	t.Helper()
	other, rest := moduleOf(strings.TrimPrefix(c.imp, modulePath))
	if strings.HasPrefix(c.imp, modulePath) && modules[other] && other != c.own && rest != "" {
		t.Errorf("%s imports %s: use the %s facade, not its internals", c.file, c.imp, other)
	}
}

func imports(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(f.Imports))
	for i, imp := range f.Imports {
		out[i], _ = strconv.Unquote(imp.Path.Value)
	}
	return out
}

func TestCoreHasNoSectorVocabulary(t *testing.T) {
	for _, path := range goFiles(t) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(path, "arch_test.go") {
			continue
		}
		if m := sectorTerms.Find(content); m != nil {
			t.Errorf("%s mentions %q: sector terms belong in a domain pack", path, m)
		}
	}
}

func TestEventKindsAreVersioned(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(internalDir, "events", "events.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range versionedKind.FindAllSubmatch(content, -1) {
		if !regexp.MustCompile(`\.v[0-9]+$`).Match(m[1]) {
			t.Errorf("job kind %q needs a .vN suffix", m[1])
		}
	}
}
