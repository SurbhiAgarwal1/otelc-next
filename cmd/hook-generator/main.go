package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: hook-generator <library-name> <target-package> [symbol]")
		fmt.Println("Example: hook-generator gin github.com/gin-gonic/gin Engine.HandleContext")
		os.Exit(1)
	}

	name := os.Args[1]
	pkg := os.Args[2]
	symbol := "ServeHTTP"
	if len(os.Args) > 3 {
		symbol = os.Args[3]
	}

	fmt.Printf("Scaffolding instrumentation rules for: %s (%s)...\n", name, pkg)

	template := fmt.Sprintf(`name: %s-telemetry
target_package: %s
target_versions: ">= 1.0.0"
target_symbol: %s
inject_imports:
  - otelc-next/internal/telemetry
injection_type: before_after
before_code: |
  // Injected startup code
  ctx, span, startTime := telemetry.StartDBSpan(ctx, "%s", "Query")
after_code: |
  // Injected finalization code
  telemetry.EndDBSpan(span, startTime, "%s", err)
`, name, pkg, symbol, name, name)

	filename := fmt.Sprintf("%s-rules.yaml", name)
	err := os.WriteFile(filename, []byte(template), 0644)
	if err != nil {
		fmt.Printf("Failed to generate hook template: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated rule scaffolding: %s\n", filename)
}
