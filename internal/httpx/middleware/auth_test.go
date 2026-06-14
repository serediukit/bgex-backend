package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
)

type stubVerifier struct {
	returnID  uuid.UUID
	returnErr error
}

func (s stubVerifier) VerifyAccessToken(_ context.Context, _ string) (uuid.UUID, error) {
	return s.returnID, s.returnErr
}

func newEngine(v middleware.AccessTokenVerifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", middleware.RequireAuth(v), func(c *gin.Context) {
		id := middleware.UserIDFrom(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"user_id": id.String()})
	})
	return r
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	r := newEngine(stubVerifier{})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	r := newEngine(stubVerifier{returnErr: errors.New("bad")})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer something")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuth_Valid(t *testing.T) {
	uid := uuid.New()
	r := newEngine(stubVerifier{returnID: uid})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer valid")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), uid.String()) {
		t.Fatalf("expected body to contain user id %q, got %s", uid, rec.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
