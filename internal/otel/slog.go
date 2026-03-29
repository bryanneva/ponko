package otel

import (
	"log/slog"

	sdklog "go.opentelemetry.io/otel/sdk/log"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// SetupSlog configures slog.Default() with the OTel bridge so that log records
// are correlated with trace/span IDs. When lp is nil, slog is left unchanged.
func SetupSlog(lp *sdklog.LoggerProvider) {
	if lp == nil {
		return
	}
	handler := otelslog.NewHandler("ponko", otelslog.WithLoggerProvider(lp))
	slog.SetDefault(slog.New(handler))
}
