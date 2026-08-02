package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
)

type stubRoleLookup struct {
	role string
	err  error
}

func (s stubRoleLookup) GetRole(_ context.Context, _ uuid.UUID) (string, error) {
	return s.role, s.err
}

func newAdminEngine(authed bool, uid uuid.UUID, l middleware.RoleLookup) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	handlers := make([]gin.HandlerFunc, 0, 3)
	if authed {
		handlers = append(handlers, middleware.RequireAuth(stubVerifier{returnID: uid}))
	}
	handlers = append(handlers, middleware.RequireAdmin(l))
	handlers = append(handlers, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	r.GET("/admin", handlers...)
	return r
}

func doAdminRequest(r *gin.Engine, authed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin", nil)
	if authed {
		req.Header.Set("Authorization", "Bearer valid")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRequireAdmin(t *testing.T) {
	uid := uuid.New()

	tests := []struct {
		name        string
		authed      bool
		lookup      stubRoleLookup
		wantStatus  int
		wantCode    string
		wantHandled bool
	}{
		{
			name:       "no authenticated user",
			authed:     false,
			lookup:     stubRoleLookup{},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "unauthorized",
		},
		{
			name:       "role user is forbidden",
			authed:     true,
			lookup:     stubRoleLookup{role: "user"},
			wantStatus: http.StatusForbidden,
			wantCode:   "forbidden",
		},
		{
			name:        "role admin is allowed",
			authed:      true,
			lookup:      stubRoleLookup{role: "admin"},
			wantStatus:  http.StatusOK,
			wantHandled: true,
		},
		{
			name:       "lookup error is internal error",
			authed:     true,
			lookup:     stubRoleLookup{err: errors.New("db down")},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newAdminEngine(tt.authed, uid, tt.lookup)
			rec := doAdminRequest(r, tt.authed)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d; body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.wantHandled {
				if !strings.Contains(rec.Body.String(), `"ok":true`) {
					t.Fatalf("expected handler to be invoked, got body: %s", rec.Body.String())
				}
				return
			}

			wantBody := `{"error":{"code":"` + tt.wantCode + `"`
			if !strings.Contains(rec.Body.String(), wantBody) {
				t.Fatalf("expected body to contain %q, got %s", wantBody, rec.Body.String())
			}
		})
	}
}
