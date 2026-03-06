package partials

import "testing"

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
		got := truncateID(tt.input)
		if got != tt.expected {
			t.Errorf("truncateID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestActivityDotColor(t *testing.T) {
	tests := []struct {
		eventType string
		expected  string
	}{
		{"message", "bg-blue-400"},
		{"response", "bg-purple-400"},
		{"model_call", "bg-purple-400"},
		{"tool_call", "bg-yellow-400"},
		{"tool_result", "bg-yellow-400"},
		{"error", "bg-red-400"},
		{"unknown", "bg-gray-400"},
		{"", "bg-gray-400"},
		{"reasoning", "bg-gray-400"},
	}

	for _, tt := range tests {
		got := activityDotColor(tt.eventType)
		if got != tt.expected {
			t.Errorf("activityDotColor(%q) = %q, want %q", tt.eventType, got, tt.expected)
		}
	}
}
