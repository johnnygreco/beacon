package collector

import (
	"compress/gzip"
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

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
)

func TestServiceRetryKeepsPendingUntilAckAndThenAdvancesCheckpoint(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		req := decodeGzipBatch(t, r)
		_ = json.NewEncoder(w).Encode(ingest.BatchAck{
			Status:            ingest.StatusCommitted,
			BatchID:           req.BatchID,
			PayloadDigest:     req.PayloadDigest,
			EventsWritten:     len(req.Events),
			RawRecordsWritten: len(req.Events),
			NextSequence:      req.Sequence + 1,
			ControlPlaneEpoch: req.ControlPlaneEpoch,
		})
	}))
	defer server.Close()

	service, state, _ := newTestService(t, server.URL, 1<<20)
	req := testBatchRequest(t, 1, "batch-retry")
	req.Checkpoints = []models.Checkpoint{{
		NodeID:      "node-test",
		CollectorID: "collector-test",
		SourceID:    "source-test",
		SourceName:  "codex",
		SourceFile:  "session.jsonl",
		LastOffset:  10,
		LastLineNo:  1,
	}}
	if _, err := service.cfg.Spool.WritePending(context.Background(), req); err != nil {
		t.Fatalf("WritePending: %v", err)
	}

	if err := service.SendPending(context.Background()); err == nil {
		t.Fatal("first SendPending returned nil, want retryable outage")
	}
	stats, err := service.cfg.Spool.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.PendingCount != 1 {
		t.Fatalf("pending after outage = %d, want 1", stats.PendingCount)
	}
	if cp := state.Checkpoint("codex", "session.jsonl"); cp != nil {
		t.Fatalf("checkpoint advanced before ack: %#v", cp)
	}

	if err := service.SendPending(context.Background()); err != nil {
		t.Fatalf("second SendPending: %v", err)
	}
	stats, err = service.cfg.Spool.Stats()
	if err != nil {
		t.Fatalf("Stats after ack: %v", err)
	}
	if stats.PendingCount != 0 || stats.InflightCount != 0 {
		t.Fatalf("active spool after ack = pending %d inflight %d, want 0/0", stats.PendingCount, stats.InflightCount)
	}
	if cp := state.Checkpoint("codex", "session.jsonl"); cp == nil || cp.LastOffset != 10 {
		t.Fatalf("checkpoint after ack = %#v, want offset 10", cp)
	}
}

func TestServiceSpoolFullPausesBeforeCheckpointAndSequenceAdvance(t *testing.T) {
	service, state, file := newTestService(t, "http://127.0.0.1:1", 16)
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if state.Next() != 1 {
		t.Fatalf("next sequence = %d, want 1 when spool write fails", state.Next())
	}
	if cp := state.Checkpoint("codex", file); cp != nil {
		t.Fatalf("checkpoint advanced while spool full: %#v", cp)
	}
	status := service.Status()
	if !status.BlockedSpoolFull {
		t.Fatalf("BlockedSpoolFull = false, want true")
	}
}

func TestServiceCheckpointAdvancesOnlyAfterAck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeGzipBatch(t, r)
		_ = json.NewEncoder(w).Encode(ingest.BatchAck{
			Status:            ingest.StatusCommitted,
			BatchID:           req.BatchID,
			PayloadDigest:     req.PayloadDigest,
			EventsWritten:     len(req.Events),
			RawRecordsWritten: len(req.Events),
			NextSequence:      req.Sequence + 1,
			ControlPlaneEpoch: req.ControlPlaneEpoch,
		})
	}))
	defer server.Close()
	service, state, file := newTestService(t, server.URL, 1<<20)

	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	if cp := state.Checkpoint("codex", file); cp != nil {
		t.Fatalf("checkpoint advanced before ack: %#v", cp)
	}
	stats, err := service.cfg.Spool.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.PendingCount != 1 {
		t.Fatalf("pending after scan = %d, want 1", stats.PendingCount)
	}

	if err := service.SendPending(context.Background()); err != nil {
		t.Fatalf("SendPending: %v", err)
	}
	cp := state.Checkpoint("codex", file)
	if cp == nil || cp.LastLineNo != 1 || cp.LastOffset == 0 {
		t.Fatalf("checkpoint after ack = %#v, want first line acknowledged", cp)
	}
}

func newTestService(t *testing.T, serverURL string, spoolMax int64) (*Service, *StateStore, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(file, []byte(`{"msg":"api_key=secret"}`+"\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	spool, err := OpenSpool(filepath.Join(dir, "spool"), spoolMax)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	state, err := OpenStateStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("OpenStateStore: %v", err)
	}
	client, err := NewClient(serverURL, "token", time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	source := capture.WatchSource{
		Name:     "codex",
		Runtime:  models.RuntimeCodex,
		Provider: models.ProviderOpenAI,
		Format:   models.FormatJSONL,
		Globs:    []string{file},
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]capture.NormalizedEvent, error) {
			return []capture.NormalizedEvent{{
				SessionID:    "session-test",
				SourceName:   "codex",
				Runtime:      models.RuntimeCodex,
				Provider:     models.ProviderOpenAI,
				Format:       models.FormatJSONL,
				EventKind:    models.EventKindMessage,
				ActorRole:    models.ActorRoleAssistant,
				Timestamp:    time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
				TextContent:  string(line),
				RawPayload:   string(line),
				SourceFile:   file,
				SourceLineNo: lineNo,
				SourceOffset: offset,
			}}, nil
		},
	}
	service, err := NewService(ServiceConfig{
		Sources: []capture.WatchSource{source},
		Identity: capture.FleetIdentity{
			NodeID:            "node-test",
			CollectorID:       "collector-test",
			ControlPlaneEpoch: "1",
			Sources: map[string]capture.FleetSourceIdentity{
				"codex": {SourceID: "source-test"},
			},
		},
		Spool:             spool,
		State:             state,
		Client:            client,
		ScanInterval:      time.Hour,
		RetryMin:          time.Millisecond,
		RetryMax:          time.Millisecond,
		HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service, state, file
}

func decodeGzipBatch(t *testing.T, r *http.Request) ingest.BatchRequest {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization = %q, want bearer token", got)
	}
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		t.Fatalf("gzip body: %v", err)
	}
	defer gz.Close()
	var req ingest.BatchRequest
	if err := json.NewDecoder(gz).Decode(&req); err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	if len(req.Events) == 0 || strings.Contains(req.Events[0].RawPayload, "secret") {
		t.Fatalf("batch was not redacted before send/spool: %#v", req.Events)
	}
	return req
}
