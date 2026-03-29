package otel

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// inMemoryLogExporter captures log records for test assertions.
type inMemoryLogExporter struct {
	records []sdklog.Record
	mu      sync.Mutex
}

func (e *inMemoryLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.records = append(e.records, records...)
	return nil
}

func (e *inMemoryLogExporter) Shutdown(_ context.Context) error { return nil }

func (e *inMemoryLogExporter) ForceFlush(_ context.Context) error { return nil }

func (e *inMemoryLogExporter) Records() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	dst := make([]sdklog.Record, len(e.records))
	copy(dst, e.records)
	return dst
}

func TestSlogBridge_IncludesTraceContext(t *testing.T) {
	traceExp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExp))

	logExp := &inMemoryLogExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logExp)))

	handler := otelslog.NewHandler("test", otelslog.WithLoggerProvider(lp))
	logger := slog.New(handler)

	ctx, span := tp.Tracer("test").Start(context.Background(), "test-op")
	logger.InfoContext(ctx, "hello from traced context", "key", "value")
	span.End()

	records := logExp.Records()
	if len(records) == 0 {
		t.Fatal("expected at least one log record")
	}

	found := false
	for _, record := range records {
		if record.Body().AsString() == "hello from traced context" {
			found = true
			traceID := record.TraceID()
			spanID := record.SpanID()
			if !traceID.IsValid() {
				t.Error("expected valid trace_id on log record")
			}
			if !spanID.IsValid() {
				t.Error("expected valid span_id on log record")
			}
			if traceID != span.SpanContext().TraceID() {
				t.Errorf("trace_id mismatch: got %s, want %s", traceID, span.SpanContext().TraceID())
			}
			if spanID != span.SpanContext().SpanID() {
				t.Errorf("span_id mismatch: got %s, want %s", spanID, span.SpanContext().SpanID())
			}
		}
	}
	if !found {
		t.Fatal("log record with expected message not found")
	}
}

func TestSlogBridge_WithoutSpan_NoTraceContext(t *testing.T) {
	logExp := &inMemoryLogExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logExp)))

	handler := otelslog.NewHandler("test", otelslog.WithLoggerProvider(lp))
	logger := slog.New(handler)

	logger.Info("no span here")

	records := logExp.Records()
	found := false
	for _, record := range records {
		if record.Body().AsString() == "no span here" {
			found = true
			if record.TraceID().IsValid() {
				t.Error("expected invalid trace_id when no span in context")
			}
		}
	}
	if !found {
		t.Fatal("log record not found")
	}
}

func TestSetupSlog_WithLoggerProvider(t *testing.T) {
	logExp := &inMemoryLogExporter{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logExp)))

	oldDefault := slog.Default()
	defer slog.SetDefault(oldDefault)

	SetupSlog(lp)

	traceExp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExp))
	ctx, span := tp.Tracer("test").Start(context.Background(), "test-op")
	slog.InfoContext(ctx, "default logger traced")
	span.End()

	records := logExp.Records()
	found := false
	for _, record := range records {
		if record.Body().AsString() == "default logger traced" {
			found = true
			if !record.TraceID().IsValid() {
				t.Error("expected valid trace_id via slog.Default()")
			}
		}
	}
	if !found {
		t.Fatal("log record not found via slog.Default()")
	}
}

func TestSetupSlog_NilProvider_NoOp(t *testing.T) {
	oldDefault := slog.Default()
	defer slog.SetDefault(oldDefault)

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

	SetupSlog(nil)

	slog.Info("still works")
	if buf.Len() == 0 {
		t.Error("expected slog to still produce output with nil provider")
	}
}
