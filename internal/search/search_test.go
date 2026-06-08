package search_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/store"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	addr := os.Getenv("BEACON_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("set BEACON_TEST_CLICKHOUSE to run ClickHouse search integration tests")
	}
	ch, err := store.Open(t.Context(), store.Options{Addrs: []string{addr}, Database: "beacon_test", ReadPoolSize: 2})
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	if err := store.Reset(t.Context(), ch.DB, ch.Database()); err != nil {
		ch.Close()
		t.Fatalf("reset: %v", err)
	}
	t.Cleanup(func() { ch.Close() })
	return ch
}

func insertEvent(t *testing.T, ch *store.Store, uid, sessionID, kind, text, model string) {
	t.Helper()
	insertEventAt(t, ch, uid, sessionID, kind, text, model, time.Now())
}

func insertEventAt(t *testing.T, ch *store.Store, uid, sessionID, kind, text, model string, ts time.Time) {
	t.Helper()
	event := models.Event{
		EventUID:     uid,
		SessionID:    sessionID,
		SourceName:   "test",
		Provider:     "test",
		EventKind:    kind,
		Timestamp:    ts,
		TextContent:  text,
		TextPreview:  text,
		Model:        model,
		EventVersion: 1,
		SourceFile:   "search-test",
	}
	err := ch.Flush(context.Background(), store.RowBatch{
		RawRecords:     []models.RawRecord{store.NewRawRecord(event)},
		ActivityEvents: []models.Event{event},
	})
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestNewSearcher(t *testing.T) {
	logger := testLogger
	s := search.NewSearcher(nil, logger, 25, 0)
	if s == nil {
		t.Fatal("expected non-nil Searcher")
	}
}

func TestSearch_EmptyDatabase(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger
	s := search.NewSearcher(ch.DB, logger, 25, 0)

	results, err := s.Search(context.Background(), search.SearchQuery{
		Query: "anything",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_LimitResults(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	for i := 0; i < 5; i++ {
		insertEvent(t, ch, fmt.Sprintf("evt-%d", i), "sess-1", "message",
			fmt.Sprintf("matching text number %d for search", i), "gpt-4")
	}

	s := search.NewSearcher(ch.DB, logger, 25, 0)

	results, err := s.Search(context.Background(), search.SearchQuery{
		Query: "matching text",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestSearch_BM25RankingDeduplicatesReplayedPostings(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	now := time.Now()
	high := models.Event{
		EventUID:     "evt-rank-high",
		SessionID:    "sess-rank",
		SourceName:   "test",
		Provider:     "test",
		EventKind:    "message",
		Timestamp:    now,
		TextContent:  strings.Repeat("needle ", 12) + "ranking focus",
		TextPreview:  "needle ranking high",
		Model:        "gpt-4",
		EventVersion: 1,
		SourceFile:   "search-test",
		SourceLineNo: 1,
		SourceOffset: 1,
		PayloadJSON:  `{"event":"high"}`,
		CreatedAt:    now,
	}
	low := models.Event{
		EventUID:     "evt-rank-low",
		SessionID:    "sess-rank",
		SourceName:   "test",
		Provider:     "test",
		EventKind:    "message",
		Timestamp:    now.Add(time.Second),
		TextContent:  "needle ranking focus",
		TextPreview:  "needle ranking low",
		Model:        "gpt-4",
		EventVersion: 1,
		SourceFile:   "search-test",
		SourceLineNo: 2,
		SourceOffset: 2,
		PayloadJSON:  `{"event":"low"}`,
		CreatedAt:    now,
	}
	other := models.Event{
		EventUID:     "evt-rank-other",
		SessionID:    "sess-rank",
		SourceName:   "test",
		Provider:     "test",
		EventKind:    "message",
		Timestamp:    now.Add(2 * time.Second),
		TextContent:  "unrelated ranking focus",
		TextPreview:  "unrelated ranking other",
		Model:        "gpt-4",
		EventVersion: 1,
		SourceFile:   "search-test",
		SourceLineNo: 3,
		SourceOffset: 3,
		PayloadJSON:  `{"event":"other"}`,
		CreatedAt:    now,
	}
	batch := store.RowBatch{
		ActivityEvents: []models.Event{high, low, other},
	}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}

	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("replay flush: %v", err)
	}

	s := search.NewSearcher(ch.DB, logger, 25, 0)
	results, err := s.Search(context.Background(), search.SearchQuery{
		Query: "needle",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 needle results, got %d: %#v", len(results), results)
	}
	if results[0].EventUID != "evt-rank-high" {
		t.Fatalf("expected repeated-term document first, got %s", results[0].EventUID)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("expected high-frequency document score %.4f to exceed %.4f", results[0].Score, results[1].Score)
	}
}

func TestProbeIndex_NoIndex(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger
	s := search.NewSearcher(ch.DB, logger, 25, 0)

	s.ProbeIndex()
	if s.IndexExists() {
		t.Error("expected IndexExists to be false on fresh db with no FTS index")
	}
}

func TestSearch_WithQueryAndLimit(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	insertEvent(t, ch, "evt-query-1", "sess-1", "message", "query search test content", "gpt-4")

	s := search.NewSearcher(ch.DB, logger, 25, 0)

	results, err := s.Search(context.Background(), search.SearchQuery{Query: "query search", Limit: 10})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result from Search")
	}
	if results[0].EventUID != "evt-query-1" {
		t.Errorf("expected evt-query-1, got %s", results[0].EventUID)
	}
}

func TestSearch_WithSessionFilter(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	insertEvent(t, ch, "evt-a", "sess-alpha", "message", "findme in session alpha", "gpt-4")
	insertEvent(t, ch, "evt-b", "sess-beta", "message", "findme in session beta", "gpt-4")

	s := search.NewSearcher(ch.DB, logger, 25, 0)

	results, err := s.Search(context.Background(), search.SearchQuery{
		Query:     "findme",
		Limit:     10,
		SessionID: "sess-alpha",
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with session filter, got %d", len(results))
	}
	if results[0].EventUID != "evt-a" {
		t.Errorf("expected evt-a, got %s", results[0].EventUID)
	}
}

func TestSearch_WithEventKindFilter(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	insertEvent(t, ch, "evt-msg", "sess-1", "message", "filterable content here", "gpt-4")
	insertEvent(t, ch, "evt-tool", "sess-1", "tool_call", "filterable content here", "gpt-4")

	s := search.NewSearcher(ch.DB, logger, 25, 0)

	results, err := s.Search(context.Background(), search.SearchQuery{
		Query:      "filterable content",
		Limit:      10,
		EventKinds: []string{"tool_call"},
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with event kind filter, got %d", len(results))
	}
	if results[0].EventUID != "evt-tool" {
		t.Errorf("expected evt-tool, got %s", results[0].EventUID)
	}
}

func TestSearch_ProjectFilterUsesSessionCWDForEventsWithoutCWD(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	now := time.Now().UTC()
	sessionID := "sess-project-fallback"
	sessionMeta := models.Event{
		EventUID:     "evt-project-meta",
		SessionID:    sessionID,
		SourceName:   "test",
		Provider:     "test",
		EventKind:    "session_meta",
		Timestamp:    now,
		TextPreview:  "session started",
		CWD:          "/Users/example/projects/beacon",
		EventVersion: 1,
		SourceFile:   "search-test",
	}
	message := models.Event{
		EventUID:     "evt-project-message",
		SessionID:    sessionID,
		SourceName:   "test",
		Provider:     "test",
		EventKind:    "message",
		ActorRole:    "assistant",
		Timestamp:    now.Add(time.Second),
		TextContent:  "project scoped needle",
		TextPreview:  "project scoped needle",
		EventVersion: 1,
		SourceFile:   "search-test",
	}
	if err := ch.Flush(context.Background(), store.RowBatch{
		ActivityEvents: []models.Event{sessionMeta},
		RawRecords:     []models.RawRecord{store.NewRawRecord(sessionMeta)},
	}); err != nil {
		t.Fatalf("flush session metadata: %v", err)
	}
	if err := ch.Flush(context.Background(), store.RowBatch{
		ActivityEvents: []models.Event{message},
		RawRecords:     []models.RawRecord{store.NewRawRecord(message)},
	}); err != nil {
		t.Fatalf("flush project message: %v", err)
	}

	s := search.NewSearcher(ch.DB, logger, 25, 0)
	results, err := s.Search(context.Background(), search.SearchQuery{
		Query:       "needle",
		Limit:       10,
		ProjectKeys: []string{"beacon"},
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 project-scoped result, got %d: %#v", len(results), results)
	}
	if results[0].EventUID != message.EventUID || results[0].ProjectKey != "beacon" ||
		results[0].ProjectPath != "/Users/example/projects/beacon" {
		t.Fatalf("unexpected project-scoped result: %#v", results[0])
	}
}

func TestSearch_NewestSortConsistentAcrossTimeRanges(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	now := time.Now()
	// Insert events at different timestamps, all matching "deploy"
	insertEventAt(t, ch, "evt-old", "sess-1", "message", "deploy to staging env", "gpt-4", now.Add(-20*24*time.Hour))
	insertEventAt(t, ch, "evt-mid", "sess-2", "message", "deploy to production env", "gpt-4", now.Add(-2*time.Hour))
	insertEventAt(t, ch, "evt-new", "sess-3", "message", "deploy hotfix to prod", "gpt-4", now.Add(-10*time.Minute))

	s := search.NewSearcher(ch.DB, logger, 25, 0)

	// Search with 30d range, sorted by newest
	results30d, err := s.Search(context.Background(), search.SearchQuery{
		Query:    "deploy",
		Limit:    10,
		SortBy:   "newest",
		FromTime: now.Add(-30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Search 30d error: %v", err)
	}

	// Search with 1h range, sorted by newest
	results1h, err := s.Search(context.Background(), search.SearchQuery{
		Query:    "deploy",
		Limit:    10,
		SortBy:   "newest",
		FromTime: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Search 1h error: %v", err)
	}

	// The newest result (evt-new) should be first in both queries
	if len(results30d) < 1 {
		t.Fatal("expected at least 1 result for 30d range")
	}
	if results30d[0].EventUID != "evt-new" {
		t.Errorf("30d newest: expected evt-new first, got %s", results30d[0].EventUID)
	}

	if len(results1h) < 1 {
		t.Fatal("expected at least 1 result for 1h range")
	}
	if results1h[0].EventUID != "evt-new" {
		t.Errorf("1h newest: expected evt-new first, got %s", results1h[0].EventUID)
	}

	// 30d should return all 3, 1h should return only 1
	if len(results30d) != 3 {
		t.Errorf("30d: expected 3 results, got %d", len(results30d))
	}
	if len(results1h) != 1 {
		t.Errorf("1h: expected 1 result, got %d", len(results1h))
	}

	// Verify 30d is properly sorted newest-first
	for i := 1; i < len(results30d); i++ {
		if results30d[i].Timestamp.After(results30d[i-1].Timestamp) {
			t.Errorf("30d results not sorted newest-first: index %d (%v) is after index %d (%v)",
				i, results30d[i].Timestamp, i-1, results30d[i-1].Timestamp)
		}
	}
}

func TestSearch_OldestSort(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	now := time.Now()
	insertEventAt(t, ch, "evt-1", "sess-1", "message", "build step one", "gpt-4", now.Add(-3*time.Hour))
	insertEventAt(t, ch, "evt-2", "sess-1", "message", "build step two", "gpt-4", now.Add(-2*time.Hour))
	insertEventAt(t, ch, "evt-3", "sess-1", "message", "build step three", "gpt-4", now.Add(-1*time.Hour))

	s := search.NewSearcher(ch.DB, logger, 25, 0)

	results, err := s.Search(context.Background(), search.SearchQuery{
		Query:  "build step",
		Limit:  10,
		SortBy: "oldest",
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].EventUID != "evt-1" {
		t.Errorf("oldest sort: expected evt-1 first, got %s", results[0].EventUID)
	}
	if results[2].EventUID != "evt-3" {
		t.Errorf("oldest sort: expected evt-3 last, got %s", results[2].EventUID)
	}
}

func TestBrowse_SortOrder(t *testing.T) {
	ch := setupTestStore(t)
	logger := testLogger

	now := time.Now()
	insertEventAt(t, ch, "evt-a", "sess-1", "message", "alpha event", "gpt-4", now.Add(-3*time.Hour))
	insertEventAt(t, ch, "evt-b", "sess-1", "message", "beta event", "gpt-4", now.Add(-2*time.Hour))
	insertEventAt(t, ch, "evt-c", "sess-1", "message", "gamma event", "gpt-4", now.Add(-1*time.Hour))

	s := search.NewSearcher(ch.DB, logger, 25, 0)

	// Browse newest
	results, err := s.Browse(context.Background(), search.SearchQuery{
		Limit:     10,
		SortBy:    "newest",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Browse newest error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].EventUID != "evt-c" {
		t.Errorf("browse newest: expected evt-c first, got %s", results[0].EventUID)
	}

	// Browse oldest
	results, err = s.Browse(context.Background(), search.SearchQuery{
		Limit:     10,
		SortBy:    "oldest",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Browse oldest error: %v", err)
	}
	if results[0].EventUID != "evt-a" {
		t.Errorf("browse oldest: expected evt-a first, got %s", results[0].EventUID)
	}
}
