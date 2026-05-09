package pages

import (
	"bytes"
	"context"
	"os"
	"strings"
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
		t.Fatal("expected 3 datasets (Input, Output, Cache)")
	}
	if datasets[0]["label"] != "Input" {
		t.Errorf("expected first dataset label 'Input', got %v", datasets[0]["label"])
	}
	if datasets[2]["label"] != "Cache" {
		t.Errorf("expected third dataset label 'Cache', got %v", datasets[2]["label"])
	}
}

func TestDashboardGoToSessionUsesParsedPathname(t *testing.T) {
	var buf bytes.Buffer
	if err := Dashboard(views.DashboardData{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `/static/js/dashboard.js`) {
		t.Fatal("dashboard page does not load dashboard.js")
	}
	script := dashboardClientScript(t)
	for _, expected := range []string{
		"new URL(String(url || ''), window.location.origin)",
		"parsed.pathname.split('/')",
		"openSessionInspector(decodeURIComponent(id))",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("dashboard script missing %q", expected)
		}
	}
	if strings.Contains(script, "split('/sessions/')[1]") {
		t.Fatal("dashboard script still parses the full href")
	}
}

func TestDashboardLiveAnalyticsChartsUseSharedRange(t *testing.T) {
	var buf bytes.Buffer
	if err := Dashboard(views.DashboardData{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := buf.String()
	script := dashboardClientScript(t)
	for _, expected := range []string{
		"dashboardTokenCumulativeChart",
		"dashboardModelActivityChart",
		"dashboard-token-cumulative-data",
		"dashboard-model-activity-data",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("dashboard live analytics missing %q", expected)
		}
	}
	for _, expected := range []string{
		"/api/dashboard/charts?range=",
		"currentRange = '24h'",
		"setDashboardMetric",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("dashboard live analytics missing %q", expected)
		}
	}
	if strings.Contains(html, "dashboardTotalTokensChart") || strings.Contains(html, "dashboardTokensByModelChart") {
		t.Fatal("dashboard still renders the old redundant chart ids")
	}
}

func dashboardClientScript(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../../static/js/dashboard.js")
	if err != nil {
		t.Fatalf("read dashboard.js: %v", err)
	}
	return string(body)
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
