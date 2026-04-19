package main

import (
	"context"
	"log/slog"
	"time"

	appOtel "github.com/bryanneva/ponko/internal/otel"
)

type httpCloser interface {
	Close() error
}

type jobStopper interface {
	Stop(ctx context.Context) error
}

func gracefulShutdown(srv httpCloser, client jobStopper, telemetry *appOtel.TelemetryState, stopTimeout time.Duration) {
	if err := srv.Close(); err != nil {
		slog.Error("error stopping http server", "error", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), stopTimeout)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		slog.Error("error stopping river client", "error", err)
	}

	if err := telemetry.Shutdown(stopCtx); err != nil {
		slog.Error("error shutting down telemetry", "error", err)
	}

	slog.Info("shutdown complete")
}
