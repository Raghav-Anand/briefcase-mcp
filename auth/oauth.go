package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	oauthCodeTTL   = 5 * time.Minute
	oauthStateTTL  = 10 * time.Minute
)

// pendingState stores PKCE and redirect info for a pending authorization request.
type pendingState struct {
	codeChallenge string // base64url(SHA256(codeVerifier)), method=S256
	redirectURI   string // Claude client callback URL
	clientID      string
	clientState   string // MCP client's state param (passed through)
	createdAt     time.Time
}

// pendingCode stores the Google ID token awaiting exchange at /oauth/token.
type pendingCode struct {
	accessToken string // Google ID token — used directly by the middleware
	redirectURI string
	createdAt   time.Time
}

// OAuthHandler implements the MCP OAuth 2.0 authorization server endpoints.
type OAuthHandler struct {
	mu           sync.Mutex
	states       map[string]pendingState
	codes        map[string]pendingCode
	serverURL    string
	clientID     string
	clientSecret string
}

// NewOAuthHandler creates a handler with config from environment variables.
func NewOAuthHandler() *OAuthHandler {
	h := &OAuthHandler{
		states:       make(map[string]pendingState),
		codes:        make(map[string]pendingCode),
		serverURL:    os.Getenv("MCP_SERVER_URL"),
		clientID:     os.Getenv("OAUTH_CLIENT_ID"),
		clientSecret: os.Getenv("OAUTH_CLIENT_SECRET"),
	}
	go h.periodicCleanup()
	return h
}

// ServeWellKnown handles GET /.well-known/oauth-authorization-server (RFC 8414).
func (h *OAuthHandler) ServeWellKnown(w http.ResponseWriter, r *http.Request) {
	base := h.serverURL
	metadata := map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// ServeAuthorize handles GET /oauth/authorize.
// Stores PKCE state and redirects the user to Google OAuth.
func (h *OAuthHandler) ServeAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	redirectURI := q.Get("redirect_uri")
	clientID := q.Get("client_id")
	clientState := q.Get("state")

	if codeChallenge == "" || redirectURI == "" {
		http.Error(w, "missing required params: code_challenge, redirect_uri", http.StatusBadRequest)
		return
	}
	if codeChallengeMethod != "" && codeChallengeMethod != "S256" {
		http.Error(w, "only S256 code_challenge_method is supported", http.StatusBadRequest)
		return
	}

	ourState := uuid.New().String()
	h.mu.Lock()
	h.states[ourState] = pendingState{
		codeChallenge: codeChallenge,
		redirectURI:   redirectURI,
		clientID:      clientID,
		clientState:   clientState,
		createdAt:     time.Now(),
	}
	h.mu.Unlock()

	params := url.Values{
		"client_id":     {h.clientID},
		"redirect_uri":  {h.serverURL + "/oauth/callback"},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {ourState},
	}
	http.Redirect(w, r, googleAuthURL+"?"+params.Encode(), http.StatusFound)
}

// ServeCallback handles GET /oauth/callback (Google redirects here after auth).
// Exchanges the Google auth code for a Google ID token and stores it for the
// /oauth/token exchange. No Firebase involved.
func (h *OAuthHandler) ServeCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ourState := q.Get("state")
	googleCode := q.Get("code")
	errParam := q.Get("error")

	if errParam != "" {
		http.Error(w, "OAuth error: "+errParam, http.StatusBadRequest)
		return
	}
	if googleCode == "" || ourState == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	pending, ok := h.states[ourState]
	delete(h.states, ourState)
	h.mu.Unlock()

	if !ok {
		http.Error(w, "unknown or expired state", http.StatusBadRequest)
		return
	}

	// Exchange Google auth code for a Google ID token. Store it directly —
	// the middleware validates it using Google's public JWKS, no Firebase needed.
	googleIDToken, err := h.exchangeGoogleCode(googleCode)
	if err != nil {
		http.Error(w, "failed to exchange Google code: "+err.Error(), http.StatusInternalServerError)
		return
	}

	authCode := uuid.New().String()
	h.mu.Lock()
	h.codes[authCode] = pendingCode{
		accessToken: googleIDToken,
		redirectURI: pending.redirectURI,
		createdAt:   time.Now(),
	}
	h.mu.Unlock()

	params := url.Values{
		"code":  {authCode},
		"state": {pending.clientState},
	}
	http.Redirect(w, r, pending.redirectURI+"?"+params.Encode(), http.StatusFound)
}

// ServeToken handles POST /oauth/token.
// Exchanges the short-lived auth code for the Google ID token (as access_token).
func (h *OAuthHandler) ServeToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}

	if r.FormValue("grant_type") != "authorization_code" {
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	if code == "" {
		http.Error(w, `{"error":"invalid_request","error_description":"code required"}`, http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	pending, ok := h.codes[code]
	delete(h.codes, code)
	h.mu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	if time.Since(pending.createdAt) > oauthCodeTTL {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "code expired"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": pending.accessToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

// exchangeGoogleCode exchanges a Google auth code for tokens and returns the ID token.
func (h *OAuthHandler) exchangeGoogleCode(code string) (string, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {h.clientID},
		"client_secret": {h.clientSecret},
		"redirect_uri":  {h.serverURL + "/oauth/callback"},
		"grant_type":    {"authorization_code"},
	}

	resp, err := http.PostForm(googleTokenURL, form)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", googleTokenURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google token endpoint %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode Google token response: %w", err)
	}
	if result.IDToken == "" {
		return "", fmt.Errorf("no id_token in Google token response")
	}
	return result.IDToken, nil
}

// periodicCleanup removes expired states and codes.
func (h *OAuthHandler) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.mu.Lock()
		for k, v := range h.states {
			if now.Sub(v.createdAt) > oauthStateTTL {
				delete(h.states, k)
			}
		}
		for k, v := range h.codes {
			if now.Sub(v.createdAt) > oauthCodeTTL {
				delete(h.codes, k)
			}
		}
		h.mu.Unlock()
	}
}

// RegisterRoutes mounts OAuth endpoints on the given mux.
func (h *OAuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-authorization-server", h.ServeWellKnown)
	mux.HandleFunc("/oauth/authorize", h.ServeAuthorize)
	mux.HandleFunc("/oauth/callback", h.ServeCallback)
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.ServeToken(w, r)
	})
}
