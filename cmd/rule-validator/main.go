package main

import (
	"fmt"
	"os"

	"otelc-next/internal/matcher"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: rule-validator <rules-directory>")
		os.Exit(1)
	}

	dir := os.Args[1]
	fmt.Printf("Analyzing and linting rules in: %s...\n", dir)

	reg := matcher.NewRegistry()
	err := reg.LoadRulesFromDirectory(dir)
	if err != nil {
		fmt.Printf("Error scanning directory: %v\n", err)
		os.Exit(1)
	}

	if len(reg.Rules) == 0 {
		fmt.Println("Warning: No rule files found (*.yaml / *.yml).")
		os.Exit(0)
	}

	errors := 0
	for _, rule := range reg.Rules {
		fmt.Printf("Linting rule '%s':\n", rule.Name)
		if rule.TargetPackage == "" {
			fmt.Println("  [FAIL] Missing 'target_package'")
			errors++
		}
		if rule.TargetSymbol == "" {
			fmt.Println("  [FAIL] Missing 'target_symbol'")
			errors++
		}
		if rule.InjectionType == "" {
			fmt.Println("  [FAIL] Missing 'injection_type'")
			errors++
		} else if rule.InjectionType != "before_after" && rule.InjectionType != "plugin_register" && rule.InjectionType != "wrap_call" {
			fmt.Printf("  [FAIL] Invalid injection_type '%s'\n", rule.InjectionType)
			errors++
		}

		if rule.BeforeCode == "" && rule.AfterCode == "" {
			fmt.Println("  [FAIL] Both 'before_code' and 'after_code' are empty")
			errors++
		}
	}

	if errors > 0 {
		fmt.Printf("\nValidation failed with %d errors.\n", errors)
		os.Exit(1)
	}

	fmt.Println("\nAll rules conform perfectly to CNCF Otelc schemas!")
}
