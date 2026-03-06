package views

import "testing"

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
