package unittest

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQueryFieldTypesAreVisible(t *testing.T) {
	pc := make([]uintptr, 1)
	assert.Equal(t, 1, runtime.Callers(2, pc))
	frame, more := runtime.CallersFrames(pc).Next()
	assert.False(t, more)
	pkgPathPart := frame.Function[:strings.LastIndex(frame.Function, `/`)]
	pkgPath := frame.File[strings.Index(frame.File, pkgPathPart):strings.LastIndex(frame.File, "/")]
	imported, err := build.Import(pkgPath, ".", build.FindOnly)
	assert.NoError(t, err)
	pkgs, first := parser.ParseDir(token.NewFileSet(), imported.Dir, nil, 0)
	assert.NoError(t, first)

	var calledFunctions map[string]struct{}
	var requiredFunctions []string
	for _, pkg := range pkgs {
		if strings.HasSuffix(pkg.Name, `_test`) {
			calledFunctions = collectCalledFunctions(t, pkg)
		} else {
			requiredFunctions = append(requiredFunctions, collectRequiredFunctions(t, pkg)...)
		}
	}
	if calledFunctions == nil {
		t.Fatalf(`"_test" package is not defined`)
	}
	for _, requiredFunction := range requiredFunctions {
		if _, ok := calledFunctions[requiredFunction]; !ok {
			t.Errorf("function %v was not mentioned in tests, "+
				"therefore it could not be properly tested by TestQueryFieldTypeConflicts. "+
				"Just add it to a global `var` of the \"*_test\" package",
				requiredFunction)
		}
	}
}

func collectRequiredFunctions(t *testing.T, pkg *ast.Package) (requiredFunctions []string) {
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if !decl.Name.IsExported() {
					continue
				}
				if decl.Type.Results == nil || len(decl.Type.Results.List) < 0 {
					continue
				}
				for _, result := range decl.Type.Results.List {
					switch _type := result.Type.(type) {
					case *ast.Ident:
						if _type.Name == `QueryField` {
							requiredFunctions = append(requiredFunctions, decl.Name.Name)
						}
					}
				}
			}
		}
	}
	return
}

func collectCalledFunctions(t *testing.T, pkg *ast.Package) map[string]struct{} {
	result := map[string]struct{}{}

	var scanRecursiveExpr func(stmt ast.Expr)
	scanRecursiveExpr = func(expr ast.Expr) { // preserved for future needs
		switch expr := expr.(type) {
		case *ast.SelectorExpr:
			scanRecursiveExpr(expr.Sel)
		case *ast.CallExpr:
			scanRecursiveExpr(expr.Fun)
		case *ast.Ident:
			result[expr.Name] = struct{}{}
		}
	}

	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			ast.Inspect(decl, func(node ast.Node) bool {
				callExpr, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				scanRecursiveExpr(callExpr)
				return true
			})
		}
	}
	return result
}
