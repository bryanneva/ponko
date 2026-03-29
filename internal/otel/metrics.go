package otel

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	workflowCompleted metric.Int64Counter
	workflowFailed    metric.Int64Counter
	metricsOnce       sync.Once
)

func initMetrics() {
	meter := otel.Meter("ponko/workflow")
	var err error
	workflowCompleted, err = meter.Int64Counter("ponko.workflow.completed",
		metric.WithDescription("Number of workflows that completed successfully"))
	if err != nil {
		slog.Warn("failed to create ponko.workflow.completed counter", "error", err)
	}
	workflowFailed, err = meter.Int64Counter("ponko.workflow.failed",
		metric.WithDescription("Number of workflows that failed"))
	if err != nil {
		slog.Warn("failed to create ponko.workflow.failed counter", "error", err)
	}
}

func RecordWorkflowCompleted(ctx context.Context) {
	metricsOnce.Do(initMetrics)
	if workflowCompleted != nil {
		workflowCompleted.Add(ctx, 1)
	}
}

func RecordWorkflowFailed(ctx context.Context) {
	metricsOnce.Do(initMetrics)
	if workflowFailed != nil {
		workflowFailed.Add(ctx, 1)
	}
}
