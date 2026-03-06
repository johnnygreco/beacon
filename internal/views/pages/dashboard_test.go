package pages

import "testing"

func TestPercent(t *testing.T) {
	tests := []struct {
		value, max int64
		expected   int
	}{
		{0, 0, 0},
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		{1, 100, 1},
		{1, 10000, 1}, // rounds to 0 but value > 0, so min 1
		{200, 100, 200},
	}

	for _, tt := range tests {
		got := percent(tt.value, tt.max)
		if got != tt.expected {
			t.Errorf("percent(%d, %d) = %d, want %d", tt.value, tt.max, got, tt.expected)
		}
	}
}
