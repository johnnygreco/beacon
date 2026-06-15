package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/web"
	"github.com/spf13/cobra"
)

func TestRunJoinEnrollsCollectorAndSendsHeartbeat(t *testing.T) {
	resetConfigState(t)
	control, enroll, server, committer := newJoinTestControlPlane(t)
	defer control.Close()
	defer server.Close()

	cmd, out := joinTestCommand(enroll.Plaintext + "\n")
	err := runJoin(cmd, []string{server.URL}, joinOptions{
		TokenStdin: true,
		Sources:    "codex",
		Name:       "Join Collector",
	})
	if err != nil {
		t.Fatalf("runJoin returned error: %v", err)
	}
	output := out.String()
	if strings.Contains(output, enroll.Plaintext) {
		t.Fatalf("join output leaked enrollment token: %q", output)
	}
	if committer.heartbeats == 0 {
		t.Fatalf("heartbeats = %d, want authenticated heartbeat", committer.heartbeats)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("load joined config: %v", err)
	}
	if cfg.Fleet.Role != config.FleetRoleCollector {
		t.Fatalf("fleet.role = %q", cfg.Fleet.Role)
	}
	if cfg.Fleet.ControlPlaneURL != server.URL {
		t.Fatalf("fleet.control_plane_url = %q, want %q", cfg.Fleet.ControlPlaneURL, server.URL)
	}
	if cfg.Fleet.NodeName != "Join Collector" {
		t.Fatalf("fleet.node_name = %q", cfg.Fleet.NodeName)
	}
	if len(cfg.Capture.Sources) != 1 || cfg.Capture.Sources[0].Name != "codex" {
		t.Fatalf("sources = %#v, want only codex", cfg.Capture.Sources)
	}
	assertFileMode(t, cfg.Fleet.IngestTokenFile, 0600)
	if !strings.Contains(output, "Authenticated heartbeat: passed") || !strings.Contains(output, "Collector smoke collection: passed") {
		t.Fatalf("join output missing validation steps: %q", output)
	}
}

func TestRunJoinPreflightFailureDoesNotWriteConfigOrSendToken(t *testing.T) {
	resetConfigState(t)
	realToken := "bcn_enroll_real_0123456789abcdef"
	var realTokenRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			if strings.Contains(r.Header.Get("Authorization"), realToken) {
				realTokenRequests++
			}
			http.Error(w, "not beacon", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cmd, _ := joinTestCommand(realToken + "\n")
	err := runJoin(cmd, []string{server.URL}, joinOptions{TokenStdin: true})
	if err == nil || !strings.Contains(err.Error(), "preflight failed") {
		t.Fatalf("runJoin error = %v, want preflight failure", err)
	}
	if !strings.Contains(err.Error(), "beacon doctor setup") {
		t.Fatalf("runJoin error = %v, want doctor guidance", err)
	}
	if realTokenRequests != 0 {
		t.Fatalf("real token requests = %d, want 0", realTokenRequests)
	}
	if _, err := os.Stat(config.DefaultConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("config was written despite preflight failure; stat error = %v", err)
	}
}

func TestRunJoinDryRunDoesNotRequireOrSendToken(t *testing.T) {
	resetConfigState(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			requests++
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cmd, out := joinTestCommand("")
	err := runJoin(cmd, []string{server.URL}, joinOptions{DryRun: true, Sources: "codex"})
	if err != nil {
		t.Fatalf("runJoin dry-run returned error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("preflight requests = %d, want one invalid-bearer request", requests)
	}
	if _, err := os.Stat(config.DefaultConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote config; stat error = %v", err)
	}
	if !strings.Contains(out.String(), "real enrollment token was not sent") {
		t.Fatalf("dry-run output = %q", out.String())
	}
}

func TestRunJoinInviteFile(t *testing.T) {
	resetConfigState(t)
	control, enroll, server, _ := newJoinTestControlPlane(t)
	defer control.Close()
	defer server.Close()

	invitePath := filepath.Join(t.TempDir(), "invite.json")
	data, err := json.Marshal(joinInviteFile{
		Schema:          "beacon.invite.v1",
		ControlPlaneURL: server.URL,
		EnrollmentToken: enroll.Plaintext,
	})
	if err != nil {
		t.Fatalf("marshal invite: %v", err)
	}
	if err := os.WriteFile(invitePath, data, 0600); err != nil {
		t.Fatalf("write invite: %v", err)
	}

	cmd, _ := joinTestCommand("")
	if err := runJoin(cmd, nil, joinOptions{InviteFile: invitePath, Sources: "codex"}); err != nil {
		t.Fatalf("runJoin invite file returned error: %v", err)
	}
}

func TestRunJoinInviteFileURLMatchesAfterNormalization(t *testing.T) {
	resetConfigState(t)
	control, enroll, server, _ := newJoinTestControlPlane(t)
	defer control.Close()
	defer server.Close()

	invitePath := filepath.Join(t.TempDir(), "invite.json")
	data, err := json.Marshal(joinInviteFile{
		Schema:          "beacon.invite.v1",
		ControlPlaneURL: server.URL + "/",
		EnrollmentToken: enroll.Plaintext,
	})
	if err != nil {
		t.Fatalf("marshal invite: %v", err)
	}
	if err := os.WriteFile(invitePath, data, 0600); err != nil {
		t.Fatalf("write invite: %v", err)
	}

	cmd, _ := joinTestCommand("")
	if err := runJoin(cmd, []string{server.URL}, joinOptions{InviteFile: invitePath, Sources: "codex"}); err != nil {
		t.Fatalf("runJoin invite file with equivalent URL returned error: %v", err)
	}
}

func TestRunJoinRejectsUnsupportedInviteSchema(t *testing.T) {
	resetConfigState(t)
	invitePath := filepath.Join(t.TempDir(), "invite.json")
	data, err := json.Marshal(joinInviteFile{
		Schema:          "beacon.invite.v2",
		ControlPlaneURL: "http://127.0.0.1:4600",
		EnrollmentToken: "bcn_enroll_secret",
	})
	if err != nil {
		t.Fatalf("marshal invite: %v", err)
	}
	if err := os.WriteFile(invitePath, data, 0600); err != nil {
		t.Fatalf("write invite: %v", err)
	}

	_, err = readJoinInviteFile(invitePath)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("readJoinInviteFile error = %v, want unsupported schema", err)
	}
}

func TestRunJoinLoopbackURLPrintsTunnelWarning(t *testing.T) {
	resetConfigState(t)
	cmd, out := joinTestCommand("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := runJoin(cmd, []string{server.URL}, joinOptions{DryRun: true})
	if err != nil {
		t.Fatalf("runJoin dry-run returned error: %v", err)
	}
	if !strings.Contains(out.String(), "local tunnel forwards") {
		t.Fatalf("join output = %q, want loopback tunnel warning", out.String())
	}
}

func TestRunJoinForceRefusesDifferentControlPlaneWithExistingIdentity(t *testing.T) {
	resetConfigState(t)
	control, enroll, serverA, _ := newJoinTestControlPlane(t)
	defer control.Close()
	defer serverA.Close()

	cmd, _ := joinTestCommand(enroll.Plaintext + "\n")
	if err := runJoin(cmd, []string{serverA.URL}, joinOptions{TokenStdin: true, Sources: "codex"}); err != nil {
		t.Fatalf("initial runJoin returned error: %v", err)
	}

	var enrollRequests int
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			enrollRequests++
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer serverB.Close()

	cmd, _ = joinTestCommand("bcn_enroll_other\n")
	err := runJoin(cmd, []string{serverB.URL}, joinOptions{TokenStdin: true, Force: true, Sources: "codex"})
	if err == nil || !strings.Contains(err.Error(), "different control plane") {
		t.Fatalf("runJoin error = %v, want different control plane refusal", err)
	}
	if enrollRequests != 0 {
		t.Fatalf("new control-plane enrollment requests = %d, want 0", enrollRequests)
	}
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("load config after rejected join: %v", err)
	}
	if cfg.Fleet.ControlPlaneURL != serverA.URL {
		t.Fatalf("control_plane_url = %q, want original %q", cfg.Fleet.ControlPlaneURL, serverA.URL)
	}
}

func TestRunJoinForceAllowsExistingLocalIdentityConversion(t *testing.T) {
	resetConfigState(t)
	path := filepath.Join(t.TempDir(), "beacon.toml")
	metadataPath := filepath.Join(t.TempDir(), "metadata.db")
	body := `
[fleet]
role = "both"
metadata_path = "` + metadataPath + `"
node_name = "Local Dashboard"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	local, _, err := initializeControlPlane(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initializeControlPlane: %v", err)
	}
	if err := local.Close(); err != nil {
		t.Fatalf("close local control plane: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			writeBeaconEnrollUnauthorized(w, `{"error":"unauthorized"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cmd, out := joinTestCommand("")
	err = runJoin(cmd, []string{server.URL}, joinOptions{DryRun: true, Force: true, Sources: "codex"})
	if err != nil {
		t.Fatalf("runJoin dry-run force returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Dry run: config was not written") {
		t.Fatalf("join output = %q, want dry-run completion", out.String())
	}
}

func TestRunRemoteEnrollRefusesDifferentControlPlaneWithExistingIdentity(t *testing.T) {
	resetConfigState(t)
	control, enroll, serverA, _ := newJoinTestControlPlane(t)
	defer control.Close()
	defer serverA.Close()

	cmd, _ := joinTestCommand(enroll.Plaintext + "\n")
	if err := runJoin(cmd, []string{serverA.URL}, joinOptions{TokenStdin: true, Sources: "codex"}); err != nil {
		t.Fatalf("initial runJoin returned error: %v", err)
	}
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("load joined config: %v", err)
	}

	var requests int
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "do not call", http.StatusBadGateway)
	}))
	defer serverB.Close()

	err = runRemoteEnroll(newEnrollCmd(), cfg, serverB.URL, "bcn_enroll_other")
	if err == nil || !strings.Contains(err.Error(), "different control plane") {
		t.Fatalf("runRemoteEnroll error = %v, want different control plane refusal", err)
	}
	if requests != 0 {
		t.Fatalf("new control-plane requests = %d, want 0", requests)
	}
}

func TestPreflightEnrollmentRouteRejectsRedirectWithoutFollowing(t *testing.T) {
	resetConfigState(t)
	var redirected int
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected++
	}))
	defer attacker.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			http.Redirect(w, r, attacker.URL+"/stolen", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := preflightEnrollmentRoute(context.Background(), server.URL, publicURLCheckOptions{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("preflightEnrollmentRoute error = %v, want redirect status failure", err)
	}
	if redirected != 0 {
		t.Fatalf("redirect target received %d requests, want none", redirected)
	}
}

func TestRunJoinRejectsTokenSourcesTogether(t *testing.T) {
	resetConfigState(t)
	control, enroll, server, _ := newJoinTestControlPlane(t)
	defer control.Close()
	defer server.Close()

	invitePath := filepath.Join(t.TempDir(), "invite.json")
	data, err := json.Marshal(joinInviteFile{ControlPlaneURL: server.URL, EnrollmentToken: enroll.Plaintext})
	if err != nil {
		t.Fatalf("marshal invite: %v", err)
	}
	if err := os.WriteFile(invitePath, data, 0600); err != nil {
		t.Fatalf("write invite: %v", err)
	}

	cmd, _ := joinTestCommand(enroll.Plaintext + "\n")
	err = runJoin(cmd, nil, joinOptions{InviteFile: invitePath, TokenStdin: true})
	if err == nil || !strings.Contains(err.Error(), "use only one") {
		t.Fatalf("runJoin error = %v, want token source conflict", err)
	}
}

func TestRunJoinRejectsMissingTokenWithoutWritingConfig(t *testing.T) {
	resetConfigState(t)

	cmd, _ := joinTestCommand("")
	err := runJoin(cmd, []string{"http://127.0.0.1:1"}, joinOptions{})
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("runJoin error = %v, want missing token source error", err)
	}
	if _, err := os.Stat(config.DefaultConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("missing token wrote config; stat error = %v", err)
	}
}

type joinTestCommitter struct {
	heartbeats int
}

func (j *joinTestCommitter) CommitIngestBatch(context.Context, store.IngestBatchMeta, store.RowBatch) (store.IngestBatchAck, error) {
	return store.IngestBatchAck{}, nil
}

func (j *joinTestCommitter) InsertCaptureHeartbeats(context.Context, []models.CaptureHeartbeat) error {
	j.heartbeats++
	return nil
}

func newJoinTestControlPlane(t *testing.T) (*controlplane.Store, *controlplane.CreatedToken, *httptest.Server, *joinTestCommitter) {
	t.Helper()
	control, err := controlplane.Open(filepath.Join(t.TempDir(), "server-control-plane.db"))
	if err != nil {
		t.Fatalf("Open control-plane: %v", err)
	}
	enroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		control.Close()
		t.Fatalf("CreateToken enroll: %v", err)
	}
	committer := &joinTestCommitter{}
	handlers := web.NewIngestHandlers(control, committer, 0, 0, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/ingest/v1/enroll", handlers.Enroll)
	mux.HandleFunc("/api/ingest/v1/heartbeats", handlers.Heartbeat)
	mux.HandleFunc("/api/ingest/v1/batches", handlers.Batch)
	server := httptest.NewServer(mux)
	return control, enroll, server, committer
}

func joinTestCommand(input string) (*cobra.Command, *bytes.Buffer) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd, &out
}
