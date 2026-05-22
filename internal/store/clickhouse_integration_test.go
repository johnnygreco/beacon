package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func setupLiveClickHouse(t *testing.T) *Store {
	t.Helper()
	addr := os.Getenv("BEACON_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("set BEACON_TEST_CLICKHOUSE to run ClickHouse store integration tests")
	}

	opts := DefaultOptions()
	opts.Addrs = []string{addr}
	opts.Database = "beacon_test_store"
	ch, err := Open(context.Background(), opts)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	if err := Reset(context.Background(), ch.DB, ch.Database()); err != nil {
		ch.Close()
		t.Fatalf("reset: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

func TestClickHouseFlushRefreshesDeduplicatedProjection(t *testing.T) {
	ch := setupLiveClickHouse(t)
	now := time.Now().UTC()
	sessionID := "live-session"
	fullPayload := strings.Repeat("SECRET_FULL_ONLY ", 200)

	batch := RowBatch{
		ActivityEvents: []models.Event{
			{
				EventUID:         "evt-message",
				SessionID:        sessionID,
				SourceName:       "test-source",
				Runtime:          "test-runtime",
				Provider:         "test-provider",
				Format:           "jsonl",
				EventKind:        "message",
				ActorRole:        "user",
				Timestamp:        now,
				TextContent:      "alpha beta beta",
				TextPreview:      "alpha beta beta",
				EventVersion:     1,
				SourceFile:       "live.jsonl",
				SourceLineNo:     1,
				SourceOffset:     0,
				SourceGeneration: 3,
			},
			{
				EventUID:         "evt-tool",
				SessionID:        sessionID,
				SourceName:       "test-source",
				Runtime:          "test-runtime",
				Provider:         "test-provider",
				Format:           "jsonl",
				EventKind:        "tool_call",
				ToolName:         "Bash",
				Model:            "gpt-4",
				Timestamp:        now.Add(time.Second),
				InputTokens:      3,
				OutputTokens:     4,
				DurationMs:       42,
				EventVersion:     1,
				SourceFile:       "live.jsonl",
				SourceLineNo:     2,
				SourceOffset:     10,
				SourceGeneration: 3,
			},
		},
		ToolPayloads: []models.ToolPayload{
			{
				EventUID:     "evt-tool",
				ToolName:     "Bash",
				ToolPhase:    "call",
				InputJSON:    fullPayload,
				InputPreview: `{"command":"echo preview"}`,
			},
		},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("replay flush: %v", err)
	}

	var eventCount, totalTokens, toolCalls uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(event_count, updated_at),
		        argMax(total_tokens, updated_at),
		        argMax(tool_call_count, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&eventCount, &totalTokens, &toolCalls); err != nil {
		t.Fatalf("projection query: %v", err)
	}
	if eventCount != 2 || totalTokens != 7 || toolCalls != 1 {
		t.Fatalf("projection = events %d tokens %d tools %d", eventCount, totalTokens, toolCalls)
	}

	var analyticsEvents, analyticsTokens, analyticsCalls, analyticsToolCalls, analyticsDuration uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT sum(event_count),
		        sum(total_tokens),
		        sum(call_count),
		        sum(tool_call_count),
		        sum(duration_ms_sum)
		 FROM (
			SELECT session_id, minute, provider, model, tool_name, event_kind,
			       argMax(event_count, updated_at) AS event_count,
			       argMax(total_tokens, updated_at) AS total_tokens,
			       argMax(call_count, updated_at) AS call_count,
			       argMax(tool_call_count, updated_at) AS tool_call_count,
			       argMax(duration_ms_sum, updated_at) AS duration_ms_sum
			FROM analytics_projection
			WHERE session_id = ?
			GROUP BY session_id, minute, provider, model, tool_name, event_kind
		 )`, sessionID).Scan(&analyticsEvents, &analyticsTokens, &analyticsCalls, &analyticsToolCalls, &analyticsDuration); err != nil {
		t.Fatalf("analytics projection query: %v", err)
	}
	if analyticsEvents != 2 || analyticsTokens != 7 || analyticsCalls != 1 || analyticsToolCalls != 1 || analyticsDuration != 42 {
		t.Fatalf("analytics projection = events %d tokens %d calls %d tools %d duration %d",
			analyticsEvents, analyticsTokens, analyticsCalls, analyticsToolCalls, analyticsDuration)
	}

	var runtime, format string
	var generation uint32
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(runtime, captured_at),
		        argMax(format, captured_at),
		        argMax(source_generation, captured_at)
		 FROM activity_events
		 WHERE event_uid = ?`, "evt-tool").Scan(&runtime, &format, &generation); err != nil {
		t.Fatalf("source metadata query: %v", err)
	}
	if runtime != "test-runtime" || format != "jsonl" || generation != 3 {
		t.Fatalf("source metadata = runtime %q format %q generation %d", runtime, format, generation)
	}

	var secretDocs uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT countIf(position(searchable_text, 'SECRET_FULL_ONLY') > 0)
		 FROM search_documents
		 WHERE session_id = ?`, sessionID).Scan(&secretDocs); err != nil {
		t.Fatalf("search document query: %v", err)
	}
	if secretDocs != 0 {
		t.Fatalf("full payload leaked into search_documents: %d docs", secretDocs)
	}
}

func TestClickHouseProjectionIgnoresZeroTimestampEventsForDuration(t *testing.T) {
	ch := setupLiveClickHouse(t)
	sessionID := "missing-timestamp-session"
	start := time.Date(2026, 5, 22, 12, 0, 0, 123000000, time.UTC)
	end := start.Add(2 * time.Minute)

	batch := RowBatch{
		ActivityEvents: []models.Event{
			{
				EventUID:     "evt-file-history",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "event_msg",
				PayloadType:  "file-history-snapshot",
				ActorRole:    "system",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 1,
			},
			{
				EventUID:     "evt-start",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "message",
				ActorRole:    "user",
				Timestamp:    start,
				TextPreview:  "start",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 2,
			},
			{
				EventUID:     "evt-end",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "message",
				ActorRole:    "assistant",
				Timestamp:    end,
				TextPreview:  "end",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 3,
			},
			{
				EventUID:     "evt-last-prompt",
				SessionID:    sessionID,
				SourceName:   "claude",
				Runtime:      "claude-code",
				Provider:     "anthropic",
				Format:       "jsonl",
				EventKind:    "session_end",
				PayloadType:  "last-prompt",
				ActorRole:    "system",
				SourceFile:   "claude.jsonl",
				SourceLineNo: 4,
			},
		},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var projectedStart, projectedEnd time.Time
	var hasSessionEnd, eventCount uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(started_at, updated_at),
		        argMax(ended_at, updated_at),
		        argMax(has_session_end, updated_at),
		        argMax(event_count, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&projectedStart, &projectedEnd, &hasSessionEnd, &eventCount); err != nil {
		t.Fatalf("projection query: %v", err)
	}
	if !projectedStart.Equal(start) || !projectedEnd.Equal(end) {
		t.Fatalf("projection range = %s..%s, want %s..%s", projectedStart, projectedEnd, start, end)
	}
	if hasSessionEnd != 1 {
		t.Fatalf("has_session_end = %d, want 1", hasSessionEnd)
	}
	if eventCount != 4 {
		t.Fatalf("event_count = %d, want 4", eventCount)
	}
	if duration := projectedEnd.Sub(projectedStart); duration != 2*time.Minute {
		t.Fatalf("duration = %s, want 2m", duration)
	}

	refreshed, err := ch.RefreshAllProjections(context.Background(), 0)
	if err != nil {
		t.Fatalf("refresh all projections: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("refreshed sessions = %d, want 1", refreshed)
	}

	var rebuiltHasSessionEnd uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(has_session_end, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&rebuiltHasSessionEnd); err != nil {
		t.Fatalf("rebuilt projection query: %v", err)
	}
	if rebuiltHasSessionEnd != 1 {
		t.Fatalf("rebuilt has_session_end = %d, want 1", rebuiltHasSessionEnd)
	}
}

func TestClickHouseProjectionRejectsLegacyLastPromptEventMsgAsSessionEnd(t *testing.T) {
	ch := setupLiveClickHouse(t)
	sessionID := "legacy-last-prompt-session"
	start := time.Date(2026, 5, 22, 13, 0, 0, 0, time.UTC)

	batch := RowBatch{
		ActivityEvents: []models.Event{
			{
				EventUID:   "evt-message",
				SessionID:  sessionID,
				SourceName: "claude",
				Runtime:    "claude-code",
				Provider:   "anthropic",
				Format:     "jsonl",
				EventKind:  "message",
				ActorRole:  "user",
				Timestamp:  start,
				SourceFile: "claude.jsonl",
			},
			{
				EventUID:    "evt-legacy-last-prompt",
				SessionID:   sessionID,
				SourceName:  "claude",
				Runtime:     "claude-code",
				Provider:    "anthropic",
				Format:      "jsonl",
				EventKind:   "event_msg",
				PayloadType: "last-prompt",
				ActorRole:   "system",
				Timestamp:   start.Add(time.Minute),
				SourceFile:  "claude.jsonl",
			},
		},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, err := ch.RefreshAllProjections(context.Background(), 0); err != nil {
		t.Fatalf("refresh all projections: %v", err)
	}

	var hasSessionEnd uint64
	if err := ch.DB.QueryRowContext(context.Background(),
		`SELECT argMax(has_session_end, updated_at)
		 FROM session_projection
		 WHERE session_id = ?`, sessionID).Scan(&hasSessionEnd); err != nil {
		t.Fatalf("projection query: %v", err)
	}
	if hasSessionEnd != 0 {
		t.Fatalf("legacy event_msg last-prompt has_session_end = %d, want 0", hasSessionEnd)
	}
}
