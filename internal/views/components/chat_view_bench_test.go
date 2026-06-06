package components

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

func BenchmarkViewRenderChatTranscript(b *testing.B) {
	turns := benchChatTurns(80)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if err := ChatView(turns).Render(ctx, io.Discard); err != nil {
			b.Fatalf("render chat transcript: %v", err)
		}
	}
}

func benchChatTurns(n int) []views.ChatTurn {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	turns := make([]views.ChatTurn, 0, n)
	for i := 0; i < n; i++ {
		user := views.EventSummary{
			EventUID:    fmt.Sprintf("bench-user-%04d", i),
			EventKind:   models.EventKindMessage,
			ActorRole:   models.ActorRoleUser,
			TextContent: fmt.Sprintf("Measure Beacon performance turn %d across page load, MCP search, and transcript rendering.", i),
			Model:       "gpt-5.4",
			Tokens:      120,
			Timestamp:   base.Add(time.Duration(i) * time.Minute),
		}
		assistant := views.EventSummary{
			EventUID:    fmt.Sprintf("bench-assistant-%04d", i),
			EventKind:   models.EventKindMessage,
			ActorRole:   models.ActorRoleAssistant,
			TextContent: "The benchmark should stay deterministic and cover the hot code paths.\n\n```go\nfunc benchmarkTarget() string { return \"beacon\" }\n```",
			Model:       "gpt-5.4",
			Tokens:      480,
			Timestamp:   base.Add(time.Duration(i)*time.Minute + 15*time.Second),
		}
		reasoning := views.EventSummary{
			EventUID:    fmt.Sprintf("bench-reasoning-%04d", i),
			EventKind:   models.EventKindReasoning,
			ActorRole:   models.ActorRoleAssistant,
			TextContent: strings.Repeat("consider parse throughput, search posting construction, and dashboard JSON serialization ", 3),
			Model:       "gpt-5.4",
			Tokens:      320,
			Timestamp:   base.Add(time.Duration(i)*time.Minute + 20*time.Second),
		}
		call := views.EventSummary{
			EventUID:   fmt.Sprintf("bench-tool-call-%04d", i),
			EventKind:  models.EventKindToolCall,
			ActorRole:  models.ActorRoleAssistant,
			ToolName:   "Bash",
			ToolUseID:  fmt.Sprintf("toolu-%04d", i),
			DurationMs: int64(100 + i%50),
			Timestamp:  base.Add(time.Duration(i)*time.Minute + 30*time.Second),
		}
		result := views.EventSummary{
			EventUID:    fmt.Sprintf("bench-tool-result-%04d", i),
			EventKind:   models.EventKindToolResult,
			ActorRole:   models.ActorRoleTool,
			ToolName:    "Bash",
			ToolUseID:   call.ToolUseID,
			TextContent: strings.Repeat("internal/perf/bench_test.go\ninternal/mcp/tools.go\n", 3),
			Timestamp:   base.Add(time.Duration(i)*time.Minute + 35*time.Second),
		}
		turns = append(turns, views.ChatTurn{
			TurnSeq:     i + 1,
			TotalTokens: user.Tokens + assistant.Tokens + reasoning.Tokens,
			StartedAt:   user.Timestamp,
			Blocks: []views.ChatBlock{
				{Kind: views.ChatBlockUserMessage, Message: &user},
				{Kind: views.ChatBlockReasoning, Messages: []views.EventSummary{reasoning}},
				{Kind: views.ChatBlockAssistantMessage, Message: &assistant},
				{
					Kind: views.ChatBlockToolChain,
					ToolChain: []views.ToolChainItem{{
						CallEvent:   call,
						ResultEvent: &result,
						ToolName:    "Bash",
						InputJSON:   `{"command":"rg benchmark internal","description":"find benchmark code"}`,
						OutputJSON:  result.TextContent,
						Params: &views.ToolCallParams{
							Command:     "rg benchmark internal",
							Description: "find benchmark code",
						},
					}},
				},
			},
			ToolStats: []views.ToolStatEntry{{Name: "Bash", Count: 1}},
		})
	}
	return turns
}
