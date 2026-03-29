package jobs

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/testutil"
)

func TestStartJobSpan_CreatesSpanWithCorrectName(t *testing.T) {
	exporter := testutil.SetupTestTracer(t)

	ctx := context.Background()
	_, span := startJobSpan(ctx, "plan", "wf-123", "")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	if spans[0].Name != "job.plan" {
		t.Errorf("span name = %q, want %q", spans[0].Name, "job.plan")
	}
}

func TestStartJobSpan_SetsWorkflowIDAttribute(t *testing.T) {
	exporter := testutil.SetupTestTracer(t)

	ctx := context.Background()
	_, span := startJobSpan(ctx, "execute", "wf-456", "")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "workflow.id" && attr.Value.AsString() == "wf-456" {
			found = true
		}
	}
	if !found {
		t.Fatal("span missing workflow.id=wf-456 attribute")
	}
}

func TestStartJobSpan_LinksToParentTraceContext(t *testing.T) {
	_ = testutil.SetupTestTracer(t)

	tracer := otel.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "parent")
	parentTraceID := parentSpan.SpanContext().TraceID()
	traceCtx := appOtel.InjectTraceContext(parentCtx)
	parentSpan.End()

	ctx, span := startJobSpan(context.Background(), "process", "wf-789", traceCtx)
	span.End()

	sc := trace.SpanFromContext(ctx).SpanContext()
	if sc.TraceID() != parentTraceID {
		t.Errorf("trace ID = %s, want %s (parent trace)", sc.TraceID(), parentTraceID)
	}
}

func TestStartJobSpan_EmptyTraceContext_StillCreatesSpan(t *testing.T) {
	exporter := testutil.SetupTestTracer(t)

	ctx := context.Background()
	_, span := startJobSpan(ctx, "respond", "wf-000", "")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span even without trace context")
	}

	if spans[0].Name != "job.respond" {
		t.Errorf("span name = %q, want %q", spans[0].Name, "job.respond")
	}
}

func TestStartJobSpan_ErrorRecordedOnSpan(t *testing.T) {
	exporter := testutil.SetupTestTracer(t)

	_, span := startJobSpan(context.Background(), "plan", "wf-err", "")
	recordJobError(span, fmt.Errorf("something went wrong"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	if spans[0].Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error", spans[0].Status.Code)
	}

	if len(spans[0].Events) == 0 {
		t.Fatal("expected error event on span")
	}

	foundExceptionMessage := false
	for _, event := range spans[0].Events {
		if event.Name == "exception" {
			for _, attr := range event.Attributes {
				if attr.Key == "exception.message" && attr.Value.AsString() == "something went wrong" {
					foundExceptionMessage = true
				}
			}
		}
	}
	if !foundExceptionMessage {
		t.Fatal("expected exception.message attribute on error event")
	}
}
