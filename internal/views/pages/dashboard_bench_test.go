package pages

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

func BenchmarkViewRenderDashboard(b *testing.B) {
	data := benchDashboardData()
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if err := Dashboard(data).Render(ctx, io.Discard); err != nil {
			b.Fatalf("render dashboard: %v", err)
		}
	}
}

func benchDashboardData() views.DashboardData {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	active := make([]views.SessionSummary, 0, 24)
	completed := make([]views.SessionSummary, 0, 80)
	activity := make([]views.ActivityItem, 0, 80)
	for i := 0; i < 80; i++ {
		session := views.SessionSummary{
			ID:                fmt.Sprintf("bench-session-%04d", i),
			Actor:             "codex",
			Provider:          models.ProviderOpenAI,
			Status:            "completed",
			StartedAt:         base.Add(-time.Duration(i+20) * time.Minute),
			EndedAt:           base.Add(-time.Duration(i) * time.Minute),
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
			SubagentCount:     i % 3,
		}
		completed = append(completed, session)
		if i < 24 {
			activeSession := session
			activeSession.Status = "active"
			activeSession.EndedAt = base.Add(time.Duration(i) * time.Second)
			activeSession.HasSessionEnd = false
			active = append(active, activeSession)
		}
		activity = append(activity, views.ActivityItem{
			ID:        fmt.Sprintf("bench-activity-%04d", i),
			Type:      models.EventKindMessage,
			Summary:   fmt.Sprintf("dashboard benchmark activity item %d", i),
			SessionID: session.ID,
			Provider:  models.ProviderOpenAI,
			Timestamp: base.Add(-time.Duration(i) * time.Minute),
		})
	}
	return views.DashboardData{
		Metrics: []views.MetricData{
			{Label: "Total Sessions", Value: "2.5K", Sublabel: "all time", Trend: "neutral"},
			{Label: "Active Sessions", Value: "24", Sublabel: "live", Trend: "up"},
			{Label: "Input Tokens", Value: "120M", Sublabel: "prompt", Trend: "neutral"},
			{Label: "Output Tokens", Value: "18M", Sublabel: "completion", Trend: "neutral"},
		},
		ActiveSessions:    active,
		CompletedSessions: completed,
		RecentActivity:    activity,
		TokenCumulative:   benchModelSeriesChart(),
		HasMoreSessions:   true,
		DashboardName:     "Benchmark Dashboard",
	}
}

func benchModelSeriesChart() views.ModelSeriesChart {
	labels := make([]string, 96)
	datasets := make([]views.ModelSeriesDataset, 0, 8)
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := range labels {
		labels[i] = base.Add(time.Duration(i*15) * time.Minute).Format(time.RFC3339)
	}
	for i := 0; i < 8; i++ {
		values := make([]float64, len(labels))
		for j := range values {
			values[j] = float64((i + 1) * (j + 1) * 75)
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
	return views.ModelSeriesChart{
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
	}
}
