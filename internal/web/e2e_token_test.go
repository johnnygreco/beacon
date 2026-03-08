package web

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/ingestion"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

// toJSONL marshals a map to a JSONL line for the parser.
func toJSONL(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// insertNormalizedEvents simulates the batcher: converts NormalizedEvents to
// models.Events and inserts them into the database.
func insertNormalizedEvents(t *testing.T, db *database.DB, events []ingestion.NormalizedEvent) {
	t.Helper()
	ctx := context.Background()
	for i, evt := range events {
		uid := fmt.Sprintf("e2e-%s-%d", evt.SessionID, i)
		preview := evt.TextContent
		if len(preview) > 320 {
			preview = preview[:320]
		}
		event := &models.Event{
			EventUID:          uid,
			SessionID:         evt.SessionID,
			SourceName:        evt.SourceName,
			Provider:          evt.Provider,
			EventKind:         evt.EventKind,
			PayloadType:       evt.PayloadType,
			ActorRole:         evt.ActorRole,
			Timestamp:         evt.Timestamp,
			TextContent:       evt.TextContent,
			TextPreview:       preview,
			ToolName:          evt.ToolName,
			Model:             evt.Model,
			InputTokens:       evt.InputTokens,
			OutputTokens:      evt.OutputTokens,
			CacheReadTokens:   evt.CacheReadTokens,
			CacheCreateTokens: evt.CacheCreateTokens,
			DurationMs:        evt.DurationMs,
			EventVersion:      1,
			SourceFile:        evt.SourceFile,
			SourceLineNo:      evt.SourceLineNo,
			SourceOffset:      evt.SourceOffset,
		}
		if err := database.InsertEvent(ctx, db, event); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}

		// Insert tool I/O for tool_call/tool_result
		if evt.ToolPhase != "" && (evt.ToolInput != "" || evt.ToolOutput != "") {
			tio := &models.ToolIO{
				EventUID:      uid,
				ToolName:      evt.ToolName,
				ToolPhase:     evt.ToolPhase,
				InputJSON:     evt.ToolInput,
				OutputJSON:    evt.ToolOutput,
				InputPreview:  preview,
				OutputPreview: preview,
			}
			if err := database.InsertToolIO(ctx, db, tio); err != nil {
				t.Fatalf("insert tool io %d: %v", i, err)
			}
		}
	}
}

// TestE2E_TokenAccuracy_SingleTextResponse tests the full pipeline for
// a simple single-text-block assistant response.
func TestE2E_TokenAccuracy_SingleTextResponse(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Step 1: Parse — a user message then a single-block assistant response
	userLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-1",
		"uuid":       "uuid-user-1",
		"parentUuid": "",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "user",
		"message": map[string]any{
			"role":    "user",
			"content": "What is 2+2?",
		},
	})
	assistantLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-1",
		"uuid":       "uuid-asst-1",
		"parentUuid": "uuid-user-1",
		"timestamp":  "2025-06-01T10:00:01Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "The answer is 4."},
			},
			"usage": map[string]any{
				"input_tokens":  500,
				"output_tokens": 120,
			},
		},
	})

	userEvents, err := ingestion.ParseClaudeJSONL(userLine, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	asstEvents, err := ingestion.ParseClaudeJSONL(assistantLine, "test.jsonl", 2, 100)
	if err != nil {
		t.Fatal(err)
	}

	allEvents := append(userEvents, asstEvents...)

	// Step 2: Dedup — should be a no-op for single-block responses
	allEvents = ingestion.DeduplicateTokens(allEvents)

	// Verify parser output
	if len(allEvents) != 2 {
		t.Fatalf("expected 2 events, got %d", len(allEvents))
	}
	asst := allEvents[1]
	if asst.InputTokens != 500 {
		t.Errorf("parser: expected input_tokens=500, got %d", asst.InputTokens)
	}
	if asst.OutputTokens != 120 {
		t.Errorf("parser: expected output_tokens=120, got %d", asst.OutputTokens)
	}

	// Step 3: Insert into DB
	insertNormalizedEvents(t, db, allEvents)

	// Step 4: Query — dashboard metrics
	metrics := QueryDashboardMetrics(ctx, db.ReadPool)
	var totalTokensMetric views.MetricData
	for _, m := range metrics {
		if m.Label == "Total Tokens" {
			totalTokensMetric = m
			break
		}
	}
	// Total = input + output = 500 + 120 = 620
	if totalTokensMetric.Value != "620" {
		t.Errorf("dashboard total tokens: expected 620, got %q", totalTokensMetric.Value)
	}

	// Step 5: Query — session summary
	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	sess := allSessions[0]
	if sess.TotalTokens != 620 {
		t.Errorf("session total_tokens: expected 620, got %d", sess.TotalTokens)
	}
	if sess.InputTokens != 500 {
		t.Errorf("session input_tokens: expected 500, got %d", sess.InputTokens)
	}
	if sess.OutputTokens != 120 {
		t.Errorf("session output_tokens: expected 120, got %d", sess.OutputTokens)
	}

	// Step 6: Query — conversation trace tokens
	chatTurns, turns := QuerySessionConversation(ctx, db.ReadPool, "tok-sess-1")
	if len(turns) == 0 {
		t.Fatal("expected at least 1 turn in timeline")
	}
	// The turn containing the assistant response should have 620 total tokens
	var turnTokens int64
	for _, td := range turns {
		turnTokens += td.TotalTokens
	}
	if turnTokens != 620 {
		t.Errorf("turn total_tokens sum: expected 620, got %d", turnTokens)
	}

	// Chat turns should also reflect correct tokens
	var chatTurnTokens int64
	for _, ct := range chatTurns {
		chatTurnTokens += ct.TotalTokens
	}
	if chatTurnTokens != 620 {
		t.Errorf("chat turn total_tokens sum: expected 620, got %d", chatTurnTokens)
	}
}

// TestE2E_TokenAccuracy_ThinkingPlusText tests the core bug scenario:
// two JSONL lines from the same API call (thinking + text) with identical
// usage values. After deduplication, tokens should be counted exactly once.
func TestE2E_TokenAccuracy_ThinkingPlusText(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Simulate the real Claude Code JSONL pattern:
	// Line 1: thinking block with low output_tokens (streaming artifact)
	// Line 2: text block with same usage values
	// Both share the same UUID (same API call)
	userLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-2",
		"uuid":       "uuid-user-2",
		"parentUuid": "",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "user",
		"message": map[string]any{
			"role":    "user",
			"content": "Explain recursion",
		},
	})
	thinkingLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-2",
		"uuid":       "uuid-asst-think",
		"parentUuid": "uuid-user-2",
		"timestamp":  "2025-06-01T10:00:01Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Let me think about how to explain recursion clearly. Recursion is when a function calls itself..."},
			},
			"usage": map[string]any{
				"input_tokens":                3,
				"output_tokens":               8,
				"cache_read_input_tokens":     5708,
				"cache_creation_input_tokens": 3882,
			},
		},
	})
	textLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-2",
		"uuid":       "uuid-asst-think",
		"parentUuid": "uuid-user-2",
		"timestamp":  "2025-06-01T10:00:01Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Recursion is a programming technique where a function calls itself to solve smaller subproblems."},
			},
			"usage": map[string]any{
				"input_tokens":                3,
				"output_tokens":               8,
				"cache_read_input_tokens":     5708,
				"cache_creation_input_tokens": 3882,
			},
		},
	})

	// Parse all lines
	userEvents, err := ingestion.ParseClaudeJSONL(userLine, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	thinkEvents, err := ingestion.ParseClaudeJSONL(thinkingLine, "test.jsonl", 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	textEvents, err := ingestion.ParseClaudeJSONL(textLine, "test.jsonl", 3, 200)
	if err != nil {
		t.Fatal(err)
	}

	allEvents := append(userEvents, append(thinkEvents, textEvents...)...)

	// Before dedup: both thinking and text events have tokens (Bug 1)
	if thinkEvents[0].InputTokens != 3 {
		t.Errorf("before dedup: thinking input should be 3, got %d", thinkEvents[0].InputTokens)
	}
	if textEvents[0].InputTokens != 3 {
		t.Errorf("before dedup: text input should be 3, got %d", textEvents[0].InputTokens)
	}

	// Step 2: Dedup — this is where the fix happens
	allEvents = ingestion.DeduplicateTokens(allEvents)

	// After dedup: thinking event should have zeroed tokens
	// (allEvents[0] = user, allEvents[1] = thinking, allEvents[2] = text)
	if allEvents[1].InputTokens != 0 || allEvents[1].OutputTokens != 0 ||
		allEvents[1].CacheReadTokens != 0 || allEvents[1].CacheCreateTokens != 0 {
		t.Errorf("after dedup: thinking event should have zeroed tokens: input=%d output=%d cache_read=%d cache_create=%d",
			allEvents[1].InputTokens, allEvents[1].OutputTokens,
			allEvents[1].CacheReadTokens, allEvents[1].CacheCreateTokens)
	}
	// Text event should keep its tokens
	if allEvents[2].InputTokens != 3 || allEvents[2].OutputTokens != 8 ||
		allEvents[2].CacheReadTokens != 5708 || allEvents[2].CacheCreateTokens != 3882 {
		t.Errorf("after dedup: text event should keep tokens: input=%d output=%d cache_read=%d cache_create=%d",
			allEvents[2].InputTokens, allEvents[2].OutputTokens,
			allEvents[2].CacheReadTokens, allEvents[2].CacheCreateTokens)
	}

	// Step 3: Insert into DB
	insertNormalizedEvents(t, db, allEvents)

	// Step 4: Verify session summary — tokens counted ONCE, not doubled
	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	sess := allSessions[0]
	// Total = input(3) + output(8) = 11 (NOT 22 from double-counting)
	if sess.TotalTokens != 11 {
		t.Errorf("session total_tokens: expected 11 (not double-counted), got %d", sess.TotalTokens)
	}
	if sess.InputTokens != 3 {
		t.Errorf("session input_tokens: expected 3 (not 6), got %d", sess.InputTokens)
	}
	if sess.OutputTokens != 8 {
		t.Errorf("session output_tokens: expected 8 (not 16), got %d", sess.OutputTokens)
	}
	if sess.CacheReadTokens != 5708 {
		t.Errorf("session cache_read_tokens: expected 5708 (not 11416), got %d", sess.CacheReadTokens)
	}
	if sess.CacheCreateTokens != 3882 {
		t.Errorf("session cache_create_tokens: expected 3882 (not 7764), got %d", sess.CacheCreateTokens)
	}

	// Step 5: Verify dashboard metrics
	metrics := QueryDashboardMetrics(ctx, db.ReadPool)
	for _, m := range metrics {
		if m.Label == "Total Tokens" {
			if m.Value != "11" {
				t.Errorf("dashboard total tokens: expected 11, got %q", m.Value)
			}
		}
	}

	// Step 6: Verify tokens-by-model
	byModel := QueryTokensByModelSummary(ctx, db.ReadPool)
	if len(byModel) != 1 {
		t.Fatalf("expected 1 model, got %d", len(byModel))
	}
	if byModel[0].Input != 3 {
		t.Errorf("by-model input: expected 3, got %d", byModel[0].Input)
	}
	if byModel[0].Output != 8 {
		t.Errorf("by-model output: expected 8, got %d", byModel[0].Output)
	}
	if byModel[0].Total != 11 {
		t.Errorf("by-model total: expected 11, got %d", byModel[0].Total)
	}

	// Step 7: Verify conversation trace
	chatTurns, _ := QuerySessionConversation(ctx, db.ReadPool, "tok-sess-2")
	var chatTurnTokens int64
	for _, ct := range chatTurns {
		chatTurnTokens += ct.TotalTokens
	}
	if chatTurnTokens != 11 {
		t.Errorf("chat turn tokens: expected 11, got %d", chatTurnTokens)
	}

	// Verify reasoning block has 0 tokens (thinking event got zeroed by dedup)
	for _, ct := range chatTurns {
		for _, block := range ct.Blocks {
			if block.Kind == views.ChatBlockReasoning {
				reasoningTokens := views.SumTokens(block.Messages)
				if reasoningTokens != 0 {
					t.Errorf("reasoning block tokens: expected 0 (deduped), got %d", reasoningTokens)
				}
			}
		}
	}
}

// TestE2E_TokenAccuracy_ToolUseWithResults tests token counting for
// a conversation with tool calls and tool results.
func TestE2E_TokenAccuracy_ToolUseWithResults(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	ts := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)

	// User asks to read a file
	userLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-3",
		"uuid":       "uuid-user-3",
		"parentUuid": "",
		"timestamp":  ts.Format(time.RFC3339),
		"type":       "user",
		"message": map[string]any{
			"role":    "user",
			"content": "Read main.go",
		},
	})

	// Assistant makes a tool call
	toolCallLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-3",
		"uuid":       "uuid-asst-tool",
		"parentUuid": "uuid-user-3",
		"timestamp":  ts.Add(1 * time.Second).Format(time.RFC3339),
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"id":    "toolu_abc",
					"name":  "Read",
					"input": map[string]any{"file_path": "/src/main.go"},
				},
			},
			"usage": map[string]any{
				"input_tokens":  800,
				"output_tokens": 50,
			},
		},
	})

	// Tool result
	toolResultLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-3",
		"uuid":       "uuid-result-3",
		"parentUuid": "uuid-asst-tool",
		"timestamp":  ts.Add(2 * time.Second).Format(time.RFC3339),
		"type":       "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_abc",
					"name":        "Read",
					"content":     "package main\n\nfunc main() {}",
				},
			},
		},
	})

	// Assistant responds after tool result
	responseLines := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-3",
		"uuid":       "uuid-asst-resp",
		"parentUuid": "uuid-result-3",
		"timestamp":  ts.Add(3 * time.Second).Format(time.RFC3339),
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "The main.go file contains a simple main function."},
			},
			"usage": map[string]any{
				"input_tokens":  1200,
				"output_tokens": 300,
			},
		},
	})

	// Parse all
	var allEvents []ingestion.NormalizedEvent
	for i, line := range [][]byte{userLine, toolCallLine, toolResultLine, responseLines} {
		events, err := ingestion.ParseClaudeJSONL(line, "test.jsonl", i+1, int64(i*100))
		if err != nil {
			t.Fatal(err)
		}
		allEvents = append(allEvents, events...)
	}

	allEvents = ingestion.DeduplicateTokens(allEvents)
	insertNormalizedEvents(t, db, allEvents)

	// Verify session totals: 800+50 (tool call) + 1200+300 (response) = 2350
	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	sess := allSessions[0]
	expectedInput := int64(800 + 1200)
	expectedOutput := int64(50 + 300)
	expectedTotal := expectedInput + expectedOutput

	if sess.InputTokens != expectedInput {
		t.Errorf("session input_tokens: expected %d, got %d", expectedInput, sess.InputTokens)
	}
	if sess.OutputTokens != expectedOutput {
		t.Errorf("session output_tokens: expected %d, got %d", expectedOutput, sess.OutputTokens)
	}
	if sess.TotalTokens != expectedTotal {
		t.Errorf("session total_tokens: expected %d, got %d", expectedTotal, sess.TotalTokens)
	}

	// Verify conversation trace
	chatTurns, _ := QuerySessionConversation(ctx, db.ReadPool, "tok-sess-3")
	var totalChatTokens int64
	for _, ct := range chatTurns {
		totalChatTokens += ct.TotalTokens
	}
	if totalChatTokens != expectedTotal {
		t.Errorf("chat turns total tokens: expected %d, got %d", expectedTotal, totalChatTokens)
	}

	// Verify tool chain was built correctly
	var foundToolChain bool
	for _, ct := range chatTurns {
		for _, block := range ct.Blocks {
			if block.Kind == views.ChatBlockToolChain {
				foundToolChain = true
				if len(block.ToolChain) != 1 {
					t.Errorf("expected 1 tool chain item, got %d", len(block.ToolChain))
				} else if block.ToolChain[0].ToolName != "Read" {
					t.Errorf("expected tool name Read, got %q", block.ToolChain[0].ToolName)
				}
			}
		}
	}
	if !foundToolChain {
		t.Error("expected to find a tool chain block in chat turns")
	}
}

// TestE2E_TokenAccuracy_CacheTokens tests that cache_read and cache_create
// tokens flow correctly through the full pipeline.
func TestE2E_TokenAccuracy_CacheTokens(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Assistant response with all 4 token fields
	assistantLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-4",
		"uuid":       "uuid-asst-cache",
		"parentUuid": "",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Here is the answer."},
			},
			"usage": map[string]any{
				"input_tokens":                5,
				"output_tokens":               250,
				"cache_read_input_tokens":     13106,
				"cache_creation_input_tokens": 0,
			},
		},
	})

	events, err := ingestion.ParseClaudeJSONL(assistantLine, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	events = ingestion.DeduplicateTokens(events)
	insertNormalizedEvents(t, db, events)

	// Verify cache tokens in session summary
	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	sess := allSessions[0]
	if sess.CacheReadTokens != 13106 {
		t.Errorf("session cache_read_tokens: expected 13106, got %d", sess.CacheReadTokens)
	}
	if sess.CacheCreateTokens != 0 {
		t.Errorf("session cache_create_tokens: expected 0, got %d", sess.CacheCreateTokens)
	}
	// Total = input(5) + output(250) = 255 (cache tokens are NOT part of total)
	if sess.TotalTokens != 255 {
		t.Errorf("session total_tokens: expected 255 (excludes cache), got %d", sess.TotalTokens)
	}

	// Verify cache tokens in dashboard metrics sublabel
	metrics := QueryDashboardMetrics(ctx, db.ReadPool)
	for _, m := range metrics {
		if m.Label == "Total Tokens" {
			if m.Value != "255" {
				t.Errorf("dashboard total tokens: expected 255, got %q", m.Value)
			}
			// Sublabel should mention cache tokens
			if m.Sublabel == "" {
				t.Error("expected sublabel with cache token info")
			}
		}
	}

	// Verify tokens-by-model includes cache read
	byModel := QueryTokensByModelSummary(ctx, db.ReadPool)
	if len(byModel) != 1 {
		t.Fatalf("expected 1 model, got %d", len(byModel))
	}
	if byModel[0].CacheRead != 13106 {
		t.Errorf("by-model cache_read: expected 13106, got %d", byModel[0].CacheRead)
	}
}

// TestE2E_TokenAccuracy_ZeroTokens tests that events with zero tokens
// (user messages, event_msgs) don't contribute to token counts.
func TestE2E_TokenAccuracy_ZeroTokens(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// User message — no tokens
	userLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-5",
		"uuid":       "uuid-user-5",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "user",
		"message": map[string]any{
			"role":    "user",
			"content": "Hello",
		},
	})

	// Progress line — no message, no tokens
	progressLine := toJSONL(t, map[string]any{
		"sessionId": "tok-sess-5",
		"timestamp": "2025-06-01T10:00:01Z",
		"type":      "progress",
	})

	var allEvents []ingestion.NormalizedEvent
	for i, line := range [][]byte{userLine, progressLine} {
		events, err := ingestion.ParseClaudeJSONL(line, "test.jsonl", i+1, int64(i*100))
		if err != nil {
			t.Fatal(err)
		}
		allEvents = append(allEvents, events...)
	}
	allEvents = ingestion.DeduplicateTokens(allEvents)
	insertNormalizedEvents(t, db, allEvents)

	// All token aggregations should be zero
	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	sess := allSessions[0]
	if sess.TotalTokens != 0 {
		t.Errorf("session total_tokens: expected 0, got %d", sess.TotalTokens)
	}
	if sess.InputTokens != 0 {
		t.Errorf("session input_tokens: expected 0, got %d", sess.InputTokens)
	}
	if sess.OutputTokens != 0 {
		t.Errorf("session output_tokens: expected 0, got %d", sess.OutputTokens)
	}
}

// TestE2E_TokenAccuracy_MissingUsageField tests that events with no usage
// field produce zero token counts (not errors).
func TestE2E_TokenAccuracy_MissingUsageField(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Assistant message without usage field
	assistantLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-6",
		"uuid":       "uuid-asst-nousage",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Response without usage data."},
			},
		},
	})

	events, err := ingestion.ParseClaudeJSONL(assistantLine, "test.jsonl", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	events = ingestion.DeduplicateTokens(events)

	// Parser should produce zero tokens when usage field is absent
	if events[0].InputTokens != 0 {
		t.Errorf("parser: expected input_tokens=0 for missing usage, got %d", events[0].InputTokens)
	}
	if events[0].OutputTokens != 0 {
		t.Errorf("parser: expected output_tokens=0 for missing usage, got %d", events[0].OutputTokens)
	}

	insertNormalizedEvents(t, db, events)

	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	if allSessions[0].TotalTokens != 0 {
		t.Errorf("session total_tokens: expected 0, got %d", allSessions[0].TotalTokens)
	}
}

// TestE2E_TokenAccuracy_MultipleAPICalls tests that tokens from different
// API calls (different UUIDs) are summed correctly without interference.
func TestE2E_TokenAccuracy_MultipleAPICalls(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	ts := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)

	// First API call: thinking + text (same UUID)
	thinking1 := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-7",
		"uuid":       "uuid-call-1",
		"parentUuid": "",
		"timestamp":  ts.Format(time.RFC3339),
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Thinking about call 1..."},
			},
			"usage": map[string]any{
				"input_tokens":            100,
				"output_tokens":           10,
				"cache_read_input_tokens": 5000,
			},
		},
	})
	text1 := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-7",
		"uuid":       "uuid-call-1",
		"parentUuid": "",
		"timestamp":  ts.Format(time.RFC3339),
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Response from call 1"},
			},
			"usage": map[string]any{
				"input_tokens":            100,
				"output_tokens":           10,
				"cache_read_input_tokens": 5000,
			},
		},
	})

	// Second API call: thinking + text (different UUID)
	thinking2 := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-7",
		"uuid":       "uuid-call-2",
		"parentUuid": "uuid-call-1",
		"timestamp":  ts.Add(5 * time.Second).Format(time.RFC3339),
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Thinking about call 2..."},
			},
			"usage": map[string]any{
				"input_tokens":                200,
				"output_tokens":               20,
				"cache_read_input_tokens":     8000,
				"cache_creation_input_tokens": 1000,
			},
		},
	})
	text2 := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-7",
		"uuid":       "uuid-call-2",
		"parentUuid": "uuid-call-1",
		"timestamp":  ts.Add(5 * time.Second).Format(time.RFC3339),
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Response from call 2"},
			},
			"usage": map[string]any{
				"input_tokens":                200,
				"output_tokens":               20,
				"cache_read_input_tokens":     8000,
				"cache_creation_input_tokens": 1000,
			},
		},
	})

	// Parse all 4 lines
	var allEvents []ingestion.NormalizedEvent
	for i, line := range [][]byte{thinking1, text1, thinking2, text2} {
		events, err := ingestion.ParseClaudeJSONL(line, "test.jsonl", i+1, int64(i*100))
		if err != nil {
			t.Fatal(err)
		}
		allEvents = append(allEvents, events...)
	}

	// Before dedup: 4 events with duplicated tokens
	allEvents = ingestion.DeduplicateTokens(allEvents)

	// After dedup: only 2 events (text1, text2) should retain tokens
	insertNormalizedEvents(t, db, allEvents)

	// Expected totals:
	// Call 1: input=100, output=10, cache_read=5000
	// Call 2: input=200, output=20, cache_read=8000, cache_create=1000
	// Totals: input=300, output=30, total=330, cache_read=13000, cache_create=1000
	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	sess := allSessions[0]
	if sess.InputTokens != 300 {
		t.Errorf("session input_tokens: expected 300, got %d", sess.InputTokens)
	}
	if sess.OutputTokens != 30 {
		t.Errorf("session output_tokens: expected 30, got %d", sess.OutputTokens)
	}
	if sess.TotalTokens != 330 {
		t.Errorf("session total_tokens: expected 330, got %d", sess.TotalTokens)
	}
	if sess.CacheReadTokens != 13000 {
		t.Errorf("session cache_read: expected 13000, got %d", sess.CacheReadTokens)
	}
	if sess.CacheCreateTokens != 1000 {
		t.Errorf("session cache_create: expected 1000, got %d", sess.CacheCreateTokens)
	}

	// Verify tokens-by-model — single model should show summed tokens
	byModel := QueryTokensByModelSummary(ctx, db.ReadPool)
	if len(byModel) != 1 {
		t.Fatalf("expected 1 model, got %d", len(byModel))
	}
	if byModel[0].Total != 330 {
		t.Errorf("by-model total: expected 330, got %d", byModel[0].Total)
	}

	// Verify session detail
	detail, err := QuerySessionDetail(ctx, db.ReadPool, "tok-sess-7")
	if err != nil {
		t.Fatalf("QuerySessionDetail: %v", err)
	}
	if detail.Session.TotalTokens != 330 {
		t.Errorf("session detail total_tokens: expected 330, got %d", detail.Session.TotalTokens)
	}
	if detail.Session.CacheReadTokens != 13000 {
		t.Errorf("session detail cache_read: expected 13000, got %d", detail.Session.CacheReadTokens)
	}
}

// TestE2E_TokenAccuracy_ThreeContentBlocks tests deduplication with three
// content blocks from a single API call (thinking + text + tool_use).
func TestE2E_TokenAccuracy_ThreeContentBlocks(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// All three lines share the same UUID
	usage := map[string]any{
		"input_tokens":                50,
		"output_tokens":               400,
		"cache_read_input_tokens":     10000,
		"cache_creation_input_tokens": 2000,
	}

	thinkingLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-8",
		"uuid":       "uuid-triple",
		"parentUuid": "",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "I need to read the file first..."},
			},
			"usage": usage,
		},
	})
	textLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-8",
		"uuid":       "uuid-triple",
		"parentUuid": "",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": "Let me read that file for you."},
			},
			"usage": usage,
		},
	})
	toolUseLine := toJSONL(t, map[string]any{
		"sessionId":  "tok-sess-8",
		"uuid":       "uuid-triple",
		"parentUuid": "",
		"timestamp":  "2025-06-01T10:00:00Z",
		"type":       "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "tool_use", "id": "toolu_xyz", "name": "Read", "input": map[string]any{"file_path": "/foo.go"}},
			},
			"usage": usage,
		},
	})

	var allEvents []ingestion.NormalizedEvent
	for i, line := range [][]byte{thinkingLine, textLine, toolUseLine} {
		events, err := ingestion.ParseClaudeJSONL(line, "test.jsonl", i+1, int64(i*100))
		if err != nil {
			t.Fatal(err)
		}
		allEvents = append(allEvents, events...)
	}

	allEvents = ingestion.DeduplicateTokens(allEvents)

	// Only the last event (tool_use) should have tokens
	for i := 0; i < 2; i++ {
		if allEvents[i].InputTokens != 0 || allEvents[i].OutputTokens != 0 {
			t.Errorf("event[%d] should have zeroed tokens: input=%d output=%d",
				i, allEvents[i].InputTokens, allEvents[i].OutputTokens)
		}
	}
	if allEvents[2].InputTokens != 50 || allEvents[2].OutputTokens != 400 {
		t.Errorf("event[2] (tool_use) should keep tokens: input=%d output=%d",
			allEvents[2].InputTokens, allEvents[2].OutputTokens)
	}

	insertNormalizedEvents(t, db, allEvents)

	// Session should have tokens counted exactly once
	active, completed := QueryDashboardSessions(ctx, db.ReadPool)
	allSessions := append(active, completed...)
	if len(allSessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(allSessions))
	}
	sess := allSessions[0]
	// Total = 50 + 400 = 450 (NOT 1350 from 3x counting)
	if sess.TotalTokens != 450 {
		t.Errorf("session total_tokens: expected 450 (not 3x counted), got %d", sess.TotalTokens)
	}
	if sess.CacheReadTokens != 10000 {
		t.Errorf("session cache_read: expected 10000 (not 30000), got %d", sess.CacheReadTokens)
	}
	if sess.CacheCreateTokens != 2000 {
		t.Errorf("session cache_create: expected 2000 (not 6000), got %d", sess.CacheCreateTokens)
	}
}
