package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/spf13/cobra"
)

func TestRunDoctorSetupFreshHomeReportsRemediation(t *testing.T) {
	resetConfigState(t)
	withNoDockerOnPath(t)
	withDoctorClickHouseReady(t)

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); !errors.Is(err, errDoctorSetupFailed) {
		t.Fatalf("runDoctorSetup error = %v, want doctor failure", err)
	}
	output := out.String()
	for _, want := range []string{
		"[PASS] config",
		"[FAIL] control-plane metadata",
		"[WARN] server.public_url",
		"beacon setup dashboard",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestRunDoctorSetupInvalidPublicURLReportsConfigFailure(t *testing.T) {
	resetConfigState(t)
	path := filepath.Join(t.TempDir(), "beacon.toml")
	body := `
[server]
public_url = "https://beacon.example/path"

[fleet]
role = "both"
metadata_path = "` + filepath.Join(t.TempDir(), "metadata.db") + `"
node_name = "Invalid URL"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); !errors.Is(err, errDoctorSetupFailed) {
		t.Fatalf("runDoctorSetup error = %v, want doctor failure", err)
	}
	if output := out.String(); !strings.Contains(output, "[FAIL] config") || !strings.Contains(output, "root URL without a path") {
		t.Fatalf("doctor output = %q, want URL validation failure", output)
	}
}

func TestRunDoctorSetupDashboardPublicChecks(t *testing.T) {
	resetConfigState(t)
	withNoDockerOnPath(t)
	withDoctorClickHouseReady(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/ingest/v1/enroll":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		case "/", "/api/status", "/api/mcp":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	withPublicURLCheckClient(t, server)

	path := filepath.Join(t.TempDir(), "beacon.toml")
	metadataPath := filepath.Join(t.TempDir(), "metadata.db")
	body := `
[server]
host = "127.0.0.1"
port = 4600
public_url = "https://beacon.example"

[fleet]
role = "both"
metadata_path = "` + metadataPath + `"
node_name = "Doctor Dashboard"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, snapshot, err := initializeControlPlane(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initializeControlPlane: %v", err)
	}
	_ = snapshot
	if err := store.Close(); err != nil {
		t.Fatalf("close control plane: %v", err)
	}

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); err != nil {
		t.Fatalf("runDoctorSetup returned error: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"[PASS] config",
		"[PASS] control-plane metadata",
		"[INFO] ClickHouse config",
		"[PASS] ClickHouse managed Docker",
		"[PASS] ClickHouse migration",
		"[PASS] public URL checks",
		"beacon invite",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestRunDoctorSetupLoopbackPublicURLIsLocalOnly(t *testing.T) {
	resetConfigState(t)
	withNoDockerOnPath(t)
	withDoctorClickHouseReady(t)
	path := filepath.Join(t.TempDir(), "beacon.toml")
	metadataPath := filepath.Join(t.TempDir(), "metadata.db")
	body := `
[server]
host = "127.0.0.1"
port = 4600
public_url = "http://127.0.0.1:4600"

[fleet]
role = "both"
metadata_path = "` + metadataPath + `"
node_name = "Loopback Dashboard"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, _, err := initializeControlPlane(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initializeControlPlane: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close control plane: %v", err)
	}

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); err != nil {
		t.Fatalf("runDoctorSetup returned error: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "local-only") || strings.Contains(output, "[FAIL] public URL checks") {
		t.Fatalf("doctor output = %q, want loopback local-only without public check failure", output)
	}
}

func TestRunDoctorSetupUsesReadOnlyClickHouseCheck(t *testing.T) {
	resetConfigState(t)
	withNoDockerOnPath(t)
	path := filepath.Join(t.TempDir(), "beacon.toml")
	metadataPath := filepath.Join(t.TempDir(), "metadata.db")
	body := `
[server]
host = "127.0.0.1"
port = 4600
public_url = "http://127.0.0.1:4600"

[fleet]
role = "both"
metadata_path = "` + metadataPath + `"
node_name = "Read Only Dashboard"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	controlStore, _, err := initializeControlPlane(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initializeControlPlane: %v", err)
	}
	if err := controlStore.Close(); err != nil {
		t.Fatalf("close control plane: %v", err)
	}

	oldStatusOpenStore := statusOpenStore
	statusOpenStore = func(context.Context, store.Options) (*store.Store, error) {
		t.Fatal("doctor setup called statusOpenStore, which runs migrations")
		return nil, nil
	}
	t.Cleanup(func() { statusOpenStore = oldStatusOpenStore })

	calledReadOnly := false
	oldReadOnly := doctorOpenClickHouseReadOnly
	doctorOpenClickHouseReadOnly = func(context.Context, store.Options) (*store.Store, error) {
		calledReadOnly = true
		return &store.Store{}, nil
	}
	t.Cleanup(func() { doctorOpenClickHouseReadOnly = oldReadOnly })

	cmd, _ := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); err != nil {
		t.Fatalf("runDoctorSetup returned error: %v", err)
	}
	if !calledReadOnly {
		t.Fatal("doctor setup did not call read-only ClickHouse check")
	}
}

func TestRunDoctorSetupReportsBroadManagedDockerClickHouseBindings(t *testing.T) {
	resetConfigState(t)
	withDoctorClickHouseReady(t)
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	dockerPath := filepath.Join(binDir, "docker")
	script := `#!/bin/sh
if [ "$1" = "container" ] && [ "$2" = "inspect" ] && [ "$3" = "beacon-clickhouse" ]; then
  exit 0
fi
if [ "$1" = "container" ] && [ "$2" = "inspect" ] && [ "$3" = "--format" ]; then
  echo '{"9000/tcp":[{"HostIp":"0.0.0.0","HostPort":"9000"}],"8123/tcp":[{"HostIp":"127.0.0.1","HostPort":"8123"}]}'
  exit 0
fi
exit 1
`
	if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
		t.Fatalf("write docker fixture: %v", err)
	}
	t.Setenv("PATH", binDir)

	path := filepath.Join(t.TempDir(), "beacon.toml")
	metadataPath := filepath.Join(t.TempDir(), "metadata.db")
	body := `
[server]
host = "127.0.0.1"
port = 4600
public_url = "http://127.0.0.1:4600"

[database]
addrs = ["127.0.0.1:9000"]

[fleet]
role = "both"
metadata_path = "` + metadataPath + `"
node_name = "Broad Docker"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, _, err := initializeControlPlane(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initializeControlPlane: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close control plane: %v", err)
	}

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); !errors.Is(err, errDoctorSetupFailed) {
		t.Fatalf("runDoctorSetup error = %v, want doctor failure", err)
	}
	output := out.String()
	for _, want := range []string{
		"[FAIL] ClickHouse managed Docker",
		"beyond loopback",
		"docker rm beacon-clickhouse",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestRunDoctorSetupCollectorJoinedState(t *testing.T) {
	resetConfigState(t)
	control, enroll, server, _ := newJoinTestControlPlane(t)
	defer control.Close()
	defer server.Close()

	cmd, _ := joinTestCommand(enroll.Plaintext + "\n")
	if err := runJoin(cmd, []string{server.URL}, joinOptions{TokenStdin: true, Sources: "codex"}); err != nil {
		t.Fatalf("runJoin returned error: %v", err)
	}

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); err != nil {
		t.Fatalf("runDoctorSetup returned error: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"[PASS] enrollment route preflight",
		"[PASS] collector metadata",
		"[PASS] source assignments",
		"[PASS] ingest token",
		"[PASS] collector spool",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, enroll.Plaintext) || strings.Contains(output, "bcn_ingest_") {
		t.Fatalf("doctor output leaked token material: %q", output)
	}
}

func TestRunDoctorSetupCollectorMissingJoinState(t *testing.T) {
	resetConfigState(t)
	path := filepath.Join(t.TempDir(), "beacon.toml")
	body := `
[fleet]
role = "collector"
metadata_path = "` + filepath.Join(t.TempDir(), "metadata.db") + `"
control_plane_url = "http://127.0.0.1:1"
ingest_token_file = "` + filepath.Join(t.TempDir(), "ingest-token") + `"
ingest_token_env = "BEACON_DOCTOR_TEST_INGEST_TOKEN"
spool_dir = "` + filepath.Join(t.TempDir(), "spool") + `"
node_name = "Doctor Collector"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); !errors.Is(err, errDoctorSetupFailed) {
		t.Fatalf("runDoctorSetup error = %v, want doctor failure", err)
	}
	output := out.String()
	for _, want := range []string{
		"[FAIL] enrollment route preflight",
		"[FAIL] collector metadata",
		"[FAIL] ingest token",
		"beacon join",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestRunDoctorSetupDoesNotPrintTokenValues(t *testing.T) {
	resetConfigState(t)
	secretToken := "bcn_ingest_secret_0123456789abcdef"
	path := filepath.Join(t.TempDir(), "beacon.toml")
	tokenPath := filepath.Join(t.TempDir(), "ingest-token")
	if err := os.WriteFile(tokenPath, []byte(secretToken+"\n"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	store, err := controlplane.Open(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	if _, err := store.EnsureLocal(context.Background(), controlplane.Bootstrap{
		NodeName:    "Secret Collector",
		CollectorID: "collector-secret",
		Sources: []controlplane.SourceRegistration{{
			Name:      "codex",
			Runtime:   "codex",
			Provider:  "openai",
			Format:    "jsonl",
			WatchRoot: t.TempDir(),
		}},
	}); err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	metadataPath := store.Path()
	if err := store.Close(); err != nil {
		t.Fatalf("close metadata: %v", err)
	}
	body := `
[[capture.sources]]
name = "codex"
runtime = "codex"
provider = "openai"
glob = "` + filepath.Join(t.TempDir(), "*.jsonl") + `"
watch_root = "` + t.TempDir() + `"
format = "jsonl"

[fleet]
role = "collector"
metadata_path = "` + metadataPath + `"
control_plane_url = "http://127.0.0.1:1"
ingest_token_file = "` + tokenPath + `"
spool_dir = "` + filepath.Join(t.TempDir(), "spool") + `"
node_name = "Secret Collector"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, path)

	cmd, out := doctorTestCommand()
	if err := runDoctorSetup(cmd, nil); !errors.Is(err, errDoctorSetupFailed) {
		t.Fatalf("runDoctorSetup error = %v, want doctor failure", err)
	}
	if output := out.String(); strings.Contains(output, secretToken) || strings.Contains(output, "bcn_ingest_") {
		t.Fatalf("doctor output leaked token material: %q", output)
	}
}

func doctorTestCommand() (*cobra.Command, *bytes.Buffer) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd, &out
}

func withDoctorClickHouseReady(t *testing.T) {
	t.Helper()
	oldOpen := doctorOpenClickHouseReadOnly
	doctorOpenClickHouseReadOnly = func(context.Context, store.Options) (*store.Store, error) {
		return &store.Store{}, nil
	}
	t.Cleanup(func() { doctorOpenClickHouseReadOnly = oldOpen })
}

func withNoDockerOnPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}
