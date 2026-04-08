package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureHandler records whether the inner handler was called and stores the request context.
type captureHandler struct {
	called bool
	req    *http.Request
}

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	h.req = r
	w.WriteHeader(http.StatusOK)
}

// nilValidator returns a *Validator with a nil JWKS — safe to use in tests that
// only exercise the header-parsing logic (which returns before calling validate).
func nilValidator() *Validator { return &Validator{} }

func TestRequireAuth_MissingHeader(t *testing.T) {
	inner := &captureHandler{}
	handler := RequireAuth(nilValidator(), inner)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/mcp", nil)

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if inner.called {
		t.Error("inner handler should not be called when Authorization header is missing")
	}
}

func TestRequireAuth_WrongScheme(t *testing.T) {
	inner := &captureHandler{}
	handler := RequireAuth(nilValidator(), inner)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/mcp", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if inner.called {
		t.Error("inner handler should not be called for non-Bearer auth")
	}
}

func TestClaimsFromContext_ReturnsNilForNilContext(t *testing.T) {
	// nil context should not panic
	claims := ClaimsFromContext(nil)
	if claims != nil {
		t.Errorf("expected nil claims from nil context, got %+v", claims)
	}
}

func TestClaimsFromContext_ReturnsNilForEmptyContext(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	claims := ClaimsFromContext(r.Context())
	if claims != nil {
		t.Errorf("expected nil claims from context without claims, got %+v", claims)
	}
}
