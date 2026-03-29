package jobs

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	appOtel "github.com/bryanneva/ponko/internal/otel"
)

func startJobSpan(ctx context.Context, kind, workflowID, traceCtx string) (context.Context, trace.Span) {
	ctx = appOtel.ExtractTraceContext(ctx, traceCtx)
	ctx, span := otel.Tracer("ponko/jobs").Start(ctx, fmt.Sprintf("job.%s", kind),
		trace.WithAttributes(attribute.String("workflow.id", workflowID)),
	)
	return ctx, span
}

func recordJobError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
