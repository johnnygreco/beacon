package views

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"bold text", "**bold**", "<strong>bold</strong>"},
		{"italic text", "*italic*", "<em>italic</em>"},
		{"code block", "```\ncode\n```", "<code>"},
		{"inline code", "`code`", "<code>code</code>"},
		{"GFM table", "| A | B |\n| - | - |\n| 1 | 2 |", "<table>"},
		{"plain text", "hello world", "<p>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderMarkdown(tt.input)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("RenderMarkdown(%q) = %q, want it to contain %q", tt.input, got, tt.contains)
			}
		})
	}

	t.Run("empty string", func(t *testing.T) {
		got := RenderMarkdown("")
		if strings.TrimSpace(got) != "" {
			t.Errorf("RenderMarkdown(\"\") = %q, want empty or whitespace", got)
		}
	})
}

func TestCleanSystemTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"command-name tag", "<command-name>/commit</command-name>", "/commit"},
		{"command-message tag", "<command-message>do it</command-message>", "do it"},
		{"command-args tag", "<command-args>--force</command-args>", "--force"},
		{
			"system-reminder tag",
			"<system-reminder>\nsome\nmultiline\ncontent\n</system-reminder>",
			"",
		},
		{
			"available-deferred-tools tag",
			"<available-deferred-tools>\ntool1\ntool2\n</available-deferred-tools>",
			"",
		},
		{
			"local-command-caveat tag",
			"<local-command-caveat>caveat text</local-command-caveat>",
			"caveat text",
		},
		{
			"persisted-output tag pair",
			"<persisted-output>saved content</persisted-output>",
			"saved content",
		},
		{
			"orphan persisted-output opening tag",
			"<persisted-output> ",
			"",
		},
		{"no tags", "plain text", "plain text"},
		{
			"mixed content",
			"Hello <system-reminder>ignore this</system-reminder> world",
			"Hello  world",
		},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanSystemTags(tt.input)
			if got != tt.expected {
				t.Errorf("CleanSystemTags(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
