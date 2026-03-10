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
	_, err := db.WriteConn().ExecContext(context.Background(),
		`INSERT INTO events (event_uid, session_id, event_kind, text_content, text_preview, model, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uid, sessionID, kind, text, text, model, time.Now())
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
