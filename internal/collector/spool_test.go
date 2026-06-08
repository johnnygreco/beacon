package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/capture"
	"github.com/johnnygreco/beacon/internal/ingest"
	"github.com/johnnygreco/beacon/internal/models"
)

func TestSpoolWritePendingOwnerOnlyAndCorruptQuarantine(t *testing.T) {
	spool, err := OpenSpool(filepath.Join(t.TempDir(), "spool"), 1<<20)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	req := testBatchRequest(t, 1, "batch-ok")
	written, err := spool.WritePending(context.Background(), req)
	if err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	assertMode(t, spool.Root(), 0700)
	assertMode(t, filepath.Join(spool.Root(), spoolPending), 0700)
	assertMode(t, written.Path, 0600)

	pending, err := spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Request.BatchID != req.BatchID {
		t.Fatalf("pending = %#v, want batch %q", pending, req.BatchID)
	}

	if err := os.WriteFile(written.Path, []byte(`{"bad":true}`), 0600); err != nil {
		t.Fatalf("corrupt spool file: %v", err)
	}
	pending, err = spool.Pending()
	if err != nil {
		t.Fatalf("Pending corrupt: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after corruption = %d, want 0", len(pending))
	}
	stats, err := spool.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.CorruptCount != 1 {
		t.Fatalf("corrupt count = %d, want 1", stats.CorruptCount)
	}
}

func TestSpoolFullRejectsWithoutWriting(t *testing.T) {
	spool, err := OpenSpool(filepath.Join(t.TempDir(), "spool"), 8)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	if _, err := spool.WritePending(context.Background(), testBatchRequest(t, 1, "batch-full")); !errors.Is(err, ErrSpoolFull) {
		t.Fatalf("WritePending error = %v, want ErrSpoolFull", err)
	}
	pending, err := spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %d, want 0", len(pending))
	}
}

func TestSpoolPartialWriteArtifactsDoNotBecomePendingBatches(t *testing.T) {
	spool, err := OpenSpool(filepath.Join(t.TempDir(), "spool"), 1<<20)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	written, err := spool.WritePending(context.Background(), testBatchRequest(t, 2, "batch-ok"))
	if err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	tmpPath := filepath.Join(spool.Root(), spoolTmp, "00000000000000000001-partial.json.tmp")
	if err := os.WriteFile(tmpPath, []byte(`{"version":1`), 0600); err != nil {
		t.Fatalf("write partial tmp spool file: %v", err)
	}
	truncatedPending := filepath.Join(spool.Root(), spoolPending, "00000000000000000001-truncated.json")
	if err := os.WriteFile(truncatedPending, []byte(`{"version":1`), 0600); err != nil {
		t.Fatalf("write truncated pending spool file: %v", err)
	}

	pending, err := spool.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Path != written.Path {
		t.Fatalf("pending after partial artifacts = %#v, want only committed batch %s", pending, written.Path)
	}
	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf("partial tmp artifact should be ignored and left for manual cleanup: %v", err)
	}
	stats, err := spool.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.CorruptCount != 1 || stats.PendingCount != 1 {
		t.Fatalf("stats after partial artifacts = %#v, want one corrupt pending and one valid pending", stats)
	}
}

func TestSpoolAckDeletesCommittedBatch(t *testing.T) {
	spool, err := OpenSpool(filepath.Join(t.TempDir(), "spool"), 1<<20)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	written, err := spool.WritePending(context.Background(), testBatchRequest(t, 1, "batch-ack"))
	if err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	inflight, err := spool.MarkInflight(*written)
	if err != nil {
		t.Fatalf("MarkInflight: %v", err)
	}
	if err := spool.Ack(inflight); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if _, err := os.Stat(inflight.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("acked file stat error = %v, want not exist", err)
	}
	stats, err := spool.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.PendingCount != 0 || stats.InflightCount != 0 || stats.ActiveBytes != 0 {
		t.Fatalf("stats after ack = %#v, want empty active spool", stats)
	}
}

func testBatchRequest(t *testing.T, sequence uint64, batchID string) ingest.BatchRequest {
	t.Helper()
	req := ingest.BatchRequest{
		Schema:            ingest.SchemaV1,
		BatchID:           batchID,
		CollectorID:       "collector-test",
		NodeID:            "node-test",
		ControlPlaneEpoch: "1",
		CreatedAt:         time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		Sequence:          sequence,
		RedactionVersion:  RedactionVersion,
		SourceIDs:         []string{"source-test"},
		Events: []capture.NormalizedEvent{{
			SessionID:    "session-test",
			SourceName:   "codex",
			Runtime:      models.RuntimeCodex,
			Provider:     models.ProviderOpenAI,
			Format:       models.FormatJSONL,
			EventKind:    models.EventKindMessage,
			ActorRole:    models.ActorRoleAssistant,
			Timestamp:    time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
			TextContent:  "hello",
			RawPayload:   `{"message":"hello"}`,
			SourceFile:   "session.jsonl",
			SourceLineNo: 1,
		}},
	}
	digest, err := ingest.ComputeBatchDigest(req)
	if err != nil {
		t.Fatalf("ComputeBatchDigest: %v", err)
	}
	req.PayloadDigest = digest
	return req
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %o, want %o", path, got, want)
	}
}
