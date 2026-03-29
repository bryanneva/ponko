//go:build e2e

package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/api"
	"github.com/bryanneva/ponko/internal/jobs"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/queue"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/testutil"
	"github.com/bryanneva/ponko/internal/user"
)

type e2eServerOpts struct {
	SigningSecret string
	SlackURL      string
	AnthropicURL  string
	WithUserStore bool
	Timeout       time.Duration
}

type e2eServer struct {
	BaseURL     string
	Pool        *pgxpool.Pool
	SlackClient *slack.Client
	Claude      *llm.Client
	Ctx         context.Context
}

func setupE2EServer(t *testing.T, opts e2eServerOpts) *e2eServer {
	t.Helper()

	if opts.Timeout == 0 {
		opts.Timeout = 120 * time.Second
	}

	var anthropicKey string
	var anthropicURL string
	if opts.AnthropicURL != "" {
		anthropicKey = "fake-key"
		anthropicURL = opts.AnthropicURL
	} else {
		anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
		if anthropicKey == "" {
			t.Skip("ANTHROPIC_API_KEY not set, skipping e2e test")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	t.Cleanup(cancel)

	pool := testutil.TestDB(t)
	runMigrations(t, pool)
	cleanTestData(t, pool)

	claudeClient := llm.NewClient(anthropicKey, anthropicURL)
	slackClient := slack.NewClient("test-bot-token", opts.SlackURL)

	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.ReceiveWorker{Pool: pool})
	processWorker := &jobs.ProcessWorker{Pool: pool, Claude: claudeClient, Slack: slackClient}
	if opts.WithUserStore {
		processWorker.UserStore = &user.Store{Pool: pool, Slack: slackClient}
	}
	river.AddWorker(workers, processWorker)
	river.AddWorker(workers, &jobs.RespondWorker{Pool: pool})
	river.AddWorker(workers, &jobs.SlackReplyWorker{Slack: slackClient})
	river.AddWorker(workers, &jobs.PlanWorker{Pool: pool, Claude: claudeClient, Slack: slackClient})
	river.AddWorker(workers, &jobs.ExecuteWorker{Pool: pool, Claude: claudeClient})
	river.AddWorker(workers, &jobs.SynthesizeWorker{Pool: pool, Claude: claudeClient})

	outboxWorker := &saga.OutboxDeliverWorker{Pool: pool, Slack: slackClient}
	river.AddWorker(workers, outboxWorker)

	periodicJobs := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(2*time.Second),
			func() (river.JobArgs, *river.InsertOpts) {
				return saga.OutboxDeliverArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}

	riverClient, err := queue.New(ctx, pool, workers, periodicJobs)
	if err != nil {
		t.Fatalf("failed to create river client: %v", err)
	}

	if err := riverClient.Start(ctx); err != nil {
		t.Fatalf("failed to start river client: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := riverClient.Stop(context.Background()); stopErr != nil {
			t.Logf("error stopping river client: %v", stopErr)
		}
	})

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	var serverSlackClient *slack.Client
	if opts.SigningSecret != "" {
		serverSlackClient = slackClient
	}

	srv := api.NewServer(fmt.Sprintf("%d", port), pool, riverClient, opts.SigningSecret, serverSlackClient, "", "TestBot", nil, nil, api.AuthConfig{})
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			t.Logf("http server error: %v", serveErr)
		}
	}()
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("error closing server: %v", closeErr)
		}
	})

	return &e2eServer{
		BaseURL:     baseURL,
		Pool:        pool,
		SlackClient: slackClient,
		Claude:      claudeClient,
		Ctx:         ctx,
	}
}

func runMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	files, err := filepath.Glob("../../db/migrations/*.sql")
	if err != nil {
		t.Fatalf("failed to glob migration files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migration files found")
	}
	sort.Strings(files)

	for _, f := range files {
		migrationSQL, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read migration file %s: %v", f, err)
		}

		content := string(migrationSQL)
		upIdx := strings.Index(content, "-- +goose Up")
		downIdx := strings.Index(content, "-- +goose Down")
		if upIdx == -1 || downIdx == -1 {
			t.Fatalf("could not find goose markers in %s", f)
		}

		upSQL := content[upIdx+len("-- +goose Up") : downIdx]
		if _, err := pool.Exec(context.Background(), upSQL); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("failed to run migration %s: %v", f, err)
			}
		}
	}
}

func cleanTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(context.Background(),
		`TRUNCATE workflow_outputs, workflow_steps, conversation_turns, outbox, conversations, workflows, thread_messages, channel_configs, scheduled_messages, users CASCADE`)
	if err != nil {
		t.Fatalf("failed to clean test data: %v", err)
	}

	_, _ = pool.Exec(context.Background(), `DELETE FROM river_job`)
}

type slackPostMessageCapture struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts"`
}

type fakeSlackOpts struct {
	UsersInfoHandler http.HandlerFunc
}

func newFakeSlackServer(t *testing.T, opts *fakeSlackOpts) (*httptest.Server, *sync.Mutex, *[]slackPostMessageCapture) {
	t.Helper()

	var (
		mu       sync.Mutex
		messages []slackPostMessageCapture
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat.postMessage":
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Logf("failed to read slack request body: %v", readErr)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			var msg slackPostMessageCapture
			if unmarshalErr := json.Unmarshal(body, &msg); unmarshalErr != nil {
				t.Logf("failed to unmarshal slack request: %v", unmarshalErr)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			mu.Lock()
			messages = append(messages, msg)
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"ok":true}`)
		case "/reactions.add", "/reactions.remove":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"ok":true}`)
		case "/users.info":
			if opts != nil && opts.UsersInfoHandler != nil {
				opts.UsersInfoHandler(w, r)
			} else {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"ok":true,"user":{"tz":"America/New_York"}}`)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(func() { srv.Close() })

	return srv, &mu, &messages
}

func newFakeAnthropicServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	var callCount int

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("failed to read LLM request body: %v", readErr)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if strings.Contains(string(body), "task planner") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []map[string]string{{"type": "text", "text": `{"action":"direct"}`}},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
			})
			return
		}

		mu.Lock()
		idx := callCount
		callCount++
		mu.Unlock()

		if idx >= len(responses) {
			t.Errorf("unexpected LLM call #%d (only %d responses configured)", idx+1, len(responses))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		response := responses[idx]

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": response}},
			"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
}

func computeSlackSignature(secret string, timestamp string, body []byte) string {
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(baseString))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func sendSlackEvent(t *testing.T, ctx context.Context, baseURL, signingSecret string, payload map[string]any) {
	t.Helper()

	eventBody, _ := json.Marshal(payload)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := computeSlackSignature(signingSecret, timestamp, eventBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/slack/events", strings.NewReader(string(eventBody)))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", signature)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to POST /slack/events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func waitForSlackMessages(t *testing.T, mu *sync.Mutex, messages *[]slackPostMessageCapture, expected int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(*messages)
		mu.Unlock()

		if count >= expected {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	mu.Lock()
	count := len(*messages)
	mu.Unlock()
	t.Fatalf("expected %d Slack messages within timeout, got %d", expected, count)
}
