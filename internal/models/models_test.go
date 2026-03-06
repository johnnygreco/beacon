package models

import "testing"

func TestIsMCPTool(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"mcp__slack__send_message", true},
		{"mcp__github__create_issue", true},
		{"Read", false},
		{"Write", false},
		{"Bash", false},
		{"", false},
		{"mcp_", false},
		{"mcp__", true},
	}

	for _, tt := range tests {
		got := IsMCPTool(tt.name)
		if got != tt.expected {
			t.Errorf("IsMCPTool(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}
