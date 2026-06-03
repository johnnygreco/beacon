package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestSessionDetailRendersThemedTranscriptShell(t *testing.T) {
	data := views.SessionDetailData{
		Session: views.SessionSummary{
			ID:              "session-render-test",
			Status:          "completed",
			Provider:        "openai",
			Duration:        "38m 12s",
			TotalTokens:     123456,
			InputTokens:     100000,
			OutputTokens:    23456,
			CacheReadTokens: 31100,
			TurnCount:       14,
			ToolCallCount:   42,
			WorkingDir:      "/Users/example/projects/beacon",
		},
	}

	var buf bytes.Buffer
	if err := SessionDetail(data).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render session detail: %v", err)
	}
	html := buf.String()

	for _, expected := range []string{
		"localStorage.getItem('beacon-dashboard-resolved-theme')",
		"document.documentElement.setAttribute('data-dashboard-theme', theme)",
		"document.body.setAttribute('data-page', 'transcript')",
		`id="transcript-wrap"`,
		`class="transcript-header border`,
		`class="transcript-conversation"`,
		`class="transcript-metric-grid text-sm"`,
		`aria-pressed="true"`,
		`/sessions/session-render-test/conversation`,
		`hx-trigger="load, sse:conversation-update"`,
		`/static/js/charts/core.js?v=`,
		`/static/js/charts/bootstrap.js?v=`,
		`/static/js/transcript.js?v=`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("session detail shell missing %q", expected)
		}
	}
	for _, removed := range []string{
		`id="sidebar"`,
		`ml-14`,
		`func Nav`,
	} {
		if strings.Contains(html, removed) {
			t.Fatalf("session detail shell still contains removed sidebar marker %q", removed)
		}
	}
}

func TestMultiSeriesChartData(t *testing.T) {
	chart := views.MultiSeriesChart{
		Labels: []string{"t1", "t2"},
		Datasets: []views.ChartDataset{
			{Label: "Input", Values: []float64{100, 200}},
			{Label: "Output", Values: []float64{50, 75}},
		},
	}

	result := multiSeriesChartData(chart)
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := parsed["labels"]; !ok {
		t.Error("expected 'labels' key in JSON output")
	}
	if _, ok := parsed["datasets"]; !ok {
		t.Error("expected 'datasets' key in JSON output")
	}

	// Verify datasets contain lowercase keys from json tags
	var datasets []map[string]json.RawMessage
	if err := json.Unmarshal(parsed["datasets"], &datasets); err != nil {
		t.Fatalf("unmarshal datasets: %v", err)
	}
	if len(datasets) != 2 {
		t.Fatalf("expected 2 datasets, got %d", len(datasets))
	}
	if _, ok := datasets[0]["label"]; !ok {
		t.Error("expected 'label' key in dataset (lowercase)")
	}
	if _, ok := datasets[0]["values"]; !ok {
		t.Error("expected 'values' key in dataset (lowercase)")
	}
}

func TestMultiSeriesChartData_Empty(t *testing.T) {
	chart := views.MultiSeriesChart{}
	result := multiSeriesChartData(chart)
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// labels should be null (nil slice), datasets should be null
	if _, ok := parsed["labels"]; !ok {
		t.Error("expected 'labels' key even when empty")
	}
	if _, ok := parsed["datasets"]; !ok {
		t.Error("expected 'datasets' key even when empty")
	}
}

func TestTokensByModelData(t *testing.T) {
	models := []views.ModelTokens{
		{Model: "claude-sonnet", Provider: "anthropic", Input: 100, Output: 200, CacheRead: 50, Total: 300},
		{Model: "gpt-4o", Provider: "openai", Input: 30, Output: 40, CacheRead: 10, Total: 70},
	}

	result := tokensByModelData(models)
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Now uses same format as dashboard: labels = model names, datasets = Input/Output/Cache
	var labels []string
	if err := json.Unmarshal(parsed["labels"], &labels); err != nil {
		t.Fatalf("unmarshal labels: %v", err)
	}
	// Multi-provider: short model names only, no spacer, with provider group metadata
	// ["sonnet", "gpt-4o"]
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %v", len(labels), labels)
	}
	// Anthropic has higher total (300) so it comes first
	if labels[0] != "sonnet" {
		t.Errorf("expected first label 'sonnet', got %q", labels[0])
	}
	if labels[1] != "gpt-4o" {
		t.Errorf("expected second label 'gpt-4o', got %q", labels[1])
	}

	var datasets []map[string]json.RawMessage
	if err := json.Unmarshal(parsed["datasets"], &datasets); err != nil {
		t.Fatalf("unmarshal datasets: %v", err)
	}
	if len(datasets) != 3 {
		t.Fatalf("expected 3 datasets (Input/Output/Cache), got %d", len(datasets))
	}
}
