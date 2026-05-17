package rewrite

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"otelc-next/internal/matcher"
	"otelc-next/internal/versions"
)

// InterceptAndCompile intercepts compile command, rewrites files, and executes the real compiler.
func InterceptAndCompile(args []string, reg *matcher.MatchRegistry) error {
	if len(args) == 0 {
		return fmt.Errorf("no arguments provided")
	}

	toolPath := args[0]
	toolName := filepath.Base(toolPath)

	// Passthrough if the tool is not the compiler
	if toolName != "compile" && toolName != "compile.exe" {
		cmd := exec.Command(toolPath, args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		return cmd.Run()
	}

	// It is the compiler! Let's parse compilation arguments
	goFiles := []string{}
	compilerFlags := []string{}
	pkgName := "unknown"
	importCfgPath := ""

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			// Parse key compiler flags
			if arg == "-p" && i+1 < len(args) {
				pkgName = args[i+1]
			}
			if arg == "-importcfg" && i+1 < len(args) {
				importCfgPath = args[i+1]
			}
			compilerFlags = append(compilerFlags, arg)
			// Skip flag argument if it's not a standalone flag
			// In Go compiler, some flags take values. We handle common ones:
			if (arg == "-o" || arg == "-p" || arg == "-importcfg") && i+1 < len(args) {
				compilerFlags = append(compilerFlags, args[i+1])
				i++
			}
		} else if strings.HasSuffix(arg, ".go") {
			goFiles = append(goFiles, arg)
		} else {
			compilerFlags = append(compilerFlags, arg)
		}
	}

	// If no Go source files, pass through immediately
	if len(goFiles) == 0 {
		cmd := exec.Command(toolPath, args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		return cmd.Run()
	}

	if importCfgPath != "" {
		mergeImportCfg(importCfgPath)
	}

	// 1. Resolve target package dependencies from the closest go.mod and env variable
	startDir := filepath.Dir(goFiles[0])
	modInfo, _ := versions.FindAndParseGoMod(startDir)
	activeDeps := versions.GetActiveDepsFromEnv()
	if len(activeDeps) == 0 && modInfo != nil {
		activeDeps = modInfo.Dependencies
		if modInfo.Name != "" {
			activeDeps[modInfo.Name] = "1.0.0"
		}
	} else if modInfo != nil {
		// Merge any extra dependencies found locally
		for k, v := range modInfo.Dependencies {
			if _, exists := activeDeps[k]; !exists {
				activeDeps[k] = v
			}
		}
	}

	f, _ := os.OpenFile("otelc_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintf(f, "[otelc] Compiling pkg: %s, files: %v, activeDeps: %v\n", pkgName, goFiles, activeDeps)
		f.Close()
	}

	// 2. Initialize rewriter
	rewriter := NewRewriter(reg)
	tempFiles := make(map[string]string) // original -> temp
	modifiedAny := false

	// Create a temp directory for instrumented sources
	tempDir, err := os.MkdirTemp("", "otelc-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory for build: %w", err)
	}
	defer func() {
		// Clean up if we didn't crash
		if os.Getenv("OTELC_DEBUG_KEEP_TEMP") == "" {
			os.RemoveAll(tempDir)
		} else {
			fmt.Printf("[otelc debug] Keeping rewritten sources in: %s\n", tempDir)
		}
	}()

	// 3. Process and rewrite files
	newGoFiles := make([]string, len(goFiles))
	for i, file := range goFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read source file %s: %w", file, err)
		}

		rewritten, modified, err := rewriter.RewriteFile(file, content, activeDeps, pkgName)
		if err != nil {
			return fmt.Errorf("failed to rewrite file %s: %w", file, err)
		}

		if modified {
			f, _ := os.OpenFile("otelc_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				fmt.Fprintf(f, "[otelc] SUCCESS: Instrumented file %s for pkg %s!\n", file, pkgName)
				f.Close()
			}
			// Write rewritten content to a temp file
			tempFile := filepath.Join(tempDir, filepath.Base(file))
			err = os.WriteFile(tempFile, rewritten, 0644)
			if err != nil {
				return fmt.Errorf("failed to write rewritten file %s: %w", tempFile, err)
			}
			tempFiles[file] = tempFile
			newGoFiles[i] = tempFile
			modifiedAny = true
		} else {
			newGoFiles[i] = file
		}
	}

	if pkgName == "main" && modifiedAny {
		// Update our compiler's importcfg file to include all mappings from the global registry!
		newImportCfgPath, err := extendImportCfg(importCfgPath)
		if err == nil {
			// Replace -importcfg flag in compilerFlags with newImportCfgPath!
			for idx, arg := range compilerFlags {
				if arg == "-importcfg" && idx+1 < len(compilerFlags) {
					compilerFlags[idx+1] = newImportCfgPath
				}
			}
		}

		// Generate dynamic telemetry helper code
		initCode := `package main

import (
	"context"
	"fmt"
	"time"
	"sync"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
)

var (
	otelcOnce           sync.Once
	otelcMeter          metric.Meter
	otelcHttpServerRec  metric.Float64Histogram
	otelcDbQueryTimer   metric.Float64Histogram
	otelcGenaiTokens    metric.Int64Counter
)

func otelcInit() {
	otelcOnce.Do(func() {
		mp := otel.GetMeterProvider()
		otelcMeter = mp.Meter("otelc-next/runtime")

		var err error
		otelcHttpServerRec, _ = otelcMeter.Float64Histogram("http.server.duration",
			metric.WithDescription("Duration of HTTP server requests in ms"),
			metric.WithUnit("ms"),
		)
		otelcDbQueryTimer, _ = otelcMeter.Float64Histogram("db.client.query.duration",
			metric.WithDescription("Duration of DB queries in ms"),
			metric.WithUnit("ms"),
		)
		otelcGenaiTokens, _ = otelcMeter.Int64Counter("genai.tokens",
			metric.WithDescription("Count of tokens consumed by GenAI models"),
		)
		_ = err
	})
}

func otelcGetTracer() trace.Tracer {
	return otel.GetTracerProvider().Tracer("otelc-next/runtime")
}

func otelcStartGinSpan(req *http.Request, route string) (context.Context, trace.Span, time.Time) {
	otelcInit()
	startTime := time.Now()

	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
			attribute.String("http.target", req.URL.Path),
			attribute.String("http.route", route),
			attribute.String("client.address", req.RemoteAddr),
		),
	}

	spanName := fmt.Sprintf("%s %s", req.Method, route)
	if route == "" {
		spanName = fmt.Sprintf("%s %s", req.Method, req.URL.Path)
	}

	ctx, span := otelcGetTracer().Start(ctx, spanName, opts...)
	return ctx, span, startTime
}

func otelcEndGinSpan(span trace.Span, startTime time.Time, status int, method, route string, err error) {
	duration := time.Since(startTime).Seconds() * 1000.0

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else if status >= 500 {
		span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
	}

	span.SetAttributes(attribute.Int("http.status_code", status))
	span.End()

	if otelcHttpServerRec != nil {
		otelcHttpServerRec.Record(context.Background(), duration, metric.WithAttributes(
			attribute.String("http.method", method),
			attribute.String("http.route", route),
			attribute.Int("http.status_code", status),
		))
	}
}

func otelcStartDBSpan(ctx context.Context, system, query string) (context.Context, trace.Span, time.Time) {
	otelcInit()
	startTime := time.Now()

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", system),
			attribute.String("db.statement", query),
		),
	}

	ctx, span := otelcGetTracer().Start(ctx, fmt.Sprintf("DB Query: %s", system), opts...)
	return ctx, span, startTime
}

func otelcEndDBSpan(span trace.Span, startTime time.Time, system string, err error) {
	duration := time.Since(startTime).Seconds() * 1000.0

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "OK")
	}

	span.End()

	if otelcDbQueryTimer != nil {
		otelcDbQueryTimer.Record(context.Background(), duration, metric.WithAttributes(
			attribute.String("db.system", system),
			attribute.Bool("error", err != nil),
		))
	}
}

func otelcWrapRedisClient[T any](client T) T {
	return client
}

func otelcStartGenAISpan(ctx context.Context, vendor, model string) (context.Context, trace.Span, time.Time) {
	otelcInit()
	startTime := time.Now()

	ctx, span := otelcGetTracer().Start(ctx, fmt.Sprintf("genai.completion %s", vendor),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("genai.system", vendor),
			attribute.String("genai.model", model),
		),
	)
	return ctx, span, startTime
}

func otelcEndGenAISpan(span trace.Span, startTime time.Time, promptTokens, completionTokens int64, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	totalTokens := promptTokens + completionTokens
	span.SetAttributes(
		attribute.Int64("genai.usage.prompt_tokens", promptTokens),
		attribute.Int64("genai.usage.completion_tokens", completionTokens),
		attribute.Int64("genai.usage.total_tokens", totalTokens),
	)
	span.End()

	if otelcGenaiTokens != nil && totalTokens > 0 {
		otelcGenaiTokens.Add(context.Background(), totalTokens, metric.WithAttributes(
			attribute.String("genai.usage.type", "prompt"),
		))
	}
}
`

		telemetryFile := filepath.Join(tempDir, "otelc_telemetry.go")
		err = os.WriteFile(telemetryFile, []byte(initCode), 0644)
		if err != nil {
			return fmt.Errorf("failed to write dynamic telemetry helper file: %w", err)
		}
		newGoFiles = append(newGoFiles, telemetryFile)
	}

	// 4. Reconstruct compiler arguments
	finalArgs := []string{}
	// Add original flags
	finalArgs = append(finalArgs, compilerFlags...)
	// Add Go source files (some original, some rewritten)
	finalArgs = append(finalArgs, newGoFiles...)

	fmt.Fprintf(os.Stderr, "[otelc debug] Intercepted compile of package: %s\n", pkgName)

	if modifiedAny {
		fmt.Fprintf(os.Stderr, "[otelc] Auto-instrumented %d files in package: %s\n", len(tempFiles), pkgName)
	}

	// 5. Invoke original compile tool
	cmd := exec.Command(toolPath, finalArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func mergeImportCfg(path string) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return
	}

	globalRegistryPath := filepath.Join(os.TempDir(), "otelc_global_importcfg.txt")
	f, err := os.OpenFile(globalRegistryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	lines := strings.Split(string(bytes), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "packagefile ") {
			fmt.Fprintln(f, line)
		}
	}
}

func extendImportCfg(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("importcfg path is empty")
	}
	originalBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	globalRegistryPath := filepath.Join(os.TempDir(), "otelc_global_importcfg.txt")
	registryBytes, err := os.ReadFile(globalRegistryPath)
	if err != nil {
		// If registry doesn't exist yet, just use original
		return path, nil
	}

	// Parse existing packagefiles to avoid duplicates
	existing := make(map[string]bool)
	originalLines := strings.Split(string(originalBytes), "\n")
	for _, line := range originalLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "packagefile ") {
			parts := strings.Split(line, "=")
			if len(parts) > 0 {
				existing[parts[0]] = true
			}
		}
	}

	newContent := string(originalBytes)
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	registryLines := strings.Split(string(registryBytes), "\n")
	for _, line := range registryLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "packagefile ") {
			parts := strings.Split(line, "=")
			if len(parts) > 0 {
				pkgPath := parts[0]
				if !existing[pkgPath] {
					newContent += line + "\n"
					existing[pkgPath] = true
				}
			}
		}
	}

	tempFile := filepath.Join(filepath.Dir(path), "otelc_importcfg_extended")
	err = os.WriteFile(tempFile, []byte(newContent), 0644)
	if err != nil {
		return "", err
	}
	return tempFile, nil
}
