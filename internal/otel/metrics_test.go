package otel

import (
	"context"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordWorkflowCompleted(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(nil) })

	// Reset the sync.Once so metrics re-initialize with our test provider
	metricsOnce = sync.Once{}

	RecordWorkflowCompleted(context.Background())
	RecordWorkflowCompleted(context.Background())

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collecting metrics: %v", err)
	}

	found := findCounter(rm, "ponko.workflow.completed")
	if found != 2 {
		t.Errorf("expected ponko.workflow.completed = 2, got %d", found)
	}
}

func TestRecordWorkflowFailed(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(nil) })

	metricsOnce = sync.Once{}

	RecordWorkflowFailed(context.Background())

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collecting metrics: %v", err)
	}

	found := findCounter(rm, "ponko.workflow.failed")
	if found != 1 {
		t.Errorf("expected ponko.workflow.failed = 1, got %d", found)
	}
}

func findCounter(rm metricdata.ResourceMetrics, name string) int64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					var total int64
					for _, dp := range sum.DataPoints {
						total += dp.Value
					}
					return total
				}
			}
		}
	}
	return -1
}
