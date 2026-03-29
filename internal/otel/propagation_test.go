package otel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/bryanneva/ponko/internal/testutil"
)

func TestInjectTraceContext_WithSpan_ReturnsNonEmpty(t *testing.T) {
	testutil.SetupTestTracer(t)

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	traceCtx := InjectTraceContext(ctx)
	if traceCtx == "" {
		t.Fatal("InjectTraceContext should return non-empty string when span is active")
	}
}

func TestInjectTraceContext_NoSpan_ReturnsEmpty(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	traceCtx := InjectTraceContext(context.Background())
	if traceCtx != "" {
		t.Fatalf("InjectTraceContext should return empty string without span, got %q", traceCtx)
	}
}

func TestExtractTraceContext_EmptyString_ReturnsOriginalContext(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	original := context.WithValue(context.Background(), ctxKey("test"), "preserved")
	ctx := ExtractTraceContext(original, "")
	if ctx.Value(ctxKey("test")) != "preserved" {
		t.Fatal("ExtractTraceContext should preserve existing context values")
	}
}

func TestRoundTrip_PreservesTraceID(t *testing.T) {
	testutil.SetupTestTracer(t)

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "original-span")
	originalTraceID := span.SpanContext().TraceID()
	span.End()

	serialized := InjectTraceContext(ctx)
	if serialized == "" {
		t.Fatal("InjectTraceContext returned empty string")
	}

	restored := ExtractTraceContext(context.Background(), serialized)
	restoredSC := trace.SpanContextFromContext(restored)
	if restoredSC.TraceID() != originalTraceID {
		t.Fatalf("trace ID mismatch: got %s, want %s", restoredSC.TraceID(), originalTraceID)
	}
}

type ctxKey string
