package usage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

func setupLiveUsageClickHouse(t *testing.T) *store.Store {
	t.Helper()
	addr := os.Getenv("BEACON_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("set BEACON_TEST_CLICKHOUSE to run ClickHouse usage integration tests")
	}

	opts := store.DefaultOptions()
	opts.Addrs = []string{addr}
	opts.Database = "beacon_test_usage"
	resetter, err := store.OpenForReset(context.Background(), opts)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	if err := store.Reset(context.Background(), resetter.DB, resetter.Database()); err != nil {
		resetter.Close()
		t.Fatalf("reset: %v", err)
	}
	resetter.Close()

	ch, err := store.Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("open clickhouse store: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

func TestClickHouseUsageSummarizeDeduplicatesEventsAndFiltersSessionWorkingDir(t *testing.T) {
	ch := setupLiveUsageClickHouse(t)
	ctx := context.Background()
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	oldVersion := usageEvent("evt-versioned", "old-session", start, 100, 10)
	if err := ch.Flush(ctx, store.RowBatch{ActivityEvents: []models.Event{oldVersion}}); err != nil {
		t.Fatalf("flush old version: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	newVersion := usageEvent("evt-versioned", "new-session", start.Add(time.Minute), 7, 3)
	metadata := usageEvent("evt-cwd", "new-session", start.Add(2*time.Minute), 0, 0)
	metadata.EventKind = "session_meta"
	metadata.ActorRole = "system"
	metadata.CWD = "/work/project"
	tokenWithoutCWD := usageEvent("evt-token-without-cwd", "new-session", start.Add(3*time.Minute), 5, 5)
	if err := ch.Flush(ctx, store.RowBatch{ActivityEvents: []models.Event{newVersion, metadata, tokenWithoutCWD}}); err != nil {
		t.Fatalf("flush new version and cwd events: %v", err)
	}

	result, err := Summarize(ctx, ch.DB, Request{
		Since:      start.Add(-time.Minute).Format(time.RFC3339),
		Until:      start.Add(time.Hour).Format(time.RFC3339),
		SourceName: "codex",
		WorkingDir: "/work/project",
		GroupBy:    []string{"working_dir", "session_id"},
		Limit:      10,
	}, start.Add(time.Hour))
	if err != nil {
		t.Fatalf("summarize usage: %v", err)
	}

	if result.Summary.SessionCount != 1 || result.Summary.EventCount != 3 {
		t.Fatalf("summary counts = sessions %d events %d, want 1 session and 3 latest new-session events", result.Summary.SessionCount, result.Summary.EventCount)
	}
	if result.Summary.TotalTokens != 20 || result.Summary.InputTokens != 12 || result.Summary.OutputTokens != 8 {
		t.Fatalf("summary tokens = total %d input %d output %d, want 20/12/8", result.Summary.TotalTokens, result.Summary.InputTokens, result.Summary.OutputTokens)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(result.Groups))
	}
	if result.Groups[0].Keys["working_dir"] != "/work/project" || result.Groups[0].Keys["session_id"] != "new-session" {
		t.Fatalf("group keys = %#v", result.Groups[0].Keys)
	}
}

func usageEvent(uid, sessionID string, ts time.Time, input, output int64) models.Event {
	return models.Event{
		EventUID:     uid,
		SessionID:    sessionID,
		SourceName:   "codex",
		Provider:     "openai",
		Format:       models.FormatJSONL,
		EventKind:    "message",
		ActorRole:    "assistant",
		Timestamp:    ts,
		TextContent:  uid,
		TextPreview:  uid,
		Model:        "gpt-5.4",
		InputTokens:  input,
		OutputTokens: output,
		SourceFile:   "usage.jsonl",
		SourceLineNo: 1,
	}
}
