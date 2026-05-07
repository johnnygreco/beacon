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
