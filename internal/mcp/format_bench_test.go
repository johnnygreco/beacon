package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/search"
)

var (
	mcpBenchText     string
	mcpBenchResponse *jsonRPCResponse
)

func BenchmarkMCPFormatSearchResults(b *testing.B) {
	for _, size := range []int{25, 100} {
		results := benchSearchResults(size)
		b.Run(fmt.Sprintf("%dResults", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				mcpBenchText = FormatSearchResults(results)
			}
			if mcpBenchText == "" {
				b.Fatal("expected formatted search results")
			}
		})
	}
}

func BenchmarkMCPFormatOpenContext(b *testing.B) {
	events := benchContextEvents(41)

	b.ReportAllocs()
	for b.Loop() {
		mcpBenchText = FormatOpenContext(events, len(events)/2)
	}
	if mcpBenchText == "" {
		b.Fatal("expected formatted context")
	}
}

func BenchmarkMCPFormatSessionList(b *testing.B) {
	sessions := benchSessionInfos(50)

	b.ReportAllocs()
	for b.Loop() {
		mcpBenchText = FormatSessionList(sessions)
	}
	if mcpBenchText == "" {
		b.Fatal("expected formatted sessions")
	}
}

func BenchmarkMCPDispatchSearchWithFakeResults(b *testing.B) {
	srv := testServer()
	srv.searcher = &fakeMCPSearcher{results: benchSearchResults(25)}
	req := &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search_sessions","arguments":{"query":"binary search","limit":25,"session_id":null,"event_kinds":["message","tool_call"]}}`),
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		mcpBenchResponse = srv.dispatch(ctx, req)
	}
	if mcpBenchResponse == nil || mcpBenchResponse.Error != nil {
		b.Fatalf("expected tool response, got %+v", mcpBenchResponse)
	}
}

func benchSearchResults(n int) []search.SearchResult {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	results := make([]search.SearchResult, 0, n)
	for i := 0; i < n; i++ {
		kind := models.EventKindMessage
		toolName := ""
		if i%4 == 1 {
			kind = models.EventKindToolCall
			toolName = "Bash"
		} else if i%4 == 2 {
			kind = models.EventKindToolResult
			toolName = "Bash"
		} else if i%9 == 0 {
			kind = models.EventKindError
		}
		results = append(results, search.SearchResult{
			EventUID:    fmt.Sprintf("bench-event-%06d", i),
			SessionID:   fmt.Sprintf("bench-session-%04d", i/12),
			EventKind:   kind,
			TextPreview: fmt.Sprintf("benchmark result %d for dashboard search, MCP tool dispatch, and transcript context rendering", i),
			Score:       12.5 - float64(i)*0.05,
			Timestamp:   base.Add(time.Duration(i) * time.Second),
			ToolName:    toolName,
			Model:       "gpt-5.4",
			Provider:    models.ProviderOpenAI,
		})
	}
	return results
}

func benchContextEvents(n int) []contextEvent {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	events := make([]contextEvent, 0, n)
	for i := 0; i < n; i++ {
		kind := models.EventKindMessage
		role := models.ActorRoleAssistant
		toolName := ""
		if i%5 == 2 {
			kind = models.EventKindToolCall
			toolName = "Bash"
		} else if i%5 == 3 {
			kind = models.EventKindToolResult
			role = models.ActorRoleTool
			toolName = "Bash"
		}
		events = append(events, contextEvent{
			EventUID:    fmt.Sprintf("bench-context-%06d", i),
			EventKind:   kind,
			ActorRole:   role,
			TextPreview: fmt.Sprintf("context event %d around an MCP open target", i),
			ToolName:    toolName,
			Model:       "gpt-5.4",
			Tokens:      int64(100 + i),
			Timestamp:   base.Add(time.Duration(i) * time.Second),
		})
	}
	return events
}

func benchSessionInfos(n int) []sessionInfo {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	sessions := make([]sessionInfo, 0, n)
	for i := 0; i < n; i++ {
		sessions = append(sessions, sessionInfo{
			SessionID:     fmt.Sprintf("bench-session-%04d", i),
			SourceName:    "codex",
			StartedAt:     base.Add(time.Duration(i) * time.Minute),
			EndedAt:       base.Add(time.Duration(i+20) * time.Minute),
			EventCount:    int64(80 + i%40),
			TurnCount:     int64(12 + i%8),
			TotalTokens:   int64(50_000 + i*1000),
			ToolCallCount: int64(20 + i%15),
			MCPCallCount:  int64(i % 6),
			ErrorCount:    int64(i % 3),
			LastModel:     "gpt-5.4",
		})
	}
	return sessions
}
