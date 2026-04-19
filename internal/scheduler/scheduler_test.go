package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// countTask counts how many times Run has been called.
type countTask struct {
	name  string
	calls atomic.Int32
}

func (c *countTask) Name() string { return c.name }
func (c *countTask) Run(_ context.Context) error {
	c.calls.Add(1)
	return nil
}

// errorTask always returns an error.
type errorTask struct{}

func (e *errorTask) Name() string                { return "error-task" }
func (e *errorTask) Run(_ context.Context) error { return errors.New("boom") }

// blockingTask blocks until its release channel is closed.
type blockingTask struct {
	release chan struct{}
	started chan struct{}
	name    string
}

func (b *blockingTask) Name() string { return b.name }
func (b *blockingTask) Run(_ context.Context) error {
	close(b.started)
	<-b.release
	return nil
}

func TestScheduler_RunsAllTasksOnTick(t *testing.T) {
	a := &countTask{name: "a"}
	b := &countTask{name: "b"}

	sched := New(time.Hour, []AutomatedTask{a, b})
	sched.startedAt = time.Now()
	sched.runAll(context.Background())

	if a.calls.Load() != 1 {
		t.Errorf("task a: want 1 call, got %d", a.calls.Load())
	}
	if b.calls.Load() != 1 {
		t.Errorf("task b: want 1 call, got %d", b.calls.Load())
	}
}

func TestScheduler_RunsTasksSerially(t *testing.T) {
	// Verify that runAll executes tasks serially: the second task does not run
	// until the first completes. This is the backpressure guarantee.
	release := make(chan struct{})
	started := make(chan struct{})
	slow := &blockingTask{name: "slow", release: release, started: started}
	fast := &countTask{name: "fast"}

	sched := New(time.Hour, []AutomatedTask{slow, fast})
	sched.startedAt = time.Now()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.runAll(context.Background())
	}()

	// Wait until slow.Run has started.
	<-started

	// fast should not have run yet — slow is still blocking.
	if fast.calls.Load() != 0 {
		t.Error("expected fast task not to have run while slow is blocking")
	}

	close(release)
	<-done

	if fast.calls.Load() != 1 {
		t.Errorf("expected fast to run once after slow completes, got %d", fast.calls.Load())
	}
}

func TestScheduler_StopsOnContextCancel(t *testing.T) {
	task := &countTask{name: "t"}
	sched := New(time.Hour, []AutomatedTask{task})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.Start(ctx)
	}()

	cancel()

	select {
	case <-done:
		// pass
	case <-time.After(200 * time.Millisecond):
		t.Error("Start did not return after context cancellation")
	}
}

func TestScheduler_StatusesReflectRunning(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	slow := &blockingTask{name: "slow", release: release, started: started}

	sched := New(time.Hour, []AutomatedTask{slow})
	sched.startedAt = time.Now()

	ctx := context.Background()
	go sched.runAll(ctx)
	<-started

	statuses := sched.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].Running {
		t.Error("expected running=true while task executes")
	}

	close(release)
	time.Sleep(20 * time.Millisecond)

	statuses = sched.Statuses()
	if statuses[0].Running {
		t.Error("expected running=false after task completes")
	}
	if statuses[0].LastRunAt == nil {
		t.Error("expected LastRunAt to be set after task completes")
	}
}

func TestScheduler_StatusesRecordLastError(t *testing.T) {
	sched := New(time.Hour, []AutomatedTask{&errorTask{}})
	sched.startedAt = time.Now()
	sched.runAll(context.Background())

	statuses := sched.Statuses()
	if statuses[0].LastError != "boom" {
		t.Errorf("expected LastError=boom, got %q", statuses[0].LastError)
	}
}

func TestScheduler_NextRunAtAdvancesAfterRun(t *testing.T) {
	task := &countTask{name: "t"}
	sched := New(15*time.Minute, []AutomatedTask{task})
	sched.startedAt = time.Now()

	before := time.Now()
	sched.runAll(context.Background())

	statuses := sched.Statuses()
	if statuses[0].LastRunAt == nil {
		t.Fatal("LastRunAt should be set")
	}
	nextRun := statuses[0].NextRunAt
	if nextRun.Before(before.Add(14 * time.Minute)) {
		t.Errorf("NextRunAt %v looks too early", nextRun)
	}
}
