package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleListRecipes(t *testing.T) {
	handler := handleListRecipes()
	req := httptest.NewRequest(http.MethodGet, "/api/recipes", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", contentType)
	}

	var body struct {
		Recipes []Recipe `json:"recipes"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Recipes) == 0 {
		t.Fatal("expected at least one recipe")
	}

	found := false
	for _, r := range body.Recipes {
		if r.ID == "ask" {
			found = true
			if r.Name != "Ask a Question" {
				t.Fatalf("expected name 'Ask a Question', got %q", r.Name)
			}
			if len(r.Fields) != 1 {
				t.Fatalf("expected 1 field, got %d", len(r.Fields))
			}
			f := r.Fields[0]
			if f.Name != "message" {
				t.Fatalf("expected field name 'message', got %q", f.Name)
			}
			if !f.Required {
				t.Fatal("expected message field to be required")
			}
			if f.Type != "textarea" {
				t.Fatalf("expected field type 'textarea', got %q", f.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected to find 'ask' recipe")
	}
}

func TestHandleRunRecipe_NotFound(t *testing.T) {
	handler := handleRunRecipe(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/recipes/nonexistent/run", strings.NewReader(`{"message":"hello"}`))
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"] != "recipe not found" {
		t.Fatalf("expected error 'recipe not found', got %q", body["error"])
	}
}

func TestHandleRunRecipe_MissingFields(t *testing.T) {
	handler := handleRunRecipe(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/recipes/ask/run", strings.NewReader(`{}`))
	req.SetPathValue("id", "ask")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !strings.Contains(body["error"], "message") {
		t.Fatalf("expected error to mention 'message', got %q", body["error"])
	}
}

func TestHandleRunRecipe_InvalidBody(t *testing.T) {
	handler := handleRunRecipe(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/recipes/ask/run", strings.NewReader("not json"))
	req.SetPathValue("id", "ask")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"] != "invalid request body" {
		t.Fatalf("expected error 'invalid request body', got %q", body["error"])
	}
}
