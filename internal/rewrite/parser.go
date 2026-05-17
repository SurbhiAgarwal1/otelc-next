package rewrite

import (
	"fmt"
	"go/token"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// ParseStatements parses a string block containing Go statements into a slice of dst.Stmt.
func ParseStatements(codeBlock string) ([]dst.Stmt, error) {
	src := fmt.Sprintf("package main\n\nfunc _dummy_func() {\n%s\n}", codeBlock)
	fset := token.NewFileSet()
	file, err := decorator.ParseFile(fset, "dummy_statements.go", src, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse injected statements: %w", err)
	}

	if len(file.Decls) == 0 {
		return nil, fmt.Errorf("no declarations parsed from statement wrapper")
	}

	funcDecl, ok := file.Decls[0].(*dst.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("top declaration was not a function")
	}

	return funcDecl.Body.List, nil
}

// ParseExpr parses a Go expression string into a dst.Expr.
func ParseExpr(exprStr string) (dst.Expr, error) {
	src := fmt.Sprintf("package main\n\nvar _ = %s", exprStr)
	fset := token.NewFileSet()
	file, err := decorator.ParseFile(fset, "dummy_expr.go", src, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse injected expression: %w", err)
	}

	if len(file.Decls) == 0 {
		return nil, fmt.Errorf("no declarations parsed from expression wrapper")
	}

	genDecl, ok := file.Decls[0].(*dst.GenDecl)
	if !ok || len(genDecl.Specs) == 0 {
		return nil, fmt.Errorf("failed to find GenDecl for expression")
	}

	valueSpec, ok := genDecl.Specs[0].(*dst.ValueSpec)
	if !ok || len(valueSpec.Values) == 0 {
		return nil, fmt.Errorf("failed to find ValueSpec for expression")
	}

	return valueSpec.Values[0], nil
}

// EnsureImport adds an import to the file if it does not already exist.
func EnsureImport(file *dst.File, path string, name string) bool {
	// Check if already imported
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec, ok := spec.(*dst.ImportSpec)
			if !ok {
				continue
			}
			if importSpec.Path.Value == fmt.Sprintf(`"%s"`, path) {
				return false // Already exists
			}
		}
	}

	// Create new import spec
	var nameIdent *dst.Ident
	if name != "" {
		nameIdent = dst.NewIdent(name)
	}

	newSpec := &dst.ImportSpec{
		Name: nameIdent,
		Path: &dst.BasicLit{
			Kind:  token.STRING,
			Value: fmt.Sprintf(`"%s"`, path),
		},
	}

	// Find the existing import declaration or insert one
	for i, decl := range file.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if ok && genDecl.Tok == token.IMPORT {
			// Append to first import group
			genDecl.Specs = append(genDecl.Specs, newSpec)
			return true
		}
		// If we reach a non-import declaration, insert our import block before it
		if !ok {
			newDecl := &dst.GenDecl{
				Tok:   token.IMPORT,
				Specs: []dst.Spec{newSpec},
			}
			file.Decls = append(file.Decls[:i], append([]dst.Decl{newDecl}, file.Decls[i:]...)...)
			return true
		}
	}

	// No declarations, just append
	file.Decls = append(file.Decls, &dst.GenDecl{
		Tok:   token.IMPORT,
		Specs: []dst.Spec{newSpec},
	})
	return true
}

// ParseDeclarations parses a Go code block containing declarations into a slice of dst.Decl.
func ParseDeclarations(codeBlock string) ([]dst.Decl, error) {
	src := fmt.Sprintf("package main\n\n%s", codeBlock)
	fset := token.NewFileSet()
	file, err := decorator.ParseFile(fset, "dummy_decls.go", src, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse injected declarations: %w", err)
	}
	return file.Decls, nil
}
