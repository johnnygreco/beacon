package mcp

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
)

func testServer() *Server {
	return &Server{
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func TestDispatch_Initialize(t *testing.T) {
	srv := testServer()
	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	}

	resp := srv.dispatch(t.Context(), req)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if _, ok := result["protocolVersion"]; !ok {
		t.Error("missing protocolVersion in result")
	}
	if _, ok := result["capabilities"]; !ok {
		t.Error("missing capabilities in result")
	}
	if _, ok := result["serverInfo"]; !ok {
		t.Error("missing serverInfo in result")
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("serverInfo is not a map")
	}
	if serverInfo["name"] != "beacon" {
		t.Errorf("expected server name beacon, got %v", serverInfo["name"])
	}
}

func TestDispatch_Initialized(t *testing.T) {
	srv := testServer()
	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "initialized",
	}

	resp := srv.dispatch(t.Context(), req)
	if resp != nil {
		t.Fatalf("expected nil response for notification, got %+v", resp)
	}
}

func TestDispatch_Ping(t *testing.T) {
	srv := testServer()
	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "ping",
	}

	resp := srv.dispatch(t.Context(), req)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestDispatch_ToolsList(t *testing.T) {
	srv := testServer()
	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
		Method:  "tools/list",
	}

	resp := srv.dispatch(t.Context(), req)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}

	tools, ok := result["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected tools to be []map[string]any, got %T", result["tools"])
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		names[name] = true
	}
	for _, expected := range []string{"search", "open", "list_sessions"} {
		if !names[expected] {
			t.Errorf("missing tool: %s", expected)
		}
	}
}

func TestDispatch_UnknownMethod(t *testing.T) {
	srv := testServer()
	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`4`),
		Method:  "nonexistent/method",
	}

	resp := srv.dispatch(t.Context(), req)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

func TestDispatch_UnknownMethodNotification(t *testing.T) {
	srv := testServer()
	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		// No ID — this is a notification
		Method: "nonexistent/notification",
	}

	resp := srv.dispatch(t.Context(), req)
	if resp != nil {
		t.Fatalf("expected nil response for unknown notification, got %+v", resp)
	}
}

func TestFormatSearchResults_Empty(t *testing.T) {
	result := FormatSearchResults(nil)
	if result != "No results found." {
		t.Errorf("expected 'No results found.', got %q", result)
	}
}

func TestFormatSearchResults_WithResults(t *testing.T) {
	results := []search.SearchResult{
		{
			EventUID:    "uid-abc-123",
			SessionID:   "session-123456789012",
			EventKind:   "message",
			TextPreview: "Hello world preview",
			Score:       1.5,
			Timestamp:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			ToolName:    "grep",
			Model:       "gpt-4",
		},
	}

	output := FormatSearchResults(results)
	if !strings.Contains(output, "Found 1 results") {
		t.Error("expected 'Found 1 results' in output")
	}
	if !strings.Contains(output, "[message]") {
		t.Error("expected '[message]' in output")
	}
	if !strings.Contains(output, "session:session-1234") {
		t.Error("expected truncated session ID in output")
	}
	if !strings.Contains(output, "tool:grep") {
		t.Error("expected 'tool:grep' in output")
	}
	if !strings.Contains(output, "model:gpt-4") {
		t.Error("expected 'model:gpt-4' in output")
	}
	if !strings.Contains(output, "score: 1.50") {
		t.Error("expected 'score: 1.50' in output")
	}
	if !strings.Contains(output, "Hello world preview") {
		t.Error("expected text preview in output")
	}
	if !strings.Contains(output, `open(event_uid="uid-abc-123")`) {
		t.Error("expected open link in output")
	}
}

func TestFormatOpenContext(t *testing.T) {
	events := []contextEvent{
		{
			EventUID:    "uid-1",
			EventKind:   "message",
			ActorRole:   "user",
			TextPreview: "What is the weather?",
			Timestamp:   time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			EventUID:    "uid-2",
			EventKind:   "message",
			ActorRole:   "assistant",
			TextPreview: "Let me check that for you.",
			ToolName:    "weather",
			Model:       "gpt-4",
			Tokens:      150,
			Timestamp:   time.Date(2025, 1, 15, 10, 0, 5, 0, time.UTC),
		},
		{
			EventUID:    "uid-3",
			EventKind:   "tool_result",
			ActorRole:   "tool",
			TextPreview: "Sunny, 72F",
			ToolName:    "weather",
			Timestamp:   time.Date(2025, 1, 15, 10, 0, 10, 0, time.UTC),
		},
	}

	output := FormatOpenContext(events, 1) // target is the assistant message
	if !strings.Contains(output, ">>>") {
		t.Error("expected '>>>' target marker in output")
	}
	if !strings.Contains(output, ">>> TARGET <<<") {
		t.Error("expected '>>> TARGET <<<' in output")
	}
	if !strings.Contains(output, "tool:weather") {
		t.Error("expected 'tool:weather' in output")
	}
	if !strings.Contains(output, "model:gpt-4") {
		t.Error("expected 'model:gpt-4' in output")
	}
	if !strings.Contains(output, "150 tok") {
		t.Error("expected '150 tok' in output")
	}

	// The non-target events should have "  " prefix (two spaces), not ">>>"
	lines := strings.Split(output, "\n")
	firstEventLine := ""
	for _, l := range lines {
		if strings.Contains(l, "[message] user") {
			firstEventLine = l
			break
		}
	}
	if firstEventLine == "" {
		t.Fatal("could not find first event line")
	}
	if strings.HasPrefix(firstEventLine, ">>>") {
		t.Error("first event should not have target marker")
	}
}

func TestFormatSessionList_Empty(t *testing.T) {
	result := FormatSessionList(nil)
	if result != "No sessions found." {
		t.Errorf("expected 'No sessions found.', got %q", result)
	}
}

func TestFormatSessionList_WithSessions(t *testing.T) {
	sessions := []sessionInfo{
		{
			SessionID:     "sess-abcdef123456",
			SourceName:    "claude-code",
			StartedAt:     time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			EndedAt:       time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			EventCount:    42,
			TurnCount:     10,
			TotalTokens:   5000,
			ToolCallCount: 8,
			MCPCallCount:  2,
			ErrorCount:    1,
			LastModel:     "gpt-4",
		},
	}

	output := FormatSessionList(sessions)
	if !strings.Contains(output, "1 sessions") {
		t.Error("expected '1 sessions' in output")
	}
	if !strings.Contains(output, "[claude-code]") {
		t.Error("expected '[claude-code]' in output")
	}
	if !strings.Contains(output, "sess-abcdef1") {
		t.Error("expected truncated session ID in output")
	}
	if !strings.Contains(output, "events:42") {
		t.Error("expected 'events:42' in output")
	}
	if !strings.Contains(output, "turns:10") {
		t.Error("expected 'turns:10' in output")
	}
	if !strings.Contains(output, "tokens:5000") {
		t.Error("expected 'tokens:5000' in output")
	}
	if !strings.Contains(output, "tools:8") {
		t.Error("expected 'tools:8' in output")
	}
	if !strings.Contains(output, "mcp:2") {
		t.Error("expected 'mcp:2' in output")
	}
	if !strings.Contains(output, "errors:1") {
		t.Error("expected 'errors:1' in output")
	}
	if !strings.Contains(output, "model:gpt-4") {
		t.Error("expected 'model:gpt-4' in output")
	}
	if !strings.Contains(output, "dur:30m0s") {
		t.Error("expected duration in output")
	}
}

func TestToolDefinitions(t *testing.T) {
	defs := toolDefinitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 tool definitions, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		name, _ := d["name"].(string)
		names[name] = true
		// Verify each tool has required fields
		if _, ok := d["description"]; !ok {
			t.Errorf("tool %s missing description", name)
		}
		if _, ok := d["inputSchema"]; !ok {
			t.Errorf("tool %s missing inputSchema", name)
		}
	}

	for _, expected := range []string{"search", "open", "list_sessions"} {
		if !names[expected] {
			t.Errorf("missing tool definition: %s", expected)
		}
	}
}
