package audit

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	serverRoot     = "../.."
	savedListPath  = "../../api/audit-actions.json"
	separateModule = "tools"
)

// Un evento nuevo en el servidor sin añadir a api/audit-actions.json (o al revés) rompe esta prueba:
// la lista guardada es la que la interfaz compara con sus textos.
func TestTheSavedActionListMatchesWhatTheServerWrites(t *testing.T) {
	saved := readSavedActions(t)
	written := actionsWrittenBy(t, serverRoot)

	missing, extra := difference(written, saved), difference(saved, written)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("server/api/audit-actions.json is out of date\nwritten by the server but not in the list: %v\nin the list but no longer written: %v", missing, extra)
	}
}

func readSavedActions(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(savedListPath)
	if err != nil {
		t.Fatalf("read the saved action list: %v", err)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		t.Fatalf("parse the saved action list: %v", err)
	}
	return toSet(names)
}

// actionsWrittenBy reúne los valores de acción que el código de producción bajo root pone en un evento de auditoría.
func actionsWrittenBy(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, files := range parsePackages(t, root) {
		consts := stringConstants(files)
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				for _, value := range actionValues(n) {
					if name, ok := resolveString(value, consts); ok {
						found[name] = true
					}
				}
				return true
			})
		}
	}
	return found
}

// parsePackages agrupa por directorio los ficheros de producción de root, sin el módulo aparte de tools.
func parsePackages(t *testing.T, root string) map[string][]*ast.File {
	t.Helper()
	packages := map[string][]*ast.File{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipSeparateModule(root, path)
		}
		return parseProductionFile(fset, packages, path)
	})
	if err != nil {
		t.Fatalf("read the server code: %v", err)
	}
	return packages
}

func parseProductionFile(fset *token.FileSet, packages map[string][]*ast.File, path string) error {
	if !isProductionGoFile(path) {
		return nil
	}
	file, err := parser.ParseFile(fset, path, nil, 0)
	packages[filepath.Dir(path)] = append(packages[filepath.Dir(path)], file)
	return err
}

func skipSeparateModule(root, dir string) error {
	if dir == filepath.Join(root, separateModule) {
		return filepath.SkipDir
	}
	return nil
}

func isProductionGoFile(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}

// stringConstants devuelve las constantes de texto del paquete (const nombre = "valor").
func stringConstants(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				addStringConstants(consts, spec.(*ast.ValueSpec))
			}
		}
	}
	return consts
}

func addStringConstants(consts map[string]string, spec *ast.ValueSpec) {
	for i, name := range spec.Names {
		if i >= len(spec.Values) {
			return
		}
		if value, ok := stringLiteral(spec.Values[i]); ok {
			consts[name.Name] = value
		}
	}
}

// actionValues devuelve las expresiones que se asignan a un campo Action: `Action: x` o `v.Action = x`.
func actionValues(n ast.Node) []ast.Expr {
	switch node := n.(type) {
	case *ast.KeyValueExpr:
		if key, ok := node.Key.(*ast.Ident); ok && isActionName(key.Name) {
			return []ast.Expr{node.Value}
		}
	case *ast.AssignStmt:
		return assignedToAction(node)
	}
	return nil
}

func assignedToAction(assign *ast.AssignStmt) []ast.Expr {
	var values []ast.Expr
	for i, left := range assign.Lhs {
		selector, ok := left.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Action" && i < len(assign.Rhs) {
			values = append(values, assign.Rhs[i])
		}
	}
	return values
}

func isActionName(name string) bool { return name == "Action" || name == "action" }

// resolveString da el texto de un literal o de una constante del paquete; cualquier otra expresión se ignora.
func resolveString(expr ast.Expr, consts map[string]string) (string, bool) {
	if value, ok := stringLiteral(expr); ok {
		return value, true
	}
	if ident, ok := expr.(*ast.Ident); ok {
		value, found := consts[ident.Name]
		return value, found
	}
	return "", false
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// difference lista, ordenados, los de a que no están en b.
func difference(a, b map[string]bool) []string {
	var only []string
	for name := range a {
		if !b[name] {
			only = append(only, name)
		}
	}
	sort.Strings(only)
	return only
}
