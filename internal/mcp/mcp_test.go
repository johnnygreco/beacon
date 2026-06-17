package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
)

func testServer() *Server {
	return &Server{
		logger:        slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		contextWindow: defaultOpenContextWindow,
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
		return
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
	instructions, ok := result["instructions"].(string)
	if !ok || !strings.Contains(instructions, "search_sessions") || !strings.Contains(instructions, "create_annotation") || !strings.Contains(instructions, "not current workspace truth") {
		t.Fatalf("instructions = %#v, want Beacon MCP workflow guidance", result["instructions"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("serverInfo is not a map")
	}
	if serverInfo["name"] != "beacon" {
		t.Errorf("expected server name beacon, got %v", serverInfo["name"])
	}
}

func TestRunReturnsWhenContextCancelsClosableInput(t *testing.T) {
	srv := testServer()
	input := &blockingReadCloser{closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.run(ctx, input, io.Discard)
	}()
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not return after context cancellation")
	}
	select {
	case <-input.closed:
	default:
		t.Fatal("input was not closed on context cancellation")
	}
}

type blockingReadCloser struct {
	closed    chan struct{}
	closeOnce sync.Once
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, io.EOF
}

func (r *blockingReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
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
		return
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
		return
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
	if len(tools) != len(expectedMCPToolNames()) {
		t.Fatalf("expected %d tools, got %d", len(expectedMCPToolNames()), len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		names[name] = true
	}
	for _, expected := range expectedMCPToolNames() {
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
		return
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
	if !strings.Contains(result, `"schema":"beacon.mcp.search_sessions.v1"`) || !strings.Contains(result, `"results":[]`) {
		t.Errorf("expected structured empty search results, got %q", result)
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
	for _, expected := range []string{`"event_id":"event:uid-abc-123"`, `"session_id":"session:session-123456789012"`, `"open_ref":{"type":"event"`, `"event_kind":"message"`, `"tool_name":"grep"`, `"model":"gpt-4"`, `"score":1.5`, "Hello world preview"} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %q in output", expected)
		}
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
	for _, expected := range []string{`"schema":"beacon.mcp.open.v1"`, `"event_id":"event:uid-2"`, `"tool_name":"weather"`, `"model":"gpt-4"`, `"tokens":150`, `"target":true`} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %q in output", expected)
		}
	}
}

func TestMCPFormattersEscapeHTMLPayloads(t *testing.T) {
	payload := `<script>alert(1)</script><img src=x onerror="alert(1)">`
	searchOutput := FormatSearchResults([]search.SearchResult{{
		EventUID:    "uid-xss",
		SessionID:   "session-xss",
		EventKind:   "message",
		TextPreview: payload,
		Timestamp:   time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	}})
	openOutput := FormatOpenContext([]contextEvent{{
		EventUID:    "uid-xss",
		EventKind:   "message",
		ActorRole:   "user",
		TextPreview: payload,
		Timestamp:   time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	}}, 0)

	for _, output := range []string{searchOutput, openOutput} {
		if strings.Contains(output, "<script") || strings.Contains(output, "<img") {
			t.Fatalf("MCP formatter emitted raw HTML payload: %s", output)
		}
		if !strings.Contains(output, `\u003cscript\u003ealert(1)\u003c/script\u003e`) {
			t.Fatalf("MCP formatter did not HTML-escape JSON payload: %s", output)
		}
	}
}

func TestFormatSessionList_Empty(t *testing.T) {
	result := FormatSessionList(nil)
	if !strings.Contains(result, `"schema":"beacon.mcp.list_sessions.v1"`) || !strings.Contains(result, `"results":[]`) {
		t.Errorf("expected structured empty session list, got %q", result)
	}
}

func TestFormatSessionList_WithSessions(t *testing.T) {
	sessions := []sessionInfo{
		{
			SessionID:     "sess-abcdef123456",
			SourceName:    "claude-code",
			Provider:      "anthropic",
			StartedAt:     time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			EndedAt:       time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			EventCount:    42,
			TurnCount:     10,
			TotalTokens:   5000,
			ToolCallCount: 8,
			MCPCallCount:  2,
			ErrorCount:    1,
			LastModel:     "gpt-4",
			WorkingDir:    "/work/beacon",
		},
	}

	output := FormatSessionList(sessions, sessionListMetadata{ResultCount: 1, TotalMatchingCount: 3, Limit: 1, ResultComplete: false, NextCursor: "offset:1"})
	for _, expected := range []string{`"session_id":"session:sess-abcdef123456"`, `"source_name":"claude-code"`, `"provider":"anthropic"`, `"event_count":42`, `"turn_count":10`, `"total_tokens":5000`, `"tool_call_count":8`, `"mcp_call_count":2`, `"error_count":1`, `"last_model":"gpt-4"`, `"working_dir":"/work/beacon"`, `"total_matching_count":3`, `"next_cursor":"offset:1"`} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %q in output", expected)
		}
	}
}

func TestToolDefinitions(t *testing.T) {
	defs := toolDefinitions()
	if len(defs) != len(expectedMCPToolNames()) {
		t.Fatalf("expected %d tool definitions, got %d", len(expectedMCPToolNames()), len(defs))
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

	for _, expected := range expectedMCPToolNames() {
		if !names[expected] {
			t.Errorf("missing tool definition: %s", expected)
		}
	}
}

func expectedMCPToolNames() []string {
	return []string{
		"search_sessions",
		"open",
		"list_sessions",
		"usage_summary",
		"create_annotation",
		"update_annotation",
		"list_annotations",
		"get_annotation",
		"delete_annotation",
	}
}
