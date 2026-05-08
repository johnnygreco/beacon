package web

import (
	"fmt"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestBuildChatTurns_EmptyInput(t *testing.T) {
	result := buildChatTurns(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 chat turns, got %d", len(result))
	}
}

func TestBuildDashboardModelCharts_CumulativeTokensAndActivity(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	points := []dashboardModelPoint{
		{Bucket: t0, Provider: "openai", Model: "gpt-5.4", Tokens: 100, InputTokens: 60, OutputTokens: 40, ToolCalls: 2, Calls: 4},
		{Bucket: t0.Add(15 * time.Minute), Provider: "openai", Model: "gpt-5.4", Tokens: 50, ToolCalls: 1, Calls: 1, Errors: 1},
		{Bucket: t0, Provider: "anthropic", Model: "claude-opus-4-6", Tokens: 25, Calls: 1},
		{Bucket: t0.Add(15 * time.Minute), Provider: "anthropic", Model: "claude-opus-4-6", Tokens: 75, ToolCalls: 3, Calls: 3},
	}

	tokens, activity := buildDashboardModelCharts(points, 15, "hour")

	if len(tokens.Labels) != 2 || len(tokens.Datasets) != 2 {
		t.Fatalf("unexpected token chart shape: %#v", tokens)
	}
	if tokens.Datasets[0].Label != "gpt-5.4" {
		t.Fatalf("expected top model first, got %q", tokens.Datasets[0].Label)
	}
	if got := tokens.Datasets[0].Values; len(got) != 2 || got[0] != 100 || got[1] != 150 {
		t.Fatalf("gpt cumulative values = %#v", got)
	}
	if got := tokens.Datasets[1].Values; len(got) != 2 || got[0] != 25 || got[1] != 100 {
		t.Fatalf("claude cumulative values = %#v", got)
	}
	if tokens.Summary.TotalTokens != 250 || tokens.Summary.ToolCallCount != 6 || tokens.Summary.ErrorCount != 1 || tokens.Summary.ModelCount != 2 {
		t.Fatalf("summary = %#v", tokens.Summary)
	}

	errorRate := activity.Metrics["error_rate"].Datasets[0].Values
	if len(errorRate) != 2 || errorRate[0] != 0 || errorRate[1] != 50 {
		t.Fatalf("error rate values = %#v", errorRate)
	}
	toolCalls := activity.Metrics["tool_calls"].Datasets[1].Values
	if len(toolCalls) != 2 || toolCalls[0] != 0 || toolCalls[1] != 3 {
		t.Fatalf("tool call values = %#v", toolCalls)
	}
}

func TestDashboardBucketMinutes(t *testing.T) {
	cases := map[string]int{
		"1h":  1,
		"24h": 15,
		"7d":  120,
		"30d": 720,
		"":    1440,
	}
	for rangeVal, want := range cases {
		if got := dashboardBucketMinutes(rangeVal); got != want {
			t.Fatalf("dashboardBucketMinutes(%q) = %d, want %d", rangeVal, got, want)
		}
	}
}

func TestBuildChatTurns_SingleUserMessage(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq:     1,
		TotalTokens: 100,
		StartedAt:   time.Now(),
		Events: []views.EventSummary{{
			EventKind:   "message",
			ActorRole:   "user",
			TextContent: "Hello",
		}},
	}}

	result := buildChatTurns(turns)
	if len(result) != 1 {
		t.Fatalf("expected 1 chat turn, got %d", len(result))
	}
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result[0].Blocks))
	}
	if result[0].Blocks[0].Kind != views.ChatBlockUserMessage {
		t.Errorf("expected user_message, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_AssistantMessage(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{{
			EventKind:   "message",
			ActorRole:   "assistant",
			TextContent: "Hi there!",
		}},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result[0].Blocks))
	}
	if result[0].Blocks[0].Kind != views.ChatBlockAssistantMessage {
		t.Errorf("expected assistant_message, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ToolCallWithResult(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "file.txt"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "contents"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block (tool chain), got %d", len(result[0].Blocks))
	}
	block := result[0].Blocks[0]
	if block.Kind != views.ChatBlockToolChain {
		t.Errorf("expected tool_chain, got %s", block.Kind)
	}
	if len(block.ToolChain) != 1 {
		t.Fatalf("expected 1 tool chain item, got %d", len(block.ToolChain))
	}
	item := block.ToolChain[0]
	if item.ToolName != "Read" {
		t.Errorf("expected tool name Read, got %s", item.ToolName)
	}
	if item.ResultEvent == nil {
		t.Error("expected result event to be set")
	}
	if item.OutputPreview != "contents" {
		t.Errorf("expected output preview 'contents', got '%s'", item.OutputPreview)
	}
}

func TestBuildChatTurns_OrphanToolResult(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_result", ToolName: "Bash", OutputPreview: "output"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result[0].Blocks))
	}
	if result[0].Blocks[0].Kind != views.ChatBlockToolChain {
		t.Errorf("expected tool_chain, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ReasoningBlock(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "reasoning", TextContent: "thinking..."},
		},
	}}

	result := buildChatTurns(turns)
	if result[0].Blocks[0].Kind != views.ChatBlockReasoning {
		t.Errorf("expected reasoning, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ErrorBlock(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "error", TextContent: "something failed"},
		},
	}}

	result := buildChatTurns(turns)
	if result[0].Blocks[0].Kind != views.ChatBlockError {
		t.Errorf("expected error, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_MixedConversation(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "message", ActorRole: "user", TextContent: "Do something"},
			{EventKind: "reasoning", TextContent: "thinking"},
			{EventKind: "message", ActorRole: "assistant", TextContent: "I'll help"},
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "input"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "output"},
			{EventKind: "tool_call", ToolName: "Write", InputPreview: "data"},
			{EventKind: "tool_result", ToolName: "Write", OutputPreview: "ok"},
			{EventKind: "message", ActorRole: "assistant", TextContent: "Done!"},
		},
	}}

	result := buildChatTurns(turns)
	blocks := result[0].Blocks

	expected := []string{
		views.ChatBlockUserMessage,
		views.ChatBlockReasoning,
		views.ChatBlockAssistantMessage,
		views.ChatBlockToolChain,
		views.ChatBlockAssistantMessage,
	}

	if len(blocks) != len(expected) {
		t.Fatalf("expected %d blocks, got %d", len(expected), len(blocks))
	}
	for i, exp := range expected {
		if blocks[i].Kind != exp {
			t.Errorf("block %d: expected %s, got %s", i, exp, blocks[i].Kind)
		}
	}

	// The tool chain should have 2 items (Read and Write)
	toolChain := blocks[3].ToolChain
	if len(toolChain) != 2 {
		t.Errorf("expected 2 tool chain items, got %d", len(toolChain))
	}
}

func TestBuildChatTurns_MultipleTurns(t *testing.T) {
	turns := []views.TurnDetail{
		{
			TurnSeq: 1,
			Events: []views.EventSummary{
				{EventKind: "message", ActorRole: "user", TextContent: "First"},
			},
		},
		{
			TurnSeq: 2,
			Events: []views.EventSummary{
				{EventKind: "message", ActorRole: "user", TextContent: "Second"},
			},
		},
	}

	result := buildChatTurns(turns)
	if len(result) != 2 {
		t.Errorf("expected 2 chat turns, got %d", len(result))
	}
	if result[0].TurnSeq != 1 || result[1].TurnSeq != 2 {
		t.Errorf("turn sequences incorrect: %d, %d", result[0].TurnSeq, result[1].TurnSeq)
	}
}

func TestParseToolParams_Empty(t *testing.T) {
	result := parseToolParams("")
	if result != nil {
		t.Error("expected nil for empty input")
	}
}

func TestParseToolParams_InvalidJSON(t *testing.T) {
	result := parseToolParams("not json")
	if result != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestParseToolParams_BashCommand(t *testing.T) {
	result := parseToolParams(`{"command":"ls -la","description":"list files"}`)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Command != "ls -la" {
		t.Errorf("expected command 'ls -la', got '%s'", result.Command)
	}
	if result.Description != "list files" {
		t.Errorf("expected description 'list files', got '%s'", result.Description)
	}
}

func TestParseToolParams_EditTool(t *testing.T) {
	result := parseToolParams(`{"file_path":"/tmp/test.go","old_string":"foo","new_string":"bar"}`)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FilePath != "/tmp/test.go" {
		t.Errorf("expected file_path '/tmp/test.go', got '%s'", result.FilePath)
	}
	if result.OldString != "foo" {
		t.Errorf("expected old_string 'foo', got '%s'", result.OldString)
	}
	if result.NewString != "bar" {
		t.Errorf("expected new_string 'bar', got '%s'", result.NewString)
	}
}

func TestParseToolParams_SearchTool(t *testing.T) {
	result := parseToolParams(`{"pattern":"func.*Test","path":"./internal"}`)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Pattern != "func.*Test" {
		t.Errorf("expected pattern 'func.*Test', got '%s'", result.Pattern)
	}
	if result.Path != "./internal" {
		t.Errorf("expected path './internal', got '%s'", result.Path)
	}
}

func TestBuildChatTurns_ToolStats(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "f1"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "c1"},
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "f2"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "c2"},
			{EventKind: "tool_call", ToolName: "Edit", InputPreview: "e1"},
			{EventKind: "tool_result", ToolName: "Edit", OutputPreview: "ok"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(result))
	}
	stats := result[0].ToolStats
	if len(stats) != 2 {
		t.Fatalf("expected 2 tool stat entries, got %d", len(stats))
	}
	// Sorted by count descending, then name ascending
	if stats[0].Name != "Read" || stats[0].Count != 2 {
		t.Errorf("expected first stat Read:2, got %s:%d", stats[0].Name, stats[0].Count)
	}
	if stats[1].Name != "Edit" || stats[1].Count != 1 {
		t.Errorf("expected second stat Edit:1, got %s:%d", stats[1].Name, stats[1].Count)
	}
}

func TestBuildChatTurns_InputJSONAndParams(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Bash", InputPreview: "ls", InputJSON: `{"command":"ls -la","description":"list files"}`},
			{EventKind: "tool_result", ToolName: "Bash", OutputPreview: "file1\nfile2"},
		},
	}}

	result := buildChatTurns(turns)
	item := result[0].Blocks[0].ToolChain[0]
	if item.InputJSON != `{"command":"ls -la","description":"list files"}` {
		t.Errorf("InputJSON not preserved: %s", item.InputJSON)
	}
	if item.Params == nil {
		t.Fatal("expected Params to be populated")
	}
	if item.Params.Command != "ls -la" {
		t.Errorf("expected command 'ls -la', got '%s'", item.Params.Command)
	}
}

func TestBuildChatTurns_UnknownEventKind(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "unknown_event", TextContent: "data"},
		},
	}}

	result := buildChatTurns(turns)
	// Unknown events should be treated as assistant messages
	if result[0].Blocks[0].Kind != views.ChatBlockAssistantMessage {
		t.Errorf("expected assistant_message for unknown kind, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ToolCallWithoutResult(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "file.txt"},
			{EventKind: "message", ActorRole: "assistant", TextContent: "Moving on"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(result[0].Blocks))
	}
	// First block: tool chain with no result
	if result[0].Blocks[0].Kind != views.ChatBlockToolChain {
		t.Errorf("expected tool_chain, got %s", result[0].Blocks[0].Kind)
	}
	if result[0].Blocks[0].ToolChain[0].ResultEvent != nil {
		t.Error("expected no result event for tool call without result")
	}
}

func TestSetSessionTiming_Active(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-5 * time.Minute)
	end := time.Now().Add(-1 * time.Minute) // ended 1 min ago — still active

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", s.Status)
	}
	if s.Duration == "" {
		t.Error("expected non-empty duration")
	}
	if s.EndedAt != end {
		t.Errorf("expected EndedAt to be set")
	}
}

func TestScanSessionSummaryIncludesErrorCount(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Minute)
	end := now.Add(-6 * time.Minute)
	scanner := stubScanner{values: []any{
		"session-1",
		"codex",
		start,
		end,
		int64(3),
		int64(120),
		int64(70),
		int64(50),
		int64(10),
		int64(5),
		int64(4),
		int64(1),
		int64(2),
		"gpt-5.4",
		"/repo",
		"parent-1",
		1,
		"openai",
	}}

	s, err := scanSessionSummary(scanner, now)
	if err != nil {
		t.Fatalf("scanSessionSummary: %v", err)
	}
	if s.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2", s.ErrorCount)
	}
	if s.ActiveModel != "gpt-5.4" || s.Provider != "openai" || !s.HasSessionEnd {
		t.Fatalf("summary fields shifted during scan: %#v", s)
	}
}

func TestAPISessionSummaryFromViewIncludesErrorCount(t *testing.T) {
	api := apiSessionSummaryFromView(views.SessionSummary{
		ID:         "session-1",
		ErrorCount: 2,
	})
	if api.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2", api.ErrorCount)
	}
}

type stubScanner struct {
	values []any
}

func (s stubScanner) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan destinations = %d, values = %d", len(dest), len(s.values))
	}
	for i, value := range s.values {
		switch d := dest[i].(type) {
		case *string:
			v, ok := value.(string)
			if !ok {
				return fmt.Errorf("value %d is %T, want string", i, value)
			}
			*d = v
		case *time.Time:
			v, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("value %d is %T, want time.Time", i, value)
			}
			*d = v
		case *int:
			switch v := value.(type) {
			case int:
				*d = v
			case int64:
				*d = int(v)
			default:
				return fmt.Errorf("value %d is %T, want int", i, value)
			}
		case *int64:
			switch v := value.(type) {
			case int64:
				*d = v
			case int:
				*d = int64(v)
			default:
				return fmt.Errorf("value %d is %T, want int64", i, value)
			}
		default:
			return fmt.Errorf("unsupported destination %d: %T", i, dest[i])
		}
	}
	return nil
}

func TestSetSessionTiming_Completed(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-20 * time.Minute)
	end := time.Now().Add(-10 * time.Minute) // ended 10 min ago — completed

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", s.Status)
	}
	if s.Duration != "10m 0s" {
		t.Errorf("expected duration '10m 0s', got '%s'", s.Duration)
	}
}

func TestSetSessionTiming_RecentlyActive(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-10 * time.Minute)
	// Ended within active threshold (90s) — active
	end := time.Now().Add(-30 * time.Second)

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "active" {
		t.Errorf("expected status 'active' within 90s, got '%s'", s.Status)
	}

	// Between active (90s) and idle (5m) threshold — idle
	var s2 views.SessionSummary
	end2 := time.Now().Add(-2 * time.Minute)
	setSessionTiming(&s2, start, end2, time.Now())

	if s2.Status != "idle" {
		t.Errorf("expected status 'idle' at 2 minutes, got '%s'", s2.Status)
	}

	// Past idle threshold — completed
	var s3 views.SessionSummary
	end3 := time.Now().Add(-5*time.Minute - 1*time.Second)
	setSessionTiming(&s3, start, end3, time.Now())

	if s3.Status != "completed" {
		t.Errorf("expected status 'completed' past 5 minutes, got '%s'", s3.Status)
	}
}

func TestSetSessionTiming_ZeroEndTime(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-2 * time.Minute)
	end := time.Time{} // zero time — lastActivity falls back to startedAt (2m ago → idle)

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "idle" {
		t.Errorf("expected status 'idle' for zero endedAt with 2m start, got '%s'", s.Status)
	}
	if s.Duration == "" {
		t.Error("expected non-empty duration")
	}
}

func TestSetSessionTiming_HasSessionEnd(t *testing.T) {
	var s views.SessionSummary
	s.HasSessionEnd = true
	start := time.Now().Add(-1 * time.Minute)
	end := time.Now().Add(-30 * time.Second) // recent activity, but has session_end

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "completed" {
		t.Errorf("expected status 'completed' with session_end signal, got '%s'", s.Status)
	}
}

func TestDeduplicateTurns_OrphanMerge(t *testing.T) {
	turns := []views.TurnDetail{
		{TurnSeq: 1, Events: []views.EventSummary{
			{EventUID: "a1", EventKind: "message", ActorRole: "user", TextContent: "hello"},
		}},
		{TurnSeq: 2, Events: []views.EventSummary{
			{EventUID: "a2", EventKind: "message", ActorRole: "user", TextContent: "hello"},
			{EventUID: "b1", EventKind: "message", ActorRole: "assistant", TextContent: "hi"},
		}},
	}

	result := deduplicateTurns(turns)
	if len(result) != 1 {
		t.Fatalf("expected 1 turn after orphan merge, got %d", len(result))
	}
	if result[0].TurnSeq != 2 {
		t.Errorf("expected turn 2 to remain, got turn %d", result[0].TurnSeq)
	}
}

func TestDeduplicateTurns_LastSingleTurnKept(t *testing.T) {
	turns := []views.TurnDetail{
		{TurnSeq: 1, Events: []views.EventSummary{
			{EventUID: "a1", EventKind: "message", ActorRole: "user", TextContent: "hello"},
			{EventUID: "b1", EventKind: "message", ActorRole: "assistant", TextContent: "hi"},
		}},
		{TurnSeq: 2, Events: []views.EventSummary{
			{EventUID: "a2", EventKind: "message", ActorRole: "user", TextContent: "bye"},
		}},
	}

	result := deduplicateTurns(turns)
	if len(result) != 2 {
		t.Fatalf("expected 2 turns (last single turn kept), got %d", len(result))
	}
}

func TestDeduplicateTurns_DifferentUIDsNotDeduped(t *testing.T) {
	turns := []views.TurnDetail{
		{TurnSeq: 1, Events: []views.EventSummary{
			{EventUID: "uid-1", EventKind: "tool_call", ToolName: "Read", InputJSON: `{"file_path":"f.go"}`},
			{EventUID: "uid-2", EventKind: "tool_call", ToolName: "Read", InputJSON: `{"file_path":"f.go"}`},
		}},
	}

	result := deduplicateTurns(turns)
	if len(result[0].Events) != 2 {
		t.Errorf("expected 2 events (different UIDs), got %d", len(result[0].Events))
	}
}
