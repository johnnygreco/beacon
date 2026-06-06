package web

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

var (
	webBenchBytes          []byte
	webBenchSessionItems   []APISessionSummary
	webBenchSearchResults  []APIDashboardSearchResult
	webBenchSerializedSize int
)

func BenchmarkAPIModelConversionSessionSummaries(b *testing.B) {
	sessions := benchViewSessions(200)

	b.ReportAllocs()
	for b.Loop() {
		items := make([]APISessionSummary, 0, len(sessions))
		for _, session := range sessions {
			items = append(items, apiSessionSummaryFromView(session))
		}
		webBenchSessionItems = items
	}
	if len(webBenchSessionItems) != len(sessions) {
		b.Fatalf("converted %d sessions, want %d", len(webBenchSessionItems), len(sessions))
	}
}

func BenchmarkAPIJSONSerialization(b *testing.B) {
	sessions := benchViewSessions(200)
	sessionItems := make([]APISessionSummary, 0, len(sessions))
	for _, session := range sessions {
		sessionItems = append(sessionItems, apiSessionSummaryFromView(session))
	}
	searchItems := benchDashboardSearchResults(200)
	charts := benchDashboardCharts()

	cases := []struct {
		name    string
		payload any
	}{
		{
			name: "DashboardSessions",
			payload: APIDashboardSessionsResponse{
				State:   "completed",
				Range:   "24h",
				Query:   "benchmark",
				Offset:  0,
				Limit:   len(sessionItems),
				HasMore: true,
				Items:   sessionItems,
			},
		},
		{
			name: "DashboardSearch",
			payload: APIDashboardSearchResponse{
				State:     "ready",
				Query:     "binary search",
				Range:     "24h",
				EventKind: "event",
				Sort:      "relevance",
				Limit:     len(searchItems),
				HasMore:   true,
				Items:     searchItems,
			},
		},
		{
			name:    "DashboardCharts",
			payload: charts,
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var err error
				webBenchBytes, err = json.Marshal(tc.payload)
				if err != nil {
					b.Fatalf("marshal %s: %v", tc.name, err)
				}
			}
			webBenchSerializedSize = len(webBenchBytes)
			if webBenchSerializedSize == 0 {
				b.Fatal("expected serialized payload")
			}
		})
	}
}

func BenchmarkDashboardSearchResultShaping(b *testing.B) {
	sessions := benchViewSessions(200)

	b.ReportAllocs()
	for b.Loop() {
		items := make([]APIDashboardSearchResult, 0, len(sessions))
		for _, session := range sessions {
			items = append(items, dashboardSearchSessionResult(session))
		}
		dashboardSortSearchItems(items, "newest")
		webBenchSearchResults = items
	}
	if len(webBenchSearchResults) != len(sessions) {
		b.Fatalf("shaped %d results, want %d", len(webBenchSearchResults), len(sessions))
	}
}

func benchViewSessions(n int) []views.SessionSummary {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	sessions := make([]views.SessionSummary, 0, n)
	for i := 0; i < n; i++ {
		child := views.SessionSummary{
			ID:              fmt.Sprintf("bench-child-%04d", i),
			Actor:           "codex",
			Provider:        models.ProviderOpenAI,
			Status:          "completed",
			StartedAt:       base.Add(time.Duration(i) * time.Minute),
			EndedAt:         base.Add(time.Duration(i+4) * time.Minute),
			Duration:        "4m",
			TotalTokens:     int64(12_000 + i),
			InputTokens:     int64(10_000 + i),
			OutputTokens:    int64(2_000 + i),
			ToolCallCount:   int64(i % 8),
			MCPCallCount:    int64(i % 3),
			ErrorCount:      int64(i % 2),
			ActiveModel:     "gpt-5.4",
			WorkingDir:      fmt.Sprintf("/Users/donnie/projects/project-%02d", i%20),
			ParentSessionID: fmt.Sprintf("bench-session-%04d", i),
			HasSessionEnd:   true,
		}
		session := views.SessionSummary{
			ID:                fmt.Sprintf("bench-session-%04d", i),
			Actor:             "codex",
			Provider:          models.ProviderOpenAI,
			Status:            "completed",
			StartedAt:         base.Add(time.Duration(i) * time.Minute),
			EndedAt:           base.Add(time.Duration(i+20) * time.Minute),
			Duration:          "20m",
			TurnCount:         int64(10 + i%20),
			TotalTokens:       int64(80_000 + i*100),
			InputTokens:       int64(65_000 + i*100),
			OutputTokens:      int64(15_000 + i*20),
			CacheReadTokens:   int64(20_000 + i*10),
			CacheCreateTokens: int64(i * 3),
			ToolCallCount:     int64(25 + i%20),
			MCPCallCount:      int64(i % 6),
			ErrorCount:        int64(i % 4),
			ActiveModel:       "gpt-5.4",
			WorkingDir:        fmt.Sprintf("/Users/donnie/projects/project-%02d", i%20),
			HasSessionEnd:     true,
			SubagentCount:     1,
			ChildSessions:     []views.SessionSummary{child},
		}
		sessions = append(sessions, session)
	}
	return sessions
}

func benchDashboardSearchResults(n int) []APIDashboardSearchResult {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	items := make([]APIDashboardSearchResult, 0, n)
	for i := 0; i < n; i++ {
		kind := models.EventKindMessage
		toolName := ""
		if i%4 == 1 {
			kind = models.EventKindToolCall
			toolName = "Bash"
		} else if i%4 == 2 {
			kind = models.EventKindToolResult
			toolName = "Bash"
		}
		items = append(items, APIDashboardSearchResult{
			ResultType:   "event",
			EventUID:     fmt.Sprintf("bench-event-%06d", i),
			SessionID:    fmt.Sprintf("bench-session-%04d", i/10),
			EventKind:    kind,
			Snippet:      fmt.Sprintf("benchmark dashboard search result %d", i),
			ToolName:     toolName,
			Provider:     models.ProviderOpenAI,
			Model:        "gpt-5.4",
			Score:        12.0 - float64(i)*0.01,
			Timestamp:    base.Add(time.Duration(i) * time.Second),
			RelativeTime: views.RelativeTime(base.Add(time.Duration(i) * time.Second)),
			SessionTitle: fmt.Sprintf("project-%02d", i%20),
			WorkingDir:   fmt.Sprintf("/Users/donnie/projects/project-%02d", i%20),
		})
	}
	return items
}

func benchDashboardCharts() APIDashboardCharts {
	labels := make([]string, 96)
	datasets := make([]views.ModelSeriesDataset, 0, 12)
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := range labels {
		labels[i] = base.Add(time.Duration(i*15) * time.Minute).Format(time.RFC3339)
	}
	for i := 0; i < 12; i++ {
		values := make([]float64, len(labels))
		for j := range values {
			values[j] = float64((i + 1) * (j + 1) * 100)
		}
		datasets = append(datasets, views.ModelSeriesDataset{
			Label:           fmt.Sprintf("gpt-5.4-%02d", i),
			Model:           "gpt-5.4",
			Provider:        models.ProviderOpenAI,
			ProviderLabel:   views.ProviderLabel(models.ProviderOpenAI),
			Values:          values,
			TotalTokens:     int64((i + 1) * 100_000),
			InputTokens:     int64((i + 1) * 80_000),
			OutputTokens:    int64((i + 1) * 20_000),
			CacheReadTokens: int64((i + 1) * 40_000),
			ToolCallCount:   int64((i + 1) * 120),
			CallCount:       int64((i + 1) * 240),
			ErrorCount:      int64(i % 3),
		})
	}
	return APIDashboardCharts{
		Range: "24h",
		TokenCumulative: views.ModelSeriesChart{
			Labels:        labels,
			Datasets:      datasets,
			TimeUnit:      "minute",
			BucketMinutes: 15,
			Summary: views.ModelAnalyticsSummary{
				TotalTokens:   1_200_000,
				ToolCallCount: 450,
				CallCount:     900,
				ErrorCount:    4,
				ErrorRate:     0.004,
				ModelCount:    len(datasets),
			},
		},
		ModelActivity: views.ModelMetricChart{
			Labels:        labels,
			Metrics:       map[string]views.ModelMetricSeries{},
			TimeUnit:      "minute",
			BucketMinutes: 15,
		},
	}
}
