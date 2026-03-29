package otel

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// InjectTraceContext serializes the trace context from ctx into a W3C traceparent string.
// Returns empty string if no active span exists in the context.
func InjectTraceContext(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// ExtractTraceContext deserializes a W3C traceparent string into the given context,
// preserving existing context values (deadlines, River client, etc.).
func ExtractTraceContext(ctx context.Context, traceCtx string) context.Context {
	if traceCtx == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{"traceparent": traceCtx}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
