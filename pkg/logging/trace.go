package logging

import (
	"context"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/trace"
)

// WithContext returns a logger enriched with trace information from ctx.
// If ctx contains a valid OpenTelemetry SpanContext, the returned logger
// will include the trace ID (hex encoded) and whether the span was sampled.
func WithContext(ctx context.Context, log logr.Logger) logr.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return log
	}
	return log.WithValues(
		"traceid", sc.TraceID().String(),
		"sampled", sc.IsSampled(),
	)
}
