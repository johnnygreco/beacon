package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func TestReadSourceFileSkipsUnterminatedFinalLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	firstLine := `{"msg":"complete"}`
	if err := os.WriteFile(file, []byte(firstLine+"\n"+`{"msg":"partial"`), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	source := WatchSource{
		Name:     "codex",
		Runtime:  models.RuntimeCodex,
		Provider: models.ProviderOpenAI,
		Format:   models.FormatJSONL,
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
			return []NormalizedEvent{{
				SessionID:    "session",
				SourceName:   "codex",
				Runtime:      models.RuntimeCodex,
				Provider:     models.ProviderOpenAI,
				Format:       models.FormatJSONL,
				EventKind:    models.EventKindMessage,
				ActorRole:    models.ActorRoleAssistant,
				Timestamp:    time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
				RawPayload:   string(line),
				SourceFile:   file,
				SourceLineNo: lineNo,
				SourceOffset: offset,
			}}, nil
		},
	}

	result, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("ReadSourceFile: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("events = %d, want only the complete line", len(result.Events))
	}
	if result.Checkpoint == nil || result.Checkpoint.LastLineNo != 1 || result.Checkpoint.LastOffset != int64(len(firstLine)+1) {
		t.Fatalf("checkpoint = %#v, want complete-line offset", result.Checkpoint)
	}
	if len(result.CaptureErrors) != 0 {
		t.Fatalf("capture errors = %#v, want none for held partial line", result.CaptureErrors)
	}

	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString("}\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append completion: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close append: %v", err)
	}
	next, err := ReadSourceFile(context.Background(), source, file, result.Checkpoint, nil)
	if err != nil {
		t.Fatalf("ReadSourceFile next: %v", err)
	}
	if len(next.Events) != 1 || next.Events[0].SourceLineNo != 2 {
		t.Fatalf("next events = %#v, want completed second line", next.Events)
	}
}

func TestReadWholeSourceFileSkipsUnchangedCheckpoint(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.db")
	if err := os.WriteFile(file, []byte("one"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	var parses int
	source := WatchSource{
		Name:     "hermes",
		Runtime:  models.RuntimeHermesAgent,
		Provider: models.ProviderMulti,
		Format:   models.FormatSQLite,
		FileParser: func(file string) ([]NormalizedEvent, error) {
			parses++
			return []NormalizedEvent{{
				SessionID:  "session",
				SourceName: "hermes",
				Runtime:    models.RuntimeHermesAgent,
				Provider:   models.ProviderMulti,
				Format:     models.FormatSQLite,
				EventKind:  models.EventKindMessage,
				ActorRole:  models.ActorRoleAssistant,
				Timestamp:  time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
				SourceFile: file,
			}}, nil
		},
	}

	first, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first.Events) != 1 || parses != 1 || first.Checkpoint == nil {
		t.Fatalf("first read events=%d parses=%d checkpoint=%#v", len(first.Events), parses, first.Checkpoint)
	}
	second, err := ReadSourceFile(context.Background(), source, file, first.Checkpoint, nil)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(second.Events) != 0 || second.Checkpoint != nil || parses != 1 {
		t.Fatalf("unchanged read events=%d checkpoint=%#v parses=%d, want skipped", len(second.Events), second.Checkpoint, parses)
	}
}

func TestReadSourceFileWindowAdvancesLineCheckpoint(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(file, []byte("{\"msg\":\"one\"}\n{\"msg\":\"two\"}\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	source := testLineWindowSource(file)

	first, err := ReadSourceFileWindow(context.Background(), source, file, nil, nil, 1)
	if err != nil {
		t.Fatalf("first window: %v", err)
	}
	if len(first.Events) != 1 || !first.HasMore || first.Checkpoint == nil || first.Checkpoint.LastLineNo != 1 {
		t.Fatalf("first window = events %d hasMore %v checkpoint %#v, want first line with more", len(first.Events), first.HasMore, first.Checkpoint)
	}
	second, err := ReadSourceFileWindow(context.Background(), source, file, first.Checkpoint, nil, 1)
	if err != nil {
		t.Fatalf("second window: %v", err)
	}
	if len(second.Events) != 1 || second.HasMore || second.Checkpoint == nil || second.Checkpoint.LastLineNo != 2 {
		t.Fatalf("second window = events %d hasMore %v checkpoint %#v, want final line", len(second.Events), second.HasMore, second.Checkpoint)
	}
}

func TestReadSourceFileWindowAdvancesWithinMultiEventLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	line := `{"msg":"multi"}`
	if err := os.WriteFile(file, []byte(line+"\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	source := WatchSource{
		Name:     "codex",
		Runtime:  models.RuntimeCodex,
		Provider: models.ProviderOpenAI,
		Format:   models.FormatJSONL,
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
			return []NormalizedEvent{
				{SessionID: "session", EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC), TextContent: "first", SourceFile: file, SourceLineNo: lineNo, SourceOffset: offset, RawPayload: string(line), Model: "gpt-5.5"},
				{SessionID: "session", EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 8, 12, 0, 1, 0, time.UTC), TextContent: "second", SourceFile: file, SourceLineNo: lineNo, SourceOffset: offset, RawPayload: string(line)},
				{SessionID: "session", EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 8, 12, 0, 2, 0, time.UTC), TextContent: "third", SourceFile: file, SourceLineNo: lineNo, SourceOffset: offset, RawPayload: string(line)},
			}, nil
		},
	}

	first, err := ReadSourceFileWindow(context.Background(), source, file, nil, nil, 1)
	if err != nil {
		t.Fatalf("first window: %v", err)
	}
	if len(first.Events) != 1 || first.Events[0].TextContent != "first" || !first.HasMore || first.Checkpoint == nil {
		t.Fatalf("first window events=%#v checkpoint=%#v hasMore=%v", first.Events, first.Checkpoint, first.HasMore)
	}
	if first.Checkpoint.LastOffset != 0 || first.Checkpoint.LastLineNo != 1 {
		t.Fatalf("first checkpoint = %#v, want pending line at offset 0 line 1", first.Checkpoint)
	}
	state, ok := decodeLineParserCheckpointState(first.Checkpoint.StateJSON, nil)
	if !ok || state.PendingLineNo != 1 || state.PendingLineOffset != 0 || state.PendingEventIndex != 1 {
		t.Fatalf("first pending state = %#v ok=%v, want event index 1", state, ok)
	}

	second, err := ReadSourceFileWindow(context.Background(), source, file, first.Checkpoint, nil, 1)
	if err != nil {
		t.Fatalf("second window: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].TextContent != "second" || second.Events[0].Model != "gpt-5.5" || !second.HasMore || second.Checkpoint == nil {
		t.Fatalf("second window events=%#v checkpoint=%#v hasMore=%v", second.Events, second.Checkpoint, second.HasMore)
	}
	state, ok = decodeLineParserCheckpointState(second.Checkpoint.StateJSON, nil)
	if !ok || state.PendingEventIndex != 2 {
		t.Fatalf("second pending state = %#v ok=%v, want event index 2", state, ok)
	}

	third, err := ReadSourceFileWindow(context.Background(), source, file, second.Checkpoint, nil, 1)
	if err != nil {
		t.Fatalf("third window: %v", err)
	}
	if len(third.Events) != 1 || third.Events[0].TextContent != "third" || third.Events[0].Model != "gpt-5.5" || third.HasMore || third.Checkpoint == nil {
		t.Fatalf("third window events=%#v checkpoint=%#v hasMore=%v", third.Events, third.Checkpoint, third.HasMore)
	}
	if third.Checkpoint.LastOffset != int64(len(line)+1) || third.Checkpoint.LastLineNo != 1 {
		t.Fatalf("third checkpoint = %#v, want line end", third.Checkpoint)
	}
	state, ok = decodeLineParserCheckpointState(third.Checkpoint.StateJSON, nil)
	if !ok || state.PendingEventIndex != 0 || state.PendingLineNo != 0 {
		t.Fatalf("third pending state = %#v ok=%v, want cleared cursor", state, ok)
	}
}

func TestReadSourceFileWindowParsesLargeMultiEventLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "large-session.jsonl")
	line := strings.Repeat("x", scannerMaxTokenBytes+1)
	if err := os.WriteFile(file, []byte(line+"\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	source := WatchSource{
		Name:     "claude",
		Runtime:  models.RuntimeClaudeCode,
		Provider: models.ProviderAnthropic,
		Format:   models.FormatJSONL,
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
			return []NormalizedEvent{
				{SessionID: "session", EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC), TextContent: strings.Repeat("oversize", 1024), SourceFile: file, SourceLineNo: lineNo, SourceOffset: offset, RawPayload: string(line[:1024])},
				{SessionID: "session", EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 8, 12, 0, 1, 0, time.UTC), TextContent: "sendable sibling", SourceFile: file, SourceLineNo: lineNo, SourceOffset: offset, RawPayload: "sendable"},
			}, nil
		},
	}

	first, err := ReadSourceFileWindow(context.Background(), source, file, nil, nil, 1)
	if err != nil {
		t.Fatalf("first window: %v", err)
	}
	if len(first.Events) != 1 || first.Events[0].TextContent == "sendable sibling" || !first.HasMore || first.Checkpoint == nil {
		t.Fatalf("first large-line window events=%#v checkpoint=%#v hasMore=%v", first.Events, first.Checkpoint, first.HasMore)
	}
	second, err := ReadSourceFileWindow(context.Background(), source, file, first.Checkpoint, nil, 1)
	if err != nil {
		t.Fatalf("second window: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].TextContent != "sendable sibling" || second.HasMore || second.Checkpoint == nil {
		t.Fatalf("second large-line window events=%#v checkpoint=%#v hasMore=%v", second.Events, second.Checkpoint, second.HasMore)
	}
	if second.Checkpoint.LastOffset != int64(len(line)+1) {
		t.Fatalf("second checkpoint offset = %d, want line end %d", second.Checkpoint.LastOffset, len(line)+1)
	}
}

func TestReadSourceFileSkipsNoProgressCheckpointForPartialLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	firstLine := `{"msg":"complete"}`
	if err := os.WriteFile(file, []byte(firstLine+"\n"+`{"msg":"partial"`), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	source := testLineWindowSource(file)
	first, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	second, err := ReadSourceFile(context.Background(), source, file, first.Checkpoint, nil)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(second.Events) != 0 || len(second.CaptureErrors) != 0 || second.Checkpoint != nil {
		t.Fatalf("second read = events %d errors %d checkpoint %#v, want no progress", len(second.Events), len(second.CaptureErrors), second.Checkpoint)
	}
}

func TestReadSourceFileSkipsReplayParseErrorsBeforeCheckpoint(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(file, []byte("bad\n{\"msg\":\"two\"}\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	source := WatchSource{
		Name:     "codex",
		Runtime:  models.RuntimeCodex,
		Provider: models.ProviderOpenAI,
		Format:   models.FormatJSONL,
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
			if string(line) == "bad" {
				return nil, errors.New("bad json")
			}
			return []NormalizedEvent{{
				SessionID:    "session",
				SourceName:   "codex",
				Runtime:      models.RuntimeCodex,
				Provider:     models.ProviderOpenAI,
				Format:       models.FormatJSONL,
				EventKind:    models.EventKindMessage,
				ActorRole:    models.ActorRoleAssistant,
				Timestamp:    time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
				RawPayload:   string(line),
				SourceFile:   file,
				SourceLineNo: lineNo,
				SourceOffset: offset,
			}}, nil
		},
	}

	first, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first.CaptureErrors) != 1 || len(first.Events) != 1 || first.Checkpoint == nil {
		t.Fatalf("first read errors=%#v events=%#v checkpoint=%#v", first.CaptureErrors, first.Events, first.Checkpoint)
	}
	if err := os.WriteFile(file, []byte("bad\n{\"msg\":\"two\"}\n{\"msg\":\"three\"}\n"), 0644); err != nil {
		t.Fatalf("append source: %v", err)
	}

	second, err := ReadSourceFile(context.Background(), source, file, first.Checkpoint, nil)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(second.CaptureErrors) != 0 {
		t.Fatalf("second read replayed parse errors before checkpoint: %#v", second.CaptureErrors)
	}
	if len(second.Events) != 1 || second.Events[0].SourceLineNo != 3 {
		t.Fatalf("second read events=%#v, want only line 3", second.Events)
	}

	fresh, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("fresh read: %v", err)
	}
	if len(fresh.CaptureErrors) != 1 || fresh.CaptureErrors[0].ID != first.CaptureErrors[0].ID {
		t.Fatalf("line parse error IDs = first %#v fresh %#v, want stable", first.CaptureErrors, fresh.CaptureErrors)
	}
}

func TestReadWholeSourceFileWindowUsesEventIndexCheckpoint(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.db")
	if err := os.WriteFile(file, []byte("state"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	source := WatchSource{
		Name:     "hermes",
		Runtime:  models.RuntimeHermesAgent,
		Provider: models.ProviderMulti,
		Format:   models.FormatSQLite,
		FileParser: func(file string) ([]NormalizedEvent, error) {
			return []NormalizedEvent{
				{SessionID: "one", SourceName: "hermes", Runtime: models.RuntimeHermesAgent, Provider: models.ProviderMulti, Format: models.FormatSQLite, EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC), SourceFile: file},
				{SessionID: "two", SourceName: "hermes", Runtime: models.RuntimeHermesAgent, Provider: models.ProviderMulti, Format: models.FormatSQLite, EventKind: models.EventKindMessage, ActorRole: models.ActorRoleAssistant, Timestamp: time.Date(2026, 6, 8, 12, 0, 1, 0, time.UTC), SourceFile: file},
			}, nil
		},
	}

	first, err := ReadSourceFileWindow(context.Background(), source, file, nil, nil, 1)
	if err != nil {
		t.Fatalf("first window: %v", err)
	}
	if len(first.Events) != 1 || first.Events[0].SessionID != "one" || !first.HasMore || first.Checkpoint == nil || first.Checkpoint.LastLineNo != 1 || first.Checkpoint.LastOffset != 0 {
		t.Fatalf("first whole-file window = %#v checkpoint=%#v hasMore=%v", first.Events, first.Checkpoint, first.HasMore)
	}
	second, err := ReadSourceFileWindow(context.Background(), source, file, first.Checkpoint, nil, 1)
	if err != nil {
		t.Fatalf("second window: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].SessionID != "two" || second.HasMore || second.Checkpoint == nil || second.Checkpoint.LastOffset == 0 {
		t.Fatalf("second whole-file window = %#v checkpoint=%#v hasMore=%v", second.Events, second.Checkpoint, second.HasMore)
	}
}

func TestReadWholeSourceFileSQLiteSidecarsInvalidateCheckpoint(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.db")
	if err := os.WriteFile(file, []byte("state"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	var parses int
	source := WatchSource{
		Name:     "hermes",
		Runtime:  models.RuntimeHermesAgent,
		Provider: models.ProviderMulti,
		Format:   models.FormatSQLite,
		FileParser: func(file string) ([]NormalizedEvent, error) {
			parses++
			events := []NormalizedEvent{{
				SessionID:  "one",
				SourceName: "hermes",
				Runtime:    models.RuntimeHermesAgent,
				Provider:   models.ProviderMulti,
				Format:     models.FormatSQLite,
				EventKind:  models.EventKindMessage,
				ActorRole:  models.ActorRoleAssistant,
				Timestamp:  time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
				SourceFile: file,
			}}
			if _, err := os.Stat(file + "-wal"); err == nil {
				events = append(events, NormalizedEvent{
					SessionID:  "two",
					SourceName: "hermes",
					Runtime:    models.RuntimeHermesAgent,
					Provider:   models.ProviderMulti,
					Format:     models.FormatSQLite,
					EventKind:  models.EventKindMessage,
					ActorRole:  models.ActorRoleAssistant,
					Timestamp:  time.Date(2026, 6, 8, 12, 0, 1, 0, time.UTC),
					SourceFile: file,
				})
			}
			return events, nil
		},
	}

	first, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first.Events) != 1 || first.Checkpoint == nil || parses != 1 {
		t.Fatalf("first read events=%#v checkpoint=%#v parses=%d", first.Events, first.Checkpoint, parses)
	}
	if err := os.WriteFile(file+"-wal", []byte("committed rows still in wal"), 0644); err != nil {
		t.Fatalf("write wal sidecar: %v", err)
	}
	second, err := ReadSourceFile(context.Background(), source, file, first.Checkpoint, nil)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if parses != 2 || len(second.Events) != 2 {
		t.Fatalf("second read parses=%d events=%#v, want WAL sidecar to invalidate checkpoint", parses, second.Events)
	}
}

func TestReadWholeSourceFileSidecarChangeDuringParseForcesReread(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.db")
	if err := os.WriteFile(file, []byte("state"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	var parses int
	source := WatchSource{
		Name:     "hermes",
		Runtime:  models.RuntimeHermesAgent,
		Provider: models.ProviderMulti,
		Format:   models.FormatSQLite,
		FileParser: func(file string) ([]NormalizedEvent, error) {
			parses++
			if parses == 1 {
				if err := os.WriteFile(file+"-wal", []byte("changed during parse"), 0644); err != nil {
					return nil, err
				}
			}
			return []NormalizedEvent{{
				SessionID:  "one",
				SourceName: "hermes",
				Runtime:    models.RuntimeHermesAgent,
				Provider:   models.ProviderMulti,
				Format:     models.FormatSQLite,
				EventKind:  models.EventKindMessage,
				ActorRole:  models.ActorRoleAssistant,
				Timestamp:  time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
				SourceFile: file,
			}}, nil
		},
	}

	first, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first.Events) != 1 || first.Checkpoint == nil || !first.HasMore || first.Checkpoint.LastOffset != 0 {
		t.Fatalf("first read events=%#v checkpoint=%#v hasMore=%v, want non-complete checkpoint", first.Events, first.Checkpoint, first.HasMore)
	}
	second, err := ReadSourceFile(context.Background(), source, file, first.Checkpoint, nil)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if parses != 2 || len(second.Events) != 1 || second.Checkpoint == nil || second.HasMore || second.Checkpoint.LastOffset == 0 {
		t.Fatalf("second read parses=%d events=%#v checkpoint=%#v hasMore=%v, want stable reread completion", parses, second.Events, second.Checkpoint, second.HasMore)
	}
}

func TestReadWholeSourceFileParseErrorRetriesAndDeduplicates(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "state.db")
	if err := os.WriteFile(file, []byte("bad"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	var parses int
	source := WatchSource{
		Name:     "hermes",
		Runtime:  models.RuntimeHermesAgent,
		Provider: models.ProviderMulti,
		Format:   models.FormatSQLite,
		FileParser: func(string) ([]NormalizedEvent, error) {
			parses++
			return nil, errors.New("cannot parse database")
		},
	}
	first, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first.CaptureErrors) != 1 || first.Checkpoint == nil || first.Checkpoint.LastOffset != 0 {
		t.Fatalf("first read errors=%#v checkpoint=%#v", first.CaptureErrors, first.Checkpoint)
	}
	second, err := ReadSourceFile(context.Background(), source, file, first.Checkpoint, nil)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if len(second.CaptureErrors) != 0 || second.Checkpoint != nil {
		t.Fatalf("second unchanged read errors=%#v checkpoint=%#v, want duplicate diagnostic suppressed", second.CaptureErrors, second.Checkpoint)
	}
	if parses != 2 {
		t.Fatalf("parses = %d, want retry before suppressing duplicate diagnostic", parses)
	}

	fresh, err := ReadSourceFile(context.Background(), source, file, nil, nil)
	if err != nil {
		t.Fatalf("fresh read: %v", err)
	}
	if fresh.CaptureErrors[0].ID != first.CaptureErrors[0].ID {
		t.Fatalf("parse error IDs = %q and %q, want stable", first.CaptureErrors[0].ID, fresh.CaptureErrors[0].ID)
	}
}

func testLineWindowSource(file string) WatchSource {
	return WatchSource{
		Name:     "codex",
		Runtime:  models.RuntimeCodex,
		Provider: models.ProviderOpenAI,
		Format:   models.FormatJSONL,
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
			return []NormalizedEvent{{
				SessionID:    "session",
				SourceName:   "codex",
				Runtime:      models.RuntimeCodex,
				Provider:     models.ProviderOpenAI,
				Format:       models.FormatJSONL,
				EventKind:    models.EventKindMessage,
				ActorRole:    models.ActorRoleAssistant,
				Timestamp:    time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
				RawPayload:   string(line),
				SourceFile:   file,
				SourceLineNo: lineNo,
				SourceOffset: offset,
			}}, nil
		},
	}
}
