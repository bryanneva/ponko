package worker

import (
	"context"

	"github.com/riverqueue/river"
)

// PlaceholderArgs defines the job arguments for the placeholder worker.
type PlaceholderArgs struct{}

func (PlaceholderArgs) Kind() string { return "placeholder" }

// PlaceholderWorker is a no-op worker used to verify end-to-end job processing.
type PlaceholderWorker struct {
	river.WorkerDefaults[PlaceholderArgs]
}

func (w *PlaceholderWorker) Work(_ context.Context, _ *river.Job[PlaceholderArgs]) error {
	return nil
}

// RegisterPlaceholder adds the placeholder worker to a River workers set.
func RegisterPlaceholder(workers *river.Workers) {
	river.AddWorker(workers, &PlaceholderWorker{})
}
