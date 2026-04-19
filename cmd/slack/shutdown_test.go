package main

import (
	"context"
	"errors"
	"testing"
	"time"

	appOtel "github.com/bryanneva/ponko/internal/otel"
)

type mockCloser struct {
	err    error
	called bool
}

func (m *mockCloser) Close() error {
	m.called = true
	return m.err
}

type mockStopper struct {
	ctx    context.Context
	err    error
	slowBy time.Duration
	called bool
}

func (m *mockStopper) Stop(ctx context.Context) error {
	m.called = true
	m.ctx = ctx
	if m.slowBy > 0 {
		select {
		case <-time.After(m.slowBy):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

func noopTelemetry() *appOtel.TelemetryState {
	ts, _ := appOtel.InitTelemetry(context.Background())
	return ts
}

func TestGracefulShutdown_CallsBothCloseAndStop(t *testing.T) {
	srv := &mockCloser{}
	client := &mockStopper{}

	gracefulShutdown(srv, client, noopTelemetry(), 5*time.Second)

	if !srv.called {
		t.Error("expected http server Close to be called")
	}
	if !client.called {
		t.Error("expected river client Stop to be called")
	}
}

func TestGracefulShutdown_StopCalledEvenIfCloseErrors(t *testing.T) {
	srv := &mockCloser{err: errors.New("close failed")}
	client := &mockStopper{}

	gracefulShutdown(srv, client, noopTelemetry(), 5*time.Second)

	if !client.called {
		t.Error("expected river client Stop to be called even when Close errors")
	}
}

func TestGracefulShutdown_StopReceivesTimeoutContext(t *testing.T) {
	client := &mockStopper{}

	gracefulShutdown(&mockCloser{}, client, noopTelemetry(), 3*time.Second)

	deadline, ok := client.ctx.Deadline()
	if !ok {
		t.Fatal("expected Stop context to have a deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 2*time.Second || remaining > 4*time.Second {
		t.Errorf("expected ~3s deadline, got %v remaining", remaining)
	}
}

func TestGracefulShutdown_StopTimesOutWhenJobsStall(t *testing.T) {
	client := &mockStopper{slowBy: 5 * time.Second}

	start := time.Now()
	gracefulShutdown(&mockCloser{}, client, noopTelemetry(), 100*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("expected shutdown to respect timeout, took %v", elapsed)
	}
}
