package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/models"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInsertEvent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	e := &models.Event{
		EventUID:          "evt-001",
		SessionID:         "sess-001",
		SourceName:        "claude",
		Provider:          "anthropic",
		EventKind:         "message",
		PayloadType:       "text",
		ActorRole:         "assistant",
		Timestamp:         now,
		TextContent:       "Hello, world!",
		TextPreview:       "Hello, world!",
		ToolName:          "",
		Model:             "claude-opus-4-20250514",
		InputTokens:       100,
		OutputTokens:      50,
		CacheReadTokens:   10,
		CacheCreateTokens: 5,
		DurationMs:        200,
		CostUSD:           0.005,
		EventVersion:      1,
		CWD:               "/tmp",
		SourceFile:        "test.jsonl",
		SourceLineNo:      1,
		SourceOffset:      0,
	}

	if err := database.InsertEvent(ctx, db, e); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	var (
		uid, sessionID, sourceName, provider, eventKind string
		payloadType, actorRole, textContent, model, cwd string
		inputTokens, outputTokens, cacheRead, cacheCreate int64
		durationMs int64
		costUSD    float64
		ts         time.Time
	)
	err := db.ReadPool.QueryRowContext(ctx,
		`SELECT event_uid, session_id, source_name, provider, event_kind,
		        payload_type, actor_role, text_content, model, cwd,
		        input_tokens, output_tokens, cache_read_tokens, cache_create_tokens,
		        duration_ms, cost_usd, timestamp
		 FROM events WHERE event_uid = ?`, "evt-001").Scan(
		&uid, &sessionID, &sourceName, &provider, &eventKind,
		&payloadType, &actorRole, &textContent, &model, &cwd,
		&inputTokens, &outputTokens, &cacheRead, &cacheCreate,
		&durationMs, &costUSD, &ts,
	)
	if err != nil {
		t.Fatalf("read back event: %v", err)
	}

	if uid != "evt-001" {
		t.Errorf("event_uid = %q, want %q", uid, "evt-001")
	}
	if sessionID != "sess-001" {
		t.Errorf("session_id = %q, want %q", sessionID, "sess-001")
	}
	if sourceName != "claude" {
		t.Errorf("source_name = %q, want %q", sourceName, "claude")
	}
	if provider != "anthropic" {
		t.Errorf("provider = %q, want %q", provider, "anthropic")
	}
	if eventKind != "message" {
		t.Errorf("event_kind = %q, want %q", eventKind, "message")
	}
	if payloadType != "text" {
		t.Errorf("payload_type = %q, want %q", payloadType, "text")
	}
	if actorRole != "assistant" {
		t.Errorf("actor_role = %q, want %q", actorRole, "assistant")
	}
	if textContent != "Hello, world!" {
		t.Errorf("text_content = %q, want %q", textContent, "Hello, world!")
	}
	if model != "claude-opus-4-20250514" {
		t.Errorf("model = %q, want %q", model, "claude-opus-4-20250514")
	}
	if cwd != "/tmp" {
		t.Errorf("cwd = %q, want %q", cwd, "/tmp")
	}
	if inputTokens != 100 {
		t.Errorf("input_tokens = %d, want %d", inputTokens, 100)
	}
	if outputTokens != 50 {
		t.Errorf("output_tokens = %d, want %d", outputTokens, 50)
	}
	if cacheRead != 10 {
		t.Errorf("cache_read_tokens = %d, want %d", cacheRead, 10)
	}
	if cacheCreate != 5 {
		t.Errorf("cache_create_tokens = %d, want %d", cacheCreate, 5)
	}
	if durationMs != 200 {
		t.Errorf("duration_ms = %d, want %d", durationMs, 200)
	}
	if costUSD != 0.005 {
		t.Errorf("cost_usd = %f, want %f", costUSD, 0.005)
	}
}

func TestInsertEvent_Duplicate(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	e := &models.Event{
		EventUID:     "evt-dup",
		SessionID:    "sess-001",
		EventKind:    "message",
		Timestamp:    time.Now(),
		TextContent:  "first",
		EventVersion: 1,
	}

	if err := database.InsertEvent(ctx, db, e); err != nil {
		t.Fatalf("first InsertEvent: %v", err)
	}

	// Second insert with same UID should be silently ignored (INSERT OR IGNORE).
	e2 := &models.Event{
		EventUID:     "evt-dup",
		SessionID:    "sess-001",
		EventKind:    "message",
		Timestamp:    time.Now(),
		TextContent:  "second",
		EventVersion: 1,
	}
	if err := database.InsertEvent(ctx, db, e2); err != nil {
		t.Fatalf("duplicate InsertEvent: %v", err)
	}

	var content string
	err := db.ReadPool.QueryRowContext(ctx,
		`SELECT text_content FROM events WHERE event_uid = ?`, "evt-dup").Scan(&content)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if content != "first" {
		t.Errorf("text_content = %q, want %q (original should be preserved)", content, "first")
	}
}

func TestInsertEventLink(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Insert parent and child events first.
	for _, uid := range []string{"evt-parent", "evt-child"} {
		e := &models.Event{
			EventUID:     uid,
			SessionID:    "sess-001",
			EventKind:    "message",
			Timestamp:    time.Now(),
			EventVersion: 1,
		}
		if err := database.InsertEvent(ctx, db, e); err != nil {
			t.Fatalf("InsertEvent(%s): %v", uid, err)
		}
	}

	link := &models.EventLink{
		EventUID:       "evt-parent",
		LinkedEventUID: "evt-child",
		LinkType:       "parent",
	}
	if err := database.InsertEventLink(ctx, db, link); err != nil {
		t.Fatalf("InsertEventLink: %v", err)
	}

	var eventUID, linkedUID, linkType string
	err := db.ReadPool.QueryRowContext(ctx,
		`SELECT event_uid, linked_event_uid, link_type FROM event_links
		 WHERE event_uid = ? AND linked_event_uid = ?`,
		"evt-parent", "evt-child").Scan(&eventUID, &linkedUID, &linkType)
	if err != nil {
		t.Fatalf("read back link: %v", err)
	}
	if eventUID != "evt-parent" {
		t.Errorf("event_uid = %q, want %q", eventUID, "evt-parent")
	}
	if linkedUID != "evt-child" {
		t.Errorf("linked_event_uid = %q, want %q", linkedUID, "evt-child")
	}
	if linkType != "parent" {
		t.Errorf("link_type = %q, want %q", linkType, "parent")
	}
}

func TestInsertToolIO(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Insert the parent event first.
	e := &models.Event{
		EventUID:     "evt-tool",
		SessionID:    "sess-001",
		EventKind:    "tool_call",
		ToolName:     "Read",
		Timestamp:    time.Now(),
		EventVersion: 1,
	}
	if err := database.InsertEvent(ctx, db, e); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	tio := &models.ToolIO{
		EventUID:      "evt-tool",
		ToolName:      "Read",
		ToolPhase:     "call",
		InputJSON:     `{"path":"/tmp/test.go"}`,
		OutputJSON:    `{"content":"package main"}`,
		InputPreview:  "/tmp/test.go",
		OutputPreview: "package main",
	}
	if err := database.InsertToolIO(ctx, db, tio); err != nil {
		t.Fatalf("InsertToolIO: %v", err)
	}

	var toolName, phase, inputJSON, outputJSON, inputPrev, outputPrev string
	err := db.ReadPool.QueryRowContext(ctx,
		`SELECT tool_name, tool_phase, input_json, output_json, input_preview, output_preview
		 FROM tool_io WHERE event_uid = ?`, "evt-tool").Scan(
		&toolName, &phase, &inputJSON, &outputJSON, &inputPrev, &outputPrev,
	)
	if err != nil {
		t.Fatalf("read back tool_io: %v", err)
	}
	if toolName != "Read" {
		t.Errorf("tool_name = %q, want %q", toolName, "Read")
	}
	if phase != "call" {
		t.Errorf("tool_phase = %q, want %q", phase, "call")
	}
	if inputJSON != `{"path":"/tmp/test.go"}` {
		t.Errorf("input_json = %q, want %q", inputJSON, `{"path":"/tmp/test.go"}`)
	}
	if outputJSON != `{"content":"package main"}` {
		t.Errorf("output_json = %q, want %q", outputJSON, `{"content":"package main"}`)
	}
	if inputPrev != "/tmp/test.go" {
		t.Errorf("input_preview = %q, want %q", inputPrev, "/tmp/test.go")
	}
	if outputPrev != "package main" {
		t.Errorf("output_preview = %q, want %q", outputPrev, "package main")
	}
}

func TestInsertIngestError(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	ie := &models.IngestError{
		ID:              "err-001",
		SourceFile:      "test.jsonl",
		SourceLineNo:    42,
		ErrorClass:      "parse_error",
		ErrorMessage:    "unexpected token",
		ContextFragment: `{"bad json`,
	}
	if err := database.InsertIngestError(ctx, db, ie); err != nil {
		t.Fatalf("InsertIngestError: %v", err)
	}

	var id, srcFile, errClass, errMsg, fragment string
	var lineNo int
	err := db.ReadPool.QueryRowContext(ctx,
		`SELECT id, source_file, source_line_no, error_class, error_message, context_fragment
		 FROM ingest_errors WHERE id = ?`, "err-001").Scan(
		&id, &srcFile, &lineNo, &errClass, &errMsg, &fragment,
	)
	if err != nil {
		t.Fatalf("read back ingest_error: %v", err)
	}
	if id != "err-001" {
		t.Errorf("id = %q, want %q", id, "err-001")
	}
	if srcFile != "test.jsonl" {
		t.Errorf("source_file = %q, want %q", srcFile, "test.jsonl")
	}
	if lineNo != 42 {
		t.Errorf("source_line_no = %d, want %d", lineNo, 42)
	}
	if errClass != "parse_error" {
		t.Errorf("error_class = %q, want %q", errClass, "parse_error")
	}
	if errMsg != "unexpected token" {
		t.Errorf("error_message = %q, want %q", errMsg, "unexpected token")
	}
	if fragment != `{"bad json` {
		t.Errorf("context_fragment = %q, want %q", fragment, `{"bad json`)
	}
}

func TestUpsertCheckpoint_InsertAndUpdate(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	cp := &models.Checkpoint{
		SourceName:       "claude",
		SourceFile:       "/tmp/logs/session.jsonl",
		SourceInode:      12345,
		SourceGeneration: 1,
		LastOffset:       1024,
		LastLineNo:       50,
	}
	if err := database.UpsertCheckpoint(ctx, db, cp); err != nil {
		t.Fatalf("UpsertCheckpoint (insert): %v", err)
	}

	// Read back the initial checkpoint.
	var sourceName, sourceFile string
	var inode, offset int64
	var gen, lineNo int
	err := db.ReadPool.QueryRowContext(ctx,
		`SELECT source_name, source_file, source_inode, source_generation, last_offset, last_line_no
		 FROM ingest_checkpoints WHERE source_name = ? AND source_file = ?`,
		"claude", "/tmp/logs/session.jsonl").Scan(
		&sourceName, &sourceFile, &inode, &gen, &offset, &lineNo,
	)
	if err != nil {
		t.Fatalf("read back checkpoint: %v", err)
	}
	if offset != 1024 {
		t.Errorf("last_offset = %d, want %d", offset, 1024)
	}
	if lineNo != 50 {
		t.Errorf("last_line_no = %d, want %d", lineNo, 50)
	}

	// Upsert with updated values.
	cp.LastOffset = 2048
	cp.LastLineNo = 100
	if err := database.UpsertCheckpoint(ctx, db, cp); err != nil {
		t.Fatalf("UpsertCheckpoint (update): %v", err)
	}

	err = db.ReadPool.QueryRowContext(ctx,
		`SELECT last_offset, last_line_no FROM ingest_checkpoints
		 WHERE source_name = ? AND source_file = ?`,
		"claude", "/tmp/logs/session.jsonl").Scan(&offset, &lineNo)
	if err != nil {
		t.Fatalf("read back updated checkpoint: %v", err)
	}
	if offset != 2048 {
		t.Errorf("updated last_offset = %d, want %d", offset, 2048)
	}
	if lineNo != 100 {
		t.Errorf("updated last_line_no = %d, want %d", lineNo, 100)
	}
}

func TestLoadCheckpoints(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	checkpoints := []*models.Checkpoint{
		{
			SourceName:       "claude",
			SourceFile:       "/tmp/logs/a.jsonl",
			SourceInode:      111,
			SourceGeneration: 1,
			LastOffset:       100,
			LastLineNo:       10,
		},
		{
			SourceName:       "claude",
			SourceFile:       "/tmp/logs/b.jsonl",
			SourceInode:      222,
			SourceGeneration: 1,
			LastOffset:       200,
			LastLineNo:       20,
		},
		{
			SourceName:       "other",
			SourceFile:       "/tmp/logs/c.jsonl",
			SourceInode:      333,
			SourceGeneration: 1,
			LastOffset:       300,
			LastLineNo:       30,
		},
	}
	for _, cp := range checkpoints {
		if err := database.UpsertCheckpoint(ctx, db, cp); err != nil {
			t.Fatalf("UpsertCheckpoint(%s): %v", cp.SourceFile, err)
		}
	}

	result, err := database.LoadCheckpoints(ctx, db.ReadPool, "claude")
	if err != nil {
		t.Fatalf("LoadCheckpoints: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("LoadCheckpoints returned %d entries, want 2", len(result))
	}

	a, ok := result["/tmp/logs/a.jsonl"]
	if !ok {
		t.Fatal("missing checkpoint for a.jsonl")
	}
	if a.LastOffset != 100 {
		t.Errorf("a.jsonl offset = %d, want %d", a.LastOffset, 100)
	}
	if a.SourceInode != 111 {
		t.Errorf("a.jsonl inode = %d, want %d", a.SourceInode, 111)
	}

	b, ok := result["/tmp/logs/b.jsonl"]
	if !ok {
		t.Fatal("missing checkpoint for b.jsonl")
	}
	if b.LastOffset != 200 {
		t.Errorf("b.jsonl offset = %d, want %d", b.LastOffset, 200)
	}
}

func TestResetSchema(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Insert some data into multiple tables.
	e := &models.Event{
		EventUID:     "evt-reset",
		SessionID:    "sess-reset",
		EventKind:    "message",
		Timestamp:    time.Now(),
		EventVersion: 1,
	}
	if err := database.InsertEvent(ctx, db, e); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	ie := &models.IngestError{
		ID:           "err-reset",
		SourceFile:   "test.jsonl",
		ErrorClass:   "test",
		ErrorMessage: "test error",
	}
	if err := database.InsertIngestError(ctx, db, ie); err != nil {
		t.Fatalf("InsertIngestError: %v", err)
	}

	// Reset the schema.
	if err := database.ResetSchema(ctx, db); err != nil {
		t.Fatalf("ResetSchema: %v", err)
	}

	// Verify tables still exist but are empty.
	tables := []string{"events", "event_links", "tool_io", "ingest_errors", "ingest_checkpoints"}
	for _, tbl := range tables {
		var count int
		err := db.ReadPool.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+tbl).Scan(&count)
		if err != nil {
			t.Errorf("table %q: query failed (table may not exist): %v", tbl, err)
			continue
		}
		if count != 0 {
			t.Errorf("table %q has %d rows after reset, want 0", tbl, count)
		}
	}
}
