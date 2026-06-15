package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/web"
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

func TestRunSetupDashboardReportsPendingPublicChecks(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	withConfigFile(t, cfgPath)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	withSetupPublicURLCheckClient(t, server)

	cmd, out := bufferedCommand()
	err := runSetupDashboard(cmd, setupDashboardOptions{
		CollectorURL: "https://beacon.example",
		Name:         "Public Dashboard",
	})
	if err != nil {
		t.Fatalf("runSetupDashboard returned error: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Public URL checks: pending/failed") {
		t.Fatalf("setup output = %q, want pending/failed public URL check status", output)
	}
	if !strings.Contains(output, "beacon doctor setup") {
		t.Fatalf("setup output = %q, want doctor guidance", output)
	}
}

func TestRunSetupDashboardFailsLivePublicCheckFailures(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	withConfigFile(t, cfgPath)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	withSetupPublicURLCheckClient(t, server)

	cmd, out := bufferedCommand()
	err := runSetupDashboard(cmd, setupDashboardOptions{
		CollectorURL: "https://beacon.example",
		Name:         "Public Dashboard",
	})
	if err == nil || !strings.Contains(err.Error(), "public URL setup checks failed") {
		t.Fatalf("runSetupDashboard error = %v, want hard public URL check failure", err)
	}
	output := out.String()
	if !strings.Contains(output, "Public URL checks: failed") {
		t.Fatalf("setup output = %q, want failed public URL check status", output)
	}
	if !strings.Contains(output, "beacon doctor setup") {
		t.Fatalf("setup output = %q, want doctor guidance", output)
	}
}

func TestRunSetupDashboardFailsHostGuardHealthCheck(t *testing.T) {
	resetConfigState(t)
	cfgPath := filepath.Join(t.TempDir(), "beacon.toml")
	withConfigFile(t, cfgPath)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path %s; host guard health failure should stop setup checks", r.URL.Path)
		}
		w.Header().Set(web.HostGuardRejectedHeader, "rejected")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Beacon host guard rejected the request"))
	}))
	defer server.Close()
	withSetupPublicURLCheckClient(t, server)

	cmd, out := bufferedCommand()
	err := runSetupDashboard(cmd, setupDashboardOptions{
		CollectorURL: "https://beacon.example",
		Name:         "Host Guard Dashboard",
	})
	if err == nil || !strings.Contains(err.Error(), "public URL setup checks failed") {
		t.Fatalf("runSetupDashboard error = %v, want hard public URL check failure", err)
	}
	output := out.String()
	if !strings.Contains(output, "Public URL checks: failed") || !strings.Contains(output, "host guard") {
		t.Fatalf("setup output = %q, want failed host guard public URL check status", output)
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
	if !strings.Contains(output, "Checks: local-only") {
		t.Fatalf("invite output = %q, want local-only check label", output)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "beacon join") && strings.Contains(line, tokens[0]) {
			t.Fatalf("remote command includes token inline: %q", line)
		}
	}
	if !strings.Contains(output, "beacon join http://127.0.0.1:4600 --token-stdin") {
		t.Fatalf("invite output = %q, want beacon join command", output)
	}
}

func TestInviteRecommendedCommandShellQuotesURL(t *testing.T) {
	collectorURL := "https://beacon.example;touch"
	wantJoin := "beacon join 'https://beacon.example;touch' --token-stdin"

	var textOut bytes.Buffer
	writeInviteText(&textOut, collectorURL, "bcn_enroll_secret", time.Unix(0, 0).UTC(), false)
	if !strings.Contains(textOut.String(), wantJoin) {
		t.Fatalf("invite text = %q, want quoted join command %q", textOut.String(), wantJoin)
	}

	var jsonOut bytes.Buffer
	if err := writeInviteJSON(&jsonOut, collectorURL, "bcn_enroll_secret", time.Unix(0, 0).UTC(), false); err != nil {
		t.Fatalf("writeInviteJSON: %v", err)
	}
	var payload struct {
		RecommendedCommand string `json:"recommended_command"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &payload); err != nil {
		t.Fatalf("decode invite JSON: %v", err)
	}
	if !strings.Contains(payload.RecommendedCommand, wantJoin) {
		t.Fatalf("recommended command = %q, want quoted join command %q", payload.RecommendedCommand, wantJoin)
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

func TestRunInviteUnsafePublicURLWarns(t *testing.T) {
	configPath, _ := writeInitTestConfig(t)
	withConfigFile(t, configPath)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	withPublicURLCheckClient(t, server)

	cmd, out := bufferedCommand()
	err := runInvite(cmd, inviteOptions{
		CollectorURL:    "https://beacon.example",
		TTL:             time.Minute,
		Format:          "text",
		UnsafePublicURL: true,
	})
	if err != nil {
		t.Fatalf("runInvite returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Warning: public URL connectivity checks passed") {
		t.Fatalf("invite output = %q, want unsafe public URL warning", out.String())
	}
}

func TestRunInviteSaveURLDoesNotWriteWhenPublicChecksFail(t *testing.T) {
	configPath, _ := writeInitTestConfig(t)
	withConfigFile(t, configPath)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	withPublicURLCheckClient(t, server)

	cmd, _ := bufferedCommand()
	err := runInvite(cmd, inviteOptions{
		CollectorURL: "https://beacon.example",
		SaveURL:      true,
		TTL:          time.Minute,
		Format:       "text",
	})
	if err == nil || !strings.Contains(err.Error(), "public URL checks pass") {
		t.Fatalf("runInvite error = %v, want public URL check refusal", err)
	}
	if !strings.Contains(err.Error(), "beacon doctor setup") {
		t.Fatalf("runInvite error = %v, want doctor guidance", err)
	}
	cfg, loadErr := config.Load(configPath)
	if loadErr != nil {
		t.Fatalf("load config: %v", loadErr)
	}
	if cfg.Server.PublicURL != "" {
		t.Fatalf("server.public_url = %q, want unchanged empty value", cfg.Server.PublicURL)
	}
	if cfg.Auth.Mode != config.AuthModeLoopback {
		t.Fatalf("auth.mode = %q, want unchanged loopback", cfg.Auth.Mode)
	}
}

func TestRunInviteSaveURLPreservesReverseProxyAuthMode(t *testing.T) {
	configPath, _ := writeInitTestConfig(t)
	withConfigFile(t, configPath)
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	if _, err := f.WriteString("\n[auth]\nmode = \"reverse-proxy\"\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append auth config: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close config: %v", err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		case "/", "/api/status", "/api/mcp":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	withPublicURLCheckClient(t, server)

	cmd, _ := bufferedCommand()
	if err := runInvite(cmd, inviteOptions{
		CollectorURL: "https://beacon.example",
		SaveURL:      true,
		TTL:          time.Minute,
		Format:       "text",
	}); err != nil {
		t.Fatalf("runInvite returned error: %v", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Server.PublicURL != "https://beacon.example" {
		t.Fatalf("server.public_url = %q, want saved URL", cfg.Server.PublicURL)
	}
	if cfg.Auth.Mode != config.AuthModeReverseProxy {
		t.Fatalf("auth.mode = %q, want preserved reverse-proxy", cfg.Auth.Mode)
	}
}

func TestInviteCommandDefaultTTLIsThirtyMinutes(t *testing.T) {
	cmd := newInviteCmd()
	flag := cmd.Flags().Lookup("ttl")
	if flag == nil {
		t.Fatal("invite --ttl flag is missing")
		return
	}
	if got := flag.DefValue; got != defaultInviteTTL.String() {
		t.Fatalf("invite --ttl default = %q, want %q", got, defaultInviteTTL.String())
	}
}

func TestPublicURLChecksPassWhenRoutesAreProtected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			if r.Header.Get("Authorization") == "" {
				writeBeaconEnrollUnauthorized(w, `{"error":"missing bearer token"}`)
				return
			}
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
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

func TestPublicURLChecksPassWhenProtectedRoutesAreNotPublished(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		case "/", "/api/status", "/api/mcp":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()}); err != nil {
		t.Fatalf("runPublicURLChecks returned error: %v", err)
	}
}

func TestPublicURLChecksPassWhenProtectedRoutesRedirectToAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		case "/", "/api/status", "/api/mcp":
			http.Redirect(w, r, "/login", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()}); err != nil {
		t.Fatalf("runPublicURLChecks returned error: %v", err)
	}
}

func TestPublicURLChecksRejectGenericProxyUnauthorizedEnrollment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()})
	if err == nil || !strings.Contains(err.Error(), "did not reach Beacon") {
		t.Fatalf("runPublicURLChecks error = %v, want Beacon marker failure", err)
	}
}

func TestPublicURLChecksRejectHealthAndEnrollmentRedirects(t *testing.T) {
	t.Run("health", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/health-login", http.StatusTemporaryRedirect)
		}))
		defer server.Close()

		err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()})
		if err == nil || !strings.Contains(err.Error(), "health check returned HTTP 307") {
			t.Fatalf("runPublicURLChecks error = %v, want health redirect failure", err)
		}
	})
	t.Run("enrollment", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/health":
				w.WriteHeader(http.StatusOK)
			case "/api/ingest/v1/enroll":
				http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		err := runPublicURLChecks(context.Background(), server.URL, publicURLCheckOptions{Client: server.Client()})
		if err == nil || !strings.Contains(err.Error(), "enrollment auth check returned HTTP 307") {
			t.Fatalf("runPublicURLChecks error = %v, want enrollment redirect failure", err)
		}
	})
}

func TestPublicURLChecksRejectExposedDashboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
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
			writeBeaconEnrollUnauthorized(w, `{"error":"missing bearer token"}`)
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

func writeBeaconEnrollUnauthorized(w http.ResponseWriter, body string) {
	w.Header().Set(web.IngestRouteHeader, web.IngestRouteEnroll)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(body))
}

func withPublicURLCheckClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	oldClient := newPublicURLCheckClient
	t.Cleanup(func() {
		newPublicURLCheckClient = oldClient
	})
	newPublicURLCheckClient = func() *http.Client {
		return publicURLTestClient(server)
	}
}

func withSetupPublicURLCheckClient(t *testing.T, server *httptest.Server) {
	t.Helper()
	oldClient := newSetupPublicURLCheckClient
	t.Cleanup(func() {
		newSetupPublicURLCheckClient = oldClient
	})
	newSetupPublicURLCheckClient = func() *http.Client {
		return publicURLTestClient(server)
	}
}

func publicURLTestClient(server *httptest.Server) *http.Client {
	dialer := &net.Dialer{}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test-only local TLS fixture with a synthetic public hostname.
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	return &http.Client{Transport: transport, Timeout: time.Second}
}
