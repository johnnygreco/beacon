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
