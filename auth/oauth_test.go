package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── In-memory store for tests ─────────────────────────────────────────────────

type memStore struct {
	mu     sync.Mutex
	states map[string]pendingState
	codes  map[string]pendingCode
}

func newMemStore() *memStore {
	return &memStore{
		states: make(map[string]pendingState),
		codes:  make(map[string]pendingCode),
	}
}

func (m *memStore) saveState(_ context.Context, key string, s pendingState) error {
	m.mu.Lock()
	m.states[key] = s
	m.mu.Unlock()
	return nil
}

func (m *memStore) getAndDeleteState(_ context.Context, key string) (pendingState, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.states[key]
	if ok {
		delete(m.states, key)
	}
	return s, ok, nil
}

func (m *memStore) saveCode(_ context.Context, key string, c pendingCode) error {
	m.mu.Lock()
	m.codes[key] = c
	m.mu.Unlock()
	return nil
}

func (m *memStore) getAndDeleteCode(_ context.Context, key string) (pendingCode, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.codes[key]
	if ok {
		delete(m.codes, key)
	}
	return c, ok, nil
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func newTestHandler() (*OAuthHandler, *memStore) {
	store := newMemStore()
	return &OAuthHandler{
		serverURL:    "https://example.com",
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		store:        store,
	}, store
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestServeWellKnown(t *testing.T) {
	h, _ := newTestHandler()
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
	h, _ := newTestHandler()

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
	h, _ := newTestHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/oauth/authorize?code_challenge=abc&redirect_uri=https://cb&code_challenge_method=plain", nil)
	h.ServeAuthorize(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for plain method, got %d", w.Code)
	}
}

func TestServeAuthorize_StoresStateAndRedirects(t *testing.T) {
	h, store := newTestHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/oauth/authorize?code_challenge=mychallenge&redirect_uri=https%3A%2F%2Fclaude.ai%2Fcb&state=client-state-42", nil)
	h.ServeAuthorize(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect (302), got %d", w.Code)
	}

	store.mu.Lock()
	stateCount := len(store.states)
	store.mu.Unlock()
	if stateCount != 1 {
		t.Errorf("expected 1 pending state, got %d", stateCount)
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "accounts.google.com") {
		t.Errorf("expected redirect to Google OAuth, got: %s", location)
	}
}

func TestServeToken_InvalidCode(t *testing.T) {
	h, _ := newTestHandler()

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
	h, store := newTestHandler()

	store.saveCode(context.Background(), "old-code", pendingCode{
		AccessToken: "google-id-token",
		RedirectURI: "https://claude.ai/cb",
		CreatedAt:   time.Now().Add(-10 * time.Minute), // well past TTL
	})

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
	h, store := newTestHandler()

	store.saveCode(context.Background(), "good-code", pendingCode{
		AccessToken: "google-id-token-xyz",
		RedirectURI: "https://claude.ai/cb",
		CreatedAt:   time.Now(),
	})

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
	if resp["access_token"] != "google-id-token-xyz" {
		t.Errorf("expected Google ID token as access_token, got: %v", resp["access_token"])
	}
	if resp["token_type"] != "Bearer" {
		t.Errorf("expected token_type 'Bearer', got: %v", resp["token_type"])
	}

	// Code should be consumed (one-time use).
	store.mu.Lock()
	_, stillExists := store.codes["good-code"]
	store.mu.Unlock()
	if stillExists {
		t.Error("auth code should be deleted after successful exchange")
	}
}

func TestServeToken_UnsupportedGrantType(t *testing.T) {
	h, _ := newTestHandler()
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
	h, _ := newTestHandler()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 from well-known route, got %d", w.Code)
	}
}
