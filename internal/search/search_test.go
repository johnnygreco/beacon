package search_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/search"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertEvent(t *testing.T, db *database.DB, uid, sessionID, kind, text, model string) {
	t.Helper()
	insertEventAt(t, db, uid, sessionID, kind, text, model, time.Now())
}

func insertEventAt(t *testing.T, db *database.DB, uid, sessionID, kind, text, model string, ts time.Time) {
	t.Helper()
	_, err := db.WriteConn().ExecContext(context.Background(),
		`INSERT INTO events (event_uid, session_id, event_kind, text_content, text_preview, model, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uid, sessionID, kind, text, text, model, ts)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestNewSearcher(t *testing.T) {
	db := setupTestDB(t)
	logger := testLogger
	s := search.NewSearcher(db.ReadPool, logger, 25, 0)
	if s == nil {
		t.Fatal("expected non-nil Searcher")
	}
}

func TestSearch_NoIndex_ILIKEFallback(t *testing.T) {
	db := setupTestDB(t)
	logger := testLogger

	insertEvent(t, db, "evt-1", "sess-1", "message", "The quick brown fox jumps over the lazy dog", "gpt-4")
	insertEvent(t, db, "evt-2", "sess-1", "message", "Hello world from the test suite", "gpt-4")
	insertEvent(t, db, "evt-3", "sess-2", "tool_call", "Searching for something else entirely", "claude-3")

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

	results, err := s.Search(context.Background(), search.SearchQuery{
		Query: "quick brown fox",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result from ILIKE fallback")
	}

	found := false
	for _, r := range results {
		if r.EventUID == "evt-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find evt-1 in results")
	}
}

func TestSearch_EmptyDatabase(t *testing.T) {
	db := setupTestDB(t)
	logger := testLogger
	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

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
	db := setupTestDB(t)
	logger := testLogger

	for i := 0; i < 5; i++ {
		insertEvent(t, db, fmt.Sprintf("evt-%d", i), "sess-1", "message",
			fmt.Sprintf("matching text number %d for search", i), "gpt-4")
	}

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

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

func TestProbeIndex_NoIndex(t *testing.T) {
	db := setupTestDB(t)
	logger := testLogger
	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

	s.ProbeIndex()
	if s.IndexExists() {
		t.Error("expected IndexExists to be false on fresh db with no FTS index")
	}
}

func TestLegacySearch(t *testing.T) {
	db := setupTestDB(t)
	logger := testLogger

	insertEvent(t, db, "evt-legacy-1", "sess-1", "message", "legacy search test content", "gpt-4")

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

	results, err := s.LegacySearch(context.Background(), "legacy search", 10)
	if err != nil {
		t.Fatalf("LegacySearch error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result from LegacySearch")
	}
	if results[0].EventUID != "evt-legacy-1" {
		t.Errorf("expected evt-legacy-1, got %s", results[0].EventUID)
	}
}

func TestSearch_WithSessionFilter(t *testing.T) {
	db := setupTestDB(t)
	logger := testLogger

	insertEvent(t, db, "evt-a", "sess-alpha", "message", "findme in session alpha", "gpt-4")
	insertEvent(t, db, "evt-b", "sess-beta", "message", "findme in session beta", "gpt-4")

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

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
	db := setupTestDB(t)
	logger := testLogger

	insertEvent(t, db, "evt-msg", "sess-1", "message", "filterable content here", "gpt-4")
	insertEvent(t, db, "evt-tool", "sess-1", "tool_call", "filterable content here", "gpt-4")

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

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

func TestSearch_NewestSortConsistentAcrossTimeRanges(t *testing.T) {
	db := setupTestDB(t)
	logger := testLogger

	now := time.Now()
	// Insert events at different timestamps, all matching "deploy"
	insertEventAt(t, db, "evt-old", "sess-1", "message", "deploy to staging env", "gpt-4", now.Add(-20*24*time.Hour))
	insertEventAt(t, db, "evt-mid", "sess-2", "message", "deploy to production env", "gpt-4", now.Add(-2*time.Hour))
	insertEventAt(t, db, "evt-new", "sess-3", "message", "deploy hotfix to prod", "gpt-4", now.Add(-10*time.Minute))

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

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
	db := setupTestDB(t)
	logger := testLogger

	now := time.Now()
	insertEventAt(t, db, "evt-1", "sess-1", "message", "build step one", "gpt-4", now.Add(-3*time.Hour))
	insertEventAt(t, db, "evt-2", "sess-1", "message", "build step two", "gpt-4", now.Add(-2*time.Hour))
	insertEventAt(t, db, "evt-3", "sess-1", "message", "build step three", "gpt-4", now.Add(-1*time.Hour))

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

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
	db := setupTestDB(t)
	logger := testLogger

	now := time.Now()
	insertEventAt(t, db, "evt-a", "sess-1", "message", "alpha event", "gpt-4", now.Add(-3*time.Hour))
	insertEventAt(t, db, "evt-b", "sess-1", "message", "beta event", "gpt-4", now.Add(-2*time.Hour))
	insertEventAt(t, db, "evt-c", "sess-1", "message", "gamma event", "gpt-4", now.Add(-1*time.Hour))

	s := search.NewSearcher(db.ReadPool, logger, 25, 0)

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
