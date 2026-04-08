package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestHandler creates an OAuthHandler with dummy config (no real OAuth calls).
func newTestHandler() *OAuthHandler {
	return &OAuthHandler{
		states:       make(map[string]pendingState),
		codes:        make(map[string]pendingCode),
		serverURL:    "https://example.com",
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}
}

func TestServeWellKnown(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	h.ServeWellKnown(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var meta map[string]any
	if err := json.NewDecoder(w.Body).Decode(&meta); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	required := []string{
		"issuer",
		"authorization_endpoint",
		"token_endpoint",
		"response_types_supported",
		"code_challenge_methods_supported",
	}
	for _, field := range required {
		if _, ok := meta[field]; !ok {
			t.Errorf("missing required field %q in OAuth metadata", field)
		}
	}
	if meta["authorization_endpoint"] != "https://example.com/oauth/authorize" {
		t.Errorf("unexpected authorization_endpoint: %v", meta["authorization_endpoint"])
	}
}

func TestServeAuthorize_MissingParams(t *testing.T) {
	h := newTestHandler()

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"missing code_challenge", "redirect_uri=https://claude.ai/callback"},
		{"missing redirect_uri", "code_challenge=abc123&code_challenge_method=S256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/oauth/authorize?"+tc.query, nil)
			h.ServeAuthorize(w, r)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}

func TestServeAuthorize_UnsupportedChallengeMethod(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/oauth/authorize?code_challenge=abc&redirect_uri=https://cb&code_challenge_method=plain", nil)
	h.ServeAuthorize(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for plain method, got %d", w.Code)
	}
}

func TestServeAuthorize_StoresStateAndRedirects(t *testing.T) {
	h := newTestHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/oauth/authorize?code_challenge=mychallenge&redirect_uri=https%3A%2F%2Fclaude.ai%2Fcb&state=client-state-42", nil)
	h.ServeAuthorize(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect (302), got %d", w.Code)
	}

	// Verify state was stored.
	h.mu.Lock()
	stateCount := len(h.states)
	h.mu.Unlock()
	if stateCount != 1 {
		t.Errorf("expected 1 pending state, got %d", stateCount)
	}

	// Redirect should go to Google OAuth.
	location := w.Header().Get("Location")
	if !strings.Contains(location, "accounts.google.com") {
		t.Errorf("expected redirect to Google OAuth, got: %s", location)
	}
}

func TestServeToken_InvalidCode(t *testing.T) {
	h := newTestHandler()

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"bogus-code"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeToken(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid code, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_grant" {
		t.Errorf("expected invalid_grant error, got: %v", resp)
	}
}

func TestServeToken_ExpiredCode(t *testing.T) {
	h := newTestHandler()

	// Plant a code that is already expired.
	h.mu.Lock()
	h.codes["old-code"] = pendingCode{
		accessToken: "firebase-token",
		redirectURI:   "https://claude.ai/cb",
		createdAt:     time.Now().Add(-10 * time.Minute), // well past TTL
	}
	h.mu.Unlock()

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"old-code"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeToken(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for expired code, got %d", w.Code)
	}
}

func TestServeToken_ValidCode(t *testing.T) {
	h := newTestHandler()

	// Plant a fresh valid code.
	h.mu.Lock()
	h.codes["good-code"] = pendingCode{
		accessToken: "firebase-id-token-xyz",
		redirectURI:   "https://claude.ai/cb",
		createdAt:     time.Now(),
	}
	h.mu.Unlock()

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"good-code"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeToken(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] != "firebase-id-token-xyz" {
		t.Errorf("expected Google ID token as access_token, got: %v", resp["access_token"])
	}
	if resp["token_type"] != "Bearer" {
		t.Errorf("expected token_type 'Bearer', got: %v", resp["token_type"])
	}

	// Code should be consumed (one-time use).
	h.mu.Lock()
	_, stillExists := h.codes["good-code"]
	h.mu.Unlock()
	if stillExists {
		t.Error("auth code should be deleted after successful exchange")
	}
}

func TestServeToken_UnsupportedGrantType(t *testing.T) {
	h := newTestHandler()
	form := url.Values{"grant_type": {"client_credentials"}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeToken(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported grant type, got %d", w.Code)
	}
}

func TestRegisterRoutes(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Verify well-known route is reachable.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 from well-known route, got %d", w.Code)
	}
}
