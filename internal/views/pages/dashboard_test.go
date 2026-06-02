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
	if !strings.Contains(html, `/static/js/dashboard/controls.js`) {
		t.Fatal("dashboard page does not load split dashboard scripts")
	}
	script := dashboardClientScript(t)
	for _, expected := range []string{
		"new URL(String(url || ''), window.location.origin)",
		"parsed.pathname.split('/')",
		"openSessionInspector(decodeURIComponent(id), launcher)",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("dashboard script missing %q", expected)
		}
	}
	if strings.Contains(script, "split('/sessions/')[1]") {
		t.Fatal("dashboard script still parses the full href")
	}
}

func TestDashboardDefaultNameUsesGenericTitleAndHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := Dashboard(views.DashboardData{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{
		"<title>Dashboard | Beacon</title>",
		`data-dashboard-default-name=""`,
		">Beacon Realtime Dashboard</h1>",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("dashboard default naming missing %q", expected)
		}
	}
}

func TestDashboardConfiguredNameRendersSafely(t *testing.T) {
	var buf bytes.Buffer
	name := `Workstation <script>alert("x")</script>`
	if err := Dashboard(views.DashboardData{DashboardName: name}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{
		"<title>Workstation &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; | Beacon</title>",
		`data-dashboard-default-name="Workstation &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;"`,
		">Workstation &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;</h1>",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("dashboard configured naming missing %q in:\n%s", expected, html)
		}
	}
	if strings.Contains(html, name+"</h1>") || strings.Contains(html, "<script>alert") {
		t.Fatal("dashboard configured name was rendered without escaping")
	}
}

func TestDashboardLiveAnalyticsUsesSingleTokenChart(t *testing.T) {
	var buf bytes.Buffer
	if err := Dashboard(views.DashboardData{}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render dashboard: %v", err)
	}
	html := buf.String()
	script := dashboardClientScript(t)
	for _, expected := range []string{
		"dashboardTokenCumulativeChart",
		"dashboard-token-cumulative-data",
		"Tokens by Model Over Time",
		"dashboard-chart-range-control",
		"dashboard-chart-refresh-btn",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("dashboard live analytics missing %q", expected)
		}
	}
	for _, expected := range []string{
		"requestURL('/api/dashboard/charts'",
		"currentChartRange = '24h'",
		"setDashboardChartRange",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("dashboard live analytics missing %q", expected)
		}
	}
	for _, unexpected := range []string{
		"dashboardTotalTokensChart",
		"dashboardTokensByModelChart",
		"dashboardModelActivityChart",
		"dashboard-model-activity-data",
		"dashboard-model-metric-control",
		"Model Health",
	} {
		if strings.Contains(html, unexpected) {
			t.Fatalf("dashboard still renders redundant analytics UI %q", unexpected)
		}
	}
	for _, unexpected := range []string{
		"setDashboardMetric",
		"currentDashboardMetric",
		"updateDashboardModelActivityChart",
	} {
		if strings.Contains(script, unexpected) {
			t.Fatalf("dashboard client script still references redundant analytics UI %q", unexpected)
		}
	}
}

func dashboardClientScript(t *testing.T) string {
	t.Helper()
	paths := []string{
		"../../../static/js/dashboard/utils.js",
		"../../../static/js/dashboard/theme.js",
		"../../../static/js/dashboard/inspector.js",
		"../../../static/js/dashboard/timeline.js",
		"../../../static/js/dashboard/state.js",
		"../../../static/js/dashboard/name.js",
		"../../../static/js/dashboard/table.js",
		"../../../static/js/dashboard/render.js",
		"../../../static/js/dashboard/controls.js",
	}
	var out strings.Builder
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		out.Write(body)
		out.WriteByte('\n')
	}
	return out.String()
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
