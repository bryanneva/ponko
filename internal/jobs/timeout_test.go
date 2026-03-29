package jobs

import (
	"testing"

	"github.com/riverqueue/river"
)

func TestProcessWorkerTimeout(t *testing.T) {
	w := &ProcessWorker{}
	got := w.Timeout(&river.Job[ProcessArgs]{})
	if got != toolUseJobTimeout {
		t.Errorf("expected %v, got %v", toolUseJobTimeout, got)
	}
}

func TestExecuteWorkerTimeout(t *testing.T) {
	w := &ExecuteWorker{}
	got := w.Timeout(&river.Job[ExecuteArgs]{})
	if got != toolUseJobTimeout {
		t.Errorf("expected %v, got %v", toolUseJobTimeout, got)
	}
}

func TestPlanWorkerTimeout(t *testing.T) {
	w := &PlanWorker{}
	got := w.Timeout(&river.Job[PlanArgs]{})
	if got != singleCallTimeout {
		t.Errorf("expected %v, got %v", singleCallTimeout, got)
	}
}

func TestSynthesizeWorkerTimeout(t *testing.T) {
	w := &SynthesizeWorker{}
	got := w.Timeout(&river.Job[SynthesizeArgs]{})
	if got != singleCallTimeout {
		t.Errorf("expected %v, got %v", singleCallTimeout, got)
	}
}

func TestProactiveMessageWorkerTimeout(t *testing.T) {
	w := &ProactiveMessageWorker{}
	got := w.Timeout(&river.Job[ProactiveMessageArgs]{})
	if got != toolUseJobTimeout {
		t.Errorf("expected %v, got %v", toolUseJobTimeout, got)
	}
}
