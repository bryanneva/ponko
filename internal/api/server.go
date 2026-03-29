package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/bryanneva/ponko/internal/jobs"
	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/slack"
	"github.com/bryanneva/ponko/internal/workflow"
)

func NewServer(port string, pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx], slackSigningSecret string, slackClient *slack.Client, apiKey, botName string, toolNames []string, embeddedDist fs.FS, authCfg AuthConfig) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /api/workflows/start", apiKeyAuth(apiKey, handleStartWorkflow(pool, riverClient)))
	mux.HandleFunc("GET /api/workflows/{id}", apiKeyOrSession(apiKey, authCfg.SigningKey, handleGetWorkflow(pool)))
	mux.HandleFunc("GET /api/workflows/recent", requireSession(authCfg.SigningKey, handleRecentWorkflows(pool)))
	mux.HandleFunc("POST /slack/events", handleSlackEvents(slackSigningSecret, pool, riverClient, slackClient, apiKey, botName))
	mux.HandleFunc("POST /slack/interactions", handleSlackInteractions(slackSigningSecret, pool, riverClient, slackClient))
	mux.HandleFunc("POST /slack/commands", handleSlackCommand(pool, apiKey, botName))

	mux.HandleFunc("GET /api/conversations/recent", requireSession(authCfg.SigningKey, handleRecentConversations(pool)))
	mux.HandleFunc("GET /api/conversations/{id}", apiKeyOrSession(apiKey, authCfg.SigningKey, handleGetConversation(pool)))

	mux.HandleFunc("GET /api/channels", requireSession(authCfg.SigningKey, handleListChannels(pool, slackClient)))
	mux.HandleFunc("GET /api/channels/{id}/config", requireSession(authCfg.SigningKey, handleGetChannelConfig(pool, slackClient)))
	mux.HandleFunc("PUT /api/channels/{id}/config", requireSession(authCfg.SigningKey, handlePutChannelConfig(pool)))
	mux.HandleFunc("GET /api/tools", requireSession(authCfg.SigningKey, handleListTools(toolNames)))
	mux.HandleFunc("GET /api/jobs/summary", requireSession(authCfg.SigningKey, handleJobsSummary(pool)))
	mux.HandleFunc("GET /api/jobs/recent", requireSession(authCfg.SigningKey, handleRecentJobs(pool)))
	mux.HandleFunc("POST /api/jobs/{id}/discard", apiKeyAuth(apiKey, handleDiscardJob(pool)))

	mux.HandleFunc("GET /api/recipes", requireSession(authCfg.SigningKey, handleListRecipes()))
	mux.HandleFunc("POST /api/recipes/{id}/run", requireSession(authCfg.SigningKey, handleRunRecipe(pool, riverClient)))

	// Auth routes
	mux.HandleFunc("GET /api/auth/slack", handleAuthSlack(authCfg))
	mux.HandleFunc("GET /api/auth/slack/callback", handleAuthSlackCallback(authCfg))
	mux.HandleFunc("GET /api/auth/me", handleAuthMe(authCfg.SigningKey))
	mux.HandleFunc("POST /api/auth/logout", handleAuthLogout(authCfg.Secure))

	if embeddedDist != nil {
		mux.HandleFunc("GET /", spaHandler(embeddedDist))
	}

	return &http.Server{
		Addr:    ":" + port,
		Handler: otelhttp.NewMiddleware("ponko")(loggingMiddleware(mux)),
	}
}

type startWorkflowRequest struct {
	WorkflowType string          `json:"workflow_type"`
	Payload      json.RawMessage `json:"payload"`
}

func handleStartWorkflow(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req startWorkflowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.WorkflowType == "" {
			writeError(w, http.StatusBadRequest, "workflow_type is required")
			return
		}

		workflowID, err := workflow.CreateWorkflow(r.Context(), pool, req.WorkflowType)
		if err != nil {
			slog.Error("failed to create workflow", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create workflow")
			return
		}

		if req.WorkflowType == "echo" {
			var payload struct {
				Message string `json:"message"`
			}
			if unmarshalErr := json.Unmarshal(req.Payload, &payload); unmarshalErr != nil {
				slog.Error("failed to parse echo payload", "error", unmarshalErr)
				writeError(w, http.StatusBadRequest, "invalid echo payload: must contain 'message' field")
				return
			}
			_, err = riverClient.Insert(r.Context(), jobs.ReceiveArgs{
				WorkflowID:   workflowID,
				Message:      payload.Message,
				TraceContext: appOtel.InjectTraceContext(r.Context()),
			}, nil)
			if err != nil {
				slog.Error("failed to enqueue receive job", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to start workflow")
				return
			}
		}

		writeJSON(w, http.StatusCreated, map[string]string{"workflow_id": workflowID})
	}
}

func handleGetWorkflow(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !isValidUUID(id) {
			writeError(w, http.StatusBadRequest, "invalid workflow_id: must be a valid UUID")
			return
		}

		wf, err := workflow.GetWorkflow(r.Context(), pool, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "workflow not found")
				return
			}
			slog.Error("failed to get workflow", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get workflow")
			return
		}

		writeJSON(w, http.StatusOK, wf)
	}
}

func handleRecentWorkflows(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
				limit = parsed
			}
		}
		workflows, err := workflow.ListRecent(r.Context(), pool, limit)
		if err != nil {
			slog.Error("failed to list workflows", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list workflows")
			return
		}
		writeJSON(w, http.StatusOK, workflows)
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isValidUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

func apiKeyOrSession(apiKey string, signingKey []byte, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" && len(signingKey) == 0 {
			next(w, r)
			return
		}
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			if apiKey != "" && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(apiKey)) == 1 {
				next(w, r)
				return
			}
		}
		if authedReq, ok := validateSession(r, signingKey); ok {
			next(w, authedReq)
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized")
	}
}

func apiKeyAuth(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(apiKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start).String(),
		)
	})
}
