package pages

import (
	"encoding/json"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

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
	// Multi-provider with spacer: ["Claude Code · sonnet", "", "Codex · gpt-4o"]
	if len(labels) != 3 {
		t.Fatalf("expected 3 labels (2 models + 1 spacer), got %d: %v", len(labels), labels)
	}
	// Anthropic has higher total (300) so it comes first
	if labels[0] != "Claude Code · sonnet" {
		t.Errorf("expected first label 'Claude Code · sonnet', got %q", labels[0])
	}
	if labels[1] != "" {
		t.Errorf("expected spacer label '', got %q", labels[1])
	}
	if labels[2] != "Codex · gpt-4o" {
		t.Errorf("expected third label 'Codex · gpt-4o', got %q", labels[2])
	}

	var datasets []map[string]json.RawMessage
	if err := json.Unmarshal(parsed["datasets"], &datasets); err != nil {
		t.Fatalf("unmarshal datasets: %v", err)
	}
	if len(datasets) != 3 {
		t.Fatalf("expected 3 datasets (Input/Output/Cache), got %d", len(datasets))
	}
}
