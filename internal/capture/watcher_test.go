package capture

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/johnnygreco/beacon/internal/models"
)

var watcherTestLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

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

func TestNewWatcherDefaultsWorkersAndCheckpointManagers(t *testing.T) {
	events := make(chan BatchEvent, 1)
	w := NewWatcher(
		[]WatchSource{{Name: "codex"}, {Name: "hermes"}},
		events,
		nil,
		watcherTestLogger,
		time.Second,
		time.Minute,
		true,
		0,
	)

	if w.backfillWorkers != 4 {
		t.Fatalf("backfillWorkers = %d, want default 4", w.backfillWorkers)
	}
	if w.eventCh != events || !w.backfillOnStart {
		t.Fatalf("watcher fields not preserved: eventCh=%v backfill=%v", w.eventCh == events, w.backfillOnStart)
	}
	for _, source := range []string{"codex", "hermes"} {
		if w.checkpoints[source] == nil {
			t.Fatalf("missing checkpoint manager for %s", source)
		}
	}
}

func TestRunReturnsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := WatchSource{Name: "codex"}
	w := newFakeWatcher(src, newFakeWatcherStore(), make(chan BatchEvent, 1))
	w.debounceDelay = time.Hour
	w.reconcileInterval = time.Hour

	if err := w.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}

func TestWatchDirRegistersExistingDirectoryOnce(t *testing.T) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer fsw.Close()
	dir := t.TempDir()
	watched := make(map[string]bool)
	w := &Watcher{logger: watcherTestLogger}

	w.watchDir(fsw, watched, dir)
	w.watchDir(fsw, watched, dir)
	w.watchDir(fsw, watched, filepath.Join(dir, "missing"))

	if len(watched) != 1 || !watched[dir] {
		t.Fatalf("watched dirs = %#v, want only %q", watched, dir)
	}
}

func TestCheckpointManagerLoadSaveAndRotation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(file, []byte("short\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeWatcherStore()
	fake.seed("codex", models.Checkpoint{
		SourceName:       "codex",
		SourceFile:       file,
		SourceInode:      123,
		SourceGeneration: 2,
		LastOffset:       100,
		LastLineNo:       10,
	})
	cm := NewCheckpointManager(fake, "codex")

	if err := cm.Load(ctx); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cm.Get(file); got == nil || got.SourceGeneration != 2 {
		t.Fatalf("loaded checkpoint = %#v, want generation 2", got)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if !cm.CheckRotation(file, fi) {
		t.Fatal("CheckRotation = false, want true when file shrinks below checkpoint offset")
	}

	next := &models.Checkpoint{
		SourceName:       "codex",
		SourceFile:       file,
		SourceInode:      fileInode(fi),
		SourceGeneration: 3,
		LastOffset:       fi.Size(),
		LastLineNo:       1,
	}
	if err := cm.Save(ctx, next); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := cm.Get(file); got == nil || got.SourceGeneration != 3 || got.LastOffset != fi.Size() {
		t.Fatalf("saved checkpoint = %#v, want generation 3 offset %d", got, fi.Size())
	}
	if len(fake.saved) != 1 || fake.saved[0].SourceGeneration != 3 {
		t.Fatalf("fake saved = %#v, want generation 3", fake.saved)
	}
}

func TestProcessFileLegacyCheckpointReplaysContextButEmitsOnlyNewRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	content := "old-model\nold-token\nnew-message\n"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	checkpointOffset := int64(len("old-model\n" + "old-token\n"))
	fake := newFakeWatcherStore()
	fake.seed("codex", models.Checkpoint{
		SourceName:       "codex",
		SourceFile:       file,
		SourceGeneration: 2,
		LastOffset:       checkpointOffset,
		LastLineNo:       2,
	})
	events := make(chan BatchEvent, 8)
	src := WatchSource{
		Name:     "codex",
		Runtime:  "codex",
		Provider: "openai",
		Format:   "jsonl",
		Parser:   lineTextParser(t),
	}
	w := newFakeWatcher(src, fake, events)

	w.processFile(ctx, src, file)

	got := drainBatchEvents(events)
	if len(got) != 1 {
		t.Fatalf("events = %#v, want only the new post-checkpoint row", got)
	}
	if got[0].TextContent != "new-message" || got[0].SourceLineNo != 3 || got[0].SourceOffset != checkpointOffset {
		t.Fatalf("event = %#v, want line 3 at checkpoint offset", got[0])
	}
	if got[0].Model != "old-model" {
		t.Fatalf("replayed model context = %q, want old-model", got[0].Model)
	}
	if got[0].SourceGeneration != 2 || got[0].Runtime != "codex" || got[0].Provider != "openai" {
		t.Fatalf("event metadata = generation %d runtime %q provider %q", got[0].SourceGeneration, got[0].Runtime, got[0].Provider)
	}
	saved := fake.lastCheckpoint(t, file)
	if saved.LastOffset != int64(len(content)) || saved.LastLineNo != 3 || saved.SourceGeneration != 2 {
		t.Fatalf("saved checkpoint = %#v, want end of file generation 2", saved)
	}
	if state, ok := decodeLineParserCheckpointState(saved.StateJSON, nil); !ok || state.ReplayStartLineNo != 1 || state.ReplayStartOffset != 0 {
		t.Fatalf("saved replay state = %#v ok=%v, want replay from line 1 offset 0", state, ok)
	}
}

func TestProcessFileRotationIncrementsSourceGeneration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "rotating.jsonl")
	if err := os.WriteFile(file, []byte("fresh\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeWatcherStore()
	fake.seed("codex", models.Checkpoint{
		SourceName:       "codex",
		SourceFile:       file,
		SourceGeneration: 4,
		LastOffset:       100,
		LastLineNo:       8,
	})
	events := make(chan BatchEvent, 4)
	src := WatchSource{Name: "codex", Parser: lineTextParser(t)}
	w := newFakeWatcher(src, fake, events)

	w.processFile(ctx, src, file)

	got := drainBatchEvents(events)
	if len(got) != 1 {
		t.Fatalf("events = %#v, want fresh rotated row", got)
	}
	if got[0].SourceGeneration != 5 {
		t.Fatalf("event source generation = %d, want 5", got[0].SourceGeneration)
	}
	if len(fake.saved) < 2 {
		t.Fatalf("saved checkpoints = %#v, want rotation and final checkpoint", fake.saved)
	}
	if fake.saved[0].SourceGeneration != 5 || fake.saved[0].LastOffset != 0 {
		t.Fatalf("rotation checkpoint = %#v, want generation 5 offset 0", fake.saved[0])
	}
	if saved := fake.lastCheckpoint(t, file); saved.SourceGeneration != 5 || saved.LastLineNo != 1 {
		t.Fatalf("final checkpoint = %#v, want generation 5 line 1", saved)
	}
}

func TestProcessFileRecordsParseErrorsAndContinues(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "errors.jsonl")
	if err := os.WriteFile(file, []byte("bad\nok\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeWatcherStore()
	events := make(chan BatchEvent, 4)
	src := WatchSource{
		Name: "codex",
		Parser: func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
			if string(line) == "bad" {
				return nil, errors.New("bad line")
			}
			return []NormalizedEvent{lineEvent(file, lineNo, offset, string(line))}, nil
		},
	}
	w := newFakeWatcher(src, fake, events)

	w.processFile(ctx, src, file)

	if len(fake.captureErrors) != 1 {
		t.Fatalf("capture errors = %#v, want one parse error", fake.captureErrors)
	}
	if fake.captureErrors[0].SourceLineNo != 1 || fake.captureErrors[0].ErrorClass != "parse_error" || fake.captureErrors[0].ContextFragment != "bad" {
		t.Fatalf("capture error = %#v", fake.captureErrors[0])
	}
	got := drainBatchEvents(events)
	if len(got) != 1 || got[0].TextContent != "ok" {
		t.Fatalf("events after parse error = %#v, want ok row", got)
	}
	if saved := fake.lastCheckpoint(t, file); saved.LastLineNo != 2 {
		t.Fatalf("checkpoint after parse error = %#v, want line 2", saved)
	}
}

func TestProcessWholeFileParserEmitsRowsAndSavesCheckpoint(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "state.db")
	if err := os.WriteFile(file, []byte("sqlite fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeWatcherStore()
	events := make(chan BatchEvent, 4)
	src := WatchSource{
		Name:     "hermes",
		Runtime:  "hermes-agent",
		Provider: "multi",
		Format:   "sqlite",
		FileParser: func(file string) ([]NormalizedEvent, error) {
			return []NormalizedEvent{
				{SessionID: "session-1", EventKind: "message", TextContent: "hello", SourceFile: file, RawPayload: "one"},
				{SessionID: "session-1", EventKind: "message", TextContent: "world", SourceFile: file, RawPayload: "two"},
			}, nil
		},
	}
	w := newFakeWatcher(src, fake, events)

	w.processFile(ctx, src, file)

	got := drainBatchEvents(events)
	if len(got) != 2 {
		t.Fatalf("events = %#v, want two whole-file rows", got)
	}
	for _, evt := range got {
		if evt.SourceName != "hermes" || evt.Runtime != "hermes-agent" || evt.Provider != "multi" || evt.Format != "sqlite" {
			t.Fatalf("whole-file metadata = %#v", evt)
		}
	}
	if saved := fake.lastCheckpoint(t, file); saved.LastOffset != 0 || saved.LastLineNo != 0 {
		t.Fatalf("whole-file checkpoint = %#v, want zero offset/line", saved)
	}
}

func TestProcessWholeFileParserRecordsParseError(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "broken.db")
	if err := os.WriteFile(file, []byte("broken"), 0644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeWatcherStore()
	src := WatchSource{
		Name: "hermes",
		FileParser: func(string) ([]NormalizedEvent, error) {
			return nil, errors.New("sqlite parse failed")
		},
	}
	w := newFakeWatcher(src, fake, make(chan BatchEvent, 1))

	w.processFile(ctx, src, file)

	if len(fake.captureErrors) != 1 || fake.captureErrors[0].ErrorMessage != "sqlite parse failed" {
		t.Fatalf("capture errors = %#v, want sqlite parse failure", fake.captureErrors)
	}
	if len(fake.saved) != 0 {
		t.Fatalf("checkpoints saved after whole-file parse error = %#v, want none", fake.saved)
	}
}

func TestProcessFilesCancellationReturnsWithoutLeakingWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := WatchSource{Name: "codex", Parser: lineTextParser(t)}
	w := newFakeWatcher(src, newFakeWatcherStore(), make(chan BatchEvent, 1))
	w.backfillWorkers = 4
	files := []string{"one.jsonl", "two.jsonl", "three.jsonl", "four.jsonl"}
	done := make(chan struct{})

	go func() {
		w.processFiles(ctx, src, files)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("processFiles did not return after cancellation")
	}
}

func TestResolveGlobsDeduplicatesMatches(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.jsonl")
	second := filepath.Join(dir, "two.jsonl")
	if err := os.WriteFile(first, []byte("one\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("two\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w := &Watcher{logger: watcherTestLogger}

	got := w.resolveGlobs([]string{filepath.Join(dir, "*.jsonl"), first})
	if len(got) != 2 {
		t.Fatalf("resolved globs = %#v, want two unique files", got)
	}
	seen := map[string]bool{got[0]: true, got[1]: true}
	if !seen[first] || !seen[second] {
		t.Fatalf("resolved globs = %#v, want %q and %q", got, first, second)
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

type fakeWatcherStore struct {
	mu            sync.Mutex
	checkpoints   map[string]map[string]*models.Checkpoint
	saved         []models.Checkpoint
	captureErrors []models.CaptureError
}

func newFakeWatcherStore() *fakeWatcherStore {
	return &fakeWatcherStore{checkpoints: make(map[string]map[string]*models.Checkpoint)}
}

func (f *fakeWatcherStore) seed(source string, cp models.Checkpoint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.checkpoints[source] == nil {
		f.checkpoints[source] = make(map[string]*models.Checkpoint)
	}
	cpCopy := cp
	f.checkpoints[source][cp.SourceFile] = &cpCopy
}

func (f *fakeWatcherStore) LoadCheckpoints(_ context.Context, sourceName string) (map[string]*models.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make(map[string]*models.Checkpoint)
	for file, cp := range f.checkpoints[sourceName] {
		cpCopy := *cp
		result[file] = &cpCopy
	}
	return result, nil
}

func (f *fakeWatcherStore) UpsertCheckpoint(_ context.Context, cp models.Checkpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.checkpoints[cp.SourceName] == nil {
		f.checkpoints[cp.SourceName] = make(map[string]*models.Checkpoint)
	}
	cpCopy := cp
	f.checkpoints[cp.SourceName][cp.SourceFile] = &cpCopy
	f.saved = append(f.saved, cp)
	return nil
}

func (f *fakeWatcherStore) InsertCaptureError(_ context.Context, err models.CaptureError) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captureErrors = append(f.captureErrors, err)
	return nil
}

func (f *fakeWatcherStore) lastCheckpoint(t *testing.T, file string) models.Checkpoint {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.saved) - 1; i >= 0; i-- {
		if f.saved[i].SourceFile == file {
			return f.saved[i]
		}
	}
	t.Fatalf("no saved checkpoint for %s; saved=%#v", file, f.saved)
	return models.Checkpoint{}
}

func newFakeWatcher(src WatchSource, fake *fakeWatcherStore, events chan<- BatchEvent) *Watcher {
	w := &Watcher{
		sources:         []WatchSource{src},
		eventCh:         events,
		store:           fake,
		logger:          watcherTestLogger,
		backfillWorkers: 1,
		checkpoints: map[string]*CheckpointManager{
			src.Name: NewCheckpointManager(fake, src.Name),
		},
	}
	_ = w.checkpoints[src.Name].Load(context.Background())
	return w
}

func lineTextParser(t *testing.T) func([]byte, string, int, int64) ([]NormalizedEvent, error) {
	t.Helper()
	return func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error) {
		text := string(line)
		evt := lineEvent(file, lineNo, offset, text)
		if strings.Contains(text, "model") {
			evt.Model = text
		}
		if strings.Contains(text, "token") {
			evt.PayloadType = "token_count"
			evt.TokenUsageTotalKey = "1:0:0:1"
			evt.InputTokens = 1
		}
		return []NormalizedEvent{evt}, nil
	}
}

func lineEvent(file string, lineNo int, offset int64, text string) NormalizedEvent {
	return NormalizedEvent{
		SessionID:    "session-1",
		EventKind:    "message",
		TextContent:  text,
		SourceFile:   file,
		SourceLineNo: lineNo,
		SourceOffset: offset,
		RawPayload:   text,
		MessageUUID:  text,
		Timestamp:    time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
	}
}

func drainBatchEvents(ch <-chan BatchEvent) []NormalizedEvent {
	var events []NormalizedEvent
	for {
		select {
		case evt := <-ch:
			if evt.Insert != nil {
				events = append(events, evt.Insert.Normalized)
			}
		default:
			return events
		}
	}
}
