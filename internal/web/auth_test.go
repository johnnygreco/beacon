package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRouterMCPRouteUsesConfiguredMCPHandler(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("ok")}},
		nil,
		nil,
		nil,
		WithMCPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)

	mcpReq := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"initialized"}`))
	mcpReq.Header.Set("Content-Type", "application/json")
	mcpRec := httptest.NewRecorder()
	router.ServeHTTP(mcpRec, mcpReq)
	if mcpRec.Code != http.StatusNoContent {
		t.Fatalf("MCP status = %d, want %d", mcpRec.Code, http.StatusNoContent)
	}
}

func TestMutationRequestGuardRejectsCrossSiteRequests(t *testing.T) {
	handler := MutationRequestGuardMiddleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name: "cross-site origin",
			headers: map[string]string{
				"Content-Type": "application/json",
				"Origin":       "http://evil.example",
			},
		},
		{
			name: "cross-site fetch metadata",
			headers: map[string]string{
				"Content-Type":   "application/json",
				"Sec-Fetch-Site": "cross-site",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/annotations", strings.NewReader(`{"note":"x"}`))
			req.Host = "127.0.0.1:4600"
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
			}
		})
	}
}

func TestMutationRequestGuardRequiresJSONWhenConfigured(t *testing.T) {
	handler := MutationRequestGuardMiddleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, contentType := range []string{"", "text/plain"} {
		t.Run(contentType, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/annotations", strings.NewReader(`{"note":"x"}`))
			if contentType != "" {
				req.Header.Set("Content-Type", contentType)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
			}
		})
	}
}

func TestMutationRequestGuardAllowsSameOriginJSONAndDeleteWithoutContentType(t *testing.T) {
	jsonHandler := MutationRequestGuardMiddleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	jsonReq := httptest.NewRequest(http.MethodPost, "/api/annotations", strings.NewReader(`{"note":"x"}`))
	jsonReq.Host = "127.0.0.1:4600"
	jsonReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	jsonReq.Header.Set("Origin", "http://127.0.0.1:4600")
	jsonReq.Header.Set("Sec-Fetch-Site", "same-origin")
	jsonRec := httptest.NewRecorder()

	jsonHandler.ServeHTTP(jsonRec, jsonReq)

	if jsonRec.Code != http.StatusNoContent {
		t.Fatalf("same-origin JSON status = %d, want %d", jsonRec.Code, http.StatusNoContent)
	}

	deleteHandler := MutationRequestGuardMiddleware(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/annotations/ann-1", nil)
	deleteReq.Header.Set("Sec-Fetch-Site", "none")
	deleteRec := httptest.NewRecorder()

	deleteHandler.ServeHTTP(deleteRec, deleteReq)

	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d", deleteRec.Code, http.StatusNoContent)
	}
}

func TestRouterGlobalMiddlewareAppliesToHealthAndStatic(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("ok")}},
		nil,
		nil,
		nil,
		WithGlobalMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Test-Global-Middleware", "applied")
				next.ServeHTTP(w, r)
			})
		}),
	)
	for _, path := range []string{"/health", "/api/health", "/static/app.js"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Header().Get("X-Test-Global-Middleware") != "applied" {
				t.Fatalf("global middleware header = %q, want applied", rec.Header().Get("X-Test-Global-Middleware"))
			}
		})
	}
}
