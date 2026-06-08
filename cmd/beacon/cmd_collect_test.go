package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/collector"
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/controlplane"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/web"
)

func TestRunRemoteEnrollAndCollectOnceSmoke(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "codex")
	if err := os.MkdirAll(sourceDir, 0700); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	secret := "bcn_ingest_token123_0123456789abcdef"
	sourceFile := filepath.Join(sourceDir, "rollout-2026-06-08T12-00-00-smoke.jsonl")
	line := `{"type":"response_item","session_id":"smoke-session","timestamp":"2026-06-08T12:00:00Z","payload":{"id":"event-1","type":"message","role":"assistant","content":[{"type":"output_text","text":"use token ` + secret + `"}]}}` + "\n"
	if err := os.WriteFile(sourceFile, []byte(line), 0600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	configPath := filepath.Join(dir, "beacon.toml")
	metadataPath := filepath.Join(dir, "collector-control-plane.db")
	tokenPath := filepath.Join(dir, "ingest-token")
	spoolDir := filepath.Join(dir, "spool")
	body := `
[capture]
enabled = true
reconcile_interval = "30s"

[[capture.sources]]
name = "codex"
runtime = "codex"
provider = "openai"
glob = "` + sourceFile + `"
watch_root = "` + sourceDir + `"
format = "jsonl"

[fleet]
role = "collector"
metadata_path = "` + metadataPath + `"
ingest_token_file = "` + tokenPath + `"
spool_dir = "` + spoolDir + `"
spool_max_bytes = 1048576
spool_batch_size = 10
retry_min = "1ms"
retry_max = "5s"
heartbeat_interval = "30s"
node_name = "Smoke Collector"
`
	if err := os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, configPath)

	control, err := controlplane.Open(filepath.Join(dir, "server-control-plane.db"))
	if err != nil {
		t.Fatalf("Open server control-plane: %v", err)
	}
	defer control.Close()
	enroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken enroll: %v", err)
	}
	committer := &collectSmokeCommitter{}
	handlers := web.NewIngestHandlers(control, committer, 0, 0, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ingest/v1/enroll", handlers.Enroll)
	mux.HandleFunc("/api/ingest/v1/batches", handlers.Batch)
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if err := runRemoteEnroll(newEnrollCmd(), cfg, server.URL, enroll.Plaintext); err != nil {
		t.Fatalf("runRemoteEnroll: %v", err)
	}
	assertFileMode(t, tokenPath, 0600)

	if err := runCollect(newCollectCmd(), true, server.URL); err != nil {
		t.Fatalf("runCollect --once: %v", err)
	}
	if committer.calls != 1 {
		t.Fatalf("committer calls = %d, want 1", committer.calls)
	}
	if len(committer.rows.ActivityEvents) == 0 || len(committer.rows.RawRecords) == 0 {
		t.Fatalf("committed rows missing events/raw records: %#v", committer.rows)
	}
	joined := committer.joinedPayload()
	if strings.Contains(joined, secret) {
		t.Fatalf("committed rows leaked secret token: %s", joined)
	}
	if !strings.Contains(joined, "[REDACTED_TOKEN]") {
		t.Fatalf("committed rows did not contain redaction marker: %s", joined)
	}

	reopenedState, err := collector.OpenStateStore(collectorStatePath(cfg))
	if err != nil {
		t.Fatalf("OpenStateStore: %v", err)
	}
	if cp := reopenedState.Checkpoint("codex", sourceFile); cp == nil || cp.LastLineNo != 1 || cp.LastOffset == 0 {
		t.Fatalf("acked checkpoint = %#v, want first line advanced", cp)
	}
	reopenedSpool, err := collector.OpenSpool(spoolDir, cfg.Fleet.SpoolMaxBytes)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	stats, err := reopenedSpool.Stats()
	if err != nil {
		t.Fatalf("Spool Stats: %v", err)
	}
	if stats.PendingCount != 0 || stats.InflightCount != 0 || stats.CorruptCount != 0 {
		t.Fatalf("spool stats after ack = %#v, want empty active spool", stats)
	}

	if _, err := control.BeginReset(context.Background()); err != nil {
		t.Fatalf("BeginReset server: %v", err)
	}
	if _, err := control.CompleteReset(context.Background()); err != nil {
		t.Fatalf("CompleteReset server: %v", err)
	}
	secondEnroll, err := control.CreateToken(context.Background(), controlplane.CreateTokenRequest{Type: controlplane.TokenTypeEnroll})
	if err != nil {
		t.Fatalf("CreateToken second enroll: %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte(line+strings.Replace(line, "event-1", "event-2", 1)), 0600); err != nil {
		t.Fatalf("append second source event: %v", err)
	}
	if err := runRemoteEnroll(newEnrollCmd(), cfg, server.URL, secondEnroll.Plaintext); err != nil {
		t.Fatalf("second runRemoteEnroll: %v", err)
	}
	if err := runCollect(newCollectCmd(), true, server.URL); err != nil {
		t.Fatalf("second runCollect --once after reset: %v", err)
	}
	if committer.calls != 2 {
		t.Fatalf("committer calls after reset replay = %d, want 2", committer.calls)
	}
	if committer.meta.ControlPlaneEpoch != "2" {
		t.Fatalf("replayed batch epoch = %q, want 2", committer.meta.ControlPlaneEpoch)
	}
	if len(committer.rows.ActivityEvents) != 2 {
		t.Fatalf("replayed activity events = %d, want full file replay of 2", len(committer.rows.ActivityEvents))
	}
}

func TestRunCollectRejectsControlPlaneRoleBeforeMetadata(t *testing.T) {
	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "control-plane.db")
	configPath := filepath.Join(dir, "beacon.toml")
	body := `
[fleet]
role = "control-plane"
metadata_path = "` + metadataPath + `"
control_plane_url = "http://127.0.0.1:1"
ingest_token_file = "` + filepath.Join(dir, "ingest-token") + `"
spool_dir = "` + filepath.Join(dir, "spool") + `"
`
	if err := os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	withConfigFile(t, configPath)

	err := runCollect(newCollectCmd(), true, "")
	if err == nil {
		t.Fatal("runCollect returned nil error")
	}
	if !strings.Contains(err.Error(), `beacon collect requires fleet.role "collector"`) {
		t.Fatalf("runCollect error = %q, want role rejection", err.Error())
	}
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata file stat error = %v, want not created", statErr)
	}
}

type collectSmokeCommitter struct {
	calls int
	meta  store.IngestBatchMeta
	rows  store.RowBatch
}

func (c *collectSmokeCommitter) CommitIngestBatch(_ context.Context, meta store.IngestBatchMeta, rows store.RowBatch) (store.IngestBatchAck, error) {
	c.calls++
	c.meta = meta
	c.rows = rows
	return store.IngestBatchAck{
		BatchID:           meta.BatchID,
		PayloadDigest:     meta.PayloadDigest,
		EventsWritten:     len(rows.ActivityEvents),
		RawRecordsWritten: len(rows.RawRecords),
		NextSequence:      meta.Sequence + 1,
		ControlPlaneEpoch: meta.ControlPlaneEpoch,
	}, nil
}

func (c *collectSmokeCommitter) InsertCaptureHeartbeats(context.Context, []models.CaptureHeartbeat) error {
	return nil
}

func (c *collectSmokeCommitter) joinedPayload() string {
	var b strings.Builder
	for _, event := range c.rows.ActivityEvents {
		b.WriteString(event.TextContent)
		b.WriteString(event.PayloadJSON)
	}
	for _, raw := range c.rows.RawRecords {
		b.WriteString(raw.PayloadJSON)
	}
	return b.String()
}
