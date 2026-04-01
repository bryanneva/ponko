package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/bryanneva/ponko/internal/api"
	"github.com/bryanneva/ponko/internal/db"
	"github.com/bryanneva/ponko/internal/jobs"
	"github.com/bryanneva/ponko/internal/llm"
	"github.com/bryanneva/ponko/internal/mcp"
	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/queue"
	"github.com/bryanneva/ponko/internal/saga"
	"github.com/bryanneva/ponko/internal/schedule"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/user"
	"github.com/bryanneva/ponko/web"
)

func main() {
	slog.Info("starting server")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://agent:agent@localhost:5433/agent_dev?sslmode=disable"
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		slog.Error("failed to parse database config", "error", err)
		os.Exit(1)
	}
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.MaxConnLifetime = 30 * time.Minute

	pool, poolErr := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if poolErr != nil {
		slog.Error("failed to connect to database", "error", poolErr)
		os.Exit(1)
	}
	defer pool.Close()

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "/app/db/migrations"
	}
	if migrateErr := db.Migrate(ctx, pool, migrationsDir); migrateErr != nil {
		slog.Error("failed to run migrations", "error", migrateErr)
		os.Exit(1)
	}
	slog.Info("app migrations complete")

	telemetry, telErr := appOtel.InitTelemetry(ctx)
	if telErr != nil {
		slog.Error("failed to initialize telemetry", "error", telErr)
		os.Exit(1)
	}

	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		slog.Warn("ANTHROPIC_API_KEY not set, ProcessJob will fail")
	}
	claudeClient := llm.NewClient(anthropicKey, "")

	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackBotToken == "" {
		slog.Warn("SLACK_BOT_TOKEN not set, Slack replies will fail")
	}
	slackClient := slack.NewClient(slackBotToken, "")

	var mcpMultiClient *mcp.MultiClient
	var servers []mcp.Server
	mcpTransport := otelhttp.NewTransport(http.DefaultTransport)

	mcpServerURLs := os.Getenv("MCP_SERVER_URLS")
	if mcpServerURLs != "" {
		accessKey := os.Getenv("MCP_ACCESS_KEY")
		for _, rawURL := range strings.Split(mcpServerURLs, ",") {
			u := strings.TrimSpace(rawURL)
			if u == "" {
				continue
			}
			servers = append(servers, &mcp.Client{
				HTTP:        &http.Client{Timeout: 30 * time.Second, Transport: mcpTransport},
				BaseURL:     u,
				BearerToken: accessKey,
			})
		}
	}

	if linearURL := os.Getenv("LINEAR_MCP_URL"); linearURL != "" {
		linearClient := &mcp.Client{
			HTTP:        &http.Client{Timeout: 30 * time.Second, Transport: mcpTransport},
			BaseURL:     linearURL,
			BearerToken: os.Getenv("LINEAR_MCP_ACCESS_TOKEN"),
		}
		servers = append(servers, &mcp.OAuthRefresher{
			Client:       linearClient,
			TokenURL:     os.Getenv("LINEAR_MCP_TOKEN_URL"),
			ClientID:     os.Getenv("LINEAR_MCP_CLIENT_ID"),
			ClientSecret: os.Getenv("LINEAR_MCP_CLIENT_SECRET"),
			RefreshToken: os.Getenv("LINEAR_MCP_REFRESH_TOKEN"),
		})
	}

	if githubURL := os.Getenv("GITHUB_MCP_URL"); githubURL != "" {
		if githubPAT := os.Getenv("GITHUB_PAT"); githubPAT != "" {
			servers = append(servers, &mcp.Client{
				HTTP:        &http.Client{Timeout: 30 * time.Second, Transport: mcpTransport},
				BaseURL:     githubURL,
				BearerToken: githubPAT,
			})
		} else {
			slog.Warn("GITHUB_MCP_URL set but GITHUB_PAT missing, GitHub tools disabled")
		}
	}

	if len(servers) == 0 {
		slog.Warn("no MCP servers configured, MCP tools will be disabled")
	} else {
		var multiErr error
		mcpMultiClient, multiErr = mcp.NewMultiClient(ctx, servers)
		if multiErr != nil {
			slog.Warn("MCP tool discovery failed, tools will be disabled", "error", multiErr)
		}
	}

	tzName := os.Getenv("TIMEZONE")
	if tzName == "" {
		tzName = "America/Los_Angeles"
	}
	tz, tzErr := time.LoadLocation(tzName)
	if tzErr != nil {
		slog.Warn("invalid TIMEZONE, falling back to America/Los_Angeles", "timezone", tzName, "error", tzErr)
		var fallbackErr error
		tz, fallbackErr = time.LoadLocation("America/Los_Angeles")
		if fallbackErr != nil {
			slog.Error("failed to load fallback timezone", "error", fallbackErr)
			os.Exit(1)
		}
	}

	botName := os.Getenv("BOT_NAME")
	if botName == "" {
		botName = "Ponko"
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.ReceiveWorker{Pool: pool})
	userStore := &user.Store{Pool: pool, Slack: slackClient}

	scheduleTools := []llm.Tool{
		schedule.CreateScheduleTool,
		schedule.ListSchedulesTool,
		schedule.CancelScheduleTool,
		slack.ReadSlackThreadTool,
	}

	var mcpClient llm.ToolCaller
	if mcpMultiClient != nil {
		mcpClient = mcpMultiClient
	}
	toolDispatcher := &jobs.ToolDispatcher{
		Pool:      pool,
		UserStore: userStore,
		MCPClient: mcpClient,
		Slack:     slackClient,
	}

	var allTools []llm.Tool
	if mcpMultiClient != nil {
		allTools = append(allTools, mcpMultiClient.Tools()...)
	}
	allTools = append(allTools, scheduleTools...)

	processWorker := &jobs.ProcessWorker{
		SystemPrompt: os.Getenv("SYSTEM_PROMPT"),
		BotName:      botName,
		Pool:         pool,
		Claude:       claudeClient,
		Slack:        slackClient,
		UserStore:    userStore,
		Timezone:     tz,
		MCPClient:    toolDispatcher,
		BaseURL:      os.Getenv("DASHBOARD_URL"),
		Tools:        allTools,
	}
	river.AddWorker(workers, processWorker)
	river.AddWorker(workers, &jobs.RespondWorker{Pool: pool})
	river.AddWorker(workers, &jobs.SlackReplyWorker{Slack: slackClient})
	river.AddWorker(workers, &jobs.PlanWorker{
		Pool:       pool,
		Claude:     claudeClient,
		Slack:      slackClient,
		AppBaseURL: os.Getenv("DASHBOARD_URL"),
	})
	river.AddWorker(workers, &jobs.ExecuteWorker{
		Pool:         pool,
		Claude:       claudeClient,
		MCPClient:    toolDispatcher,
		Tools:        allTools,
		UserStore:    userStore,
		Timezone:     tz,
		SystemPrompt: os.Getenv("SYSTEM_PROMPT"),
		BotName:      botName,
	})
	river.AddWorker(workers, &jobs.SynthesizeWorker{Pool: pool, Claude: claudeClient})

	proactiveWorker := &jobs.ProactiveMessageWorker{
		SystemPrompt: os.Getenv("SYSTEM_PROMPT"),
		BotName:      botName,
		Pool:         pool,
		Claude:       claudeClient,
		Slack:        slackClient,
		Timezone:     tz,
		MCPClient:    toolDispatcher,
		Tools:        allTools,
	}
	river.AddWorker(workers, proactiveWorker)

	schedulerTickWorker := &jobs.SchedulerTickWorker{Pool: pool}
	river.AddWorker(workers, schedulerTickWorker)

	outboxDeliverWorker := &saga.OutboxDeliverWorker{Pool: pool, Slack: slackClient}
	river.AddWorker(workers, outboxDeliverWorker)

	periodicJobs := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(60*time.Second),
			func() (river.JobArgs, *river.InsertOpts) {
				return jobs.SchedulerTickArgs{}, nil
			},
			nil,
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(5*time.Second),
			func() (river.JobArgs, *river.InsertOpts) {
				return saga.OutboxDeliverArgs{}, nil
			},
			nil,
		),
	}

	client, err := queue.New(ctx, pool, workers, periodicJobs)
	if err != nil {
		slog.Error("failed to initialize river queue", "error", err)
		os.Exit(1)
	}

	if err := client.Start(context.Background()); err != nil {
		slog.Error("failed to start river client", "error", err)
		os.Exit(1)
	}
	slog.Info("river worker pool started")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slackSigningSecret := os.Getenv("SLACK_SIGNING_SECRET")
	if slackSigningSecret == "" {
		slog.Warn("SLACK_SIGNING_SECRET not set, Slack signature verification will reject all requests")
	}

	apiKey := os.Getenv("PONKO_API_KEY")
	if apiKey == "" {
		slog.Warn("PONKO_API_KEY not set, workflow endpoints are unauthenticated")
	}

	toolNames := make([]string, 0, len(allTools))
	for _, t := range allTools {
		toolNames = append(toolNames, t.Name)
	}

	cookieSigningKey := os.Getenv("COOKIE_SIGNING_KEY")
	if cookieSigningKey == "" {
		slog.Warn("COOKIE_SIGNING_KEY not set, dashboard auth will be disabled")
	}
	dashboardURL := os.Getenv("DASHBOARD_URL")
	authCfg := api.AuthConfig{
		ClientID:     os.Getenv("SLACK_CLIENT_ID"),
		ClientSecret: os.Getenv("SLACK_CLIENT_SECRET"),
		SigningKey:    []byte(cookieSigningKey),
		DashboardURL: dashboardURL,
		TeamID:       os.Getenv("SLACK_TEAM_ID"),
		Secure:       dashboardURL != "" && strings.HasPrefix(dashboardURL, "https"),
	}

	srv := api.NewServer(port, pool, client, slackSigningSecret, slackClient, apiKey, botName, toolNames, web.DistFS, authCfg)

	go func() {
		slog.Info("http server listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")

	gracefulShutdown(srv, client, telemetry, 8*time.Second)
}
