package otel

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

type TelemetryState struct {
	TracerProvider trace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider

	// sdkTracer is needed separately because TracerProvider may be noop
	sdkTracer *sdktrace.TracerProvider
}

func (ts *TelemetryState) Shutdown(ctx context.Context) error {
	var errs []error
	if ts.sdkTracer != nil {
		errs = append(errs, ts.sdkTracer.Shutdown(ctx))
	}
	if ts.MeterProvider != nil {
		errs = append(errs, ts.MeterProvider.Shutdown(ctx))
	}
	if ts.LoggerProvider != nil {
		errs = append(errs, ts.LoggerProvider.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

func InitTelemetry(ctx context.Context) (*TelemetryState, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	exporterMode := os.Getenv("OTEL_EXPORTER")

	if endpoint == "" && exporterMode != "stdout" {
		slog.Info("OTel disabled: no endpoint configured")
		mp := sdkmetric.NewMeterProvider()
		lp := sdklog.NewLoggerProvider()
		return &TelemetryState{
			TracerProvider: nooptrace.NewTracerProvider(),
			MeterProvider:  mp,
			LoggerProvider: lp,
		}, nil
	}

	var tp *sdktrace.TracerProvider
	var mp *sdkmetric.MeterProvider
	var lp *sdklog.LoggerProvider

	if exporterMode == "stdout" {
		traceExp, err := stdouttrace.New()
		if err != nil {
			return nil, err
		}
		metricExp, err := stdoutmetric.New()
		if err != nil {
			return nil, err
		}
		logExp, err := stdoutlog.New()
		if err != nil {
			return nil, err
		}
		tp = sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp))
		mp = sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)))
		lp = sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)))
	} else {
		traceExp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		metricExp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, err
		}
		logExp, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, err
		}
		tp = sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp))
		mp = sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)))
		lp = sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)))
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	SetupSlog(lp)

	if err := runtime.Start(); err != nil {
		slog.Warn("failed to start runtime metrics", "error", err)
	}

	slog.Info("OTel initialized", "endpoint", endpoint, "exporter", exporterMode)

	return &TelemetryState{
		TracerProvider: tp,
		MeterProvider:  mp,
		LoggerProvider: lp,
		sdkTracer:      tp,
	}, nil
}
