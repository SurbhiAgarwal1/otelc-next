package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v5"
	"github.com/sashabaranov/go-openai"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

var (
	redisClient *redis.Client
	dbConn      *pgx.Conn
	openaiCli   *openai.Client
)

// InitTracer sets up a console trace exporter for testing compilation telemetry
func InitTracer() (*sdktrace.TracerProvider, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("microservice-demo"),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}

func main() {
	log.Println("Starting Microservice Distributed Tracing Demo...")

	// 1. Initialize OpenTelemetry Provider
	tp, err := InitTracer()
	if err != nil {
		log.Fatalf("failed to init tracer: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer: %v", err)
		}
	}()

	// 2. Initialize Redis (simulated or real local)
	redisClient = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// 3. Initialize OpenAI Client (using standard library)
	openaiCli = openai.NewClient("mock-api-key-12345")

	// 4. Initialize Gin API Router
	r := gin.Default()

	r.GET("/api/predict", handlePredictRequest)

	// In a real run, this spins up the HTTP server
	log.Println("Server is running on port :8091...")
	// We can start it in a goroutine or call Run. For this demonstration/test,
	// we will run a quick self-test request if requested by env var, or just run.
	if os.Getenv("OTELC_TEST_RUN") == "true" {
		go func() {
			time.Sleep(1 * time.Second)
			log.Println("Self-test: Triggering internal API call...")
			resp, err := http.Get("http://localhost:8091/api/predict")
			if err != nil {
				log.Printf("Self-test request failed: %v", err)
			} else {
				log.Printf("Self-test request succeeded: %s", resp.Status)
			}
			// Force flush tracer provider to display async spans before calling os.Exit
			if tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
				_ = tp.ForceFlush(context.Background())
			}
			time.Sleep(500 * time.Millisecond) // Give time for stdout to sync
			os.Exit(0)
		}()
	}

	_ = r.Run(":8091")
}

// Handlers are kept completely free of tracing boilerplate!
// `otelc` AST Rewriter automatically injects telemetry spans in Gin, DB, Redis and OpenAI calls!
func handlePredictRequest(c *gin.Context) {
	ctx := c.Request.Context()

	// 1. Read key from Redis cache
	log.Println("[API] Querying Redis cache...")
	val, err := redisClient.Get(ctx, "last_prediction").Result()
	if err != nil {
		log.Printf("[API] Redis cache miss: %v", err)
	} else {
		log.Printf("[API] Redis cache hit: %s", val)
	}

	// 2. Run query on Postgres DB
	log.Println("[API] Querying Postgres database...")
	var dbVersion string
	if dbConn != nil {
		err = dbConn.QueryRow(ctx, "SELECT version()").Scan(&dbVersion)
		if err != nil {
			log.Printf("[API] DB query error: %v", err)
		}
	} else {
		log.Println("[API] DB connection mock: Executed SELECT version()")
	}

	// 3. Perform OpenAI Completion query
	log.Println("[API] Invoking OpenAI chat generation...")
	resp, err := openaiCli.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT3Dot5Turbo,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: "Predict the next trending technology in cloud observability.",
			},
		},
	})
	if err != nil {
		log.Printf("[API] GenAI API Mock/Call: %v", err)
	} else {
		log.Printf("[API] GenAI completion response: %s", resp.Choices[0].Message.Content)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"cached_val":  val,
		"db_version":  "PostgreSQL 15.2",
		"prediction":  "Compile-time automated OpenTelemetry AST rewriting is the future!",
		"timestamp":   time.Now().Unix(),
	})
}
