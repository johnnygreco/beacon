package pages

import (
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestDashboardTokensByModelData(t *testing.T) {
	models := []views.ModelTokens{
		{Model: "claude-opus-4-6", Provider: "anthropic", Input: 100, Output: 200, CacheRead: 300, Total: 600},
		{Model: "gpt-5.4", Provider: "openai", Input: 400, Output: 500, CacheRead: 600, Total: 1500},
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
	// Multi-provider: should have provider prefix
	if labels[0] != "Claude Code · opus-4-6" || labels[1] != "Codex · gpt-5.4" {
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

func TestDashboardTokensByModelData_SingleProvider(t *testing.T) {
	models := []views.ModelTokens{
		{Model: "claude-opus-4-6", Provider: "anthropic", Input: 100, Output: 200, CacheRead: 300, Total: 600},
		{Model: "claude-haiku-4-5-20251001", Provider: "anthropic", Input: 50, Output: 100, CacheRead: 150, Total: 300},
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
	// Single provider: no prefix
	if labels[0] != "opus-4-6" || labels[1] != "haiku-4-5" {
		t.Errorf("unexpected labels: %v", labels)
	}
}
