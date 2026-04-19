package main

import (
	"context"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/runtime"
)

// GroomTask adapts the groom-per-project logic to the AutomatedTask interface.
// One GroomTask covers all configured groom projects.
type GroomTask struct {
	cfg    *config.Config
	rt     runtime.AgentRuntime
	bus    event.Bus
	ctrl   budget.Controller
	logDir string
}

func NewGroomTask(cfg *config.Config, rt runtime.AgentRuntime, bus event.Bus, ctrl budget.Controller, logDir string) *GroomTask {
	return &GroomTask{cfg: cfg, rt: rt, bus: bus, ctrl: ctrl, logDir: logDir}
}

func (g *GroomTask) Name() string { return "groom" }

// Run executes one groom step per configured project. Per-project errors are
// logged but do not abort remaining projects.
func (g *GroomTask) Run(ctx context.Context) error {
	runGroomProjects(ctx, g.cfg, g.cfg.Groom.Projects, g.rt, g.bus, g.ctrl, g.logDir, false)
	return nil
}
