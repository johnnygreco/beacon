package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestOwnerTokenMiddlewareAuthenticatesBearerAndCookie(t *testing.T) {
	middleware := OwnerTokenMiddleware(func(_ context.Context, token string) bool {
		return token == "owner-secret"
	}, "owner_cookie")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name      string
		configure func(*http.Request)
		wantCode  int
	}{
		{
			name: "bearer",
			configure: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer owner-secret")
			},
			wantCode: http.StatusNoContent,
		},
		{
			name: "cookie",
			configure: func(req *http.Request) {
				req.AddCookie(&http.Cookie{Name: "owner_cookie", Value: "owner-secret"})
			},
			wantCode: http.StatusNoContent,
		},
		{
			name: "query token ignored",
			configure: func(req *http.Request) {
				req.URL.RawQuery = "token=owner-secret"
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "malformed bearer",
			configure: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearerowner-secret")
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name:      "missing",
			configure: func(req *http.Request) {},
			wantCode:  http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.configure(req)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestRouterAuthProtectsDashboardAPIAndSSEButNotHealthOrStatic(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("ok")}},
		nil,
		nil,
		nil,
		WithAuthMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})
		}),
	)

	tests := []struct {
		path string
		want int
	}{
		{path: "/", want: http.StatusTeapot},
		{path: "/api/status", want: http.StatusTeapot},
		{path: "/sse/dashboard", want: http.StatusTeapot},
		{path: "/health", want: http.StatusOK},
		{path: "/api/health", want: http.StatusOK},
		{path: "/static/app.js", want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestRouterMCPRouteUsesAPIAuthMiddleware(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("ok")}},
		nil,
		nil,
		nil,
		WithAuthMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})
		}),
		WithAPIAuthMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}),
		WithMCPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"initialized"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestLoopbackHostMiddlewareRejectsDNSRebindingHosts(t *testing.T) {
	middleware := LoopbackHostMiddleware("127.0.0.1")
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		host string
		want int
	}{
		{host: "127.0.0.1:4600", want: http.StatusNoContent},
		{host: "localhost:4600", want: http.StatusNoContent},
		{host: "[::1]:4600", want: http.StatusNoContent},
		{host: "evil.example:4600", want: http.StatusForbidden},
		{host: "", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestRouterGlobalMiddlewareProtectsHealthAndStatic(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("ok")}},
		nil,
		nil,
		nil,
		WithGlobalMiddleware(LoopbackHostMiddleware("127.0.0.1")),
	)
	for _, path := range []string{"/health", "/api/health", "/static/app.js"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "evil.example:4600"
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
			}
		})
	}
}
