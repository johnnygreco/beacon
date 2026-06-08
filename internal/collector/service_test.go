package collector

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
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

func TestServiceSendPendingRequeuesInflightBeforeLaterPending(t *testing.T) {
	var sequences []uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeGzipBatch(t, r)
		sequences = append(sequences, req.Sequence)
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
	service, _, _ := newTestService(t, server.URL, 1<<20)
	first, err := service.cfg.Spool.WritePending(context.Background(), testBatchRequest(t, 1, "batch-inflight"))
	if err != nil {
		t.Fatalf("WritePending first: %v", err)
	}
	if _, err := service.cfg.Spool.MarkInflight(*first); err != nil {
		t.Fatalf("MarkInflight: %v", err)
	}
	if _, err := service.cfg.Spool.WritePending(context.Background(), testBatchRequest(t, 2, "batch-pending")); err != nil {
		t.Fatalf("WritePending second: %v", err)
	}

	if err := service.SendPending(context.Background()); err != nil {
		t.Fatalf("SendPending: %v", err)
	}
	if len(sequences) != 2 || sequences[0] != 1 || sequences[1] != 2 {
		t.Fatalf("sent sequences = %v, want [1 2]", sequences)
	}
}

func TestServiceSendPendingPausesWhenCorruptEarlierBatchIsQuarantined(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		attempts.Add(1)
	}))
	defer server.Close()
	service, _, _ := newTestService(t, server.URL, 1<<20)
	corruptPath := filepath.Join(service.cfg.Spool.Root(), "pending", "00000000000000000001-corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("{"), 0600); err != nil {
		t.Fatalf("write corrupt pending spool file: %v", err)
	}
	if _, err := service.cfg.Spool.WritePending(context.Background(), testBatchRequest(t, 2, "batch-pending")); err != nil {
		t.Fatalf("WritePending second: %v", err)
	}

	if err := service.SendPending(context.Background()); err != nil {
		t.Fatalf("SendPending: %v", err)
	}
	if got := attempts.Load(); got != 0 {
		t.Fatalf("send attempts = %d, want paused while quarantine is non-empty", got)
	}
	stats, err := service.cfg.Spool.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.CorruptCount != 1 || stats.PendingCount != 0 {
		t.Fatalf("spool stats = %#v, want quarantined corrupt batch and discarded non-contiguous later batch", stats)
	}
}

func TestClientDoesNotForwardBearerTokenAcrossRedirect(t *testing.T) {
	var redirected atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer attacker.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret-token", time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.SendBatch(context.Background(), testBatchRequest(t, 1, "batch-redirect"))
	var sendErr *SendError
	if !errors.As(err, &sendErr) || sendErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("SendBatch error = %v, want 307 SendError", err)
	}
	if got := redirected.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests, want none", got)
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

func TestValidateBatchBodySizeRejectsGzipTransportLimit(t *testing.T) {
	body := []byte("{}")
	compressedLen, err := gzipEncodedLen(body)
	if err != nil {
		t.Fatalf("gzip body: %v", err)
	}
	if compressedLen <= len(body) {
		t.Fatalf("test body compressed to %d <= raw %d; need gzip overhead to exceed raw body", compressedLen, len(body))
	}
	if err := validateEncodedBatchBodySize(body, len(body)); err == nil || !strings.Contains(err.Error(), "gzip body exceeds ingest limit") {
		t.Fatalf("validateEncodedBatchBodySize error = %v, want gzip transport limit", err)
	}
}

func TestServiceOversizeBatchSplitsBeforeSpool(t *testing.T) {
	line := `{"msg":"` + strings.Repeat("x", 512) + `"}`
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = line
	}
	service, state, file := newTestServiceWithLines(t, "http://127.0.0.1:1", 1<<20, len(lines), lines)
	service.cfg.MaxBatchBodyBytes = 9000
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) <= 1 {
		t.Fatalf("pending after split = %d, want multiple batches", len(pending))
	}
	if got := state.Next(); got != uint64(len(pending)+1) {
		t.Fatalf("next sequence = %d, want %d", got, len(pending)+1)
	}
	if cp := state.SpooledCheckpoint("codex", file); cp == nil || cp.LastLineNo != len(lines) {
		t.Fatalf("spooled checkpoint after split = %#v, want line %d", cp, len(lines))
	}
	for i, batch := range pending {
		if batch.Request.Sequence != uint64(i+1) {
			t.Fatalf("pending[%d] sequence = %d, want %d", i, batch.Request.Sequence, i+1)
		}
	}
}

func TestServiceSingleOversizeRecordEmitsCaptureErrorAndAdvances(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.db")
	if err := os.WriteFile(file, []byte("sqlite"), 0600); err != nil {
		t.Fatalf("write whole-file source: %v", err)
	}
	spool, err := OpenSpool(filepath.Join(dir, "spool"), int64(ingest.MaxBodyBytes*2))
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	state, err := OpenStateStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("OpenStateStore: %v", err)
	}
	client, err := NewClient("http://127.0.0.1:1", "token", time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	hugePayload := strings.Repeat("x", 4096)
	source := capture.WatchSource{
		Name:     "hermes",
		Runtime:  models.RuntimeHermesAgent,
		Provider: models.ProviderMulti,
		Format:   models.FormatSQLite,
		Globs:    []string{file},
		FileParser: func(file string) ([]capture.NormalizedEvent, error) {
			return []capture.NormalizedEvent{
				{SessionID: "session-test", SourceName: "hermes", Runtime: models.RuntimeHermesAgent, Provider: models.ProviderMulti, Format: models.FormatSQLite, EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC), TextContent: hugePayload, RawPayload: hugePayload, SourceFile: file},
				{SessionID: "session-test", SourceName: "hermes", Runtime: models.RuntimeHermesAgent, Provider: models.ProviderMulti, Format: models.FormatSQLite, EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 7, 12, 0, 1, 0, time.UTC), TextContent: "small", RawPayload: `{"msg":"small"}`, SourceFile: file},
			}, nil
		},
	}
	service, err := NewService(ServiceConfig{
		Sources: []capture.WatchSource{source},
		Identity: capture.FleetIdentity{
			NodeID:            "node-test",
			CollectorID:       "collector-test",
			ControlPlaneEpoch: "1",
			Sources: map[string]capture.FleetSourceIdentity{
				"hermes": {SourceID: "source-hermes"},
			},
		},
		Spool:             spool,
		State:             state,
		Client:            client,
		BatchSize:         1,
		MaxBatchBodyBytes: 2048,
		ScanInterval:      time.Hour,
		RetryMin:          time.Millisecond,
		RetryMax:          time.Millisecond,
		HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending batches = %d, want oversize error and following small event", len(pending))
	}
	if len(pending[0].Request.Events) != 0 || len(pending[0].Request.CaptureErrors) != 1 {
		t.Fatalf("first batch = events %#v errors %#v, want one capture error", pending[0].Request.Events, pending[0].Request.CaptureErrors)
	}
	if pending[0].Request.CaptureErrors[0].ErrorClass != "oversize_record" {
		t.Fatalf("capture error class = %q, want oversize_record", pending[0].Request.CaptureErrors[0].ErrorClass)
	}
	if len(pending[1].Request.Events) != 1 || pending[1].Request.Events[0].TextContent != "small" {
		t.Fatalf("second batch events = %#v, want small follow-up event", pending[1].Request.Events)
	}
	if got := state.Next(); got != 3 {
		t.Fatalf("next sequence = %d, want 3", got)
	}
	if cp := state.SpooledCheckpoint("hermes", file); cp == nil || cp.LastLineNo != 2 {
		t.Fatalf("spooled checkpoint = %#v, want line 2", cp)
	}
}

func TestServiceSpoolFullDuringMultiChunkReadLeavesNoPartialBatch(t *testing.T) {
	service, state, _ := newTestServiceWithLines(t, "http://127.0.0.1:1", 1<<20, 1, []string{
		`{"msg":"api_key=first"}`,
		`{"msg":"api_key=second"}`,
	})
	source := service.cfg.Sources[0]
	result, err := capture.ReadSourceFileWindow(context.Background(), source, source.Globs[0], nil, nil, 1)
	if err != nil {
		t.Fatalf("ReadSourceFile: %v", err)
	}
	if len(result.Events) != 1 || !result.HasMore {
		t.Fatalf("window = events %d hasMore %v, want one event with more", len(result.Events), result.HasMore)
	}
	firstReq, err := service.buildBatchRequest(1, "source-test", RedactEvents(result.Events), nil, enrichCollectorCheckpoints(result.Checkpoint, service.cfg.Identity, source.Name, "source-test"))
	if err != nil {
		t.Fatalf("build first request: %v", err)
	}
	firstPayload, err := encodeSpoolEnvelope(firstReq)
	if err != nil {
		t.Fatalf("encode first request: %v", err)
	}
	service.cfg.Spool.maxBytes = int64(len(firstPayload)) + 1

	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce: %v", err)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending after partial spool capacity = %d, want first window", len(pending))
	}
	if state.Next() != 2 {
		t.Fatalf("next sequence after first window = %d, want 2", state.Next())
	}
	if cp := state.SpooledCheckpoint("codex", source.Globs[0]); cp == nil || cp.LastLineNo != 1 {
		t.Fatalf("spooled checkpoint = %#v, want first line", cp)
	}
}

func TestServiceTerminalSendErrorKeepsBatchPendingAndBlocksScan(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	service, _, _ := newTestService(t, server.URL, 1<<20)
	req := testBatchRequest(t, 1, "batch-terminal")
	if _, err := service.cfg.Spool.WritePending(context.Background(), req); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := service.SendPending(context.Background()); err == nil {
		t.Fatal("SendPending returned nil, want terminal send error")
	}
	stats, err := service.cfg.Spool.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.PendingCount != 1 || stats.InflightCount != 0 || stats.CorruptCount != 0 {
		t.Fatalf("spool stats after terminal error = %#v, want one pending and no quarantine", stats)
	}
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce with pending terminal batch: %v", err)
	}
	if err := service.SendPending(context.Background()); !errors.Is(err, ErrTerminalBlocked) {
		t.Fatalf("second SendPending error = %v, want ErrTerminalBlocked", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("terminal batch send attempts = %d, want 1", got)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Request.BatchID != req.BatchID {
		t.Fatalf("pending after blocked scan = %#v, want original batch", pending)
	}
}

func TestServiceRetryableOutageContinuesSpoolingFromSpooledCheckpoint(t *testing.T) {
	service, state, file := newTestService(t, "http://127.0.0.1:1", 1<<20)
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("initial ScanOnce: %v", err)
	}
	if state.Checkpoint("codex", file) != nil {
		t.Fatalf("acked checkpoint advanced before server ack")
	}
	firstSpooled := state.SpooledCheckpoint("codex", file)
	if firstSpooled == nil || firstSpooled.LastLineNo != 1 {
		t.Fatalf("spooled checkpoint after first scan = %#v, want line 1", firstSpooled)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open source append: %v", err)
	}
	if _, err := f.WriteString(`{"msg":"api_key=second"}` + "\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append source: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close source append: %v", err)
	}

	if err := service.SendPending(context.Background()); err == nil {
		t.Fatal("SendPending returned nil, want retryable outage")
	}
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce during outage: %v", err)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending batches = %d, want 2", len(pending))
	}
	if pending[0].Request.Sequence != 1 || pending[1].Request.Sequence != 2 {
		t.Fatalf("pending sequences = %d/%d, want 1/2", pending[0].Request.Sequence, pending[1].Request.Sequence)
	}
	if len(pending[1].Request.Events) != 1 || pending[1].Request.Events[0].SourceLineNo != 2 {
		t.Fatalf("second batch events = %#v, want line 2 only", pending[1].Request.Events)
	}
	if state.Checkpoint("codex", file) != nil {
		t.Fatalf("acked checkpoint advanced before ack during outage")
	}
	if got := state.Next(); got != 3 {
		t.Fatalf("next sequence = %d, want 3", got)
	}
}

func TestServiceResetPendingPausesScanAndResumesAfterAck(t *testing.T) {
	var resetPending atomic.Bool
	resetPending.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resetPending.Load() {
			http.Error(w, `{"error":"control-plane reset pending"}`, http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/api/ingest/v1/heartbeats" {
			_ = json.NewEncoder(w).Encode(ingest.HeartbeatResponse{
				Schema:            ingest.SchemaV1,
				Status:            "ok",
				ControlPlaneEpoch: "1",
			})
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
	service, state, file := newTestService(t, server.URL, 1<<20)

	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("initial ScanOnce: %v", err)
	}
	appendSourceLine(t, file, `{"msg":"api_key=second"}`)
	if err := service.SendPending(context.Background()); !errors.Is(err, ErrResetPending) {
		t.Fatalf("SendPending during reset = %v, want ErrResetPending", err)
	}
	status := service.Status()
	if !status.BlockedResetPending || status.BlockedEpochMismatch {
		t.Fatalf("status during reset = %#v, want reset-pending block only", status)
	}
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce during reset: %v", err)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending during reset: %v", err)
	}
	if len(pending) != 1 || pending[0].Request.Sequence != 1 {
		t.Fatalf("pending during reset = %#v, want original sequence 1 only", pending)
	}

	resetPending.Store(false)
	if err := service.SendHeartbeat(context.Background()); err != nil {
		t.Fatalf("SendHeartbeat after reset cleared: %v", err)
	}
	if err := service.SendPending(context.Background()); err != nil {
		t.Fatalf("SendPending after reset cleared: %v", err)
	}
	if status := service.Status(); status.BlockedResetPending {
		t.Fatalf("status after ack = %#v, want reset-pending cleared", status)
	}
	if cp := state.Checkpoint("codex", file); cp == nil || cp.LastLineNo != 1 {
		t.Fatalf("acked checkpoint after reset cleared = %#v, want line 1", cp)
	}
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce after reset cleared: %v", err)
	}
	pending, err = service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending after reset cleared: %v", err)
	}
	if len(pending) != 1 || len(pending[0].Request.Events) != 1 || pending[0].Request.Events[0].SourceLineNo != 2 {
		t.Fatalf("pending after reset cleared = %#v, want replay from line 2", pending)
	}
}

func TestServiceEpochMismatchBlocksScanUntilReenrollment(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, `{"error":"control_plane_epoch mismatch"}`, http.StatusConflict)
	}))
	defer server.Close()
	service, _, file := newTestService(t, server.URL, 1<<20)

	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("initial ScanOnce: %v", err)
	}
	appendSourceLine(t, file, `{"msg":"api_key=second"}`)
	if err := service.SendPending(context.Background()); !errors.Is(err, ErrEpochMismatch) {
		t.Fatalf("SendPending epoch mismatch = %v, want ErrEpochMismatch", err)
	}
	status := service.Status()
	if !status.BlockedEpochMismatch || status.BlockedResetPending {
		t.Fatalf("status after epoch mismatch = %#v, want epoch block only", status)
	}
	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce while epoch blocked: %v", err)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending while epoch blocked: %v", err)
	}
	if len(pending) != 1 || pending[0].Request.Sequence != 1 {
		t.Fatalf("pending while epoch blocked = %#v, want original stale batch only", pending)
	}
	if err := service.SendPending(context.Background()); !errors.Is(err, ErrEpochMismatch) {
		t.Fatalf("second SendPending epoch mismatch = %v, want ErrEpochMismatch", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("epoch mismatch send attempts = %d, want no retry after local block", got)
	}
}

func TestServiceNewEpochClearsStaleSpoolAndReplaysFileBackedSource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(file, []byte(strings.Join([]string{
		`{"msg":"api_key=first"}`,
		`{"msg":"api_key=second"}`,
	}, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	spool, err := OpenSpool(filepath.Join(dir, "spool"), 1<<20)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	state, err := OpenStateStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("OpenStateStore: %v", err)
	}
	if _, err := state.EnsureEpoch("1"); err != nil {
		t.Fatalf("EnsureEpoch old: %v", err)
	}
	staleCheckpoint := models.Checkpoint{
		NodeID:      "node-test",
		CollectorID: "collector-test",
		SourceID:    "source-test",
		SourceName:  "codex",
		SourceFile:  file,
		LastOffset:  24,
		LastLineNo:  1,
	}
	if err := state.MarkSpooled(2, []models.Checkpoint{staleCheckpoint}); err != nil {
		t.Fatalf("MarkSpooled old: %v", err)
	}
	oldReq := testBatchRequest(t, 1, "batch-old-epoch")
	oldReq.ControlPlaneEpoch = "1"
	oldReq.Checkpoints = []models.Checkpoint{staleCheckpoint}
	if _, err := spool.WritePending(context.Background(), oldReq); err != nil {
		t.Fatalf("WritePending old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(spool.Root(), spoolQuarantine, "00000000000000000001-old-corrupt.json"), []byte("{"), 0600); err != nil {
		t.Fatalf("write old quarantine fixture: %v", err)
	}
	client, err := NewClient("http://127.0.0.1:1", "token", time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	service, err := NewService(ServiceConfig{
		Sources: []capture.WatchSource{jsonlTestSource("codex", file)},
		Identity: capture.FleetIdentity{
			NodeID:            "node-test",
			CollectorID:       "collector-test",
			ControlPlaneEpoch: "2",
			Sources: map[string]capture.FleetSourceIdentity{
				"codex": {SourceID: "source-test"},
			},
		},
		Spool:             spool,
		State:             state,
		Client:            client,
		BatchSize:         500,
		ScanInterval:      time.Hour,
		RetryMin:          time.Millisecond,
		RetryMax:          time.Millisecond,
		HeartbeatInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	stats, err := service.cfg.Spool.Stats()
	if err != nil {
		t.Fatalf("Stats after NewService: %v", err)
	}
	if stats.PendingCount != 0 || stats.InflightCount != 0 || stats.CorruptCount != 0 {
		t.Fatalf("active spool after epoch reset = %#v, want empty", stats)
	}
	if state.Epoch() != "2" || state.Next() != 1 || state.AckedNext() != 1 {
		t.Fatalf("state after epoch reset = epoch %q next %d acked %d, want epoch 2 sequence 1/1", state.Epoch(), state.Next(), state.AckedNext())
	}
	if cp := state.SpooledCheckpoint("codex", file); cp != nil {
		t.Fatalf("stale spooled checkpoint survived epoch reset: %#v", cp)
	}

	if err := service.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce after epoch reset: %v", err)
	}
	pending, err := service.cfg.Spool.Pending()
	if err != nil {
		t.Fatalf("Pending after epoch reset: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending after epoch reset = %d, want one replay batch", len(pending))
	}
	req := pending[0].Request
	if req.ControlPlaneEpoch != "2" || req.Sequence != 1 {
		t.Fatalf("replay request epoch/sequence = %q/%d, want 2/1", req.ControlPlaneEpoch, req.Sequence)
	}
	if len(req.Events) != 2 || req.Events[0].SourceLineNo != 1 || req.Events[1].SourceLineNo != 2 {
		t.Fatalf("replay events = %#v, want both source lines from the beginning", req.Events)
	}
}

func TestServiceRunCancelsDuringRetryBackoff(t *testing.T) {
	service, _, _ := newTestService(t, "http://127.0.0.1:1", 1<<20)
	service.cfg.RetryMin = time.Hour
	service.cfg.RetryMax = time.Hour
	if _, err := service.cfg.Spool.WritePending(context.Background(), testBatchRequest(t, 1, "batch-cancel")); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after context cancellation")
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
	return newTestServiceWithLines(t, serverURL, spoolMax, 500, []string{`{"msg":"api_key=secret"}`})
}

func newTestServiceWithLines(t *testing.T, serverURL string, spoolMax int64, batchSize int, lines []string) (*Service, *StateStore, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
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
	source := jsonlTestSource("codex", file)
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
		BatchSize:         batchSize,
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

func appendSourceLine(t *testing.T, file, line string) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open source append: %v", err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append source: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close source append: %v", err)
	}
}

func jsonlTestSource(name, file string) capture.WatchSource {
	return capture.WatchSource{
		Name:     name,
		Runtime:  models.RuntimeCodex,
		Provider: models.ProviderOpenAI,
		Format:   models.FormatJSONL,
		Globs:    []string{file},
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]capture.NormalizedEvent, error) {
			return []capture.NormalizedEvent{{
				SessionID:    "session-test",
				SourceName:   name,
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
