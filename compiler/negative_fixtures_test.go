package compiler_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ast"
	"github.com/ryanwible/wrela3/compiler/diag"
	"github.com/ryanwible/wrela3/compiler/parse"
	"github.com/ryanwible/wrela3/compiler/sem"
	"github.com/ryanwible/wrela3/compiler/source"
)

var expectFixture = regexp.MustCompile(`^//\s*expect:\s*([A-Z0-9]+):\s*(.+)$`)

type expectedDiag struct {
	Code    string
	Message string
}

func TestNegativeFixtures(t *testing.T) {
	files, err := filepath.Glob("../tests/fixtures/negative/*.wrela")
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	sort.Strings(files)

	if len(files) == 0 {
		t.Fatal("no negative fixtures found")
	}

	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw := mustReadFixture(t, path)
			exp, fixtureSource := parseExpectedHeader(t, path, raw)
			modules := parseFixtureForHarness(t, path, fixtureSource)

			preludeModules(modules)

			index, indexDiags := sem.BuildIndex(modules)
			checkedProgram, checkDiags := sem.Check(index, modules)
			_ = checkedProgram

			diags := append([]diag.Diagnostic{}, indexDiags...)
			diags = append(diags, checkDiags...)

			if !hasExpectedDiag(diags, exp) {
				t.Fatalf("expected %s: %s, got %v", exp.Code, exp.Message, diags)
			}
		})
	}
}

func mustReadFixture(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return string(content)
}

func parseExpectedHeader(t *testing.T, path, src string) (expectedDiag, string) {
	t.Helper()
	lines := strings.SplitN(src, "\n", 2)
	if len(lines) == 0 || !expectFixture.MatchString(lines[0]) {
		t.Fatalf("fixture %s missing // expect header", path)
	}
	match := expectFixture.FindStringSubmatch(lines[0])
	if len(match) != 3 {
		t.Fatalf("fixture %s malformed expect header", path)
	}
	if len(lines) == 1 {
		return expectedDiag{Code: match[1], Message: strings.TrimSpace(match[2])}, ""
	}
	return expectedDiag{Code: match[1], Message: strings.TrimSpace(match[2])}, lines[1]
}

func parseFixtureForHarness(t *testing.T, path, src string) []*ast.Module {
	t.Helper()
	files := fixtureSourceFiles(path, src)
	modules, ds := parse.ParseGraph(source.Graph{
		Files: files,
	})
	if len(ds) != 0 {
		t.Fatalf("parse fixture %s: %#v", path, ds)
	}
	if len(modules) == 0 {
		t.Fatalf("parse fixture %s: no modules", path)
	}
	return modules
}

func fixtureSourceFiles(path, src string) []*source.File {
	parts := splitFixtureModules(src)
	files := make([]*source.File, 0, len(parts))
	for i, part := range parts {
		files = append(files, source.NewFile(source.FileID(i+1), path, part))
	}
	return files
}

func splitFixtureModules(src string) []string {
	lines := strings.Split(src, "\n")
	var parts []string
	var current []string
	for _, line := range lines {
		if strings.HasPrefix(line, "module ") && len(current) != 0 {
			parts = append(parts, strings.Join(current, "\n"))
			current = nil
		}
		current = append(current, line)
	}
	if len(current) != 0 {
		parts = append(parts, strings.Join(current, "\n"))
	}
	return parts
}

func preludeModules(modules []*ast.Module) {
	for _, mod := range modules {
		existing := moduleDeclNames(mod)
		for _, decl := range negativePrelude() {
			name := declarationName(decl)
			if name == "" || existing[name] {
				continue
			}
			mod.Decls = append([]ast.Decl{decl}, mod.Decls...)
			existing[name] = true
		}
	}
}

func negativePrelude() []ast.Decl {
	return []ast.Decl{
		&ast.ClassDecl{
			Name:   "OwnedHardware",
			Unique: true,
			SpanV:  source.Span{},
		},
		&ast.DataDecl{
			Name:   "ExecutorPlacement",
			Fields: []ast.Field{{Name: "id", Type: ast.TypeRef{Name: "U64"}, Span: source.Span{}}},
			SpanV:  source.Span{},
		},
		&ast.ClassDecl{
			Name:   "DelegatedHardware",
			Fields: []ast.Field{},
			Methods: []ast.MethodDecl{
				{
					Name: "exit_to_owned_hardware",
					Params: []ast.Param{
						{Name: "self", Type: ast.TypeRef{Name: "DelegatedHardware"}, Span: source.Span{}},
					},
					Return: ast.TypeRef{Name: "OwnedHardware"},
					Body: []ast.Stmt{
						&ast.ReturnStmt{
							Value: &ast.ConstructorExpr{
								Type:  ast.TypeRef{Name: "OwnedHardware"},
								Args:  nil,
								SpanV: source.Span{},
							},
							SpanV: source.Span{},
						},
					},
					SpanV: source.Span{},
				},
			},
			Unique: true,
			SpanV:  source.Span{},
		},
	}
}

func moduleDeclNames(mod *ast.Module) map[string]bool {
	out := map[string]bool{}
	for _, decl := range mod.Decls {
		name := declarationName(decl)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func declarationName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.DataDecl:
		return d.Name
	case *ast.ClassDecl:
		return d.Name
	case *ast.DriverDecl:
		return d.Name
	case *ast.DriverPathDecl:
		return d.Name
	case *ast.ExecutorDecl:
		return d.Name
	case *ast.ImageDecl:
		return d.Name
	default:
		_ = d
		return ""
	}
}

func hasExpectedDiag(diags []diag.Diagnostic, expected expectedDiag) bool {
	for _, d := range diags {
		if d.Code == expected.Code && strings.TrimSpace(d.Message) == expected.Message {
			return true
		}
	}
	return false
}
