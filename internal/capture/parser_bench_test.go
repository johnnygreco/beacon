package capture

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

var (
	captureBenchEvents     []NormalizedEvent
	captureBenchRowBatch   store.RowBatch
	captureBenchEventCount int
)

func BenchmarkCaptureParseCodexJSONL(b *testing.B) {
	lines := benchCodexJSONLLines()
	file := "/tmp/rollout-2026-06-01T10-00-00-11111111-2222-3333-4444-555555555555.jsonl"
	total := 0

	b.ReportAllocs()
	for b.Loop() {
		for i, line := range lines {
			events, err := ParseCodexJSONL(line, file, i+1, int64(i*512))
			if err != nil {
				b.Fatalf("parse codex line %d: %v", i, err)
			}
			total += len(events)
		}
	}
	captureBenchEventCount = total
}

func BenchmarkCaptureParseClaudeJSONL(b *testing.B) {
	lines := benchClaudeJSONLLines()
	total := 0

	b.ReportAllocs()
	for b.Loop() {
		for i, line := range lines {
			events, err := ParseClaudeJSONL(line, "/tmp/claude-bench.jsonl", i+1, int64(i*512))
			if err != nil {
				b.Fatalf("parse claude line %d: %v", i, err)
			}
			total += len(events)
		}
	}
	captureBenchEventCount = total
}

func BenchmarkCaptureNormalizeTokenAndModelStream(b *testing.B) {
	seed := benchNormalizedEvents(1024)

	b.ReportAllocs()
	for b.Loop() {
		events := append([]NormalizedEvent(nil), seed...)
		PropagateModel(events)
		captureBenchEvents = DeduplicateTokens(events)
	}
}

func BenchmarkCaptureBuildInsertRowBatch(b *testing.B) {
	events := benchNormalizedEvents(1024)

	b.ReportAllocs()
	for b.Loop() {
		captureBenchRowBatch = buildInsertRowBatch(events, 0.000003, 0.000015)
	}
	if len(captureBenchRowBatch.ActivityEvents) == 0 {
		b.Fatal("expected activity events")
	}
}

func benchCodexJSONLLines() [][]byte {
	return [][]byte{
		[]byte(`{"type":"session_meta","session_id":"bench-codex","timestamp":"2026-06-01T10:00:00Z","payload":{"description":"Profile dashboard search and MCP flows","cwd":"/Users/donnie/projects/code/beacon"}}`),
		[]byte(`{"type":"turn_context","session_id":"bench-codex","timestamp":"2026-06-01T10:00:01Z","payload":{"model":"gpt-5.4","cwd":"/Users/donnie/projects/code/beacon"}}`),
		[]byte(`{"type":"response_item","session_id":"bench-codex","timestamp":"2026-06-01T10:00:02Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The benchmark plan should measure parser throughput, search index construction, MCP dispatch, and dashboard JSON serialization."}],"info":{"last_token_usage":{"input_tokens":1800,"cached_input_tokens":300,"output_tokens":160},"total_token_usage":{"input_tokens":1800,"cached_input_tokens":300,"output_tokens":160,"total_tokens":1960},"model":"gpt-5.4"}}}`),
		[]byte(`{"type":"response_item","session_id":"bench-codex","timestamp":"2026-06-01T10:00:03Z","payload":{"type":"function_call","name":"Bash","arguments":"{\"command\":\"rg --files internal | rg bench\",\"description\":\"find benchmark files\"}","call_id":"call_bench_1","info":{"model":"gpt-5.4"}}}`),
		[]byte(`{"type":"response_item","session_id":"bench-codex","timestamp":"2026-06-01T10:00:04Z","payload":{"type":"function_call_output","name":"Bash","call_id":"call_bench_1","output":"internal/perf/bench_test.go\ninternal/views/pages/dashboard_test.go\n"}}`),
		[]byte(`{"type":"event_msg","session_id":"bench-codex","timestamp":"2026-06-01T10:00:05Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":2200,"cached_input_tokens":400,"output_tokens":220},"total_token_usage":{"input_tokens":2200,"cached_input_tokens":400,"output_tokens":220,"total_tokens":2420},"model":"gpt-5.4"}}}`),
	}
}

func benchClaudeJSONLLines() [][]byte {
	return [][]byte{
		[]byte(`{"sessionId":"bench-claude","uuid":"uuid-user-1","parentUuid":"","timestamp":"2026-06-01T10:00:00Z","type":"user","message":{"role":"user","content":"Measure Beacon page load, MCP search, and transcript rendering performance."},"cwd":"/Users/donnie/projects/code/beacon"}`),
		[]byte(`{"sessionId":"bench-claude","uuid":"uuid-assistant-1","parentUuid":"uuid-user-1","timestamp":"2026-06-01T10:00:01Z","type":"assistant","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"thinking","thinking":"Identify hot paths before changing behavior."},{"type":"text","text":"I will add focused benchmarks for parsing, indexing, MCP formatting, view rendering, and API serialization."},{"type":"tool_use","id":"toolu_bench_1","name":"Read","input":{"file_path":"internal/perf/bench_test.go"}}],"usage":{"input_tokens":2400,"cache_read_input_tokens":600,"output_tokens":260}}}`),
		[]byte(`{"sessionId":"bench-claude","uuid":"uuid-tool-1","parentUuid":"uuid-assistant-1","timestamp":"2026-06-01T10:00:02Z","type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bench_1","content":"package perf_test\n\nfunc BenchmarkSearchBM25(b *testing.B) { ... }"}]}}`),
	}
}

func benchNormalizedEvents(n int) []NormalizedEvent {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	events := make([]NormalizedEvent, 0, n)
	for i := 0; i < n; i++ {
		sessionID := fmt.Sprintf("bench-session-%03d", i/80)
		messageUUID := fmt.Sprintf("msg-%04d", i)
		parentUUID := ""
		if i%3 != 0 {
			parentUUID = fmt.Sprintf("msg-%04d", i-1)
		}
		evt := NormalizedEvent{
			SessionID:          sessionID,
			SourceName:         "codex",
			Runtime:            models.RuntimeCodex,
			Provider:           models.ProviderOpenAI,
			Format:             models.FormatJSONL,
			EventKind:          models.EventKindMessage,
			PayloadType:        "text",
			ActorRole:          models.ActorRoleAssistant,
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			TextContent:        strings.Repeat("benchmark dashboard search mcp responsive transcript token analytics ", 2+(i%4)),
			Model:              "gpt-5.4",
			InputTokens:        int64(1200 + i%400),
			OutputTokens:       int64(180 + i%90),
			CacheReadTokens:    int64(i % 250),
			DurationMs:         int64(100 + i%500),
			ParentUUID:         parentUUID,
			MessageUUID:        messageUUID,
			TokenUsageTotalKey: fmt.Sprintf("%d:%d:%d:%d", 1200+i, i%250, 180+i%90, 1380+i),
			SourceFile:         "/tmp/bench.jsonl",
			SourceLineNo:       i + 1,
			SourceOffset:       int64(i * 512),
			RawPayload:         fmt.Sprintf(`{"line":%d,"content":"benchmark payload"}`, i),
			SourceGeneration:   i / 5000,
			CacheCreateTokens:  int64(i % 50),
			CWD:                "/Users/donnie/projects/code/beacon",
			ParentSessionID:    "",
			ErrorCode:          "",
			ErrorMessage:       "",
			ToolUseID:          "",
			ToolName:           "",
			ToolPhase:          "",
			ToolInput:          "",
			ToolOutput:         "",
			CostUSD:            0,
		}
		switch i % 8 {
		case 2:
			evt.EventKind = models.EventKindReasoning
			evt.TextContent = strings.Repeat("consider query shape and render cost ", 8)
		case 3:
			evt.EventKind = models.EventKindToolCall
			evt.ToolName = "Bash"
			evt.ToolPhase = models.ToolPhaseCall
			evt.ToolUseID = fmt.Sprintf("toolu-%04d", i)
			evt.ToolInput = `{"command":"rg benchmark internal","description":"find benchmark files"}`
			evt.TextContent = evt.ToolName
		case 4:
			evt.EventKind = models.EventKindToolResult
			evt.ActorRole = models.ActorRoleTool
			evt.ToolName = "Bash"
			evt.ToolPhase = models.ToolPhaseResult
			evt.ToolUseID = fmt.Sprintf("toolu-%04d", i-1)
			evt.ToolOutput = strings.Repeat("internal/perf/bench_test.go\n", 10)
			evt.TextContent = evt.ToolOutput
		case 5:
			evt.PayloadType = "token_count"
		case 7:
			evt.EventKind = models.EventKindToolError
			evt.ActorRole = models.ActorRoleTool
			evt.ErrorCode = "tool_execution_failed"
			evt.ErrorMessage = "Process exited with code 1"
			evt.TextContent = evt.ErrorMessage
		}
		events = append(events, evt)
	}
	return events
}
