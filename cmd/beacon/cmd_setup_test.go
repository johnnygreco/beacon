package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/spf13/cobra"
)

func TestRunSetupDashboardWritesPublicOwnerTokenConfig(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	withConfigFile(t, cfgPath)

	cmd, out := bufferedCommand()
	err := runSetupDashboard(cmd, setupDashboardOptions{
		CollectorURL: "https://beacon.example/",
		Name:         "Team Beacon",
	})
	if err != nil {
		t.Fatalf("runSetupDashboard returned error: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load guided config: %v", err)
	}
	if cfg.Server.PublicURL != "https://beacon.example" {
		t.Fatalf("server.public_url = %q", cfg.Server.PublicURL)
	}
	if cfg.Auth.Mode != config.AuthModeOwnerToken {
		t.Fatalf("auth.mode = %q, want %q", cfg.Auth.Mode, config.AuthModeOwnerToken)
	}
	if cfg.Fleet.Role != config.FleetRoleBoth {
		t.Fatalf("fleet.role = %q", cfg.Fleet.Role)
	}
	if cfg.Dashboard.Name != "Team Beacon" || cfg.Fleet.NodeName != "Team Beacon" {
		t.Fatalf("names = dashboard %q fleet %q", cfg.Dashboard.Name, cfg.Fleet.NodeName)
	}
	if !controlplane.Exists(cfg.Fleet.MetadataPath) {
		t.Fatalf("metadata path %s was not initialized", cfg.Fleet.MetadataPath)
	}
	if tokens := tokensFromOutput(out.String()); len(tokens) != 1 {
		t.Fatalf("tokens in output = %v, want one owner token", tokens)
	}
}

func TestRunSetupDashboardDryRunDoesNotWrite(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	withConfigFile(t, cfgPath)

	cmd, out := bufferedCommand()
	err := runSetupDashboard(cmd, setupDashboardOptions{
		CollectorURL: "https://beacon.example",
		Name:         "Dry Run",
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("runSetupDashboard returned error: %v", err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote config; stat error = %v", err)
	}
	if !strings.Contains(out.String(), "Planned changes:") || !strings.Contains(out.String(), "auth.mode") {
		t.Fatalf("dry-run output did not include guided changes: %q", out.String())
	}
}

func TestRunSetupDashboardRejectsLoopbackCollectorURLWithoutLocalTunnel(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	withConfigFile(t, cfgPath)

	cmd, _ := bufferedCommand()
	err := runSetupDashboard(cmd, setupDashboardOptions{CollectorURL: "http://127.0.0.1:4600"})
	if err == nil || !strings.Contains(err.Error(), "--local-tunnel") {
		t.Fatalf("runSetupDashboard error = %v, want local tunnel guidance", err)
	}
}

func TestRunSetupDashboardRequiresForceForExistingConflicts(t *testing.T) {
	configPath, _ := writeInitTestConfig(t)
	withConfigFile(t, configPath)

	cmd, _ := bufferedCommand()
	err := runSetupDashboard(cmd, setupDashboardOptions{Name: "New Name"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("runSetupDashboard error = %v, want force guidance", err)
	}

	cmd, _ = bufferedCommand()
	err = runSetupDashboard(cmd, setupDashboardOptions{Name: "New Name", Force: true})
	if err != nil {
		t.Fatalf("runSetupDashboard with force returned error: %v", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Dashboard.Name != "New Name" || cfg.Fleet.NodeName != "New Name" {
		t.Fatalf("names = dashboard %q fleet %q, want New Name", cfg.Dashboard.Name, cfg.Fleet.NodeName)
	}
}

func TestRunInviteMintsLoopbackLocalTunnelToken(t *testing.T) {
	configPath, _ := writeInitTestConfig(t)
	withConfigFile(t, configPath)

	cmd, out := bufferedCommand()
	err := runInvite(cmd, inviteOptions{
		CollectorURL: "http://127.0.0.1:4600",
		LocalTunnel:  true,
		TTL:          time.Minute,
		Format:       "text",
	})
	if err != nil {
		t.Fatalf("runInvite returned error: %v", err)
	}
	output := out.String()
	tokens := tokensFromOutput(output)
	if len(tokens) != 1 {
		t.Fatalf("tokens in output = %v, want one enrollment token", tokens)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "beacon enroll") && strings.Contains(line, tokens[0]) {
			t.Fatalf("remote command includes token inline: %q", line)
		}
	}
}

func TestRunInviteRejectsLoopbackURLWithoutLocalTunnel(t *testing.T) {
	configPath, _ := writeInitTestConfig(t)
	withConfigFile(t, configPath)

	cmd, _ := bufferedCommand()
	err := runInvite(cmd, inviteOptions{
		CollectorURL: "http://127.0.0.1:4600",
		TTL:          time.Minute,
		Format:       "text",
	})
	if err == nil || !strings.Contains(err.Error(), "--local-tunnel") {
		t.Fatalf("runInvite error = %v, want local tunnel guidance", err)
	}
}

func TestPublicURLChecksPassWhenRoutesAreProtected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"missing bearer token"}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		case "/", "/api/status", "/api/mcp":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()}); err != nil {
		t.Fatalf("runPublicURLChecks returned error: %v", err)
	}
}

func TestPublicURLChecksRejectExposedDashboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()})
	if err == nil || !strings.Contains(err.Error(), "protected route") {
		t.Fatalf("runPublicURLChecks error = %v, want protected route failure", err)
	}
}

func TestPublicURLChecksRejectStrippedAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"missing bearer token"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()})
	if err == nil || !strings.Contains(err.Error(), "Authorization header") {
		t.Fatalf("runPublicURLChecks error = %v, want Authorization header failure", err)
	}
}

func bufferedCommand() (*cobra.Command, *bytes.Buffer) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd, &out
}
