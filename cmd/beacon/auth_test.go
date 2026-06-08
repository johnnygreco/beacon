package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
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
	if len(options) != 1 {
		t.Fatalf("owner-token options = %d, want middleware option", len(options))
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
