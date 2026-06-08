package capture

import (
	"context"
	"os"
	"path/filepath"
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
