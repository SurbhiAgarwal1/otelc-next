package rewrite

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"strings"

	"otelc-next/internal/matcher"
	"otelc-next/internal/versions"
	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// Rewriter manages the rewriting process of a single source file.
type Rewriter struct {
	Registry *matcher.MatchRegistry
}

// NewRewriter creates a new Rewriter instance.
func NewRewriter(reg *matcher.MatchRegistry) *Rewriter {
	return &Rewriter{
		Registry: reg,
	}
}

// RewriteFile parses a Go file, matches and applies instrumentation rules, and returns rewritten bytes.
// Returns (rewrittenBytes, modified, error).
func (r *Rewriter) RewriteFile(filename string, content []byte, activeDeps map[string]string, pkgName string) ([]byte, bool, error) {
	fset := token.NewFileSet()
	file, err := decorator.ParseFile(fset, filename, content, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse file %s: %w", filename, err)
	}

	// 1. Analyze imports in the current file to match package paths
	fileImports := make(map[string]string)
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
			path := strings.Trim(importSpec.Path.Value, `"`)
			// Resolve package version using activeDeps or default "1.0.0"
			ver, ok := activeDeps[path]
			if !ok {
				// Also handle package subpaths (e.g. github.com/gin-gonic/gin/render -> github.com/gin-gonic/gin)
				for depPkg, depVer := range activeDeps {
					if strings.HasPrefix(path, depPkg) {
						ver = depVer
						break
					}
				}
			}
			if ver == "" {
				ver = "1.0.0" // Fallback default
			}
			fileImports[path] = ver
		}
	}

	// 2. Find rules that match the package currently being compiled, OR imported packages
	matchedRules := make([]*matcher.Rule, 0)
	for _, rule := range r.Registry.Rules {
		isCompilingTarget := false
		isImported := false
		if _, ok := fileImports[rule.TargetPackage]; ok {
			isImported = true
		}

		if isCompilingTarget || isImported {
			// Resolve package version
			ver := "1.0.0"
			if v, ok := activeDeps[rule.TargetPackage]; ok {
				ver = v
			}
			if versions.MatchVersion(ver, rule.TargetVersions) {
				matchedRules = append(matchedRules, rule)
			}
		}
	}

	// If this is the main application or a package importing our target packages, run call-site rewriting!
	isAppPackage := (pkgName == "main" || strings.HasSuffix(pkgName, "/main") || strings.Contains(filename, "examples/microservice-demo"))
	if isAppPackage {
		hasTargetImport := false
		for imp := range fileImports {
			if imp == "github.com/gin-gonic/gin" || imp == "github.com/go-redis/redis/v8" ||
				imp == "github.com/sashabaranov/go-openai" || imp == "github.com/jackc/pgx/v5" {
				hasTargetImport = true
				break
			}
		}
		if hasTargetImport {
			rewrittenCallSites, err := rewriteCallSites(file, fileImports)
			if err != nil {
				return nil, false, err
			}
			if rewrittenCallSites {
				var buf bytes.Buffer
				err = decorator.Fprint(&buf, file)
				if err != nil {
					return nil, false, fmt.Errorf("failed to format rewritten AST: %w", err)
				}
				return buf.Bytes(), true, nil
			}
		}
	}

	if len(matchedRules) == 0 {
		return nil, false, nil // No changes
	}

	modified := false

	// 3. Apply matches to the AST/DST declarations
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}

		// Build target symbol name (e.g. "Engine.HandleContext" or "ServeHTTP")
		recvName := ""
		if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
			recvType := funcDecl.Recv.List[0].Type
			recvName = getTypeName(recvType)
		}

		symbolName := funcDecl.Name.Name
		if recvName != "" {
			symbolName = fmt.Sprintf("%s.%s", recvName, symbolName)
		}

		// Match against matchedRules
		for _, rule := range matchedRules {
			if rule.TargetSymbol == symbolName {
				// Inject imports required by the rule
				for _, imp := range rule.InjectImports {
					if EnsureImport(file, imp, "") {
						modified = true
					}
				}

				// Perform injection based on type
				switch rule.InjectionType {
				case "before_after":
					err := injectBeforeAfter(funcDecl, rule.BeforeCode, rule.AfterCode)
					if err != nil {
						return nil, false, fmt.Errorf("error injecting rule %s in %s: %w", rule.Name, symbolName, err)
					}
					modified = true

				case "plugin_register", "wrap_call":
					// Simple hook replacement or register after body
					err := injectPluginRegister(funcDecl, rule.AfterCode)
					if err != nil {
						return nil, false, fmt.Errorf("error injecting rule %s in %s: %w", rule.Name, symbolName, err)
					}
					modified = true
				}
			}
		}
	}

	if !modified {
		return nil, false, nil
	}

	// Print the modified DST back to source code
	var buf bytes.Buffer
	err = decorator.Fprint(&buf, file)
	if err != nil {
		return nil, false, fmt.Errorf("failed to format rewritten AST: %w", err)
	}

	return buf.Bytes(), true, nil
}

// Extract base type name from receiver type (handling pointers, etc.)
func getTypeName(expr dst.Expr) string {
	switch t := expr.(type) {
	case *dst.Ident:
		return t.Name
	case *dst.StarExpr:
		return getTypeName(t.X)
	case *dst.IndexExpr:
		// Go generics support (e.g. Client[T])
		return getTypeName(t.X)
	default:
		return ""
	}
}

// Injects tracing statements at the top of a function and wraps exit code in a defer.
func injectBeforeAfter(funcDecl *dst.FuncDecl, beforeCode, afterCode string) error {
	var bodyPrefix []dst.Stmt

	// 1. Parse and append before statements
	if beforeCode != "" {
		beforeStmts, err := ParseStatements(beforeCode)
		if err != nil {
			return fmt.Errorf("failed to parse before statements: %w", err)
		}
		bodyPrefix = append(bodyPrefix, beforeStmts...)
	}

	// 2. Parse and append after statements in a deferred closure
	if afterCode != "" {
		deferCode := fmt.Sprintf("defer func() {\n%s\n}()", afterCode)
		deferStmts, err := ParseStatements(deferCode)
		if err != nil {
			return fmt.Errorf("failed to parse defer statement: %w", err)
		}
		bodyPrefix = append(bodyPrefix, deferStmts...)
	}

	// Prepend all prefix statements to the function body
	funcDecl.Body.List = append(bodyPrefix, funcDecl.Body.List...)
	return nil
}

// Injects general plugin setup or hook registration.
func injectPluginRegister(funcDecl *dst.FuncDecl, afterCode string) error {
	if afterCode == "" {
		return nil
	}

	registerStmts, err := ParseStatements(afterCode)
	if err != nil {
		return fmt.Errorf("failed to parse register statements: %w", err)
	}

	// Append registrations to the end of the function body
	funcDecl.Body.List = append(funcDecl.Body.List, registerStmts...)
	return nil
}

// walkStmt recursively walks a single statement to find and rewrite nested call expressions.
func walkStmt(stmt dst.Stmt, rewriteExpr func(dst.Expr) dst.Expr) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *dst.ExprStmt:
		s.X = rewriteExpr(s.X)
	case *dst.AssignStmt:
		for i, rhs := range s.Rhs {
			s.Rhs[i] = rewriteExpr(rhs)
		}
	case *dst.ReturnStmt:
		for i, r := range s.Results {
			s.Results[i] = rewriteExpr(r)
		}
	case *dst.IfStmt:
		s.Cond = rewriteExpr(s.Cond)
		walkBlock(s.Body, rewriteExpr)
		if s.Else != nil {
			switch el := s.Else.(type) {
			case *dst.BlockStmt:
				walkBlock(el, rewriteExpr)
			case dst.Stmt:
				walkStmt(el, rewriteExpr)
			}
		}
	case *dst.ForStmt:
		s.Cond = rewriteExpr(s.Cond)
		walkBlock(s.Body, rewriteExpr)
	case *dst.RangeStmt:
		s.X = rewriteExpr(s.X)
		walkBlock(s.Body, rewriteExpr)
	case *dst.BlockStmt:
		walkBlock(s, rewriteExpr)
	case *dst.GoStmt:
		s.Call = rewriteExpr(s.Call).(*dst.CallExpr)
	case *dst.DeferStmt:
		s.Call = rewriteExpr(s.Call).(*dst.CallExpr)
	}
}

// walkBlock recursively walks all statements inside a block.
func walkBlock(block *dst.BlockStmt, rewriteExpr func(dst.Expr) dst.Expr) {
	if block == nil {
		return
	}
	for _, stmt := range block.List {
		walkStmt(stmt, rewriteExpr)
	}
}

// rewriteCallSites scans a main/client file, wraps all driver API call-sites, and appends helper functions.
func rewriteCallSites(file *dst.File, fileImports map[string]string) (bool, error) {
	modified := false

	needGinWrapper := false
	needOpenAIWrapper := false
	needPgxWrapper := false
	needRedisHook := false
	_ = needRedisHook

	// Traverse the AST/DST declarations and walk inside all function bodies
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*dst.FuncDecl)
		if !ok || funcDecl.Body == nil {
			continue
		}

		// Helper to recursively rewrite expressions inside function bodies
		var rewriteExpr func(expr dst.Expr) dst.Expr
		rewriteExpr = func(expr dst.Expr) dst.Expr {
			if expr == nil {
				return nil
			}

			// Handle statement blocks and call expressions
			switch e := expr.(type) {
			case *dst.CallExpr:
				// Recursively rewrite arguments first
				for i, arg := range e.Args {
					e.Args[i] = rewriteExpr(arg)
				}

				// Check call types
				switch fun := e.Fun.(type) {
				case *dst.SelectorExpr:
					// Recursively rewrite receiver
					fun.X = rewriteExpr(fun.X)

					ident, isIdent := fun.X.(*dst.Ident)
					if isIdent && ident.Name == "redis" && (fun.Sel.Name == "NewClient" || fun.Sel.Name == "NewClusterClient") {
						// Wrap redis client call
						needRedisHook = true
						modified = true
						return &dst.CallExpr{
							Fun:  dst.NewIdent("otelcWrapRedisClient"),
							Args: []dst.Expr{e},
						}
					}

					if fun.Sel.Name == "CreateChatCompletion" {
						needOpenAIWrapper = true
						modified = true
						return &dst.CallExpr{
							Fun:  dst.NewIdent("otelcOpenAICreateChatCompletion"),
							Args: []dst.Expr{fun.X, e.Args[0], e.Args[1]},
						}
					}

					if fun.Sel.Name == "QueryRow" {
						if len(e.Args) >= 2 {
							needPgxWrapper = true
							modified = true
							args := []dst.Expr{fun.X, e.Args[0], e.Args[1]}
							if len(e.Args) > 2 {
								args = append(args, e.Args[2:]...)
							}
							return &dst.CallExpr{
								Fun:  dst.NewIdent("otelcPgxQueryRow"),
								Args: args,
							}
						}
					}

					if fun.Sel.Name == "GET" || fun.Sel.Name == "POST" || fun.Sel.Name == "PUT" || fun.Sel.Name == "DELETE" || fun.Sel.Name == "PATCH" || fun.Sel.Name == "Use" {
						if len(e.Args) >= 2 {
							lastIdx := len(e.Args) - 1
							lastArg := e.Args[lastIdx]
							_, isIdent := lastArg.(*dst.Ident)
							_, isFuncLit := lastArg.(*dst.FuncLit)
							if isIdent || isFuncLit {
								needGinWrapper = true
								modified = true
								e.Args[lastIdx] = &dst.CallExpr{
									Fun:  dst.NewIdent("otelcWrapGinHandler"),
									Args: []dst.Expr{lastArg},
								}
							}
						}
					}
				}
			}
			return expr
		}

		// Rewrite statements inside the function body recursively
		for _, stmt := range funcDecl.Body.List {
			walkStmt(stmt, rewriteExpr)
		}
	}

	if !modified {
		return false, nil
	}

	// Inject the required helper wrappers at the end of the file!
	var wrapperDecls []dst.Decl

	if needGinWrapper {
		ginDecls, err := ParseDeclarations(`
func otelcWrapGinHandler(h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span, startTime := otelcStartGinSpan(c.Request, c.FullPath())
		c.Request = c.Request.WithContext(ctx)
		defer func() {
			otelcEndGinSpan(span, startTime, c.Writer.Status(), c.Request.Method, c.FullPath(), nil)
		}()
		h(c)
	}
}
`)
		if err == nil {
			wrapperDecls = append(wrapperDecls, ginDecls...)
		}
	}

	if needOpenAIWrapper {
		openaiDecls, err := ParseDeclarations(`
func otelcOpenAICreateChatCompletion(client *openai.Client, ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	ctx, span, startTime := otelcStartGenAISpan(ctx, "openai", req.Model)
	resp, err := client.CreateChatCompletion(ctx, req)
	var promptTokens, completionTokens int64
	if err == nil {
		promptTokens = int64(resp.Usage.PromptTokens)
		completionTokens = int64(resp.Usage.CompletionTokens)
	}
	otelcEndGenAISpan(span, startTime, promptTokens, completionTokens, err)
	return resp, err
}
`)
		if err == nil {
			wrapperDecls = append(wrapperDecls, openaiDecls...)
		}
	}

	if needPgxWrapper {
		pgxDecls, err := ParseDeclarations(`
func otelcPgxQueryRow(conn *pgx.Conn, ctx context.Context, sql string, args ...any) pgx.Row {
	ctx, span, startTime := otelcStartDBSpan(ctx, "postgresql", sql)
	defer func() {
		otelcEndDBSpan(span, startTime, "postgresql", nil)
	}()
	return conn.QueryRow(ctx, sql, args...)
}
`)
		if err == nil {
			wrapperDecls = append(wrapperDecls, pgxDecls...)
		}
	}

	file.Decls = append(file.Decls, wrapperDecls...)
	return true, nil
}
