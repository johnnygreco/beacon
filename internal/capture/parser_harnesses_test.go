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
}

func TestParseOpenCodeSQLite_MockStateDB(t *testing.T) {
	dbPath := createSQLiteFixture(t, "opencode_state.sql")

	events, err := ParseOpenCodeSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := len(events), 7; got != want {
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
	if got, want := sumInputTokens(deduped), int64(900); got != want {
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
