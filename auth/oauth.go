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

	oauthStatesCollection = "oauth_states"
	oauthCodesCollection  = "oauth_codes"
)

// firestoreState is the Firestore representation of a pending OAuth state.
type firestoreState struct {
	CodeChallenge string    `firestore:"code_challenge"`
	RedirectURI   string    `firestore:"redirect_uri"`
	ClientID      string    `firestore:"client_id"`
	ClientState   string    `firestore:"client_state"`
	CreatedAt     time.Time `firestore:"created_at"`
}

// firestoreCode is the Firestore representation of a pending auth code.
type firestoreCode struct {
	AccessToken string    `firestore:"access_token"`
	RedirectURI string    `firestore:"redirect_uri"`
	CreatedAt   time.Time `firestore:"created_at"`
}

// OAuthHandler implements the MCP OAuth 2.0 authorization server endpoints.
// States and codes are persisted in Firestore so the flow works correctly
// across multiple Cloud Run instances.
type OAuthHandler struct {
	serverURL    string
	clientID     string
	clientSecret string
	fs           *firestore.Client
}

// NewOAuthHandler creates a handler with config from environment variables.
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
		fs:           fs,
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
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metadata)
}

// ServeRegister handles POST /oauth/register (RFC 7591 dynamic client registration).
// Claude Code and other MCP clients require this to self-register before the OAuth flow.
// Since we proxy all auth through Google OAuth using our own server credentials, we
// accept any registration and return a stable client_id.
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

	clientID := uuid.New().String()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                req.GrantTypes,
		"response_types":             req.ResponseTypes,
		"token_endpoint_auth_method": "none",
	})
}

// ServeAuthorize handles GET /oauth/authorize.
// Stores PKCE state in Firestore and redirects the user to Google OAuth.
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
	_, err := h.fs.Collection(oauthStatesCollection).Doc(ourState).Set(r.Context(), firestoreState{
		CodeChallenge: codeChallenge,
		RedirectURI:   redirectURI,
		ClientID:      clientID,
		ClientState:   clientState,
		CreatedAt:     time.Now(),
	})
	if err != nil {
		log.Printf("ServeAuthorize: store state: %v", err)
		http.Error(w, "failed to store state", http.StatusInternalServerError)
		return
	}

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
// /oauth/token exchange.
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

	// Fetch and delete the pending state from Firestore.
	stateRef := h.fs.Collection(oauthStatesCollection).Doc(ourState)
	doc, err := stateRef.Get(r.Context())
	if err != nil {
		if status.Code(err) == codes.NotFound {
			http.Error(w, "unknown or expired state", http.StatusBadRequest)
		} else {
			log.Printf("ServeCallback: get state: %v", err)
			http.Error(w, "failed to retrieve state", http.StatusInternalServerError)
		}
		return
	}
	var pending firestoreState
	if err := doc.DataTo(&pending); err != nil {
		log.Printf("ServeCallback: decode state: %v", err)
		http.Error(w, "failed to decode state", http.StatusInternalServerError)
		return
	}
	stateRef.Delete(r.Context())

	if time.Since(pending.CreatedAt) > oauthStateTTL {
		http.Error(w, "state expired", http.StatusBadRequest)
		return
	}

	googleIDToken, err := h.exchangeGoogleCode(googleCode)
	if err != nil {
		log.Printf("ServeCallback: exchange Google code: %v", err)
		http.Error(w, "failed to exchange Google code: "+err.Error(), http.StatusInternalServerError)
		return
	}

	authCode := uuid.New().String()
	_, err = h.fs.Collection(oauthCodesCollection).Doc(authCode).Set(r.Context(), firestoreCode{
		AccessToken: googleIDToken,
		RedirectURI: pending.RedirectURI,
		CreatedAt:   time.Now(),
	})
	if err != nil {
		log.Printf("ServeCallback: store code: %v", err)
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

	// Fetch and delete the pending code from Firestore.
	codeRef := h.fs.Collection(oauthCodesCollection).Doc(code)
	doc, err := codeRef.Get(r.Context())
	if err != nil {
		if status.Code(err) == codes.NotFound {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		} else {
			log.Printf("ServeToken: get code: %v", err)
			http.Error(w, "failed to retrieve code", http.StatusInternalServerError)
		}
		return
	}
	var pending firestoreCode
	if err := doc.DataTo(&pending); err != nil {
		log.Printf("ServeToken: decode code: %v", err)
		http.Error(w, "failed to decode code", http.StatusInternalServerError)
		return
	}
	codeRef.Delete(r.Context())

	if time.Since(pending.CreatedAt) > oauthCodeTTL {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "code expired"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": pending.AccessToken,
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
