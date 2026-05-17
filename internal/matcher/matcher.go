package matcher

import (
	"fmt"
	"os"
	"path/filepath"

	"otelc-next/internal/versions"
	"gopkg.in/yaml.v3"
)

// Rule represents a single declarative instrumentation rule.
type Rule struct {
	Name           string   `yaml:"name"`
	TargetPackage  string   `yaml:"target_package"`
	TargetVersions string   `yaml:"target_versions"`
	TargetSymbol   string   `yaml:"target_symbol"` // e.g. "Engine.HandleContext" or "ServeHTTP" or "NewClient"
	InjectImports  []string `yaml:"inject_imports"`
	InjectionType  string   `yaml:"injection_type"` // "before_after", "plugin_register", "wrap_call"
	BeforeCode     string   `yaml:"before_code"`
	AfterCode      string   `yaml:"after_code"`
}

// MatchRegistry stores all the registered rules.
type MatchRegistry struct {
	Rules []*Rule
}

// NewRegistry creates a new empty rules registry.
func NewRegistry() *MatchRegistry {
	return &MatchRegistry{
		Rules: make([]*Rule, 0),
	}
}

// LoadRulesFromDirectory scans a directory for yaml files and loads rules from them.
func (r *MatchRegistry) LoadRulesFromDirectory(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !file.IsDir() && (filepath.Ext(file.Name()) == ".yaml" || filepath.Ext(file.Name()) == ".yml") {
			path := filepath.Join(dir, file.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read rule file %s: %w", file.Name(), err)
			}

			var rules []*Rule
			// Try parsing as array first
			if err := yaml.Unmarshal(data, &rules); err != nil {
				// Try parsing as single rule
				var rule Rule
				if err2 := yaml.Unmarshal(data, &rule); err2 != nil {
					return fmt.Errorf("failed to unmarshal rule %s: %w (tried array: %v)", file.Name(), err2, err)
				}
				rules = []*Rule{&rule}
			}

			r.Rules = append(r.Rules, rules...)
		}
	}

	return nil
}

// LoadDefaultRules loads our standard core rules.
func (r *MatchRegistry) LoadDefaultRules() {
	// Let's populate default rules for all requested packages programmatically!
	// This ensures out-of-the-box support without external files.
	
	// 1. Gin Rule
	r.Rules = append(r.Rules, &Rule{
		Name:           "gin-http-server",
		TargetPackage:  "github.com/gin-gonic/gin",
		TargetVersions: ">= 1.7.0",
		TargetSymbol:   "Engine.HandleContext",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "before_after",
		BeforeCode: `
ctx, span, startTime := telemetry.StartGinSpan(c.Request, c.FullPath())
c.Request = c.Request.WithContext(ctx)
`,
		AfterCode: `
telemetry.EndGinSpan(span, startTime, c.Writer.Status(), c.Request.Method, c.FullPath(), nil)
`,
	})

	// 2. PGX Query Rule (Conn.Query)
	r.Rules = append(r.Rules, &Rule{
		Name:           "pgx-conn-query",
		TargetPackage:  "github.com/jackc/pgx/v5",
		TargetVersions: ">= 5.0.0",
		TargetSymbol:   "Conn.Query",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "before_after",
		BeforeCode:     `ctx, span, startTime := telemetry.StartDBSpan(ctx, "postgresql", sql)`,
		AfterCode:      `telemetry.EndDBSpan(span, startTime, "postgresql", err)`,
	})

	// 3. GORM DB Open Auto-Register Plugin
	r.Rules = append(r.Rules, &Rule{
		Name:           "gorm-open",
		TargetPackage:  "gorm.io/gorm",
		TargetVersions: ">= 1.20.0",
		TargetSymbol:   "Open",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "plugin_register",
		BeforeCode:     ``,
		AfterCode: `
if err == nil {
	telemetry.RegisterGormPlugin(db)
}
`,
	})

	// 4. Redis Client Auto-Register Hook
	r.Rules = append(r.Rules, &Rule{
		Name:           "redis-client",
		TargetPackage:  "github.com/go-redis/redis/v8",
		TargetVersions: ">= 8.0.0",
		TargetSymbol:   "NewClient",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "plugin_register",
		AfterCode:      `telemetry.RegisterRedisHook(client)`,
	})

	// 5. Kafka Writer Rule (kafka-go)
	r.Rules = append(r.Rules, &Rule{
		Name:           "kafka-producer",
		TargetPackage:  "github.com/segmentio/kafka-go",
		TargetVersions: ">= 0.4.0",
		TargetSymbol:   "Writer.WriteMessages",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "before_after",
		BeforeCode: `
var span trace.Span
ctx, span = telemetry.StartKafkaProducerSpan(ctx, w.Topic)
defer span.End()
`,
	})

	// 6. Logrus Hook Rule
	r.Rules = append(r.Rules, &Rule{
		Name:           "logrus-setup",
		TargetPackage:  "github.com/sirupsen/logrus",
		TargetVersions: ">= 1.8.0",
		TargetSymbol:   "New",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "plugin_register",
		AfterCode: `
if l := log; l != nil {
	l.AddHook(telemetry.NewLogrusHook())
}
`,
	})

	// 7. slog Setup Rule
	r.Rules = append(r.Rules, &Rule{
		Name:           "slog-handler",
		TargetPackage:  "log/slog",
		TargetVersions: ">= 1.21.0",
		TargetSymbol:   "New",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "plugin_register",
		AfterCode:      `h = telemetry.WrapSlogHandler(h)`,
	})

	// 8. client-go Informer Rule
	r.Rules = append(r.Rules, &Rule{
		Name:           "k8sclient-informer",
		TargetPackage:  "k8s.io/client-go/tools/cache",
		TargetVersions: ">= 0.20.0",
		TargetSymbol:   "NewSharedIndexInformer",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "plugin_register",
	})

	// 9. OpenAI client instrument
	r.Rules = append(r.Rules, &Rule{
		Name:           "openai-completion",
		TargetPackage:  "github.com/sashabaranov/go-openai",
		TargetVersions: ">= 1.5.0",
		TargetSymbol:   "Client.CreateChatCompletion",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "before_after",
		BeforeCode:     `ctx, span, startTime := telemetry.StartGenAISpan(ctx, "openai", req.Model)`,
		AfterCode: `
var promptTokens, completionTokens int64
if err == nil {
	promptTokens = int64(resp.Usage.PromptTokens)
	completionTokens = int64(resp.Usage.CompletionTokens)
}
telemetry.EndGenAISpan(span, startTime, promptTokens, completionTokens, err)
`,
	})

	// 10. LangChainGo chain run
	r.Rules = append(r.Rules, &Rule{
		Name:           "langchaingo-chain",
		TargetPackage:  "github.com/tmc/langchaingo/chains",
		TargetVersions: ">= 0.1.0",
		TargetSymbol:   "Call",
		InjectImports:  []string{"otelc-next/internal/telemetry"},
		InjectionType:  "before_after",
		BeforeCode:     `ctx, span, startTime := telemetry.StartGenAISpan(ctx, "langchaingo", "ChainCall")`,
		AfterCode:      `telemetry.EndGenAISpan(span, startTime, 0, 0, err)`,
	})
}

// FindMatchingRules finds all rules that match the imported package path and versions.
func (r *MatchRegistry) FindMatchingRules(imports map[string]string) []*Rule {
	var matches []*Rule

	for _, rule := range r.Rules {
		// Check if package is imported
		ver, imported := imports[rule.TargetPackage]
		if !imported {
			continue
		}

		// Check versions constraint
		if versions.MatchVersion(ver, rule.TargetVersions) {
			matches = append(matches, rule)
		}
	}

	return matches
}
