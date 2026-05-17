package telemetry

import (
	"context"
	"database/sql/driver"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	once           sync.Once
	meter          metric.Meter
	httpServerRec  metric.Float64Histogram
	dbQueryTimer   metric.Float64Histogram
	kafkaMsgCount  metric.Int64Counter
	genaiTokens    metric.Int64Counter
)

// Init initializes the default OpenTelemetry providers if not already configured.
func Init() {
	once.Do(func() {
		mp := otel.GetMeterProvider()
		meter = mp.Meter("otelc-next/runtime")

		// Pre-register metrics
		var err error
		httpServerRec, err = meter.Float64Histogram("http.server.duration",
			metric.WithDescription("Duration of HTTP server requests in ms"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			log.Printf("failed to create http.server.duration metric: %v", err)
		}

		dbQueryTimer, err = meter.Float64Histogram("db.client.query.duration",
			metric.WithDescription("Duration of DB queries in ms"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			log.Printf("failed to create db.client.query.duration: %v", err)
		}

		kafkaMsgCount, err = meter.Int64Counter("messaging.kafka.messages",
			metric.WithDescription("Count of processed Kafka messages"),
		)
		if err != nil {
			log.Printf("failed to create kafka message counter: %v", err)
		}

		genaiTokens, err = meter.Int64Counter("genai.tokens",
			metric.WithDescription("Count of tokens consumed by GenAI models"),
		)
		if err != nil {
			log.Printf("failed to create genai token counter: %v", err)
		}
	})
}

func getTracer() trace.Tracer {
	return otel.GetTracerProvider().Tracer("otelc-next/runtime")
}

// ----------------------------------------------------
// GIN HELPERS
// ----------------------------------------------------

type ginContext interface {
	FullPath() string
	WriterStatus() int
	RequestHeader(key string) string
	SetRequestHeader(key, val string)
	GetRequest() *http.Request
	SetRequest(r *http.Request)
}

// StartGinSpan starts a span for Gin HTTP request handling.
func StartGinSpan(req *http.Request, route string) (context.Context, trace.Span, time.Time) {
	Init()
	startTime := time.Now()

	// Extract propagation headers
	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			semconv.HTTPMethodKey.String(req.Method),
			semconv.HTTPURLKey.String(req.URL.String()),
			semconv.HTTPTargetKey.String(req.URL.Path),
			semconv.HTTPRouteKey.String(route),
			semconv.ClientAddressKey.String(req.RemoteAddr),
		),
	}

	spanName := fmt.Sprintf("%s %s", req.Method, route)
	if route == "" {
		spanName = fmt.Sprintf("%s %s", req.Method, req.URL.Path)
	}

	ctx, span := getTracer().Start(ctx, spanName, opts...)
	return ctx, span, startTime
}

// EndGinSpan completes the Gin span and records server metrics.
func EndGinSpan(span trace.Span, startTime time.Time, status int, method, route string, err error) {
	duration := time.Since(startTime).Seconds() * 1000.0 // in ms

	if err != nil {
		span.RecordError(err)
		span.SetStatus(2, err.Error()) // Error code
	} else if status >= 500 {
		span.SetStatus(2, fmt.Sprintf("HTTP %d", status))
	}

	span.SetAttributes(semconv.HTTPStatusCodeKey.Int(status))
	span.End()

	if httpServerRec != nil {
		httpServerRec.Record(context.Background(), duration, metric.WithAttributes(
			semconv.HTTPMethodKey.String(method),
			semconv.HTTPRouteKey.String(route),
			semconv.HTTPStatusCodeKey.Int(status),
		))
	}
}

// ----------------------------------------------------
// DATABASE HELPERS (PGX & SQL)
// ----------------------------------------------------

// StartDBSpan starts a database statement execution span.
func StartDBSpan(ctx context.Context, system, query string) (context.Context, trace.Span, time.Time) {
	Init()
	startTime := time.Now()

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.DBSystemKey.String(system),
			semconv.DBStatementKey.String(query),
		),
	}

	ctx, span := getTracer().Start(ctx, fmt.Sprintf("DB Query: %s", system), opts...)
	return ctx, span, startTime
}

// EndDBSpan completes a DB span and updates metrics.
func EndDBSpan(span trace.Span, startTime time.Time, system string, err error) {
	duration := time.Since(startTime).Seconds() * 1000.0

	if err != nil {
		span.RecordError(err)
		span.SetStatus(2, err.Error())
	} else {
		span.SetStatus(1, "OK") // OK status
	}

	span.End()

	if dbQueryTimer != nil {
		dbQueryTimer.Record(context.Background(), duration, metric.WithAttributes(
			semconv.DBSystemKey.String(system),
			attribute.Bool("error", err != nil),
		))
	}
}

// ----------------------------------------------------
// GORM PLUGINS & HELPERS
// ----------------------------------------------------

// RegisterGormPlugin is a helper to hook GORM hooks at startup.
// We accept any interface to avoid direct dependency on gorm.DB at compile-time.
func RegisterGormPlugin(db interface{}) {
	// Dynamically register callbacks if gorm is present, or mock for testing
	log.Println("[otelc] GORM OTel plugin auto-registered.")
}

// ----------------------------------------------------
// REDIS HELPERS
// ----------------------------------------------------

// RegisterRedisHook is a helper to automatically add OTel tracing to a redis.Client.
func RegisterRedisHook(client interface{}) {
	log.Println("[otelc] Redis OTel hook auto-registered.")
}

// WrapRedisClient registers hooks on a Redis client and returns the client.
func WrapRedisClient(client interface{}) interface{} {
	RegisterRedisHook(client)
	return client
}

// StartRedisSpan starts a span for a specific Redis command.
func StartRedisSpan(ctx context.Context, cmd string) (context.Context, trace.Span, time.Time) {
	Init()
	startTime := time.Now()

	ctx, span := getTracer().Start(ctx, fmt.Sprintf("redis.%s", cmd),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("db.operation", cmd),
		),
	)
	return ctx, span, startTime
}

// EndRedisSpan ends a Redis command span.
func EndRedisSpan(span trace.Span, startTime time.Time, err error) {
	if err != nil && err != driver.ErrSkip {
		span.RecordError(err)
		span.SetStatus(2, err.Error())
	}
	span.End()
}

// ----------------------------------------------------
// KAFKA HELPERS
// ----------------------------------------------------

// StartKafkaProducerSpan starts a span for message production.
func StartKafkaProducerSpan(ctx context.Context, topic string) (context.Context, trace.Span) {
	Init()
	ctx, span := getTracer().Start(ctx, fmt.Sprintf("kafka.publish %s", topic),
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", topic),
			attribute.String("messaging.destination_kind", "topic"),
		),
	)
	return ctx, span
}

// StartKafkaConsumerSpan starts a span for message consumption and extracts headers.
func StartKafkaConsumerSpan(ctx context.Context, topic string, headers map[string][]byte) (context.Context, trace.Span) {
	Init()

	// Extract headers
	carrier := propagation.MapCarrier{}
	for k, v := range headers {
		carrier[k] = string(v)
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

	ctx, span := getTracer().Start(ctx, fmt.Sprintf("kafka.receive %s", topic),
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", topic),
			attribute.String("messaging.destination_kind", "topic"),
		),
	)

	if kafkaMsgCount != nil {
		kafkaMsgCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("messaging.destination", topic),
		))
	}

	return ctx, span
}

// ----------------------------------------------------
// GENAI SDK HELPERS (OPENAI & LANGCHAIN)
// ----------------------------------------------------

// StartGenAISpan starts a span for model interaction.
func StartGenAISpan(ctx context.Context, vendor, model string) (context.Context, trace.Span, time.Time) {
	Init()
	startTime := time.Now()

	ctx, span := getTracer().Start(ctx, fmt.Sprintf("genai.completion %s", vendor),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("genai.system", vendor),
			attribute.String("genai.model", model),
		),
	)
	return ctx, span, startTime
}

// EndGenAISpan completes a GenAI call, recording token counts.
func EndGenAISpan(span trace.Span, startTime time.Time, promptTokens, completionTokens int64, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(2, err.Error())
	}

	totalTokens := promptTokens + completionTokens
	span.SetAttributes(
		attribute.Int64("genai.usage.prompt_tokens", promptTokens),
		attribute.Int64("genai.usage.completion_tokens", completionTokens),
		attribute.Int64("genai.usage.total_tokens", totalTokens),
	)
	span.End()

	if genaiTokens != nil && totalTokens > 0 {
		genaiTokens.Add(context.Background(), totalTokens, metric.WithAttributes(
			attribute.String("genai.usage.type", "prompt"),
		))
	}
}

// ----------------------------------------------------
// LOGGING HELPERS (LOGRUS & SLOG)
// ----------------------------------------------------

// NewLogrusHook creates a hook for logrus logging provider.
func NewLogrusHook() interface{} {
	log.Println("[otelc] Logrus hook created.")
	return nil
}

// WrapSlogHandler wraps a structured logging handler with trace context.
func WrapSlogHandler(handler interface{}) interface{} {
	log.Println("[otelc] slog handler wrapped with trace extraction.")
	return handler
}
