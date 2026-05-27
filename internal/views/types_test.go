package views

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{100, "100"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999999, "1000.0K"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
	}

	for _, tt := range tests {
		got := FormatTokens(tt.input)
		if got != tt.expected {
			t.Errorf("FormatTokens(%d) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestSumTokens(t *testing.T) {
	tests := []struct {
		name     string
		events   []EventSummary
		expected int64
	}{
		{"nil slice", nil, 0},
		{"empty slice", []EventSummary{}, 0},
		{"single event", []EventSummary{{Tokens: 150}}, 150},
		{"multiple events", []EventSummary{{Tokens: 100}, {Tokens: 250}, {Tokens: 50}}, 400},
		{"zero tokens", []EventSummary{{Tokens: 0}, {Tokens: 0}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SumTokens(tt.events)
			if got != tt.expected {
				t.Errorf("SumTokens() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestContextWindowTokensForModel(t *testing.T) {
	tests := []struct {
		model string
		want  int64
	}{
		{"claude-sonnet-4", 200_000},
		{"claude-haiku-4", 200_000},
		{"gpt-5.4-codex", 1_050_000},
		{"gpt-4.1", 1_000_000},
		{"gpt-4o", 128_000},
		{"o3-pro", 200_000},
		{"local-experimental", 0},
	}
	for _, tt := range tests {
		if got := ContextWindowTokensForModel(tt.model); got != tt.want {
			t.Fatalf("ContextWindowTokensForModel(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func TestSessionWithContextEstimate(t *testing.T) {
	session := SessionSummary{
		TotalTokens: 42_000,
		ActiveModel: "claude-sonnet-4",
		ChildSessions: []SessionSummary{{
			TotalTokens: 7_400,
			ActiveModel: "unknown-local",
		}},
	}
	got := SessionWithContextEstimate(session)
	if got.ContextTokens != 0 || got.ContextWindowTokens != 200_000 || got.ContextEstimate {
		t.Fatalf("parent context fields = %#v", got)
	}
	if got.ChildSessions[0].ContextTokens != 0 || got.ChildSessions[0].ContextWindowTokens != 0 || got.ChildSessions[0].ContextEstimate {
		t.Fatalf("child context fields = %#v", got.ChildSessions[0])
	}
}

func TestChartDatasetJSONTags(t *testing.T) {
	ds := ChartDataset{Label: "Input", Values: []float64{1, 2, 3}}
	b, err := json.Marshal(ds)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify lowercase keys from json tags
	if _, ok := parsed["label"]; !ok {
		t.Error("expected lowercase 'label' key")
	}
	if _, ok := parsed["values"]; !ok {
		t.Error("expected lowercase 'values' key")
	}
	// Should NOT have uppercase keys
	if _, ok := parsed["Label"]; ok {
		t.Error("did not expect uppercase 'Label' key")
	}
}

func TestRelativeTime(t *testing.T) {
	old := time.Now().Add(-45 * 24 * time.Hour)
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{"zero time", time.Time{}, ""},
		{"just now", time.Now().Add(-10 * time.Second), "just now"},
		{"minutes ago", time.Now().Add(-3 * time.Minute), "3m ago"},
		{"hours ago", time.Now().Add(-2 * time.Hour), "2h ago"},
		{"days ago", time.Now().Add(-48 * time.Hour), "2d ago"},
		{"older dates use absolute time", old, FormatTime(old)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelativeTime(tt.input)
			if got != tt.expected {
				t.Errorf("RelativeTime() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatTimeCompactIncludesYearForOldSessions(t *testing.T) {
	old := time.Date(2001, time.February, 3, 4, 5, 0, 0, time.Local)
	if got, want := FormatTimeCompact(old), "2/3/2001 4:05 AM"; got != want {
		t.Fatalf("FormatTimeCompact(old) = %q, want %q", got, want)
	}

	recent := time.Date(time.Now().Year(), time.March, 7, 15, 4, 0, 0, time.Local)
	if got, want := FormatTimeCompact(recent), "3/7 3:04 PM"; got != want {
		t.Fatalf("FormatTimeCompact(recent) = %q, want %q", got, want)
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abcdefghij", "abcdefgh"},
		{"abcdefgh", "abcdefgh"},
		{"abc", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		got := TruncateID(tt.input)
		if got != tt.expected {
			t.Errorf("TruncateID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSortModelsByProvider(t *testing.T) {
	models := []ModelTokens{
		{Model: "opus-4-6", Provider: "anthropic", Total: 5000},
		{Model: "o4-mini", Provider: "openai", Total: 3000},
		{Model: "sonnet-4-6", Provider: "anthropic", Total: 2000},
		{Model: "o3-pro", Provider: "openai", Total: 8000},
	}

	sorted := SortModelsByProvider(models)

	// OpenAI has higher total (11K) so it should come first
	if sorted[0].Provider != "openai" || sorted[0].Model != "o3-pro" {
		t.Errorf("expected openai/o3-pro first, got %s/%s", sorted[0].Provider, sorted[0].Model)
	}
	if sorted[1].Provider != "openai" || sorted[1].Model != "o4-mini" {
		t.Errorf("expected openai/o4-mini second, got %s/%s", sorted[1].Provider, sorted[1].Model)
	}
	if sorted[2].Provider != "anthropic" || sorted[2].Model != "opus-4-6" {
		t.Errorf("expected anthropic/opus-4-6 third, got %s/%s", sorted[2].Provider, sorted[2].Model)
	}
	if sorted[3].Provider != "anthropic" || sorted[3].Model != "sonnet-4-6" {
		t.Errorf("expected anthropic/sonnet-4-6 fourth, got %s/%s", sorted[3].Provider, sorted[3].Model)
	}
}

func TestSortModelsByProvider_SingleProvider(t *testing.T) {
	models := []ModelTokens{
		{Model: "sonnet-4-6", Provider: "anthropic", Total: 2000},
		{Model: "opus-4-6", Provider: "anthropic", Total: 5000},
	}

	sorted := SortModelsByProvider(models)
	if sorted[0].Model != "opus-4-6" {
		t.Errorf("expected opus-4-6 first (highest total), got %s", sorted[0].Model)
	}
}

func TestMultiSeriesChartJSONTags(t *testing.T) {
	chart := MultiSeriesChart{
		Labels: []string{"a", "b"},
		Datasets: []ChartDataset{
			{Label: "Input", Values: []float64{10, 20}},
		},
	}
	b, err := json.Marshal(chart)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := parsed["labels"]; !ok {
		t.Error("expected lowercase 'labels' key")
	}
	if _, ok := parsed["datasets"]; !ok {
		t.Error("expected lowercase 'datasets' key")
	}
}
