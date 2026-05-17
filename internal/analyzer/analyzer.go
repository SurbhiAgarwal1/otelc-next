package analyzer

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"otelc-next/internal/matcher"
	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// PackageReport contains instrumentation readiness metrics for a package.
type PackageReport struct {
	Path             string
	Imports          []string
	MatchingRules    []string
	InstrumentedFiles []string
	SymbolCount      int
}

// AnalyzeDirectory scans a target directory for Go files and compiles an instrumentation map.
func AnalyzeDirectory(dir string, reg *matcher.MatchRegistry) (*PackageReport, error) {
	report := &PackageReport{
		Path:             dir,
		Imports:          make([]string, 0),
		MatchingRules:    make([]string, 0),
		InstrumentedFiles: make([]string, 0),
	}

	importsSet := make(map[string]bool)
	ruleSet := make(map[string]bool)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		// Parse imports from this Go file
		fset := token.NewFileSet()
		file, err := decorator.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil // Skip files that fail to parse during general scanning
		}

		fileHasInstrumentation := false
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
				impPath := strings.Trim(importSpec.Path.Value, `"`)
				importsSet[impPath] = true

				// Check default matching rules
				for _, rule := range reg.Rules {
					if rule.TargetPackage == impPath {
						ruleSet[rule.Name] = true
						fileHasInstrumentation = true
					}
				}
			}
		}

		if fileHasInstrumentation {
			report.InstrumentedFiles = append(report.InstrumentedFiles, filepath.Base(path))
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to analyze directory %s: %w", dir, err)
	}

	for imp := range importsSet {
		report.Imports = append(report.Imports, imp)
	}
	for r := range ruleSet {
		report.MatchingRules = append(report.MatchingRules, r)
	}

	return report, nil
}
