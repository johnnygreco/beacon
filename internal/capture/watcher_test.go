package capture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWatcherFindSourceExpandsHomeGlobs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	w := &Watcher{
		sources: []WatchSource{
			{
				Name:  "hermes",
				Globs: []string{"~/.hermes/state.db"},
			},
		},
	}

	file := filepath.Join(home, ".hermes", "state.db")
	src := w.findSource(file)
	if src == nil {
		t.Fatalf("findSource(%q) = nil, want hermes source", file)
	} else if src.Name != "hermes" {
		t.Fatalf("findSource(%q).Name = %q, want hermes", file, src.Name)
	}
}

func TestLineParserCheckpointStateSeedsReplayModel(t *testing.T) {
	events := []NormalizedEvent{
		{
			SessionID:    "line-parser-session",
			Model:        "gpt-5.5",
			SourceOffset: 0,
			SourceLineNo: 1,
		},
		{
			SessionID:          "line-parser-session",
			PayloadType:        "token_count",
			TokenUsageTotalKey: "100:0:0:100",
			SourceOffset:       14,
			SourceLineNo:       2,
		},
	}
	state := buildLineParserCheckpointState(lineParserState{}, events, []replayLine{{offset: 14, lineNo: 2}})
	encoded := encodeLineParserCheckpointState(state, nil)
	decoded, ok := decodeLineParserCheckpointState(encoded, nil)
	if !ok {
		t.Fatalf("decode checkpoint state failed")
	}
	if decoded.ReplayState.Models["line-parser-session"] != "gpt-5.5" {
		t.Fatalf("replay model seed = %q, want gpt-5.5", decoded.ReplayState.Models["line-parser-session"])
	}

	replayed := []NormalizedEvent{
		{
			SessionID:    "line-parser-session",
			PayloadType:  "token_count",
			SourceOffset: 14,
			SourceLineNo: 2,
		},
	}
	PropagateModelWithInitial(replayed, decoded.ReplayState.Models)
	if replayed[0].Model != "gpt-5.5" {
		t.Fatalf("replayed event model = %q, want gpt-5.5", replayed[0].Model)
	}
}

func TestLineParserCheckpointStateSeedsReplayTokenTotal(t *testing.T) {
	events := []NormalizedEvent{
		{
			SessionID:          "line-parser-session",
			PayloadType:        "token_count",
			TokenUsageTotalKey: "100:0:0:100",
			InputTokens:        100,
			SourceOffset:       0,
			SourceLineNo:       1,
		},
		{
			SessionID:          "line-parser-session",
			PayloadType:        "token_count",
			TokenUsageTotalKey: "100:0:0:100",
			InputTokens:        100,
			SourceOffset:       20,
			SourceLineNo:       2,
		},
	}
	state := buildLineParserCheckpointState(lineParserState{}, events, []replayLine{{offset: 20, lineNo: 2}})
	if state.ReplayState.TokenUsageTotals["line-parser-session"] != "100:0:0:100" {
		t.Fatalf("replay token seed = %q, want prior total", state.ReplayState.TokenUsageTotals["line-parser-session"])
	}

	replayed := []NormalizedEvent{
		{
			SessionID:          "line-parser-session",
			SourceName:         "custom-jsonl-source",
			PayloadType:        "token_count",
			TokenUsageTotalKey: "100:0:0:100",
			InputTokens:        100,
			SourceOffset:       20,
			SourceLineNo:       2,
		},
	}
	DeduplicateTokensWithInitial(replayed, state.ReplayState.TokenUsageTotals)
	if replayed[0].InputTokens != 0 {
		t.Fatalf("replayed duplicate input tokens = %d, want 0", replayed[0].InputTokens)
	}
}

func TestReplayStartFromPrefixReturnsBoundedTail(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	content := "one\n" + "two\n" + "three\n" + "four\n"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	offset, lineNo := replayStartFromPrefix(file, int64(len(content)), 4, 2, nil)
	if offset != int64(len("one\n"+"two\n")) {
		t.Fatalf("replay offset = %d, want %d", offset, len("one\n"+"two\n"))
	}
	if lineNo != 2 {
		t.Fatalf("replay lineNo = %d, want 2", lineNo)
	}
}

func TestTailLinesBeforeOffsetUsesBoundedWindow(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	outside := "outside-1\noutside-2\n"
	tail := "keep-1\nkeep-2\n"
	content := outside + tail
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lines := tailLinesBeforeOffset(file, int64(len(content)), 4, 10, int64(len(tail)), nil)
	if len(lines) != 2 {
		t.Fatalf("tail lines = %d, want 2", len(lines))
	}
	if lines[0].lineNo != 3 || string(lines[0].line) != "keep-1" {
		t.Fatalf("first tail line = line %d %q, want line 3 keep-1", lines[0].lineNo, lines[0].line)
	}
	if lines[1].lineNo != 4 || string(lines[1].line) != "keep-2" {
		t.Fatalf("second tail line = line %d %q, want line 4 keep-2", lines[1].lineNo, lines[1].line)
	}
}
