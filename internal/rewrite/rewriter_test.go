package rewrite

import (
	"strings"
	"testing"

	"otelc-next/internal/matcher"
)

func TestRewriter_RewriteFile(t *testing.T) {
	// 1. Create a dummy Gin-based source file contents
	src := `package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
)

type Engine struct{}

func (engine *Engine) HandleContext(c *gin.Context) {
	fmt.Println("Processing standard request...")
	c.Next()
}
`

	// 2. Initialize MatchRegistry with default rules
	reg := matcher.NewRegistry()
	reg.LoadDefaultRules()

	// 3. Initialize Rewriter
	rewriter := NewRewriter(reg)

	// Mock active dependencies
	activeDeps := map[string]string{
		"github.com/gin-gonic/gin": "v1.9.1",
	}

	// 4. Run rewriter
	rewritten, modified, err := rewriter.RewriteFile("test.go", []byte(src), activeDeps, "github.com/gin-gonic/gin")
	if err != nil {
		t.Fatalf("failed to rewrite file: %v", err)
	}

	if !modified {
		t.Fatalf("expected file to be modified by rules")
	}

	output := string(rewritten)

	// 5. Assertions on rewritten code
	if !strings.Contains(output, `"otelc-next/internal/telemetry"`) {
		t.Errorf("expected rewritten file to import our runtime telemetry library")
	}

	if !strings.Contains(output, "telemetry.StartGinSpan") {
		t.Errorf("expected rewritten file to call telemetry.StartGinSpan")
	}

	if !strings.Contains(output, "telemetry.EndGinSpan") {
		t.Errorf("expected rewritten file to defer telemetry.EndGinSpan")
	}

	if !strings.Contains(output, "c.Next()") {
		t.Errorf("expected rewritten file to preserve original c.Next() call")
	}

	t.Logf("Rewritten output:\n%s", output)
}
