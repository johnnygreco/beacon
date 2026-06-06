package store

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

var (
	storeBenchSearchDocs     []models.SearchDocument
	storeBenchSearchPostings []models.SearchPosting
)

func BenchmarkStoreBuildSearchRows(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		events, payloads := benchSearchIndexRows(size)
		b.Run(fmt.Sprintf("%dEvents", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				storeBenchSearchDocs, storeBenchSearchPostings = buildSearchRows(events, payloads)
			}
			if len(storeBenchSearchDocs) == 0 || len(storeBenchSearchPostings) == 0 {
				b.Fatal("expected search rows")
			}
		})
	}
}

func benchSearchIndexRows(n int) ([]models.Event, []models.ToolPayload) {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	events := make([]models.Event, 0, n)
	payloads := make([]models.ToolPayload, 0, n/2)
	for i := 0; i < n; i++ {
		kind := models.EventKindMessage
		role := models.ActorRoleAssistant
		text := strings.Repeat("dashboard search mcp responsiveness benchmark token analytics transcript rendering ", 1+i%5)
		toolName := ""
		payloadType := "text"
		if i%5 == 2 {
			kind = models.EventKindReasoning
			text = strings.Repeat("reason about ClickHouse query plans and posting frequency windows ", 3)
		}
		if i%5 == 3 {
			kind = models.EventKindToolCall
			payloadType = "tool_use"
			toolName = "Bash"
			text = toolName
		}
		if i%5 == 4 {
			kind = models.EventKindToolResult
			payloadType = models.EventKindToolResult
			role = models.ActorRoleTool
			toolName = "Bash"
			text = strings.Repeat("internal/perf/bench_test.go\ninternal/mcp/tools.go\n", 4)
		}
		eventUID := fmt.Sprintf("bench-event-%06d", i)
		events = append(events, models.Event{
			EventUID:    eventUID,
			SessionID:   fmt.Sprintf("bench-session-%04d", i/80),
			Provider:    models.ProviderOpenAI,
			EventKind:   kind,
			PayloadType: payloadType,
			ActorRole:   role,
			Timestamp:   base.Add(time.Duration(i) * time.Second),
			TextContent: text,
			TextPreview: truncateString(text, 320),
			ToolName:    toolName,
			Model:       "gpt-5.4",
			PayloadJSON: fmt.Sprintf(`{"event":%d,"query":"binary search","path":"internal/store/search_index.go"}`, i),
		})
		if toolName != "" {
			payload := models.ToolPayload{
				EventUID:      eventUID,
				ToolName:      toolName,
				InputJSON:     `{"command":"rg benchmark internal","description":"find benchmark surfaces"}`,
				OutputJSON:    strings.Repeat("BenchmarkStoreBuildSearchRows\nBenchmarkMCPFormatSearchResults\n", 5),
				InputPreview:  `{"command":"rg benchmark internal"}`,
				OutputPreview: strings.Repeat("BenchmarkStoreBuildSearchRows\n", 4),
			}
			if kind == models.EventKindToolCall {
				payload.ToolPhase = models.ToolPhaseCall
			} else {
				payload.ToolPhase = models.ToolPhaseResult
			}
			payloads = append(payloads, payload)
		}
	}
	return events, payloads
}
