package textindex

import (
	"reflect"
	"strings"
	"testing"
)

func TestTokenize(t *testing.T) {
	long := strings.Repeat("a", maxTokenLen+20)
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "empty input",
			text: "",
			want: nil,
		},
		{
			name: "letters lowercased",
			text: "Hello Beacon HELLO",
			want: []string{"hello", "beacon", "hello"},
		},
		{
			name: "digits and mixed model ids",
			text: "GPT-4o 2026 run 123",
			want: []string{"gpt-4o", "2026", "run", "123"},
		},
		{
			name: "underscores hyphens slashes and dots stay in tokens",
			text: "mcp__github__search /Users/donnie/src/main.go claude-sonnet-4.5",
			want: []string{"mcp__github__search", "/users/donnie/src/main.go", "claude-sonnet-4.5"},
		},
		{
			name: "punctuation splits tokens",
			text: "read(file), then write:file",
			want: []string{"read", "file", "then", "write", "file"},
		},
		{
			name: "stop words and single letters removed",
			text: "a an the x y beacon in repo",
			want: []string{"beacon", "repo"},
		},
		{
			name: "unicode letters are preserved and lowercased",
			text: "Crème BRÛLÉE 東京",
			want: []string{"crème", "brûlée", "東京"},
		},
		{
			name: "max token length truncates token",
			text: long,
			want: []string{strings.Repeat("a", maxTokenLen)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Tokenize(%q) = %#v, want %#v", tt.text, got, tt.want)
			}
		})
	}
}

func TestFrequencies(t *testing.T) {
	got := Frequencies([]string{"beacon", "search", "beacon", "tool", "search", "beacon"})
	want := map[string]int{
		"beacon": 3,
		"search": 2,
		"tool":   1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Frequencies() = %#v, want %#v", got, want)
	}
}

func TestFrequenciesEmpty(t *testing.T) {
	got := Frequencies(nil)
	if len(got) != 0 {
		t.Fatalf("Frequencies(nil) = %#v, want empty map", got)
	}
}
