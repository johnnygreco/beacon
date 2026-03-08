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
	models := []views.ChartDataset{
		{Label: "claude-sonnet", Values: []float64{100, 200, 50}},
		{Label: "gpt-4o", Values: []float64{30, 40, 10}},
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

	// Should have labels: ["Input", "Output", "Cache Read"]
	var labels []string
	if err := json.Unmarshal(parsed["labels"], &labels); err != nil {
		t.Fatalf("unmarshal labels: %v", err)
	}
	if len(labels) != 3 {
		t.Fatalf("expected 3 labels, got %d", len(labels))
	}
	if labels[0] != "Input" || labels[1] != "Output" || labels[2] != "Cache Read" {
		t.Errorf("unexpected labels: %v", labels)
	}

	// Should have datasets with lowercase keys
	var datasets []map[string]json.RawMessage
	if err := json.Unmarshal(parsed["datasets"], &datasets); err != nil {
		t.Fatalf("unmarshal datasets: %v", err)
	}
	if len(datasets) != 2 {
		t.Fatalf("expected 2 datasets, got %d", len(datasets))
	}
	if _, ok := datasets[0]["label"]; !ok {
		t.Error("expected 'label' key in dataset")
	}
	if _, ok := datasets[0]["data"]; !ok {
		t.Error("expected 'data' key in dataset")
	}
}
