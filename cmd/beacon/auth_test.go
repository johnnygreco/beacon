package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/mcp"
	"github.com/johnnygreco/beacon/internal/web"
)

func TestDashboardAuthOptionsNonLoopbackRequiresProtection(t *testing.T) {
	store, err := controlplane.Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	cfg := testAuthConfig()
	cfg.Server.Host = "0.0.0.0"
	cfg.Auth.Mode = config.AuthModeLoopback
	_, err = dashboardAuthOptions(context.Background(), cfg, store)
	if err == nil || !strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("dashboardAuthOptions error = %v, want non-loopback rejection", err)
	}

	cfg.Auth.Mode = config.AuthModeReverseProxy
	options, err := dashboardAuthOptions(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("dashboardAuthOptions reverse-proxy: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("reverse-proxy options = %d, want none", len(options))
	}

	cfg.Auth.Mode = config.AuthModeOwnerToken
	_, err = dashboardAuthOptions(context.Background(), cfg, store)
	if err == nil || !strings.Contains(err.Error(), "allow_insecure_owner_http") {
		t.Fatalf("dashboardAuthOptions owner-token without insecure opt-in error = %v, want insecure HTTP rejection", err)
	}
	cfg.Auth.AllowInsecureOwnerHTTP = true
	_, err = dashboardAuthOptions(context.Background(), cfg, store)
	if err == nil || !strings.Contains(err.Error(), "requires an active owner/admin token") {
		t.Fatalf("dashboardAuthOptions owner-token without token error = %v, want owner-token rejection", err)
	}
	if _, err := store.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeOwner}); err != nil {
		t.Fatalf("CreateToken owner: %v", err)
	}
	options, err = dashboardAuthOptions(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("dashboardAuthOptions owner-token with token: %v", err)
	}
	if len(options) != 2 {
		t.Fatalf("owner-token options = %d, want page and API middleware options", len(options))
	}
}

func TestDashboardAuthOptionsLoopbackAllowsLocalMode(t *testing.T) {
	store, err := controlplane.Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	cfg := testAuthConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Auth.Mode = config.AuthModeLoopback
	options, err := dashboardAuthOptions(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("dashboardAuthOptions loopback: %v", err)
	}
	if len(options) != 1 {
		t.Fatalf("loopback options = %d, want host guard middleware option", len(options))
	}
}

func TestReadTokenAPIMiddlewareAttachesScopedContexts(t *testing.T) {
	store, err := controlplane.Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	readToken, err := store.CreateToken(context.Background(), controlplane.CreateTokenRequest{
		Type:        controlplane.TokenTypeRead,
		NodeID:      "node-a",
		CollectorID: "collector-a",
		SourceIDs:   []string{"source-a", "source-b"},
	})
	if err != nil {
		t.Fatalf("CreateToken read: %v", err)
	}

	var sawHandler bool
	handler := readTokenAPIMiddleware(store, "owner_cookie")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHandler = true
		apiScope, ok := web.APIScopeFromContext(r.Context())
		if !ok {
			t.Fatal("missing web API scope")
		}
		if strings.Join(apiScope.NodeIDs, ",") != "node-a" || strings.Join(apiScope.CollectorIDs, ",") != "collector-a" || strings.Join(apiScope.SourceIDs, ",") != "source-a,source-b" {
			t.Fatalf("web API scope = %#v", apiScope)
		}
		mcpScope, ok := mcp.AuthScopeFromContext(r.Context())
		if !ok {
			t.Fatal("missing MCP scope")
		}
		if strings.Join(mcpScope.NodeIDs, ",") != "node-a" || strings.Join(mcpScope.CollectorIDs, ",") != "collector-a" || strings.Join(mcpScope.SourceIDs, ",") != "source-a,source-b" {
			t.Fatalf("MCP scope = %#v", mcpScope)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+readToken.Plaintext)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || !sawHandler {
		t.Fatalf("read token status/handler = %d/%v", rec.Code, sawHandler)
	}
}

func TestReadTokenAPIMiddlewareLeavesOwnerTokenUnscoped(t *testing.T) {
	store, err := controlplane.Open(filepath.Join(t.TempDir(), "control-plane.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ownerToken, err := store.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeOwner})
	if err != nil {
		t.Fatalf("CreateToken owner: %v", err)
	}

	handler := readTokenAPIMiddleware(store, "owner_cookie")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := mcp.AuthScopeFromContext(r.Context()); ok {
			t.Fatal("owner token unexpectedly set MCP scope")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken.Plaintext)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("owner token status = %d", rec.Code)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1": true,
		"localhost": true,
		"[::1]":     true,
		"0.0.0.0":   false,
		"::":        false,
		"10.0.0.2":  false,
	}
	for host, want := range tests {
		if got := isLoopbackHost(host); got != want {
			t.Fatalf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func testAuthConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 4600},
		Auth:   config.AuthConfig{Mode: config.AuthModeLoopback, CookieName: "beacon_owner_token"},
	}
}
