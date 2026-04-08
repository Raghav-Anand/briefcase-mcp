package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	oauthCodeTTL   = 5 * time.Minute
	oauthStateTTL  = 10 * time.Minute

	oauthStatesCollection        = "oauth_states"
	oauthCodesCollection         = "oauth_codes"
	oauthRefreshTokensCollection = "oauth_refresh_tokens"
)

type pendingState struct {
	CodeChallenge string
	RedirectURI   string
	ClientID      string
	ClientState   string
	CreatedAt     time.Time
}

type pendingCode struct {
	AccessToken  string
	RefreshToken string
	RedirectURI  string
	CreatedAt    time.Time
}

type storedRefreshToken struct {
	GoogleRefreshToken string
	CreatedAt          time.Time
}

// oauthStore is the storage backend for short-lived OAuth states and codes,
// and long-lived refresh tokens. The production implementation uses Firestore
// so the flow works across multiple Cloud Run instances; tests use an in-memory
// implementation.
type oauthStore interface {
	saveState(ctx context.Context, key string, s pendingState) error
	getAndDeleteState(ctx context.Context, key string) (pendingState, bool, error)
	saveCode(ctx context.Context, key string, c pendingCode) error
	getAndDeleteCode(ctx context.Context, key string) (pendingCode, bool, error)
	saveRefreshToken(ctx context.Context, key string, googleRefreshToken string) error
	getRefreshToken(ctx context.Context, key string) (storedRefreshToken, bool, error)
}

// ── Firestore implementation ──────────────────────────────────────────────────

type firestoreStore struct {
	client *firestore.Client
}

type fsState struct {
	CodeChallenge string    `firestore:"code_challenge"`
	RedirectURI   string    `firestore:"redirect_uri"`
	ClientID      string    `firestore:"client_id"`
	ClientState   string    `firestore:"client_state"`
	CreatedAt     time.Time `firestore:"created_at"`
}

type fsCode struct {
	AccessToken  string    `firestore:"access_token"`
	RefreshToken string    `firestore:"refresh_token"`
	RedirectURI  string    `firestore:"redirect_uri"`
	CreatedAt    time.Time `firestore:"created_at"`
}

type fsRefreshToken struct {
	GoogleRefreshToken string    `firestore:"google_refresh_token"`
	CreatedAt          time.Time `firestore:"created_at"`
}

func (f *firestoreStore) saveState(ctx context.Context, key string, s pendingState) error {
	_, err := f.client.Collection(oauthStatesCollection).Doc(key).Set(ctx, fsState{
		CodeChallenge: s.CodeChallenge,
		RedirectURI:   s.RedirectURI,
		ClientID:      s.ClientID,
		ClientState:   s.ClientState,
		CreatedAt:     s.CreatedAt,
	})
	return err
}

func (f *firestoreStore) getAndDeleteState(ctx context.Context, key string) (pendingState, bool, error) {
	doc, err := f.client.Collection(oauthStatesCollection).Doc(key).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return pendingState{}, false, nil
		}
		return pendingState{}, false, err
	}
	var fs fsState
	if err := doc.DataTo(&fs); err != nil {
		return pendingState{}, false, err
	}
	f.client.Collection(oauthStatesCollection).Doc(key).Delete(ctx)
	return pendingState{
		CodeChallenge: fs.CodeChallenge,
		RedirectURI:   fs.RedirectURI,
		ClientID:      fs.ClientID,
		ClientState:   fs.ClientState,
		CreatedAt:     fs.CreatedAt,
	}, true, nil
}

func (f *firestoreStore) saveCode(ctx context.Context, key string, c pendingCode) error {
	_, err := f.client.Collection(oauthCodesCollection).Doc(key).Set(ctx, fsCode{
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		RedirectURI:  c.RedirectURI,
		CreatedAt:    c.CreatedAt,
	})
	return err
}

func (f *firestoreStore) getAndDeleteCode(ctx context.Context, key string) (pendingCode, bool, error) {
	doc, err := f.client.Collection(oauthCodesCollection).Doc(key).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return pendingCode{}, false, nil
		}
		return pendingCode{}, false, err
	}
	var fc fsCode
	if err := doc.DataTo(&fc); err != nil {
		return pendingCode{}, false, err
	}
	f.client.Collection(oauthCodesCollection).Doc(key).Delete(ctx)
	return pendingCode{
		AccessToken:  fc.AccessToken,
		RefreshToken: fc.RefreshToken,
		RedirectURI:  fc.RedirectURI,
		CreatedAt:    fc.CreatedAt,
	}, true, nil
}

func (f *firestoreStore) saveRefreshToken(ctx context.Context, key string, googleRefreshToken string) error {
	_, err := f.client.Collection(oauthRefreshTokensCollection).Doc(key).Set(ctx, fsRefreshToken{
		GoogleRefreshToken: googleRefreshToken,
		CreatedAt:          time.Now(),
	})
	return err
}

func (f *firestoreStore) getRefreshToken(ctx context.Context, key string) (storedRefreshToken, bool, error) {
	doc, err := f.client.Collection(oauthRefreshTokensCollection).Doc(key).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return storedRefreshToken{}, false, nil
		}
		return storedRefreshToken{}, false, err
	}
	var ft fsRefreshToken
	if err := doc.DataTo(&ft); err != nil {
		return storedRefreshToken{}, false, err
	}
	return storedRefreshToken{
		GoogleRefreshToken: ft.GoogleRefreshToken,
		CreatedAt:          ft.CreatedAt,
	}, true, nil
}

// ── Handler ───────────────────────────────────────────────────────────────────

// OAuthHandler implements the MCP OAuth 2.0 authorization server endpoints.
type OAuthHandler struct {
	serverURL    string
	clientID     string
	clientSecret string
	store        oauthStore
}

// NewOAuthHandler creates a handler backed by Firestore for cross-instance state sharing.
func NewOAuthHandler(ctx context.Context) *OAuthHandler {
	projectID := os.Getenv("GCP_PROJECT_ID")
	fs, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("auth.NewOAuthHandler: firestore.NewClient: %v", err)
	}
	return &OAuthHandler{
		serverURL:    os.Getenv("MCP_SERVER_URL"),
		clientID:     os.Getenv("OAUTH_CLIENT_ID"),
		clientSecret: os.Getenv("OAUTH_CLIENT_SECRET"),
		store:        &firestoreStore{client: fs},
	}
}

// ServeWellKnown handles GET /.well-known/oauth-authorization-server (RFC 8414).
func (h *OAuthHandler) ServeWellKnown(w http.ResponseWriter, r *http.Request) {
	base := h.serverURL
	metadata := map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// ServeRegister handles POST /oauth/register (RFC 7591 dynamic client registration).
func (h *OAuthHandler) ServeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.RedirectURIs) == 0 {
		http.Error(w, `{"error":"invalid_client_metadata","error_description":"redirect_uris required"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"client_id":                  uuid.New().String(),
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                req.GrantTypes,
		"response_types":             req.ResponseTypes,
		"token_endpoint_auth_method": "none",
	})
}

// ServeAuthorize handles GET /oauth/authorize.
// Stores PKCE state and redirects the user to Google OAuth.
// Requests offline access so Google returns a refresh token.
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
	if err := h.store.saveState(r.Context(), ourState, pendingState{
		CodeChallenge: codeChallenge,
		RedirectURI:   redirectURI,
		ClientID:      clientID,
		ClientState:   clientState,
		CreatedAt:     time.Now(),
	}); err != nil {
		log.Printf("ServeAuthorize: save state: %v", err)
		http.Error(w, "failed to store state", http.StatusInternalServerError)
		return
	}

	params := url.Values{
		"client_id":     {h.clientID},
		"redirect_uri":  {h.serverURL + "/oauth/callback"},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {ourState},
		"access_type":   {"offline"}, // request a refresh token
		"prompt":        {"consent"},  // force consent screen so refresh token is always issued
	}
	http.Redirect(w, r, googleAuthURL+"?"+params.Encode(), http.StatusFound)
}

// ServeCallback handles GET /oauth/callback (Google redirects here after auth).
// Exchanges the Google auth code for an ID token and refresh token.
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

	pending, ok, err := h.store.getAndDeleteState(r.Context(), ourState)
	if err != nil {
		log.Printf("ServeCallback: get state: %v", err)
		http.Error(w, "failed to retrieve state", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "unknown or expired state", http.StatusBadRequest)
		return
	}
	if time.Since(pending.CreatedAt) > oauthStateTTL {
		http.Error(w, "state expired", http.StatusBadRequest)
		return
	}

	idToken, googleRefreshToken, err := h.exchangeGoogleCode(googleCode)
	if err != nil {
		log.Printf("ServeCallback: exchange Google code: %v", err)
		http.Error(w, "failed to exchange Google code: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Store the Google refresh token under a new opaque key that we'll hand to the client.
	var ourRefreshToken string
	if googleRefreshToken != "" {
		ourRefreshToken = uuid.New().String()
		if err := h.store.saveRefreshToken(r.Context(), ourRefreshToken, googleRefreshToken); err != nil {
			log.Printf("ServeCallback: save refresh token: %v", err)
			// Non-fatal: auth still works, just won't support silent refresh.
		}
	}

	authCode := uuid.New().String()
	if err := h.store.saveCode(r.Context(), authCode, pendingCode{
		AccessToken:  idToken,
		RefreshToken: ourRefreshToken,
		RedirectURI:  pending.RedirectURI,
		CreatedAt:    time.Now(),
	}); err != nil {
		log.Printf("ServeCallback: save code: %v", err)
		http.Error(w, "failed to store auth code", http.StatusInternalServerError)
		return
	}

	params := url.Values{
		"code":  {authCode},
		"state": {pending.ClientState},
	}
	http.Redirect(w, r, pending.RedirectURI+"?"+params.Encode(), http.StatusFound)
}

// ServeToken handles POST /oauth/token.
// Supports grant_type=authorization_code (initial auth) and grant_type=refresh_token
// (silent token refresh without user interaction).
func (h *OAuthHandler) ServeToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}

	switch r.FormValue("grant_type") {
	case "authorization_code":
		h.serveAuthorizationCode(w, r)
	case "refresh_token":
		h.serveRefreshToken(w, r)
	default:
		http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
	}
}

func (h *OAuthHandler) serveAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	if code == "" {
		http.Error(w, `{"error":"invalid_request","error_description":"code required"}`, http.StatusBadRequest)
		return
	}

	pending, ok, err := h.store.getAndDeleteCode(r.Context(), code)
	if err != nil {
		log.Printf("serveAuthorizationCode: get code: %v", err)
		http.Error(w, "failed to retrieve code", http.StatusInternalServerError)
		return
	}
	if !ok {
		writeTokenError(w, "invalid_grant", "")
		return
	}
	if time.Since(pending.CreatedAt) > oauthCodeTTL {
		writeTokenError(w, "invalid_grant", "code expired")
		return
	}

	resp := map[string]any{
		"access_token": pending.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	if pending.RefreshToken != "" {
		resp["refresh_token"] = pending.RefreshToken
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *OAuthHandler) serveRefreshToken(w http.ResponseWriter, r *http.Request) {
	ourRefreshToken := r.FormValue("refresh_token")
	if ourRefreshToken == "" {
		http.Error(w, `{"error":"invalid_request","error_description":"refresh_token required"}`, http.StatusBadRequest)
		return
	}

	stored, ok, err := h.store.getRefreshToken(r.Context(), ourRefreshToken)
	if err != nil {
		log.Printf("serveRefreshToken: get refresh token: %v", err)
		http.Error(w, "failed to retrieve refresh token", http.StatusInternalServerError)
		return
	}
	if !ok {
		writeTokenError(w, "invalid_grant", "refresh token not found")
		return
	}

	newIDToken, err := h.refreshGoogleToken(stored.GoogleRefreshToken)
	if err != nil {
		log.Printf("serveRefreshToken: refresh Google token: %v", err)
		writeTokenError(w, "invalid_grant", "failed to refresh token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  newIDToken,
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": ourRefreshToken, // same refresh token, reusable
	})
}

func writeTokenError(w http.ResponseWriter, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	resp := map[string]string{"error": code}
	if description != "" {
		resp["error_description"] = description
	}
	json.NewEncoder(w).Encode(resp)
}

// exchangeGoogleCode exchanges a Google auth code for an ID token and refresh token.
func (h *OAuthHandler) exchangeGoogleCode(code string) (idToken, refreshToken string, err error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {h.clientID},
		"client_secret": {h.clientSecret},
		"redirect_uri":  {h.serverURL + "/oauth/callback"},
		"grant_type":    {"authorization_code"},
	}

	resp, err := http.PostForm(googleTokenURL, form)
	if err != nil {
		return "", "", fmt.Errorf("POST %s: %w", googleTokenURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Google token endpoint %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("decode Google token response: %w", err)
	}
	if result.IDToken == "" {
		return "", "", fmt.Errorf("no id_token in Google token response")
	}
	return result.IDToken, result.RefreshToken, nil
}

// refreshGoogleToken exchanges a Google refresh token for a new ID token.
func (h *OAuthHandler) refreshGoogleToken(googleRefreshToken string) (string, error) {
	form := url.Values{
		"client_id":     {h.clientID},
		"client_secret": {h.clientSecret},
		"refresh_token": {googleRefreshToken},
		"grant_type":    {"refresh_token"},
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
		return "", fmt.Errorf("decode Google refresh response: %w", err)
	}
	if result.IDToken == "" {
		return "", fmt.Errorf("no id_token in Google refresh response")
	}
	return result.IDToken, nil
}

// RegisterRoutes mounts OAuth endpoints on the given mux.
func (h *OAuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-authorization-server", h.ServeWellKnown)
	mux.HandleFunc("/oauth/register", h.ServeRegister)
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
