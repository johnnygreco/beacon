package capture

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHermesSQLite_MockStateDB(t *testing.T) {
	dbPath := createSQLiteFixture(t, "hermes_state.sql")

	events, err := ParseHermesSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := len(events), 9; got != want {
		t.Fatalf("len(events) = %d, want %d", got, want)
	}
	assertEvent(t, events, "session_meta", "system", "Investigate parser fixtures")
	usage := assertEvent(t, events, "event_msg", "system", "usage")
	if usage.InputTokens != 1200 || usage.OutputTokens != 240 || usage.CacheReadTokens != 30 || usage.CacheCreateTokens != 12 {
		t.Fatalf("hermes usage tokens = input:%d output:%d cacheRead:%d cacheWrite:%d",
			usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheCreateTokens)
	}
	reasoning := assertEvent(t, events, "reasoning", "assistant", "Need to inspect the file first.")
	if reasoning.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Fatalf("hermes reasoning model = %q", reasoning.Model)
	}
	tool := assertToolEvent(t, events, "tool_call", "read_file")
	if tool.ToolUseID != "call_read_1" || tool.ToolInput != `{"path":"fixtures.json"}` {
		t.Fatalf("hermes tool call = id:%q input:%q", tool.ToolUseID, tool.ToolInput)
	}
	result := assertToolEvent(t, events, "tool_result", "read_file")
	if result.TextContent != "fixture contents" {
		t.Fatalf("hermes tool result text = %q", result.TextContent)
	}
	end := assertEvent(t, events, "session_end", "system", "cli_close")
	if end.PayloadType != "cli_close" {
		t.Fatalf("hermes session_end payload = %q", end.PayloadType)
	}
}

func TestParseHermesSQLite_CurrentReasoningSchema(t *testing.T) {
	dbPath := createSQLiteFixtureFromSQL(t, `
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  model TEXT,
  parent_session_id TEXT,
  started_at REAL NOT NULL,
  ended_at REAL,
  end_reason TEXT,
  input_tokens INTEGER DEFAULT 0,
  output_tokens INTEGER DEFAULT 0,
  cache_read_tokens INTEGER DEFAULT 0,
  cache_write_tokens INTEGER DEFAULT 0,
  reasoning_tokens INTEGER DEFAULT 0,
  cwd TEXT,
  billing_provider TEXT,
  estimated_cost_usd REAL,
  actual_cost_usd REAL,
  title TEXT
);
CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT,
  timestamp REAL NOT NULL,
  reasoning TEXT,
  reasoning_details TEXT,
  active INTEGER NOT NULL DEFAULT 1
);
INSERT INTO sessions (id, source, model, started_at, cwd, title)
VALUES ('hermes-current-1', 'cli', 'openai/gpt-5.5', 1764590500.000, '/work/hermes-current', 'Current Hermes schema');
INSERT INTO messages (session_id, role, content, timestamp, reasoning, reasoning_details, active)
VALUES (
  'hermes-current-1',
  'assistant',
  'I found the updated schema.',
  1764590501.000,
  'Use the current reasoning column.',
  '[{"type":"reasoning","text":"Fallback reasoning details."}]',
  1
);
INSERT INTO messages (session_id, role, content, timestamp, active)
VALUES ('hermes-current-1', 'assistant', 'This rewound message should stay hidden.', 1764590502.000, 0);
`)

	events, err := ParseHermesSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	meta := assertEvent(t, events, "session_meta", "system", "Current Hermes schema")
	if meta.CWD != "/work/hermes-current" {
		t.Fatalf("hermes cwd = %q", meta.CWD)
	}
	assertEvent(t, events, "reasoning", "assistant", "Use the current reasoning column.")
	assertEvent(t, events, "message", "assistant", "I found the updated schema.")
	for _, evt := range events {
		if evt.TextContent == "This rewound message should stay hidden." {
			t.Fatalf("inactive Hermes message was parsed: %#v", evt)
		}
	}
}

func TestParseHermesSQLite_NoReasoningColumnsStillParsesMessages(t *testing.T) {
	dbPath := createSQLiteFixtureFromSQL(t, `
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  model TEXT,
  parent_session_id TEXT,
  started_at REAL NOT NULL,
  ended_at REAL,
  end_reason TEXT,
  input_tokens INTEGER DEFAULT 0,
  output_tokens INTEGER DEFAULT 0,
  cache_read_tokens INTEGER DEFAULT 0,
  cache_write_tokens INTEGER DEFAULT 0,
  reasoning_tokens INTEGER DEFAULT 0,
  billing_provider TEXT,
  estimated_cost_usd REAL,
  actual_cost_usd REAL,
  title TEXT
);
CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT,
  timestamp REAL NOT NULL
);
INSERT INTO sessions (id, source, model, started_at, title)
VALUES ('hermes-minimal-1', 'cli', 'anthropic/claude-sonnet', 1764590600.000, 'Minimal Hermes schema');
INSERT INTO messages (session_id, role, content, timestamp)
VALUES ('hermes-minimal-1', 'user', 'Please keep parsing messages.', 1764590601.000);
`)

	events, err := ParseHermesSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEvent(t, events, "message", "user", "Please keep parsing messages.")
}

func TestParseOpenCodeSQLite_MockStateDB(t *testing.T) {
	dbPath := createSQLiteFixture(t, "opencode_state.sql")

	events, err := ParseOpenCodeSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := len(events), 9; got != want {
		t.Fatalf("len(events) = %d, want %d", got, want)
	}
	meta := assertEvent(t, events, "session_meta", "system", "Parser fixtures")
	if meta.CWD != "/work/opencode-fixtures" || meta.Provider != "anthropic" {
		t.Fatalf("opencode meta cwd/provider = %q/%q", meta.CWD, meta.Provider)
	}
	reasoning := assertEvent(t, events, "reasoning", "assistant", "Need to run tests.")
	if reasoning.InputTokens != 900 || reasoning.OutputTokens != 180 || reasoning.CacheReadTokens != 50 || reasoning.CacheCreateTokens != 10 {
		t.Fatalf("opencode reasoning tokens = input:%d output:%d cacheRead:%d cacheWrite:%d",
			reasoning.InputTokens, reasoning.OutputTokens, reasoning.CacheReadTokens, reasoning.CacheCreateTokens)
	}
	deduped := DeduplicateTokens(append([]NormalizedEvent(nil), events...))
	if got, want := sumInputTokens(deduped), int64(1200); got != want {
		t.Fatalf("opencode input tokens after dedup = %d, want %d", got, want)
	}
	assertEvent(t, events, "message", "assistant", "I will run the focused tests.")
	call := assertToolEvent(t, events, "tool_call", "bash")
	if call.ToolInput != `{"cmd":"go test ./internal/capture"}` {
		t.Fatalf("opencode tool input = %q", call.ToolInput)
	}
	result := assertToolEvent(t, events, "tool_result", "bash")
	if result.ToolOutput != "ok github.com/johnnygreco/beacon/internal/capture" {
		t.Fatalf("opencode tool output = %q", result.ToolOutput)
	}
	toolOnlyCall := assertToolEvent(t, events, "tool_call", "grep")
	if toolOnlyCall.InputTokens != 300 || toolOnlyCall.OutputTokens != 40 || toolOnlyCall.CostUSD != 0.004 {
		t.Fatalf("opencode tool-only usage = input:%d output:%d cost:%f",
			toolOnlyCall.InputTokens, toolOnlyCall.OutputTokens, toolOnlyCall.CostUSD)
	}
	assertEvent(t, events, "context_snapshot", "system", "Earlier fixture work was summarized.")
}

func TestParsePiSessionFile_MockJSONL(t *testing.T) {
	file := filepath.Join("testdata", "harnesses", "pi_session.jsonl")

	events, err := ParsePiSessionFile(file)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := len(events), 10; got != want {
		t.Fatalf("len(events) = %d, want %d", got, want)
	}
	meta := assertEvent(t, events, "session_meta", "system", "")
	if meta.SessionID != "pi-sess-1" || meta.CWD != "/work/pi-fixtures" {
		t.Fatalf("pi meta session/cwd = %q/%q", meta.SessionID, meta.CWD)
	}
	reasoning := assertEvent(t, events, "reasoning", "assistant", "Need to inspect the session format.")
	if reasoning.Provider != "anthropic" || reasoning.Model != "claude-sonnet-4-5" {
		t.Fatalf("pi reasoning provider/model = %q/%q", reasoning.Provider, reasoning.Model)
	}
	if reasoning.InputTokens != 800 || reasoning.OutputTokens != 160 || reasoning.CacheReadTokens != 40 || reasoning.CacheCreateTokens != 8 {
		t.Fatalf("pi reasoning tokens = input:%d output:%d cacheRead:%d cacheWrite:%d",
			reasoning.InputTokens, reasoning.OutputTokens, reasoning.CacheReadTokens, reasoning.CacheCreateTokens)
	}
	deduped := DeduplicateTokens(append([]NormalizedEvent(nil), events...))
	if got, want := sumInputTokens(deduped), int64(800); got != want {
		t.Fatalf("pi input tokens after dedup = %d, want %d", got, want)
	}
	call := assertToolEvent(t, events, "tool_call", "read_file")
	if call.ToolUseID != "call_read_1" || call.ToolInput != `{"path":"session.jsonl"}` {
		t.Fatalf("pi tool call = id:%q input:%q", call.ToolUseID, call.ToolInput)
	}
	assertToolEvent(t, events, "tool_result", "read_file")
	assertToolEvent(t, events, "tool_call", "bash")
	modelChange := assertEvent(t, events, "turn_context", "system", "gpt-5.2")
	if modelChange.Provider != "openai" {
		t.Fatalf("pi model_change provider = %q", modelChange.Provider)
	}
	assertEvent(t, events, "context_snapshot", "system", "Fixture context was compacted.")
}

func createSQLiteFixture(t *testing.T, name string) string {
	t.Helper()
	sqlPath := filepath.Join("testdata", "harnesses", name)
	body, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func createSQLiteFixtureFromSQL(t *testing.T, body string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "fixture.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(body); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func assertEvent(t *testing.T, events []NormalizedEvent, kind, role, text string) NormalizedEvent {
	t.Helper()
	for _, evt := range events {
		if evt.EventKind == kind && evt.ActorRole == role {
			if text == "" || evt.TextContent == text {
				return evt
			}
		}
	}
	t.Fatalf("missing event kind=%q role=%q text=%q in %#v", kind, role, text, events)
	return NormalizedEvent{}
}

func assertToolEvent(t *testing.T, events []NormalizedEvent, kind, name string) NormalizedEvent {
	t.Helper()
	for _, evt := range events {
		if evt.EventKind == kind && evt.ToolName == name {
			return evt
		}
	}
	t.Fatalf("missing tool event kind=%q name=%q in %#v", kind, name, events)
	return NormalizedEvent{}
}

func sumInputTokens(events []NormalizedEvent) int64 {
	var total int64
	for _, evt := range events {
		total += evt.InputTokens
	}
	return total
}
