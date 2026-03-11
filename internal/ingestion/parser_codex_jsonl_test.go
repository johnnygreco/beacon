package ingestion

import (
	"testing"
)

func TestParseCodexJSONL_SessionMeta(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "session_meta",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:00Z",
		"payload": map[string]any{
			"description": "Refactor the auth module",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 1, 0)
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
	if evt.ActorRole != "system" {
		t.Errorf("expected actor_role=system, got %q", evt.ActorRole)
	}
	if evt.TextContent != "Refactor the auth module" {
		t.Errorf("expected description text, got %q", evt.TextContent)
	}
	if evt.SessionID != "codex-sess-1" {
		t.Errorf("expected session_id=codex-sess-1, got %q", evt.SessionID)
	}
	if evt.SourceName != "codex" {
		t.Errorf("expected source_name=codex, got %q", evt.SourceName)
	}
	if evt.Provider != "openai" {
		t.Errorf("expected provider=openai, got %q", evt.Provider)
	}
}

func TestParseCodexJSONL_TurnContext(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "turn_context",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:01Z",
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "turn_context" {
		t.Errorf("expected event_kind=turn_context, got %q", evt.EventKind)
	}
	if evt.ActorRole != "system" {
		t.Errorf("expected actor_role=system, got %q", evt.ActorRole)
	}
}

func TestParseCodexJSONL_ResponseItemMessage(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:02Z",
		"payload": map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": "Here is the refactored code."},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 3, 200)
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
	if evt.TextContent != "Here is the refactored code." {
		t.Errorf("expected text content, got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_ResponseItemMessageTextType(t *testing.T) {
	// Content blocks can also use "text" type instead of "output_text".
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:02Z",
		"payload": map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Using text type block."},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 3, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].TextContent != "Using text type block." {
		t.Errorf("expected text content from text-type block, got %q", events[0].TextContent)
	}
}

func TestParseCodexJSONL_ResponseItemMessageDefaultRole(t *testing.T) {
	// When role is missing, it should default to "assistant".
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:02Z",
		"payload": map[string]any{
			"type": "message",
			"content": []map[string]any{
				{"type": "output_text", "text": "No role specified."},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 3, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].ActorRole != "assistant" {
		t.Errorf("expected default actor_role=assistant, got %q", events[0].ActorRole)
	}
}

func TestParseCodexJSONL_ResponseItemFunctionCall(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:03Z",
		"payload": map[string]any{
			"type":      "function_call",
			"name":      "shell",
			"arguments": `{"cmd": "ls -la"}`,
			"call_id":   "call_abc123",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 4, 300)
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
	if evt.ActorRole != "assistant" {
		t.Errorf("expected actor_role=assistant, got %q", evt.ActorRole)
	}
	if evt.ToolName != "shell" {
		t.Errorf("expected tool_name=shell, got %q", evt.ToolName)
	}
	if evt.ToolInput != `{"cmd": "ls -la"}` {
		t.Errorf("expected tool_input, got %q", evt.ToolInput)
	}
	if evt.ToolUseID != "call_abc123" {
		t.Errorf("expected tool_use_id=call_abc123, got %q", evt.ToolUseID)
	}
	if evt.ToolPhase != "call" {
		t.Errorf("expected tool_phase=call, got %q", evt.ToolPhase)
	}
}

func TestParseCodexJSONL_ResponseItemFunctionCallOutput(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:04Z",
		"payload": map[string]any{
			"type":    "function_call_output",
			"name":    "shell",
			"output":  "total 42\ndrwxr-xr-x  5 user  staff  160 Jun  1 10:00 .",
			"call_id": "call_abc123",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 5, 400)
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
	if evt.ActorRole != "tool" {
		t.Errorf("expected actor_role=tool, got %q", evt.ActorRole)
	}
	if evt.ToolName != "shell" {
		t.Errorf("expected tool_name=shell, got %q", evt.ToolName)
	}
	if evt.ToolOutput != "total 42\ndrwxr-xr-x  5 user  staff  160 Jun  1 10:00 ." {
		t.Errorf("expected tool_output, got %q", evt.ToolOutput)
	}
	if evt.TextContent != evt.ToolOutput {
		t.Errorf("expected text_content to match tool_output, got %q", evt.TextContent)
	}
	if evt.ToolUseID != "call_abc123" {
		t.Errorf("expected tool_use_id=call_abc123, got %q", evt.ToolUseID)
	}
	if evt.ToolPhase != "result" {
		t.Errorf("expected tool_phase=result, got %q", evt.ToolPhase)
	}
}

func TestParseCodexJSONL_ResponseItemReasoning(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:05Z",
		"payload": map[string]any{
			"type": "reasoning",
			"summary": []map[string]any{
				{"type": "summary_text", "text": "Analyzing the codebase structure. "},
				{"type": "summary_text", "text": "Found the main entry point."},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 6, 500)
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
	if evt.TextContent != "Analyzing the codebase structure. Found the main entry point." {
		t.Errorf("expected concatenated summary text, got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_ResponseItemUnknownPayloadType(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:06Z",
		"payload": map[string]any{
			"type": "some_new_type",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 7, 600)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "event_msg" {
		t.Errorf("expected event_kind=event_msg, got %q", evt.EventKind)
	}
	if evt.PayloadType != "some_new_type" {
		t.Errorf("expected payload_type=some_new_type, got %q", evt.PayloadType)
	}
}

func TestParseCodexJSONL_EventMsg(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "event_msg",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:07Z",
		"payload": map[string]any{
			"type":    "status_update",
			"message": "Processing completed successfully",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 8, 700)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "event_msg" {
		t.Errorf("expected event_kind=event_msg, got %q", evt.EventKind)
	}
	if evt.PayloadType != "status_update" {
		t.Errorf("expected payload_type=status_update, got %q", evt.PayloadType)
	}
	if evt.TextContent != "Processing completed successfully" {
		t.Errorf("expected text_content, got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_Compacted(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "compacted",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:01:00Z",
		"payload": map[string]any{
			"summary": "Previous context was compacted",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 9, 800)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "context_snapshot" {
		t.Errorf("expected event_kind=context_snapshot, got %q", evt.EventKind)
	}
	if evt.ActorRole != "system" {
		t.Errorf("expected actor_role=system, got %q", evt.ActorRole)
	}
}

func TestParseCodexJSONL_Error(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "error",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:02:00Z",
		"payload": map[string]any{
			"code":    "rate_limit_exceeded",
			"message": "Too many requests, please try again later",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 10, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "error" {
		t.Errorf("expected event_kind=error, got %q", evt.EventKind)
	}
	if evt.ActorRole != "system" {
		t.Errorf("expected actor_role=system, got %q", evt.ActorRole)
	}
	if evt.ErrorCode != "rate_limit_exceeded" {
		t.Errorf("expected error_code=rate_limit_exceeded, got %q", evt.ErrorCode)
	}
	if evt.ErrorMessage != "Too many requests, please try again later" {
		t.Errorf("expected error_message, got %q", evt.ErrorMessage)
	}
}

func TestParseCodexJSONL_UnknownTopLevelType(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "some_future_event",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:03:00Z",
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 11, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "event_msg" {
		t.Errorf("expected event_kind=event_msg, got %q", evt.EventKind)
	}
	if evt.PayloadType != "some_future_event" {
		t.Errorf("expected payload_type=some_future_event, got %q", evt.PayloadType)
	}
}

func TestParseCodexJSONL_TokenUsageFromPayloadInfo(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:02Z",
		"payload": map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": "Done."},
			},
			"info": map[string]any{
				"last_token_usage": map[string]any{
					"input_tokens":  2500,
					"output_tokens": 450,
				},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 12, 1100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.InputTokens != 2500 {
		t.Errorf("expected input_tokens=2500, got %d", evt.InputTokens)
	}
	if evt.OutputTokens != 450 {
		t.Errorf("expected output_tokens=450, got %d", evt.OutputTokens)
	}
}

func TestParseCodexJSONL_ModelFromPayload(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:02Z",
		"payload": map[string]any{
			"type":  "message",
			"role":  "assistant",
			"model": "o4-mini",
			"content": []map[string]any{
				{"type": "output_text", "text": "Hello."},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 13, 1200)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Model != "o4-mini" {
		t.Errorf("expected model=o4-mini, got %q", events[0].Model)
	}
}

func TestParseCodexJSONL_ModelFromPayloadInfoFallback(t *testing.T) {
	// When payload.model is absent, model should come from payload.info.model.
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:02Z",
		"payload": map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": "Hello."},
			},
			"info": map[string]any{
				"model": "o3-pro",
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 14, 1300)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Model != "o3-pro" {
		t.Errorf("expected model=o3-pro from info fallback, got %q", events[0].Model)
	}
}

func TestParseCodexJSONL_SessionIDFallbackToFilename(t *testing.T) {
	// When session_id is absent, the filename (without extension) is used.
	line := toJSONL(t, map[string]any{
		"type":      "turn_context",
		"timestamp": "2025-06-01T10:00:00Z",
	})

	events, err := ParseCodexJSONL(line, "/home/user/.codex/sessions/my-session-abc.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].SessionID != "my-session-abc" {
		t.Errorf("expected session_id=my-session-abc from filename, got %q", events[0].SessionID)
	}
}

func TestParseCodexJSONL_MalformedJSON(t *testing.T) {
	line := []byte(`{"type": "session_meta", broken json`)

	_, err := ParseCodexJSONL(line, "test.jsonl", 1, 0)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestParseCodexJSONL_SourceCoordinates(t *testing.T) {
	// Verify that source file, line number, and offset are propagated.
	line := toJSONL(t, map[string]any{
		"type":       "turn_context",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:00Z",
	})

	events, err := ParseCodexJSONL(line, "/data/logs/codex.jsonl", 42, 9876)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.SourceFile != "/data/logs/codex.jsonl" {
		t.Errorf("expected source_file, got %q", evt.SourceFile)
	}
	if evt.SourceLineNo != 42 {
		t.Errorf("expected source_line_no=42, got %d", evt.SourceLineNo)
	}
	if evt.SourceOffset != 9876 {
		t.Errorf("expected source_offset=9876, got %d", evt.SourceOffset)
	}
}

func TestParseCodexJSONL_CWDFromSessionMeta(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "session_meta",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:00Z",
		"payload": map[string]any{
			"cwd": "/Users/user/projects/myapp",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].CWD != "/Users/user/projects/myapp" {
		t.Errorf("expected CWD from session_meta, got %q", events[0].CWD)
	}
}

func TestParseCodexJSONL_CWDFromTurnContext(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "turn_context",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:01Z",
		"payload": map[string]any{
			"cwd": "/Users/user/projects/myapp",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].CWD != "/Users/user/projects/myapp" {
		t.Errorf("expected CWD from turn_context, got %q", events[0].CWD)
	}
}

func TestParseCodexJSONL_TaskComplete(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "event_msg",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:05:00Z",
		"payload": map[string]any{
			"type": "task_complete",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 50, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.EventKind != "session_end" {
		t.Errorf("expected event_kind=session_end for task_complete, got %q", evt.EventKind)
	}
	if evt.PayloadType != "task_complete" {
		t.Errorf("expected payload_type=task_complete, got %q", evt.PayloadType)
	}
}

func TestParseCodexJSONL_CachedInputTokens(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "event_msg",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:07Z",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"last_token_usage": map[string]any{
					"input_tokens":        9516,
					"cached_input_tokens": 8064,
					"output_tokens":       157,
				},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 21, 2100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.InputTokens != 9516 {
		t.Errorf("expected input_tokens=9516, got %d", evt.InputTokens)
	}
	if evt.OutputTokens != 157 {
		t.Errorf("expected output_tokens=157, got %d", evt.OutputTokens)
	}
	if evt.CacheReadTokens != 8064 {
		t.Errorf("expected cache_read_tokens=8064, got %d", evt.CacheReadTokens)
	}
}

func TestParseCodexJSONL_DeveloperMessageSkipped(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:02Z",
		"payload": map[string]any{
			"type": "message",
			"role": "developer",
			"content": []map[string]any{
				{"type": "input_text", "text": "system setup instructions"},
			},
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events for developer message, got %d", len(events))
	}
}

func TestParseCodexJSONL_UUIDFromFilename(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":      "turn_context",
		"timestamp": "2025-06-01T10:00:00Z",
	})

	events, err := ParseCodexJSONL(line, "/home/user/.codex/sessions/rollout-2026-03-11T14-53-45-019cde3f-7111-7153-a5e2-e1b7562aea73.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].SessionID != "019cde3f-7111-7153-a5e2-e1b7562aea73" {
		t.Errorf("expected UUID extracted from filename, got %q", events[0].SessionID)
	}
}

func TestParseCodexJSONL_CustomToolCall(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:10Z",
		"payload": map[string]any{
			"type":    "custom_tool_call",
			"name":    "apply_patch",
			"input":   "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new",
			"call_id": "call_custom_1",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 20, 2000)
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
	if evt.ToolName != "apply_patch" {
		t.Errorf("expected tool_name=apply_patch, got %q", evt.ToolName)
	}
	if evt.ToolUseID != "call_custom_1" {
		t.Errorf("expected tool_use_id=call_custom_1, got %q", evt.ToolUseID)
	}
	if evt.ToolPhase != "call" {
		t.Errorf("expected tool_phase=call, got %q", evt.ToolPhase)
	}
	if evt.ToolInput == "" {
		t.Error("expected tool_input to be populated")
	}
}

func TestParseCodexJSONL_CustomToolCallOutput(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:11Z",
		"payload": map[string]any{
			"type":    "custom_tool_call_output",
			"output":  "Patch applied successfully",
			"call_id": "call_custom_1",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 21, 2100)
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
	if evt.ToolUseID != "call_custom_1" {
		t.Errorf("expected tool_use_id=call_custom_1, got %q", evt.ToolUseID)
	}
	if evt.ToolPhase != "result" {
		t.Errorf("expected tool_phase=result, got %q", evt.ToolPhase)
	}
	if evt.TextContent != "Patch applied successfully" {
		t.Errorf("expected text_content, got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_AgentMessage(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "event_msg",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:12Z",
		"payload": map[string]any{
			"type":    "agent_message",
			"message": "Analyzing the repository structure...",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 22, 2200)
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
	if evt.TextContent != "Analyzing the repository structure..." {
		t.Errorf("expected text_content, got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_TaskStarted(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "event_msg",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:00Z",
		"payload": map[string]any{
			"type": "task_started",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 1, 0)
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
	if evt.ActorRole != "system" {
		t.Errorf("expected actor_role=system, got %q", evt.ActorRole)
	}
	if evt.TextContent != "Task started" {
		t.Errorf("expected text 'Task started', got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_UserMessage(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "event_msg",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:01Z",
		"payload": map[string]any{
			"type":    "user_message",
			"message": "Fix the login bug",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 2, 100)
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
	if evt.TextContent != "Fix the login bug" {
		t.Errorf("expected text_content, got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_TurnContextModel(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "turn_context",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:01Z",
		"payload": map[string]any{
			"model": "gpt-5.4",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Model != "gpt-5.4" {
		t.Errorf("expected model=gpt-5.4 from turn_context, got %q", events[0].Model)
	}
}

func TestParseCodexJSONL_FunctionCallOutputSummary(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:04Z",
		"payload": map[string]any{
			"type":    "function_call_output_summary",
			"name":    "shell",
			"output":  "Command completed successfully",
			"call_id": "call_sum_1",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 5, 400)
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
	if evt.ToolName != "shell" {
		t.Errorf("expected tool_name=shell, got %q", evt.ToolName)
	}
	if evt.ToolUseID != "call_sum_1" {
		t.Errorf("expected tool_use_id=call_sum_1, got %q", evt.ToolUseID)
	}
}

func TestParseCodexJSONL_EncryptedReasoning(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:05Z",
		"payload": map[string]any{
			"type":              "reasoning",
			"summary":           []map[string]any{},
			"content":           nil,
			"encrypted_content": "gAAAAABpscDl...",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 6, 500)
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
	if evt.PayloadType != "encrypted" {
		t.Errorf("expected payload_type=encrypted for encrypted reasoning, got %q", evt.PayloadType)
	}
	if evt.TextContent != "" {
		t.Errorf("expected empty text_content for encrypted reasoning, got %q", evt.TextContent)
	}
}

func TestParseCodexJSONL_ForkedFromID(t *testing.T) {
	line := toJSONL(t, map[string]any{
		"type":       "session_meta",
		"session_id": "codex-child-1",
		"timestamp":  "2025-06-01T10:00:00Z",
		"payload": map[string]any{
			"id":             "codex-child-1",
			"forked_from_id": "codex-parent-1",
			"agent_role":     "explorer",
			"agent_nickname": "Raman",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.ParentSessionID != "codex-parent-1" {
		t.Errorf("expected parent_session_id=codex-parent-1, got %q", evt.ParentSessionID)
	}
}

func TestParseCodexJSONL_SpawnAgent(t *testing.T) {
	// spawn_agent should be mapped to Agent tool name
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:10Z",
		"payload": map[string]any{
			"type":      "function_call",
			"name":      "spawn_agent",
			"arguments": `{"agent_type":"explorer","message":"Review the codebase"}`,
			"call_id":   "call_spawn_1",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 20, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.ToolName != "Agent" {
		t.Errorf("expected tool_name=Agent for spawn_agent, got %q", evt.ToolName)
	}
}

func TestParseCodexJSONL_SpawnAgentOutput(t *testing.T) {
	// spawn_agent output should have reformatted text with agentId
	line := toJSONL(t, map[string]any{
		"type":       "response_item",
		"session_id": "codex-sess-1",
		"timestamp":  "2025-06-01T10:00:11Z",
		"payload": map[string]any{
			"type":    "function_call_output",
			"output":  `{"agent_id":"019cdec0-8edd-7013-bdad-7df9b2d917ef","nickname":"Raman"}`,
			"call_id": "call_spawn_1",
		},
	})

	events, err := ParseCodexJSONL(line, "test.jsonl", 21, 2100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	evt := events[0]
	if evt.ToolName != "Agent" {
		t.Errorf("expected tool_name=Agent for spawn_agent output, got %q", evt.ToolName)
	}
	if evt.TextContent != "agentId: 019cdec0-8edd-7013-bdad-7df9b2d917ef" {
		t.Errorf("expected reformatted agentId text, got %q", evt.TextContent)
	}
}
