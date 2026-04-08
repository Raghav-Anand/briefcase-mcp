package middleware

import (
	"context"
	"net/http"
	"strings"

	internalauth "github.com/raghav-anand/briefcase-internal/auth"
)

type contextKey string

const ClaimsKey contextKey = "claims"

// Validator wraps briefcase-internal's Verifier to validate Google ID tokens.
type Validator struct {
	verifier *internalauth.Verifier
}

// NewValidator creates a Validator for the given OAuth client ID.
// JWKS keys are fetched from Google on first use and cached with automatic rotation.
func NewValidator(clientID string) *Validator {
	return &Validator{verifier: internalauth.NewVerifier(clientID)}
}

// RequireAuth validates a Google ID token from the Authorization header.
// Returns 401 if the token is missing or invalid.
func RequireAuth(v *Validator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, `{"error":"missing_token","message":"Authorization: Bearer <token> required"}`, http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")

		claims, err := v.verifier.VerifyToken(r.Context(), tokenStr)
		if err != nil {
			http.Error(w, `{"error":"invalid_token","message":"Invalid or expired Google ID token"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ClaimsFromContext extracts user claims injected by RequireAuth. Returns nil if not present.
func ClaimsFromContext(ctx context.Context) *internalauth.Claims {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(ClaimsKey).(*internalauth.Claims)
	return c
}
