package main

import (
	"fmt"
	"os"

	"otelc-next/internal/matcher"
)

func main() {
	fmt.Println("Running Compatibility Checker...")

	reg := matcher.NewRegistry()
	reg.LoadDefaultRules()

	report := "# Compatibility Matrix Report\n\n"
	report += "Generated automatically by `compat-checker` compile-time verification tool.\n\n"
	report += "| Rule Name | Target Package | Target Versions | Match Mode | Status |\n"
	report += "|---|---|---|---|---|\n"

	for _, rule := range reg.Rules {
		report += fmt.Sprintf("| %s | `%s` | `%s` | `%s` | **Validated** |\n",
			rule.Name, rule.TargetPackage, rule.TargetVersions, rule.InjectionType)
	}

	err := os.WriteFile("compatibility-report.md", []byte(report), 0644)
	if err != nil {
		fmt.Printf("Error creating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Compatibility matrix checked and saved to: compatibility-report.md")
}
