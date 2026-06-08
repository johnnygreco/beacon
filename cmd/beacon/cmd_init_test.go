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
	"sync/atomic"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/web"
)

func TestRunInitCreatesOwnerAndEnrollTokensWithoutUnsafeCommand(t *testing.T) {
	configPath, metadataPath := writeInitTestConfig(t)
	withConfigFile(t, configPath)

	cmd := newInitCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runInit(cmd, time.Minute); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	output := out.String()
	if strings.Contains(output, "On the collector") {
		t.Fatalf("init output implies remote collector enrollment: %q", output)
	}
	if !strings.Contains(output, "configured metadata store") {
		t.Fatalf("init output does not scope enrollment to the configured metadata store: %q", output)
	}
	tokens := tokensFromOutput(output)
	if len(tokens) != 2 {
		t.Fatalf("tokens in output = %v, want owner and enrollment tokens", tokens)
	}
	if !strings.HasPrefix(tokens[0], "bcn_owner_") || !strings.HasPrefix(tokens[1], "bcn_enroll_") {
		t.Fatalf("tokens = %v, want owner then enrollment token", tokens)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "beacon enroll") && strings.Contains(line, "bcn_") {
			t.Fatalf("suggested enrollment command embeds a token: %q", line)
		}
	}
	store, err := controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open metadata: %v", err)
	}
	defer store.Close()
	if _, err := store.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      tokens[0],
		AllowedTypes:   []string{controlplane.TokenTypeOwner},
		RequiredScopes: []string{controlplane.ScopeRead},
	}); err != nil {
		t.Fatalf("AuthenticateToken owner: %v", err)
	}
	if _, err := store.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      tokens[1],
		AllowedTypes:   []string{controlplane.TokenTypeEnroll},
		RequiredScopes: []string{controlplane.ScopeEnroll},
	}); err != nil {
		t.Fatalf("AuthenticateToken enroll: %v", err)
	}
}

func TestRunInitControlPlaneRoleDoesNotCreateLocalCollector(t *testing.T) {
	configPath, metadataPath := writeInitTestConfigWithRole(t, config.FleetRoleControlPlane)
	withConfigFile(t, configPath)

	cmd := newInitCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runInit(cmd, time.Minute); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if tokens := tokensFromOutput(out.String()); len(tokens) != 2 {
		t.Fatalf("tokens in output = %v, want owner and enrollment tokens", tokens)
	}
	store, err := controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open metadata: %v", err)
	}
	defer store.Close()
	snapshot, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.LocalNodeID != "" || snapshot.LocalCollectorID != "" {
		t.Fatalf("control-plane init created local identity: node=%q collector=%q", snapshot.LocalNodeID, snapshot.LocalCollectorID)
	}
	if len(snapshot.Nodes) != 0 || len(snapshot.Collectors) != 0 || len(snapshot.Sources) != 0 {
		t.Fatalf("control-plane init created collector metadata: nodes=%#v collectors=%#v sources=%#v", snapshot.Nodes, snapshot.Collectors, snapshot.Sources)
	}
}

func TestRunInitRejectsCollectorRole(t *testing.T) {
	configPath, metadataPath := writeInitTestConfigWithRole(t, config.FleetRoleCollector)
	withConfigFile(t, configPath)

	err := runInit(newInitCmd(), time.Minute)
	if err == nil {
		t.Fatal("runInit returned nil error")
	}
	if !strings.Contains(err.Error(), `fleet.role "collector" uses beacon enroll`) {
		t.Fatalf("runInit error = %q, want collector role guidance", err.Error())
	}
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata file stat error = %v, want not created", statErr)
	}
}

func TestRunEnrollRejectsControlPlaneRoleBeforeLocalEnrollment(t *testing.T) {
	configPath, metadataPath := writeInitTestConfigWithRole(t, config.FleetRoleControlPlane)
	withConfigFile(t, configPath)

	err := runEnroll(newEnrollCmd(), nil, false, "")
	if err == nil {
		t.Fatal("runEnroll returned nil error")
	}
	if !strings.Contains(err.Error(), `local enrollment requires fleet.role "both"`) {
		t.Fatalf("runEnroll error = %q, want local role rejection", err.Error())
	}
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata file stat error = %v, want not created", statErr)
	}
}

func TestRunEnrollRejectsRemoteNonCollectorRoleBeforeTokenRead(t *testing.T) {
	configPath, metadataPath := writeInitTestConfig(t)
	withConfigFile(t, configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}

	err = runEnroll(newEnrollCmd(), []string{"http://127.0.0.1:1"}, false, "")
	if err == nil {
		t.Fatal("runEnroll returned nil error")
	}
	if !strings.Contains(err.Error(), `remote enrollment requires fleet.role "collector"`) {
		t.Fatalf("runEnroll error = %q, want remote role rejection", err.Error())
	}
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata file stat error = %v, want not created", statErr)
	}
	if _, statErr := os.Stat(cfg.Fleet.IngestTokenFile); !os.IsNotExist(statErr) {
		t.Fatalf("ingest token file stat error = %v, want not created", statErr)
	}
}

func TestRunEnrollReadsTokenFromStdinAndMintsIngestToken(t *testing.T) {
	configPath, metadataPath := writeInitTestConfig(t)
	withConfigFile(t, configPath)
	store, err := controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open metadata: %v", err)
	}
	expires := time.Now().UTC().Add(time.Hour)
	enroll, err := store.CreateToken(context.Background(), controlplane.CreateTokenRequest{
		Type:      controlplane.TokenTypeEnroll,
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("CreateToken enroll: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close metadata: %v", err)
	}

	cmd := newEnrollCmd()
	cmd.SetIn(strings.NewReader(enroll.Plaintext + "\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runEnroll(cmd, nil, true, ""); err != nil {
		t.Fatalf("runEnroll: %v", err)
	}
	tokens := tokensFromOutput(out.String())
	if len(tokens) != 1 || !strings.HasPrefix(tokens[0], "bcn_ingest_") {
		t.Fatalf("tokens in output = %v, want one ingest token", tokens)
	}

	reopened, err := controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open reopened: %v", err)
	}
	defer reopened.Close()
	snapshot, err := reopened.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Sources) == 0 {
		t.Fatal("enrollment did not record source assignments")
	}
	if _, err := reopened.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      tokens[0],
		AllowedTypes:   []string{controlplane.TokenTypeIngest},
		RequiredScopes: []string{controlplane.ScopeIngest},
		NodeID:         "node-cli",
		CollectorID:    "collector-cli",
		SourceID:       snapshot.Sources[0].ID,
	}); err != nil {
		t.Fatalf("AuthenticateToken ingest: %v", err)
	}
}

func TestReadEnrollmentTokenSafeInputs(t *testing.T) {
	cmd := newEnrollCmd()
	cmd.SetIn(strings.NewReader(" from-stdin \n"))
	token, err := readEnrollmentToken(cmd, true, "")
	if err != nil {
		t.Fatalf("readEnrollmentToken stdin: %v", err)
	}
	if token != "from-stdin" {
		t.Fatalf("stdin token = %q, want trimmed token", token)
	}

	t.Setenv("BEACON_TEST_ENROLL_TOKEN", " from-env ")
	token, err = readEnrollmentToken(newEnrollCmd(), false, "BEACON_TEST_ENROLL_TOKEN")
	if err != nil {
		t.Fatalf("readEnrollmentToken env: %v", err)
	}
	if token != "from-env" {
		t.Fatalf("env token = %q, want trimmed token", token)
	}
	if _, err := readEnrollmentToken(newEnrollCmd(), false, ""); err == nil {
		t.Fatal("readEnrollmentToken without safe source returned nil error")
	}
	if _, err := readEnrollmentToken(newEnrollCmd(), true, "BEACON_TEST_ENROLL_TOKEN"); err == nil {
		t.Fatal("readEnrollmentToken with both sources returned nil error")
	}
}

func TestEnrollCommandRejectsTokenArgv(t *testing.T) {
	token := "bcn_enroll_secret"
	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"enroll", token})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("enroll command accepted a token argument")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error echoed token: %q", err.Error())
	}
	if strings.Contains(errOut.String(), token) {
		t.Fatalf("stderr echoed token: %q", errOut.String())
	}
}

func TestRunRemoteEnrollRejectsNonLoopbackHTTPBeforeRequest(t *testing.T) {
	configPath, _ := writeInitTestConfig(t)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	err = runRemoteEnroll(newEnrollCmd(), cfg, "http://beacon.example", "bcn_enroll_secret")
	if err == nil || !strings.Contains(err.Error(), "https for non-loopback") {
		t.Fatalf("runRemoteEnroll error = %v, want non-loopback HTTPS rejection", err)
	}
}

func TestPostRemoteEnrollmentDoesNotForwardSecretsAcrossRedirect(t *testing.T) {
	var redirected atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer attacker.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := postRemoteEnrollment(
		context.Background(),
		server.URL,
		"bcn_enroll_secret",
		"bcn_ingest_secret",
		enrollBootstrapFromControlPlane(controlplane.Bootstrap{
			NodeName: "CLI Node",
			Sources: []controlplane.SourceRegistration{{
				Name:      "codex",
				Runtime:   models.RuntimeCodex,
				Provider:  models.ProviderOpenAI,
				Format:    models.FormatJSONL,
				WatchRoot: "~/.codex",
			}},
		}),
	)
	if err == nil || !strings.Contains(err.Error(), "status 307") {
		t.Fatalf("postRemoteEnrollment error = %v, want 307 failure", err)
	}
	if got := redirected.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests, want none", got)
	}
}

func TestRunRemoteEnrollPersistsTokenBeforeAssignmentVerification(t *testing.T) {
	configPath, _ := writeInitTestConfigWithRole(t, config.FleetRoleCollector)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ingest.EnrollResponse{
			Schema: ingest.SchemaV1,
			Assignment: ingest.EnrollAssignment{
				NodeID:      "node-remote",
				CollectorID: "collector-remote",
				SourceIDs:   []string{"source-remote"},
				Sources: []ingest.EnrollSourceAssignment{{
					Name:     "unknown-source",
					SourceID: "source-remote",
				}},
			},
			IngestToken: "bcn_ingest_recovery",
		})
	}))
	defer server.Close()

	err = runRemoteEnroll(newEnrollCmd(), cfg, server.URL, "bcn_enroll_secret")
	if err == nil || !strings.Contains(err.Error(), "source assignment") {
		t.Fatalf("runRemoteEnroll error = %v, want assignment verification failure", err)
	}
	data, err := os.ReadFile(cfg.Fleet.IngestTokenFile)
	if err != nil {
		t.Fatalf("read recovery ingest token: %v", err)
	}
	if string(data) != "bcn_ingest_recovery\n" {
		t.Fatalf("recovery ingest token = %q, want returned token", string(data))
	}
}

func TestRunRemoteEnrollRejectsNonCollectorRoleBeforePersistence(t *testing.T) {
	configPath, metadataPath := writeInitTestConfig(t)
	withConfigFile(t, configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}

	err = runRemoteEnroll(newEnrollCmd(), cfg, "http://127.0.0.1:1", "bcn_enroll_secret")
	if err == nil {
		t.Fatal("runRemoteEnroll returned nil error")
	}
	if !strings.Contains(err.Error(), `remote enrollment requires fleet.role "collector"`) {
		t.Fatalf("runRemoteEnroll error = %q, want collector role rejection", err.Error())
	}
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata file stat error = %v, want not created", statErr)
	}
	if _, statErr := os.Stat(cfg.Fleet.IngestTokenFile); !os.IsNotExist(statErr) {
		t.Fatalf("ingest token file stat error = %v, want not created", statErr)
	}
}

func TestRunRemoteEnrollPrintsConfigAndCollectorUsesAssignedMetadata(t *testing.T) {
	configPath, metadataPath := writeInitTestConfigWithRole(t, config.FleetRoleCollector)
	withConfigFile(t, configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	control, err := controlplane.Open(filepath.Join(t.TempDir(), "server-control-plane.db"))
	if err != nil {
		t.Fatalf("Open server control-plane: %v", err)
	}
	defer control.Close()
	if _, err := control.EnsureLocal(context.Background(), controlplane.Bootstrap{
		NodeID:      "node-server",
		NodeName:    "Server",
		CollectorID: "collector-server",
		Sources: []controlplane.SourceRegistration{{
			Name:      "server-codex",
			Runtime:   models.RuntimeCodex,
			Provider:  models.ProviderOpenAI,
			Format:    models.FormatJSONL,
			WatchRoot: "~/.codex",
		}},
	}); err != nil {
		t.Fatalf("EnsureLocal server: %v", err)
	}
	enroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken enroll: %v", err)
	}
	handlers := web.NewIngestHandlers(control, cmdTestCommitter{}, 0, 0, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ingest/v1/enroll", handlers.Enroll)
	server := httptest.NewServer(mux)
	defer server.Close()

	cmd := newEnrollCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runRemoteEnroll(cmd, cfg, server.URL, enroll.Plaintext); err != nil {
		t.Fatalf("runRemoteEnroll: %v", err)
	}
	output := out.String()
	wantCommand := "Run collector: beacon --config " + configPath + " collect --control-plane-url " + server.URL
	if !strings.Contains(output, wantCommand) {
		t.Fatalf("remote enroll output = %q, want command %q", output, wantCommand)
	}
	if strings.Contains(output, enroll.Plaintext) {
		t.Fatalf("remote enroll output leaked enrollment token: %q", output)
	}

	local, err := controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open local metadata: %v", err)
	}
	snapshot, err := local.Snapshot(context.Background())
	_ = local.Close()
	if err != nil {
		t.Fatalf("local Snapshot: %v", err)
	}
	if snapshot.LocalNodeID == "node-cli" || snapshot.LocalCollectorID == "collector-cli" {
		t.Fatalf("local metadata kept claimed IDs: node=%s collector=%s", snapshot.LocalNodeID, snapshot.LocalCollectorID)
	}
	firstNodeID := snapshot.LocalNodeID
	firstCollectorID := snapshot.LocalCollectorID
	firstToken, err := os.ReadFile(cfg.Fleet.IngestTokenFile)
	if err != nil {
		t.Fatalf("read first ingest token: %v", err)
	}

	secondEnroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken second enroll: %v", err)
	}
	out.Reset()
	if err := runRemoteEnroll(cmd, cfg, server.URL, secondEnroll.Plaintext); err != nil {
		t.Fatalf("second runRemoteEnroll: %v", err)
	}
	local, err = controlplane.Open(metadataPath)
	if err != nil {
		t.Fatalf("Open local metadata after second enroll: %v", err)
	}
	snapshot, err = local.Snapshot(context.Background())
	_ = local.Close()
	if err != nil {
		t.Fatalf("second local Snapshot: %v", err)
	}
	if snapshot.LocalNodeID != firstNodeID || snapshot.LocalCollectorID != firstCollectorID {
		t.Fatalf("second enroll assignment = %s/%s, want existing %s/%s", snapshot.LocalNodeID, snapshot.LocalCollectorID, firstNodeID, firstCollectorID)
	}
	secondToken, err := os.ReadFile(cfg.Fleet.IngestTokenFile)
	if err != nil {
		t.Fatalf("read second ingest token: %v", err)
	}
	if string(secondToken) == string(firstToken) {
		t.Fatal("second remote enrollment did not rotate ingest token")
	}
	serverSnapshot, err := control.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("server Snapshot after second enroll: %v", err)
	}
	firstSourceID := sourceIDByCollectorName(serverSnapshot, firstCollectorID, "codex")
	if firstSourceID == "" {
		t.Fatalf("server snapshot missing codex source for %s: %#v", firstCollectorID, serverSnapshot.Sources)
	}
	if _, err := control.AuthenticateToken(context.Background(), controlplane.AuthenticateTokenRequest{
		Plaintext:      strings.TrimSpace(string(firstToken)),
		AllowedTypes:   []string{controlplane.TokenTypeIngest},
		RequiredScopes: []string{controlplane.ScopeIngest},
		NodeID:         firstNodeID,
		CollectorID:    firstCollectorID,
		SourceID:       firstSourceID,
	}); err != nil {
		t.Fatalf("old ingest token should remain usable until replacement is proven: %v", err)
	}

	cfg.Fleet.ControlPlaneURL = server.URL
	service, cleanup, err := buildCollectorService(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("buildCollectorService after remote enroll: %v", err)
	}
	defer cleanup()
	if got := service.Status().BlockedTerminal; got {
		t.Fatalf("new collector service terminal blocked = true")
	}
}

func TestWriteIngestTokenFileSecuresExistingFileWithoutChmodParent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("mkdir custom dir: %v", err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatalf("chmod custom dir: %v", err)
	}
	path := filepath.Join(dir, "ingest-token")
	if err := os.WriteFile(path, []byte("old\n"), 0644); err != nil {
		t.Fatalf("write existing token: %v", err)
	}
	if err := writeIngestTokenFile(path, "new-token"); err != nil {
		t.Fatalf("writeIngestTokenFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(data) != "new-token\n" {
		t.Fatalf("token data = %q, want new token", string(data))
	}
	assertFileMode(t, path, 0600)
	assertFileMode(t, dir, 0755)
}

func TestWriteIngestTokenFileCreatesPrivateParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "ingest-token")
	if err := writeIngestTokenFile(path, "new-token"); err != nil {
		t.Fatalf("writeIngestTokenFile: %v", err)
	}
	assertFileMode(t, path, 0600)
	assertFileMode(t, filepath.Dir(path), 0700)
}

func writeInitTestConfig(t *testing.T) (string, string) {
	t.Helper()
	return writeInitTestConfigWithRole(t, config.FleetRoleBoth)
}

func writeInitTestConfigWithRole(t *testing.T, role string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "control-plane.db")
	configPath := filepath.Join(dir, "beacon.toml")
	body := `
[server]
host = "127.0.0.1"
port = 4600

	[fleet]
	role = "` + role + `"
	metadata_path = "` + metadataPath + `"
	ingest_token_file = "` + filepath.Join(dir, "ingest-token") + `"
	spool_dir = "` + filepath.Join(dir, "spool") + `"
	node_id = "node-cli"
	node_name = "CLI Node"
	collector_id = "collector-cli"
`
	if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, metadataPath
}

type cmdTestCommitter struct{}

func (cmdTestCommitter) CommitIngestBatch(context.Context, store.IngestBatchMeta, store.RowBatch) (store.IngestBatchAck, error) {
	return store.IngestBatchAck{}, nil
}

func (cmdTestCommitter) InsertCaptureHeartbeats(context.Context, []models.CaptureHeartbeat) error {
	return nil
}

func withConfigFile(t *testing.T, path string) {
	t.Helper()
	old := cfgFile
	cfgFile = path
	t.Cleanup(func() {
		cfgFile = old
	})
}

func sourceIDByCollectorName(snapshot *controlplane.Snapshot, collectorID, name string) string {
	if snapshot == nil {
		return ""
	}
	for _, source := range snapshot.Sources {
		if source.CollectorID == collectorID && source.Name == name {
			return source.ID
		}
	}
	return ""
}

func tokensFromOutput(output string) []string {
	var tokens []string
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, "'\"")
		if strings.HasPrefix(field, "bcn_") {
			tokens = append(tokens, field)
		}
	}
	return tokens
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %o, want %o", path, got, want)
	}
}
