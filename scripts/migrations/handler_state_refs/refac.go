package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"log"
	"os"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

func main() {
	// Configure packages to load everything needed for AST manipulation and type checking.
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Dir: ".",
	}

	fmt.Println("Loading packages and analyzing types...")
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		log.Fatalf("Failed to load packages: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			processFile(pkg, file)
		}
	}
}

func processFile(pkg *packages.Package, file *ast.File) {
	// 1. Check if the file imports "**/*/states/*" and determine its alias.
	statesPkgName := ""
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(path, "/states/") || strings.HasSuffix(path, "/states") {
			if imp.Name != nil {
				statesPkgName = imp.Name.Name
			} else {
				parts := strings.Split(path, "/")
				statesPkgName = parts[len(parts)-1]
			}
			break
		}
	}

	// Skip files that don't import states.
	if statesPkgName == "" {
		return
	}

	modified := false

	// 2. Use astutil to traverse and cleanly mutate the AST.
	astutil.Apply(file, func(c *astutil.Cursor) bool {
		// Only evaluate nodes that are direct children of the File (top-level declarations).
		_, isFile := c.Parent().(*ast.File)
		if !isFile {
			return true
		}

		fn, ok := c.Node().(*ast.FuncDecl)
		if !ok || fn.Recv == nil { // Handlers must be methods (have a receiver)
			return true
		}

		// 3. Verify it's a transition handler via precise type checking.
		obj := pkg.TypesInfo.Defs[fn.Name]
		if obj == nil {
			return true
		}

		sig, ok := obj.Type().(*types.Signature)
		if !ok || sig.Params().Len() != 1 {
			return true
		}

		// Verify the argument strictly resolves to `*am.Event`.
		param := sig.Params().At(0)
		ptr, ok := param.Type().(*types.Pointer)
		if !ok {
			return true
		}
		named, ok := ptr.Elem().(*types.Named)
		if !ok || named.Obj().Pkg() == nil {
			return true
		}

		if named.Obj().Pkg().Path() != "github.com/pancsta/asyncmachine-go/pkg/machine" || named.Obj().Name() != "Event" {
			return true
		}

		// 4. Extract the core state name from the transition method name.
		stateName := extractStateName(fn.Name.Name)
		if stateName == "" {
			return true
		}

		// 5. Prevent double-prefixing on subsequent script runs.
		idx := c.Index()
		if idx > 0 {
			if isVarRef(file.Decls[idx-1], statesPkgName, stateName) {
				return true
			}
		}

		// 6. Construct the `var _ = ss.StateName` AST node.
		varDecl := &ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names: []*ast.Ident{ast.NewIdent("_")},
					Values: []ast.Expr{
						&ast.SelectorExpr{
							X:   ast.NewIdent(statesPkgName),
							Sel: ast.NewIdent(stateName),
						},
					},
				},
			},
		}

		// Insert the variable directly before the method.
		c.InsertBefore(varDecl)
		modified = true

		return true
	}, nil)

	// 7. Format and flush changes if the file was mutated.
	if modified {
		filename := pkg.Fset.File(file.Pos()).Name()
		var buf bytes.Buffer
		if err := format.Node(&buf, pkg.Fset, file); err != nil {
			log.Printf("Failed to format %s: %v", filename, err)
			return
		}
		if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
			log.Printf("Failed to write %s: %v", filename, err)
		} else {
			fmt.Printf("Refactored: %s\n", filename)
		}
	}
}

// extractStateName strips the standard asyncmachine transition suffixes/prefixes.
func extractStateName(methodName string) string {
	suffixes := []string{"Enter", "State", "Exit", "End", "Any"}
	for _, s := range suffixes {
		if strings.HasSuffix(methodName, s) {
			return strings.TrimSuffix(methodName, s)
		}
	}
	// "Any" can also serve as a prefix (e.g. AnyStateName).
	if strings.HasPrefix(methodName, "Any") {
		return strings.TrimPrefix(methodName, "Any")
	}
	return ""
}

// isVarRef checks if an AST declaration matches `var _ = pkgName.stateName`.
func isVarRef(decl ast.Decl, pkgName, stateName string) bool {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
		return false
	}
	vs, ok := gen.Specs[0].(*ast.ValueSpec)
	if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "_" || len(vs.Values) != 1 {
		return false
	}
	sel, ok := vs.Values[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != pkgName {
		return false
	}
	return sel.Sel.Name == stateName
}
