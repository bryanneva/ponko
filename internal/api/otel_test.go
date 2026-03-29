package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOtelHTTPMiddleware(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(t.Context()) }()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := otelhttp.NewMiddleware("ponko",
		otelhttp.WithTracerProvider(tp),
	)(inner)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span from otelhttp middleware, got none")
	}

	span := spans[0]

	// Verify span has HTTP method attribute
	foundMethod := false
	foundStatus := false
	for _, attr := range span.Attributes {
		if attr.Key == "http.request.method" {
			foundMethod = true
			if attr.Value.AsString() != "GET" {
				t.Fatalf("expected http.request.method=GET, got %s", attr.Value.AsString())
			}
		}
		if attr.Key == "http.response.status_code" {
			foundStatus = true
			if attr.Value.AsInt64() != 200 {
				t.Fatalf("expected http.response.status_code=200, got %d", attr.Value.AsInt64())
			}
		}
	}

	if !foundMethod {
		t.Fatal("span missing http.request.method attribute")
	}
	if !foundStatus {
		t.Fatal("span missing http.response.status_code attribute")
	}
}

func TestOtelHTTPMiddleware_StatusCode(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(t.Context()) }()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := otelhttp.NewMiddleware("ponko",
		otelhttp.WithTracerProvider(tp),
	)(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/start", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	foundStatus := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "http.response.status_code" {
			foundStatus = true
			if attr.Value.AsInt64() != 404 {
				t.Fatalf("expected http.response.status_code=404, got %d", attr.Value.AsInt64())
			}
		}
	}

	if !foundStatus {
		t.Fatal("span missing http.response.status_code attribute")
	}
}
