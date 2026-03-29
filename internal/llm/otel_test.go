package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOutboundHTTPClientProducesSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(t.Context()) }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "hello"}},
		})
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	// Replace the transport with otelhttp-instrumented one using our test provider
	client.httpClient.Transport = otelhttp.NewTransport(http.DefaultTransport,
		otelhttp.WithTracerProvider(tp),
	)

	// Create a parent span so the outbound span is a child
	ctx, span := tp.Tracer("test").Start(context.Background(), "parent")
	defer span.End()

	_, err := client.SendMessage(ctx, "test", ModelHaiku)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Force flush
	span.End()

	spans := exporter.GetSpans()
	if len(spans) < 2 {
		t.Fatalf("expected at least 2 spans (parent + outbound HTTP), got %d", len(spans))
	}

	// Find the HTTP client span (not the parent)
	var httpSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name != "parent" {
			httpSpan = &spans[i]
			break
		}
	}
	if httpSpan == nil {
		t.Fatal("expected an outbound HTTP span, found none")
	}

	// Verify it has HTTP method attribute
	foundMethod := false
	for _, attr := range httpSpan.Attributes {
		if attr.Key == "http.request.method" {
			foundMethod = true
			if attr.Value.AsString() != "POST" {
				t.Errorf("expected http.request.method=POST, got %s", attr.Value.AsString())
			}
		}
	}
	if !foundMethod {
		t.Error("outbound HTTP span missing http.request.method attribute")
	}

	// Verify the HTTP span is a child of the parent span
	parentSpanID := span.SpanContext().SpanID()
	if httpSpan.Parent.SpanID() != parentSpanID {
		t.Errorf("expected HTTP span parent to be %s, got %s", parentSpanID, httpSpan.Parent.SpanID())
	}
}

func TestNewClientUsesOtelTransport(t *testing.T) {
	// Verify that NewClient creates a client with otelhttp transport
	client := NewClient("test-key", "")
	transport := client.httpClient.Transport
	if transport == nil {
		t.Fatal("expected non-nil transport on http client")
	}
	// The transport should not be the default (nil) — it should be wrapped
	if transport == http.DefaultTransport {
		t.Fatal("expected transport to be wrapped with otelhttp, got default transport")
	}
}

func TestOtelTransportInjectsTraceparent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(t.Context()) }()

	// Set global propagator so otelhttp injects traceparent
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var gotTraceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("Traceparent")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "hello"}},
		})
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL)
	client.httpClient.Transport = otelhttp.NewTransport(http.DefaultTransport,
		otelhttp.WithTracerProvider(tp),
	)

	ctx, span := tp.Tracer("test").Start(context.Background(), "parent")
	defer span.End()

	_, err := client.SendMessage(ctx, "test", ModelHaiku)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotTraceparent == "" {
		t.Error("expected traceparent header to be injected on outbound request")
	}
}
