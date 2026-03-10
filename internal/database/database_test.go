package database_test

import (
	"context"
	"testing"

	"github.com/johnnygreco/beacon/internal/database"
)

func TestOpen_InMemory(t *testing.T) {
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if db == nil {
		t.Fatal("Open returned nil DB")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestOpen_WriteConn(t *testing.T) {
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if db.WriteConn() == nil {
		t.Fatal("WriteConn returned nil")
	}
}

func TestOpen_MigrationsCreateTables(t *testing.T) {
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	rows, err := db.ReadPool.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = 'main' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	found := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found[name] = true
	}

	expected := []string{"events", "event_links", "tool_io", "ingest_errors", "ingest_checkpoints"}
	for _, tbl := range expected {
		if !found[tbl] {
			t.Errorf("expected table %q not found; got %v", tbl, found)
		}
	}
}

func TestOpen_MigrationsCreateViews(t *testing.T) {
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	rows, err := db.ReadPool.QueryContext(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = 'main' AND table_type = 'VIEW'`)
	if err != nil {
		t.Fatalf("query views: %v", err)
	}
	defer rows.Close()

	found := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found[name] = true
	}

	expected := []string{
		"v_session_summary",
		"v_conversation_trace",
		"v_tokens_per_minute",
		"v_tool_stats",
		"v_tokens_by_model",
	}
	for _, v := range expected {
		if !found[v] {
			t.Errorf("expected view %q not found; got %v", v, found)
		}
	}
}

func TestOpen_MigrationsCreateIndexes(t *testing.T) {
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	rows, err := db.ReadPool.QueryContext(ctx,
		`SELECT index_name FROM duckdb_indexes() WHERE table_name = 'events'`)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer rows.Close()

	found := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found[name] = true
	}

	expected := []string{
		"idx_events_session",
		"idx_events_timestamp",
		"idx_events_kind",
		"idx_events_session_kind",
		"idx_events_kind_ts",
	}
	for _, idx := range expected {
		if !found[idx] {
			t.Errorf("expected index %q not found; got %v", idx, found)
		}
	}
}
