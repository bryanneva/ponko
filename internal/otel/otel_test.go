package otel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestInitTelemetry_NoEndpoint_ReturnsNoOp(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ts, err := InitTelemetry(context.Background())
	if err != nil {
		t.Fatalf("InitTelemetry() error = %v", err)
	}
	defer func() {
		if err := ts.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	}()

	if ts.TracerProvider == nil {
		t.Fatal("TracerProvider should not be nil")
	}
	if ts.MeterProvider == nil {
		t.Fatal("MeterProvider should not be nil")
	}
	if ts.LoggerProvider == nil {
		t.Fatal("LoggerProvider should not be nil")
	}

	// No-op provider: creating a tracer and span should work without panic
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()
}

func TestInitTelemetry_WithEndpoint_ReturnsSdkProvider(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")

	ts, err := InitTelemetry(context.Background())
	if err != nil {
		t.Fatalf("InitTelemetry() error = %v", err)
	}
	// Shutdown may fail because no collector is running — that's expected
	defer func() { _ = ts.Shutdown(context.Background()) }()

	// Should return SDK TracerProvider, not no-op
	if _, ok := ts.TracerProvider.(*trace.TracerProvider); !ok {
		t.Fatalf("expected *trace.TracerProvider, got %T", ts.TracerProvider)
	}
}

func TestInitTelemetry_StdoutExporter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER", "stdout")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ts, err := InitTelemetry(context.Background())
	if err != nil {
		t.Fatalf("InitTelemetry() error = %v", err)
	}
	defer func() {
		if err := ts.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	}()

	// Stdout mode should return SDK provider, not no-op
	if _, ok := ts.TracerProvider.(*trace.TracerProvider); !ok {
		t.Fatalf("expected *trace.TracerProvider, got %T", ts.TracerProvider)
	}
}

func TestShutdown_NoError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ts, err := InitTelemetry(context.Background())
	if err != nil {
		t.Fatalf("InitTelemetry() error = %v", err)
	}

	if err := ts.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}
