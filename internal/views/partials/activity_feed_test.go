package partials

import "testing"

func TestActivityDotColor(t *testing.T) {
	tests := []struct {
		eventType string
		expected  string
	}{
		{"message", "bg-blue-400"},
		{"tool_call", "bg-yellow-400"},
		{"error", "bg-red-400"},
		{"session_meta", "bg-teal-400"},
		{"unknown", "bg-gray-400"},
		{"", "bg-gray-400"},
	}

	for _, tt := range tests {
		got := activityDotColor(tt.eventType)
		if got != tt.expected {
			t.Errorf("activityDotColor(%q) = %q, want %q", tt.eventType, got, tt.expected)
		}
	}
}
