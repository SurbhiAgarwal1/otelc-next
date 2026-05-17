package gin

import (
    "github.com/gin-gonic/gin"
    "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
    "go.opentelemetry.io/otel"
)

// NewTracingMiddleware returns a Gin handler that creates a span for each request.
// It uses the standard otelgin middleware and names spans as "HTTP <METHOD> <PATH>".
func NewTracingMiddleware() gin.HandlerFunc {
    // Wrap the otelgin middleware to customize the span name.
    return otelgin.Middleware("otelc/gin",
        otelgin.WithTracerProvider(otel.GetTracerProvider()),
    )
}

// AttachMiddleware adds the tracing middleware to the provided router.
func AttachMiddleware(r *gin.Engine) {
    r.Use(NewTracingMiddleware())
}
