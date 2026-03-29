package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifyCookie(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-bytes!")
	payload := sessionPayload{
		UserID:      "U12345",
		DisplayName: "Test User",
		Exp:         time.Now().Add(time.Hour).Unix(),
	}

	signed, err := signCookie(payload, key)
	if err != nil {
		t.Fatalf("signCookie: %v", err)
	}

	got, err := verifyCookie(signed, key)
	if err != nil {
		t.Fatalf("verifyCookie: %v", err)
	}

	if got.UserID != payload.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, payload.UserID)
	}
	if got.DisplayName != payload.DisplayName {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, payload.DisplayName)
	}
	if got.Exp != payload.Exp {
		t.Errorf("Exp = %d, want %d", got.Exp, payload.Exp)
	}
}

func TestVerifyCookie_WrongKey(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-bytes!")
	payload := sessionPayload{
		UserID:      "U12345",
		DisplayName: "Test User",
		Exp:         time.Now().Add(time.Hour).Unix(),
	}

	signed, err := signCookie(payload, key)
	if err != nil {
		t.Fatalf("signCookie: %v", err)
	}

	_, err = verifyCookie(signed, []byte("wrong-key"))
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestVerifyCookie_TamperedPayload(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-bytes!")
	payload := sessionPayload{
		UserID:      "U12345",
		DisplayName: "Test User",
		Exp:         time.Now().Add(time.Hour).Unix(),
	}

	signed, err := signCookie(payload, key)
	if err != nil {
		t.Fatalf("signCookie: %v", err)
	}

	// Tamper with the payload portion
	tampered := "dGFtcGVyZWQ." + signed[len(signed)-44:]
	_, err = verifyCookie(tampered, key)
	if err == nil {
		t.Fatal("expected error for tampered cookie")
	}
}

func TestVerifyCookie_InvalidFormat(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-bytes!")

	_, err := verifyCookie("no-dot-separator", key)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestHandleAuthMe_NoCookie(t *testing.T) {
	handler := handleAuthMe([]byte("test-key"))
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleAuthMe_ValidSession(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-bytes!")
	payload := sessionPayload{
		UserID:      "U12345",
		DisplayName: "Bryan",
		Exp:         time.Now().Add(time.Hour).Unix(),
	}
	cookieVal, err := signCookie(payload, key)
	if err != nil {
		t.Fatalf("signCookie: %v", err)
	}

	handler := handleAuthMe(key)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieVal})
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["userId"] != "U12345" {
		t.Errorf("userId = %q, want U12345", body["userId"])
	}
	if body["displayName"] != "Bryan" {
		t.Errorf("displayName = %q, want Bryan", body["displayName"])
	}
}

func TestHandleAuthMe_ExpiredSession(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-bytes!")
	payload := sessionPayload{
		UserID:      "U12345",
		DisplayName: "Bryan",
		Exp:         time.Now().Add(-time.Hour).Unix(),
	}
	cookieVal, err := signCookie(payload, key)
	if err != nil {
		t.Fatalf("signCookie: %v", err)
	}

	handler := handleAuthMe(key)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieVal})
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleAuthLogout(t *testing.T) {
	handler := handleAuthLogout(false)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected session cookie to be cleared")
	}
}

func TestRequireSession_Middleware(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-bytes!")

	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		user := r.Context().Value(userContextKey).(SessionUser)
		writeJSON(w, http.StatusOK, map[string]string{"userId": user.UserID})
	})

	handler := requireSession(key, inner)

	t.Run("no cookie", func(t *testing.T) {
		innerCalled = false
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
		if innerCalled {
			t.Fatal("inner handler should not be called")
		}
	})

	t.Run("valid cookie", func(t *testing.T) {
		innerCalled = false
		payload := sessionPayload{
			UserID:      "U99999",
			DisplayName: "Test",
			Exp:         time.Now().Add(time.Hour).Unix(),
		}
		cookieVal, _ := signCookie(payload, key)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieVal})
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !innerCalled {
			t.Fatal("inner handler should be called")
		}
	})
}

func TestHandleAuthSlack_Redirect(t *testing.T) {
	cfg := AuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		SigningKey:    []byte("test-key"),
		DashboardURL: "https://example.com",
		Secure:       true,
	}

	handler := handleAuthSlack(cfg)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/slack", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}
	if !strings.Contains(location, "slack.com/oauth/v2/authorize") {
		t.Errorf("expected Slack OAuth URL, got %s", location)
	}
	if !strings.Contains(location, "client_id=test-client-id") {
		t.Errorf("expected client_id in URL, got %s", location)
	}
	if !strings.Contains(location, "scope=identity.basic") {
		t.Errorf("expected scope in URL, got %s", location)
	}

	// Should set state cookie
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "oauth_state" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected oauth_state cookie to be set")
	}
}

