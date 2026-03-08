package pages

import (
	"testing"

	"github.com/technodrome-ai/technodrome/internal/views"
)

func TestDashboardTokensByModelData(t *testing.T) {
	models := []views.ModelTokens{
		{Model: "claude-3", Input: 100, Output: 200, CacheRead: 300, Total: 600},
		{Model: "claude-4", Input: 400, Output: 500, CacheRead: 600, Total: 1500},
	}

	result := dashboardTokensByModelData(models)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any")
	}

	labels, ok := m["labels"].([]string)
	if !ok || len(labels) != 2 {
		t.Fatal("expected 2 labels")
	}
	if labels[0] != "claude-3" || labels[1] != "claude-4" {
		t.Errorf("unexpected labels: %v", labels)
	}

	datasets, ok := m["datasets"].([]map[string]any)
	if !ok || len(datasets) != 3 {
		t.Fatal("expected 3 datasets (Input, Output, Cache Read)")
	}
	if datasets[0]["label"] != "Input" {
		t.Errorf("expected first dataset label 'Input', got %v", datasets[0]["label"])
	}
}
