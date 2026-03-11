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
	if !ok {
		t.Fatal("expected []string labels")
	}
	// Multi-provider: short model names only (no provider prefix), no spacer.
	// OpenAI has higher total (1500) so it comes first.
	// Expected: ["gpt-5.4", "opus-4-6"] with providerGroups metadata.
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %v", len(labels), labels)
	}
	if labels[0] != "gpt-5.4" {
		t.Errorf("expected first label 'gpt-5.4', got %q", labels[0])
	}
	if labels[1] != "opus-4-6" {
		t.Errorf("expected second label 'opus-4-6', got %q", labels[1])
	}
	// Verify provider groups metadata
	groups, ok := m["providerGroups"].([]map[string]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("expected 2 provider groups, got %v", m["providerGroups"])
	}
	if groups[0]["provider"] != "Codex" {
		t.Errorf("expected first group provider 'Codex', got %v", groups[0]["provider"])
	}
	if groups[1]["provider"] != "Claude Code" {
		t.Errorf("expected second group provider 'Claude Code', got %v", groups[1]["provider"])
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
	// Single provider: no prefix, no spacer. Sorted by total desc.
	if labels[0] != "opus-4-6" || labels[1] != "haiku-4-5" {
		t.Errorf("unexpected labels: %v", labels)
	}
}
