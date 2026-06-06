package textindex

import (
	"strings"
	"testing"
)

var (
	textindexBenchTokens []string
	textindexBenchFreq   map[string]int
)

func BenchmarkTextIndexTokenize(b *testing.B) {
	cases := []struct {
		name string
		text string
	}{
		{
			name: "SearchQuery",
			text: "binary search dashboard mcp search_sessions event_uid session_id tool_call tool_result /Users/donnie/projects/code/beacon internal/perf/bench_test.go",
		},
		{
			name: "ToolPayload",
			text: strings.Repeat(`{"command":"rg --files internal | rg bench","description":"find benchmark coverage","cwd":"/Users/donnie/projects/code/beacon"} `, 24),
		},
		{
			name: "Transcript",
			text: strings.Repeat("The dashboard must feel instantaneous while filtering completed sessions, opening transcript context, and rendering token analytics. ", 80),
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				textindexBenchTokens = Tokenize(tc.text)
			}
			if len(textindexBenchTokens) == 0 {
				b.Fatal("expected tokens")
			}
		})
	}
}

func BenchmarkTextIndexFrequencies(b *testing.B) {
	tokens := Tokenize(strings.Repeat("dashboard search search_sessions mcp open list_sessions tool_call token analytics response latency ", 128))

	b.ReportAllocs()
	for b.Loop() {
		textindexBenchFreq = Frequencies(tokens)
	}
	if len(textindexBenchFreq) == 0 {
		b.Fatal("expected frequencies")
	}
}
