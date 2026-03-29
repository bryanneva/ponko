package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/bryanneva/ponko/internal/jobs"
	appOtel "github.com/bryanneva/ponko/internal/otel"
	"github.com/bryanneva/ponko/internal/workflow"
)

type Recipe struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	WorkflowType string        `json:"-"`
	MessageField string        `json:"-"`
	Fields       []RecipeField `json:"fields"`
}

type RecipeField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required"`
}

var recipeRegistry = []Recipe{
	{
		ID:           "ask",
		Name:         "Ask a Question",
		Description:  "Send a message through the full workflow pipeline",
		WorkflowType: "echo",
		MessageField: "message",
		Fields: []RecipeField{
			{
				Name:        "message",
				Label:       "Message",
				Type:        "textarea",
				Required:    true,
				Placeholder: "What would you like to know?",
			},
		},
	},
}

func findRecipe(id string) *Recipe {
	for i := range recipeRegistry {
		if recipeRegistry[i].ID == id {
			return &recipeRegistry[i]
		}
	}
	return nil
}

func handleListRecipes() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string][]Recipe{"recipes": recipeRegistry})
	}
}

func handleRunRecipe(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID := r.PathValue("id")
		recipe := findRecipe(recipeID)
		if recipe == nil {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}

		var fields map[string]string
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		for _, f := range recipe.Fields {
			if f.Required {
				if val, ok := fields[f.Name]; !ok || val == "" {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("field %q is required", f.Name))
					return
				}
			}
		}

		workflowID, err := workflow.CreateWorkflow(r.Context(), pool, recipe.WorkflowType)
		if err != nil {
			slog.Error("failed to create workflow", "error", err, "recipe", recipeID)
			writeError(w, http.StatusInternalServerError, "failed to create workflow")
			return
		}

		_, err = riverClient.Insert(r.Context(), jobs.ReceiveArgs{
			WorkflowID:   workflowID,
			Message:      fields[recipe.MessageField],
			TraceContext: appOtel.InjectTraceContext(r.Context()),
		}, nil)
		if err != nil {
			slog.Error("failed to enqueue receive job", "error", err, "recipe", recipeID)
			writeError(w, http.StatusInternalServerError, "failed to start workflow")
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{"workflow_id": workflowID})
	}
}
