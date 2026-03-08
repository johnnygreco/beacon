package ingestion

import (
	"encoding/json"
	"testing"
)

// helper to build a JSONL line from a map.
func toJSONL(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseClaudeJSONL_UserMessageStringContent(t *testing.T) {
	// Real Claude Code format: user prompts have content as a plain string.
	line := toJSONL(t, map[string]any{
		"sessionId":  "sess-1",
		"uuid":       "uuid-1-user",
		"parentUuid": "",
		"timestamp":  "2025-01-01T00:00:00Z",
		"type":       "user",
		"message": map[string]any{
			"role":    "user",
			"content": "Please help me refactor this function",
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "message" {
		t.Errorf("expected event_kind=message, got %q", evt.EventKind)
	}
	if evt.ActorRole != "user" {
		t.Errorf("expected actor_role=user, got %q", evt.ActorRole)
	}
	if evt.TextContent != "Please help me refactor this function" {
		t.Errorf("expected text_content to be the user prompt, got %q", evt.TextContent)
	}
}

func TestParseClaudeJSONL_UserMessageArrayContent(t *testing.T) {
	// Some user messages use an array with text blocks (simulator format).
	line := toJSONL(t, map[string]any{
		"sessionId":  "sess-1",
		"uuid":       "uuid-1-user",
		"parentUuid": "",
		"timestamp":  "2025-01-01T00:00:00Z",
		"type":       "human",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": "Help me with this task"},
			},
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "message" {
		t.Errorf("expected event_kind=message, got %q", evt.EventKind)
	}
	if evt.ActorRole != "user" {
		t.Errorf("expected actor_role=user, got %q", evt.ActorRole)
	}
	if evt.TextContent != "Help me with this task" {
		t.Errorf("expected text_content, got %q", evt.TextContent)
	}
}

func TestParseClaudeJSONL_AssistantTextBlock(t *testing.T) {
	// Assistant messages have content as an array with a single text block.
	line := toJSONL(t, map[string]any{
		"sessionId":  "sess-1",
		"uuid":       "uuid-2-assistant",
		"parentUuid": "uuid-1-user",
		"timestamp":  "2025-01-01T00:00:01Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "I'll help you refactor that function."},
			},
			"usage": map[string]any{
				"input_tokens":  1000,
				"output_tokens": 200,
			},
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "message" {
		t.Errorf("expected event_kind=message, got %q", evt.EventKind)
	}
	if evt.ActorRole != "assistant" {
		t.Errorf("expected actor_role=assistant, got %q", evt.ActorRole)
	}
	if evt.TextContent != "I'll help you refactor that function." {
		t.Errorf("expected text content, got %q", evt.TextContent)
	}
	if evt.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model, got %q", evt.Model)
	}
	if evt.InputTokens != 1000 {
		t.Errorf("expected input_tokens=1000, got %d", evt.InputTokens)
	}
	if evt.OutputTokens != 200 {
		t.Errorf("expected output_tokens=200, got %d", evt.OutputTokens)
	}
}

func TestParseClaudeJSONL_AssistantThinkingBlock(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"uuid":      "uuid-3",
		"timestamp": "2025-01-01T00:00:01Z",
		"type":      "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Let me think about this..."},
			},
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 3, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "reasoning" {
		t.Errorf("expected event_kind=reasoning, got %q", evt.EventKind)
	}
	if evt.ActorRole != "assistant" {
		t.Errorf("expected actor_role=assistant, got %q", evt.ActorRole)
	}
	if evt.TextContent != "Let me think about this..." {
		t.Errorf("expected thinking text, got %q", evt.TextContent)
	}
}

func TestParseClaudeJSONL_AssistantToolUse(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"uuid":      "uuid-4",
		"timestamp": "2025-01-01T00:00:02Z",
		"type":      "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "tool_use", "id": "toolu_123", "name": "Read", "input": map[string]any{"file_path": "/src/main.go"}},
			},
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 4, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "tool_call" {
		t.Errorf("expected event_kind=tool_call, got %q", evt.EventKind)
	}
	if evt.ToolName != "Read" {
		t.Errorf("expected tool_name=Read, got %q", evt.ToolName)
	}
	if evt.ToolUseID != "toolu_123" {
		t.Errorf("expected tool_use_id, got %q", evt.ToolUseID)
	}
	if evt.ToolInput == "" {
		t.Error("expected tool_input to be non-empty")
	}
}

func TestParseClaudeJSONL_ToolResult(t *testing.T) {
	// Tool results come back under role=user with tool_result content blocks.
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"uuid":      "uuid-5",
		"timestamp": "2025-01-01T00:00:03Z",
		"type":      "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_123",
					"name":        "Read",
					"content":     "package main\n\nfunc main() {}",
				},
			},
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 5, 400)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "tool_result" {
		t.Errorf("expected event_kind=tool_result, got %q", evt.EventKind)
	}
	if evt.ToolName != "Read" {
		t.Errorf("expected tool_name=Read, got %q", evt.ToolName)
	}
	if evt.TextContent != "package main\n\nfunc main() {}" {
		t.Errorf("expected text_content from tool result, got %q", evt.TextContent)
	}
}

func TestParseClaudeJSONL_ToolResultAndTextBlock(t *testing.T) {
	// Some user messages have both tool_result and text blocks.
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"uuid":      "uuid-6",
		"timestamp": "2025-01-01T00:00:04Z",
		"type":      "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_456",
					"name":        "Bash",
					"content":     "exit code 0",
				},
				{
					"type": "text",
					"text": "Now please continue with the next step",
				},
			},
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 6, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// First event: tool_result
	if events[0].EventKind != "tool_result" {
		t.Errorf("event[0]: expected event_kind=tool_result, got %q", events[0].EventKind)
	}

	// Second event: user text message
	if events[1].EventKind != "message" {
		t.Errorf("event[1]: expected event_kind=message, got %q", events[1].EventKind)
	}
	if events[1].ActorRole != "user" {
		t.Errorf("event[1]: expected actor_role=user, got %q", events[1].ActorRole)
	}
	if events[1].TextContent != "Now please continue with the next step" {
		t.Errorf("event[1]: expected text content, got %q", events[1].TextContent)
	}
}

func TestParseClaudeJSONL_NoMessageField(t *testing.T) {
	// Lines like queue-operation, progress, file-history-snapshot have no message field.
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"timestamp": "2025-01-01T00:00:00Z",
		"type":      "progress",
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventKind != "event_msg" {
		t.Errorf("expected event_kind=event_msg, got %q", events[0].EventKind)
	}
}

func TestParseClaudeJSONL_CWDExtracted(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"timestamp": "2025-01-01T00:00:00Z",
		"type":      "user",
		"cwd":       "/Users/donnie/projects/code/technodrome",
		"message": map[string]any{
			"role":    "user",
			"content": "hello",
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].CWD != "/Users/donnie/projects/code/technodrome" {
		t.Errorf("expected CWD to be set, got %q", events[0].CWD)
	}
}

func TestParseClaudeJSONL_CWDEmpty(t *testing.T) {
	// Lines without cwd should have empty CWD
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"timestamp": "2025-01-01T00:00:00Z",
		"type":      "user",
		"message": map[string]any{
			"role":    "user",
			"content": "hello",
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].CWD != "" {
		t.Errorf("expected empty CWD, got %q", events[0].CWD)
	}
}

func TestParseClaudeJSONL_Summary(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"sessionId": "sess-1",
		"timestamp": "2025-01-01T00:01:00Z",
		"type":      "summary",
		"summary":   "Refactored the main function",
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 10, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "session_meta" {
		t.Errorf("expected event_kind=session_meta, got %q", evt.EventKind)
	}
	if evt.TextContent != "Refactored the main function" {
		t.Errorf("expected summary text, got %q", evt.TextContent)
	}
}

func TestParseClaudeJSONL_MessageUUIDPropagated(t *testing.T) {
	// Verify that the uuid field is set as MessageUUID on all events.
	line := toJSONL(t, map[string]any{
		"sessionId":  "sess-1",
		"uuid":       "msg-uuid-abc",
		"parentUuid": "parent-uuid-xyz",
		"timestamp":  "2025-01-01T00:00:01Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Let me think..."},
			},
			"usage": map[string]any{
				"input_tokens":  500,
				"output_tokens": 100,
			},
		},
	})

	events, err := ParseClaudeJSONL(line, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].MessageUUID != "msg-uuid-abc" {
		t.Errorf("expected MessageUUID=msg-uuid-abc, got %q", events[0].MessageUUID)
	}
}

func TestParseClaudeJSONL_DeduplicateTokensAcrossLines(t *testing.T) {
	// Simulates two JSONL lines from the same API call (thinking + text)
	// with identical usage values. After DeduplicateTokens, only the last
	// event should retain token values.
	thinkingLine := toJSONL(t, map[string]any{
		"sessionId":  "sess-1",
		"uuid":       "msg-uuid-same",
		"parentUuid": "parent-1",
		"timestamp":  "2025-01-01T00:00:01Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "This is a very long thinking block with lots of reasoning..."},
			},
			"usage": map[string]any{
				"input_tokens":                 3,
				"output_tokens":                8,
				"cache_read_input_tokens":      5708,
				"cache_creation_input_tokens":  3882,
			},
		},
	})

	textLine := toJSONL(t, map[string]any{
		"sessionId":  "sess-1",
		"uuid":       "msg-uuid-same",
		"parentUuid": "parent-1",
		"timestamp":  "2025-01-01T00:00:01Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Here is my response."},
			},
			"usage": map[string]any{
				"input_tokens":                 3,
				"output_tokens":                8,
				"cache_read_input_tokens":      5708,
				"cache_creation_input_tokens":  3882,
			},
		},
	})

	// Parse both lines
	events1, err := ParseClaudeJSONL(thinkingLine, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	events2, err := ParseClaudeJSONL(textLine, "test.jsonl", 2, 500)
	if err != nil {
		t.Fatal(err)
	}

	// Before dedup: both events have tokens
	allEvents := append(events1, events2...)
	if allEvents[0].InputTokens != 3 {
		t.Errorf("before dedup: event[0] input should be 3, got %d", allEvents[0].InputTokens)
	}
	if allEvents[1].InputTokens != 3 {
		t.Errorf("before dedup: event[1] input should be 3, got %d", allEvents[1].InputTokens)
	}

	// After dedup: only the last event keeps tokens
	allEvents = DeduplicateTokens(allEvents)

	if allEvents[0].InputTokens != 0 || allEvents[0].OutputTokens != 0 ||
		allEvents[0].CacheReadTokens != 0 || allEvents[0].CacheCreateTokens != 0 {
		t.Errorf("after dedup: thinking event should have zeroed tokens: input=%d output=%d cache_read=%d cache_create=%d",
			allEvents[0].InputTokens, allEvents[0].OutputTokens,
			allEvents[0].CacheReadTokens, allEvents[0].CacheCreateTokens)
	}

	if allEvents[1].InputTokens != 3 || allEvents[1].OutputTokens != 8 ||
		allEvents[1].CacheReadTokens != 5708 || allEvents[1].CacheCreateTokens != 3882 {
		t.Errorf("after dedup: text event should keep tokens: input=%d output=%d cache_read=%d cache_create=%d",
			allEvents[1].InputTokens, allEvents[1].OutputTokens,
			allEvents[1].CacheReadTokens, allEvents[1].CacheCreateTokens)
	}
}
