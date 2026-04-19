// Package serve provides the HTTP observability dashboard for ponko-runner.
package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
	"github.com/bryanneva/ponko/internal/devrouter"
	"github.com/bryanneva/ponko/internal/event"
	"github.com/bryanneva/ponko/internal/groom"
	"github.com/bryanneva/ponko/internal/scheduler"
	"github.com/bryanneva/ponko/internal/sqlite"
	"github.com/bryanneva/ponko/internal/task"
)

// Server is the observability HTTP server.
type Server struct {
	ctrl          budget.Controller
	store         *sqlite.TaskStore
	pipelineStore *sqlite.PipelineStore
	sched         *scheduler.Scheduler
	eventsPath    string
	logDir        string
	repos         []config.Repo
	groomProjects []config.GroomProject
	budgetCfg     config.Budget
}

// New creates a Server.
func New(
	store *sqlite.TaskStore,
	ctrl budget.Controller,
	budgetCfg config.Budget,
	eventsPath string,
	repos []config.Repo,
	groomProjects []config.GroomProject,
	logDir string,
	sched *scheduler.Scheduler,
) *Server {
	return &Server{
		store:         store,
		ctrl:          ctrl,
		budgetCfg:     budgetCfg,
		eventsPath:    eventsPath,
		repos:         repos,
		groomProjects: groomProjects,
		logDir:        logDir,
		sched:         sched,
	}
}

// SetPipelineStore attaches a pipeline store for the /api/pipelines endpoints.
func (s *Server) SetPipelineStore(ps *sqlite.PipelineStore) { s.pipelineStore = ps }

// Handler returns an http.Handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tasks", s.handleTasks)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/budget", s.handleBudget)
	mux.HandleFunc("GET /api/jobs", s.handleJobs)
	mux.HandleFunc("GET /api/jobs/{run_id}/log", s.handleJobLog)
	mux.HandleFunc("GET /api/scheduler", s.handleScheduler)
	mux.HandleFunc("GET /api/pipelines", s.handlePipelines)
	mux.HandleFunc("GET /api/pipelines/{id}", s.handlePipelineByID)
	mux.Handle("GET /", http.HandlerFunc(handleDashboard))
	return mux
}

func (s *Server) handleScheduler(w http.ResponseWriter, _ *http.Request) {
	if s.sched == nil {
		writeJSON(w, []scheduler.TaskStatus{})
		return
	}
	writeJSON(w, s.sched.Statuses())
}

// taskResponse is the JSON shape returned by /api/tasks.
type taskResponse struct {
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	IssueURL    string    `json:"issue_url"`
	Repo        string    `json:"repo"`
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Workflow    string    `json:"workflow"`
	Status      string    `json:"status"`
	Phase       string    `json:"phase"`
	BlockReason string    `json:"block_reason,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	IssueNumber int       `json:"issue_number"`
	CostUSD     float64   `json:"cost_usd"`
	Attempts    int       `json:"attempts"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	active, err := s.store.ListByStatus(ctx,
		task.StatusQueued, task.StatusInProgress, task.StatusBlocked)
	if err != nil {
		writeError(w, fmt.Errorf("list active: %w", err))
		return
	}

	recent, err := s.store.ListByStatus(ctx, task.StatusCompleted, task.StatusFailed)
	if err != nil {
		writeError(w, fmt.Errorf("list recent: %w", err))
		return
	}

	// Limit completed/failed to 20 most recent (ListByStatus returns ASC order).
	if len(recent) > 20 {
		recent = recent[len(recent)-20:]
	}

	all := append(active, recent...) //nolint:gocritic
	out := make([]taskResponse, 0, len(all))
	for _, t := range all {
		out = append(out, taskResponse{
			ID:          t.ID,
			IssueURL:    t.IssueURL,
			Repo:        t.Repo,
			IssueNumber: t.IssueNumber,
			Title:       t.Title,
			Workflow:    t.Workflow,
			Status:      string(t.Status),
			Phase:       t.Phase,
			BlockReason: t.BlockReason,
			Attempts:    t.Attempts,
			LastError:   t.LastError,
			CostUSD:     t.CostUSD,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
		})
	}

	writeJSON(w, out)
}

// budgetResponse is the JSON shape returned by /api/budget.
type budgetResponse struct {
	Date         string  `json:"date"`
	DaySpent     float64 `json:"day_spent"`
	DayLimit     float64 `json:"day_limit"`
	DayRemaining float64 `json:"day_remaining"`
}

func (s *Server) handleBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := time.Now().UTC().Format("2006-01-02")

	spent, err := s.ctrl.DaySpent(ctx, date)
	if err != nil {
		writeError(w, fmt.Errorf("day spent: %w", err))
		return
	}

	remaining := s.budgetCfg.PerDayUSD - spent
	if remaining < 0 {
		remaining = 0
	}

	writeJSON(w, budgetResponse{
		Date:         date,
		DaySpent:     spent,
		DayLimit:     s.budgetCfg.PerDayUSD,
		DayRemaining: remaining,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
		limit = n
	}

	events, err := tailEvents(s.eventsPath, limit)
	if err != nil {
		writeError(w, fmt.Errorf("read events: %w", err))
		return
	}

	writeJSON(w, events)
}

// scanEvents reads all events from the JSONL file at path.
// Returns nil (not empty slice) when the file doesn't exist.
func scanEvents(path string) ([]event.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var all []event.Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e event.Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		all = append(all, e)
	}
	return all, scanner.Err()
}

// tailEvents reads the last n events from the JSONL file.
func tailEvents(path string, n int) ([]event.Event, error) {
	all, err := scanEvents(path)
	if err != nil {
		return nil, err
	}
	if all == nil {
		return []event.Event{}, nil
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

// readAllEvents reads every event from the JSONL file.
func readAllEvents(path string) ([]event.Event, error) {
	return scanEvents(path)
}

// Job run status values used in jobRunResponse.
const (
	jobStatusRunning   = "running"
	jobStatusCompleted = "completed"
	jobStatusFailed    = "failed"
)

// jobRunResponse describes one groom (or future job) execution.
type jobRunResponse struct {
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	RunID        string     `json:"run_id"`
	JobType      string     `json:"job_type"`
	Status       string     `json:"status"`
	Projects     []string   `json:"projects"`
	IssueCount   int        `json:"issue_count"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	DurationSecs float64    `json:"duration_secs"`
}

// groomProjectResponse summarises pipeline-state counts for one groom project.
type groomProjectResponse struct {
	Project    string `json:"project"`
	LastRun    string `json:"last_run,omitempty"`
	Pending    int    `json:"pending"`
	Evaluating int    `json:"evaluating"`
	Groomed    int    `json:"groomed"`
	Blocked    int    `json:"blocked"`
	HasGaps    int    `json:"has_gaps"`
}

// jobsResponse is the shape returned by GET /api/jobs.
type jobsResponse struct {
	Runs     []jobRunResponse       `json:"runs"`
	Projects []groomProjectResponse `json:"projects"`
}

func (s *Server) handleJobs(w http.ResponseWriter, _ *http.Request) {
	events, err := readAllEvents(s.eventsPath)
	if err != nil {
		writeError(w, fmt.Errorf("read events: %w", err))
		return
	}

	runs := buildJobRuns(events)
	projects := s.buildGroomProjects(events)

	writeJSON(w, jobsResponse{Runs: runs, Projects: projects})
}

// buildJobRuns groups job.* events by CorrelationID into run summaries.
func buildJobRuns(events []event.Event) []jobRunResponse {
	type runState struct {
		startedAt    time.Time
		projects     map[string]struct{}
		completedAt  *time.Time
		jobType      string
		status       string
		issueCount   int
		totalCost    float64
		durationSecs float64
	}

	// Preserve insertion order via a slice of keys.
	order := []string{}
	states := map[string]*runState{}

	for _, e := range events {
		if !strings.HasPrefix(string(e.Type), "job.") {
			continue
		}
		id := e.CorrelationID
		if id == "" {
			continue
		}
		if _, ok := states[id]; !ok {
			states[id] = &runState{
				projects: make(map[string]struct{}),
				status:   jobStatusRunning,
			}
			order = append(order, id)
		}
		rs := states[id]

		switch e.Type {
		case event.JobStarted:
			if jt, ok := e.Payload["job_type"].(string); ok {
				rs.jobType = jt
			}
			if p, ok := e.Payload["project"].(string); ok && p != "" {
				rs.projects[p] = struct{}{}
			}
			if n, ok := e.Payload["issue_number"].(float64); ok && n > 0 {
				rs.issueCount++
			}
			rs.startedAt = e.Timestamp

		case event.JobCompleted:
			if p, ok := e.Payload["project"].(string); ok && p != "" {
				rs.projects[p] = struct{}{}
			}
			if c, ok := e.Payload["cost_usd"].(float64); ok {
				rs.totalCost += c
			}
			if d, ok := e.Payload["duration_secs"].(float64); ok {
				rs.durationSecs += d
			}
			t := e.Timestamp
			rs.completedAt = &t
			rs.status = jobStatusCompleted

		case event.JobFailed:
			if p, ok := e.Payload["project"].(string); ok && p != "" {
				rs.projects[p] = struct{}{}
			}
			t := e.Timestamp
			rs.completedAt = &t
			rs.status = jobStatusFailed
		}
	}

	out := make([]jobRunResponse, 0, len(order))
	for _, id := range order {
		rs := states[id]
		projectList := make([]string, 0, len(rs.projects))
		for p := range rs.projects {
			projectList = append(projectList, p)
		}
		out = append(out, jobRunResponse{
			RunID:        id,
			JobType:      rs.jobType,
			Projects:     projectList,
			IssueCount:   rs.issueCount,
			TotalCostUSD: rs.totalCost,
			DurationSecs: rs.durationSecs,
			Status:       rs.status,
			StartedAt:    rs.startedAt,
			CompletedAt:  rs.completedAt,
		})
	}
	return out
}

// buildGroomProjects reads state files for each configured groom project.
func (s *Server) buildGroomProjects(events []event.Event) []groomProjectResponse {
	// Build repo name → path map.
	repoPaths := make(map[string]string, len(s.repos))
	for _, r := range s.repos {
		repoPaths[r.Name] = r.Path
	}

	// Build last-run map: project name → latest completed/failed event timestamp.
	lastRun := map[string]time.Time{}
	for _, e := range events {
		if e.Type != event.JobCompleted && e.Type != event.JobFailed {
			continue
		}
		p, _ := e.Payload["project"].(string)
		if p == "" {
			continue
		}
		if e.Timestamp.After(lastRun[p]) {
			lastRun[p] = e.Timestamp
		}
	}

	out := make([]groomProjectResponse, 0, len(s.groomProjects))
	for _, gp := range s.groomProjects {
		repoPath := repoPaths[gp.Repo]
		if repoPath == "" {
			continue
		}

		states, err := groom.ReadIssueStates(repoPath)
		if err != nil {
			continue
		}

		resp := groomProjectResponse{Project: gp.Name}
		for _, st := range states {
			switch st.Pipeline.State {
			case groom.StatePending:
				resp.Pending++
			case groom.StateEvaluating:
				resp.Evaluating++
			case groom.StateGroomed:
				resp.Groomed++
			case groom.StateBlocked:
				resp.Blocked++
			case groom.StateHasGaps:
				resp.HasGaps++
			}
		}

		if t, ok := lastRun[gp.Name]; ok {
			resp.LastRun = t.UTC().Format(time.RFC3339)
		}

		out = append(out, resp)
	}
	return out
}

// logResponse is the shape returned by GET /api/jobs/{run_id}/log.
type logResponse struct {
	Lines      []string `json:"lines"`
	NextOffset int64    `json:"next_offset"`
	Done       bool     `json:"done"`
}

func (s *Server) handleJobLog(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	offsetStr := r.URL.Query().Get("offset")
	var offset int64
	if n, err := strconv.ParseInt(offsetStr, 10, 64); err == nil && n > 0 {
		offset = n
	}

	// Guard against path traversal via URL-supplied run_id.
	if strings.ContainsAny(runID, "/\\.") {
		http.NotFound(w, r)
		return
	}

	logPath := filepath.Join(s.logDir, runID+".log")
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, logResponse{Lines: []string{}, NextOffset: 0, Done: s.isJobDone(runID)})
			return
		}
		writeError(w, fmt.Errorf("open log: %w", err))
		return
	}
	defer func() { _ = f.Close() }()

	_, err = f.Seek(offset, io.SeekStart)
	if err != nil {
		writeError(w, fmt.Errorf("seek log: %w", err))
		return
	}

	const maxLogChunk = 512 * 1024 // 512KB per poll keeps responses snappy
	data, err := io.ReadAll(io.LimitReader(f, maxLogChunk))
	if err != nil {
		writeError(w, fmt.Errorf("read log: %w", err))
		return
	}

	nextOffset := offset + int64(len(data))

	var lines []string
	if len(data) > 0 {
		raw := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for _, l := range raw {
			if l != "" {
				lines = append(lines, l)
			}
		}
	}
	if lines == nil {
		lines = []string{}
	}

	writeJSON(w, logResponse{
		Lines:      lines,
		NextOffset: nextOffset,
		Done:       s.isJobDone(runID),
	})
}

// isJobDone returns true if a job.completed or job.failed event exists for the given run ID.
//
// CORRECTNESS: must use readAllEvents (full scan), not tailEvents(N).
// CorrelationID lookup spans the entire event log — a tail scan would miss
// historical run completions once the log exceeds N events (i.e., 100+ groom runs).
func (s *Server) isJobDone(runID string) bool {
	all, err := readAllEvents(s.eventsPath)
	if err != nil {
		return false
	}
	for _, e := range all {
		if e.CorrelationID == runID && (e.Type == event.JobCompleted || e.Type == event.JobFailed) {
			return true
		}
	}
	return false
}

// pipelineResponse is the JSON shape returned by /api/pipelines.
type pipelineResponse struct {
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
	Stage                   string    `json:"stage"`
	Repo                    string    `json:"repo"`
	Track                   string    `json:"track"`
	ID                      string    `json:"id"`
	ClassificationRationale string    `json:"classification_rationale"`
	IssueURL                string    `json:"issue_url"`
	TaskID                  string    `json:"task_id"`
	IssueNumber             int       `json:"issue_number"`
	StoryCount              int       `json:"story_count"`
	StoriesCompleted        int       `json:"stories_completed"`
	CurrentStoryIndex       int       `json:"current_story_index"`
	PRNumber                int       `json:"pr_number"`
	CostUSD                 float64   `json:"cost_usd"`
}

// pipelineDetailResponse extends pipelineResponse with plan_output as parsed JSON.
type pipelineDetailResponse struct {
	PlanOutput any `json:"plan_output"`
	pipelineResponse
}

func toPipelineResponse(p *devrouter.Pipeline) pipelineResponse {
	return pipelineResponse{
		ID:                      p.ID,
		TaskID:                  p.TaskID,
		IssueURL:                p.IssueURL,
		Repo:                    p.Repo,
		IssueNumber:             p.IssueNumber,
		Track:                   string(p.Track),
		Stage:                   string(p.Stage),
		StoryCount:              p.StoryCount,
		StoriesCompleted:        p.StoriesCompleted,
		CurrentStoryIndex:       p.CurrentStoryIndex,
		PRNumber:                p.PRNumber,
		ClassificationRationale: p.ClassificationRationale,
		CostUSD:                 p.CostUSD,
		CreatedAt:               p.CreatedAt,
		UpdatedAt:               p.UpdatedAt,
	}
}

func (s *Server) handlePipelines(w http.ResponseWriter, r *http.Request) {
	if s.pipelineStore == nil {
		writeJSON(w, []pipelineResponse{})
		return
	}

	pipelines, err := s.pipelineStore.List(r.Context(), 0)
	if err != nil {
		writeError(w, fmt.Errorf("list pipelines: %w", err))
		return
	}

	out := make([]pipelineResponse, 0, len(pipelines))
	for _, p := range pipelines {
		out = append(out, toPipelineResponse(p))
	}

	writeJSON(w, out)
}

func (s *Server) handlePipelineByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if s.pipelineStore == nil {
		http.NotFound(w, r)
		return
	}

	p, err := s.pipelineStore.Get(r.Context(), id)
	if err != nil {
		writeError(w, fmt.Errorf("get pipeline: %w", err))
		return
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}

	base := toPipelineResponse(p)

	var planOutput any
	if p.PlanOutput != "" {
		if err := json.Unmarshal([]byte(p.PlanOutput), &planOutput); err != nil {
			planOutput = p.PlanOutput // fall back to raw string
		}
	}

	writeJSON(w, pipelineDetailResponse{pipelineResponse: base, PlanOutput: planOutput})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// ListenAndServe starts the HTTP server on addr (e.g. ":8765").
// It blocks until ctx is cancelled, then performs a graceful shutdown.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	fmt.Printf("ponko-runner dashboard: http://localhost%s\n", addr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
