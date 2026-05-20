package capture

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

const (
	incrementalReplayLines = 64
	scannerMaxTokenBytes   = 4 << 20
	incrementalReplayBytes = int64(incrementalReplayLines * scannerMaxTokenBytes)
)

type lineParserCheckpointState struct {
	Version           int             `json:"version"`
	ReplayStartOffset int64           `json:"replay_start_offset"`
	ReplayStartLineNo int             `json:"replay_start_line_no"`
	ReplayState       lineParserState `json:"replay_state"`
}

type lineParserState struct {
	Models           map[string]string `json:"models,omitempty"`
	TokenUsageTotals map[string]string `json:"token_usage_totals,omitempty"`
}

type replayLine struct {
	offset int64
	lineNo int
	line   []byte
	seq    int
}

// WatchSource defines a source to watch for JSONL files.
type WatchSource struct {
	Name       string
	Runtime    string
	Provider   string
	Format     string
	Globs      []string
	WatchRoots []string
	Parser     func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error)
	FileParser func(file string) ([]NormalizedEvent, error)
}

// Watcher monitors JSONL files and sends parsed events to the batcher.
type Watcher struct {
	sources           []WatchSource
	eventCh           chan<- BatchEvent
	store             *store.Store
	logger            *slog.Logger
	debounceDelay     time.Duration
	reconcileInterval time.Duration
	backfillOnStart   bool
	backfillWorkers   int
	checkpoints       map[string]*CheckpointManager
}

// NewWatcher creates a new JSONL file watcher.
func NewWatcher(sources []WatchSource, eventCh chan<- BatchEvent, ch *store.Store, logger *slog.Logger, debounce, reconcile time.Duration, backfillOnStart bool, backfillWorkers int) *Watcher {
	cps := make(map[string]*CheckpointManager)
	for _, src := range sources {
		cps[src.Name] = NewCheckpointManager(ch, src.Name)
	}
	if backfillWorkers <= 0 {
		backfillWorkers = 4
	}
	return &Watcher{
		sources:           sources,
		eventCh:           eventCh,
		store:             ch,
		logger:            logger,
		debounceDelay:     debounce,
		reconcileInterval: reconcile,
		backfillOnStart:   backfillOnStart,
		backfillWorkers:   backfillWorkers,
		checkpoints:       cps,
	}
}

// Run starts the watcher. It blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	// Load checkpoints
	for _, src := range w.sources {
		if err := w.checkpoints[src.Name].Load(ctx); err != nil {
			w.logger.Error("load checkpoints failed", "source", src.Name, "error", err)
		}
	}

	// Resolve globs once for backfill + initial watch setup
	sourceFiles := make(map[string][]string)
	for _, src := range w.sources {
		sourceFiles[src.Name] = w.resolveGlobs(src.Globs)
	}

	// Backfill: process existing files from checkpoint
	if w.backfillOnStart {
		for _, src := range w.sources {
			files := sourceFiles[src.Name]
			w.logger.Info("backfill source", "name", src.Name, "files", len(files), "workers", w.backfillWorkers)
			w.processFiles(ctx, src, files)
			w.logger.Info("backfill source complete", "name", src.Name)
		}
	} else {
		w.logger.Info("startup backfill disabled")
	}

	// Start fsnotify watcher
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	// Watch parent directories of resolved files
	watchedDirs := make(map[string]bool)
	for _, files := range sourceFiles {
		for _, f := range files {
			dir := filepath.Dir(f)
			w.watchDir(fsw, watchedDirs, dir)
		}
	}
	for _, src := range w.sources {
		for _, root := range src.WatchRoots {
			w.watchDir(fsw, watchedDirs, expandHome(root))
		}
	}

	// Debounce map
	pending := make(map[string]time.Time)
	ticker := time.NewTicker(w.reconcileInterval)
	defer ticker.Stop()

	debounceCheck := time.NewTicker(w.debounceDelay)
	defer debounceCheck.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if w.findSource(event.Name) != nil {
					pending[event.Name] = time.Now()
				}
				// If new directory, watch it
				if event.Has(fsnotify.Create) {
					if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
						w.watchDir(fsw, watchedDirs, event.Name)
					}
				}
			}

		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Error("fsnotify error", "error", err)

		case <-debounceCheck.C:
			now := time.Now()
			for file, t := range pending {
				if now.Sub(t) >= w.debounceDelay {
					delete(pending, file)
					src := w.findSource(file)
					if src != nil {
						w.processFile(ctx, *src, file)
					}
				}
			}

		case <-ticker.C:
			// Reconciliation: re-glob and process new files
			for _, src := range w.sources {
				files := w.resolveGlobs(src.Globs)
				var toProcess []string
				for _, f := range files {
					dir := filepath.Dir(f)
					w.watchDir(fsw, watchedDirs, dir)
					toProcess = append(toProcess, f)
				}
				w.processFiles(ctx, src, toProcess)
			}
		}
	}
}

func (w *Watcher) watchDir(fsw *fsnotify.Watcher, watchedDirs map[string]bool, dir string) {
	if dir == "" || watchedDirs[dir] {
		return
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return
	}
	if err := fsw.Add(dir); err != nil {
		w.logger.Warn("failed to watch dir", "dir", dir, "error", err)
		return
	}
	watchedDirs[dir] = true
}

func (w *Watcher) processFiles(ctx context.Context, src WatchSource, files []string) {
	if len(files) == 0 {
		return
	}
	if w.backfillWorkers <= 1 || len(files) == 1 {
		for _, file := range files {
			w.processFile(ctx, src, file)
		}
		return
	}

	workers := min(w.backfillWorkers, len(files))
	jobs := make(chan string)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case file, ok := <-jobs:
					if !ok {
						return
					}
					w.processFile(ctx, src, file)
				}
			}
		}()
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- file:
		}
	}
	close(jobs)
	wg.Wait()
}

func (w *Watcher) findSource(file string) *WatchSource {
	for _, src := range w.sources {
		for _, glob := range src.Globs {
			matched, _ := doublestar.PathMatch(expandHome(glob), file)
			if matched {
				return &src
			}
		}
	}
	return nil
}

func (w *Watcher) resolveGlobs(globs []string) []string {
	var files []string
	seen := make(map[string]bool)
	for _, glob := range globs {
		// Expand ~ to home directory
		expanded := expandHome(glob)
		matches, err := doublestar.FilepathGlob(expanded)
		if err != nil {
			w.logger.Warn("glob failed", "pattern", glob, "error", err)
			continue
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				files = append(files, m)
			}
		}
	}
	return files
}

func (w *Watcher) processFile(ctx context.Context, src WatchSource, file string) {
	cm := w.checkpoints[src.Name]

	fi, err := os.Stat(file)
	if err != nil {
		return
	}

	// Check for rotation
	if cm.CheckRotation(file, fi) {
		w.logger.Info("file rotation detected, resetting checkpoint", "file", file)
		cp := &models.Checkpoint{
			SourceName:       src.Name,
			SourceFile:       file,
			SourceInode:      fileInode(fi),
			SourceGeneration: 0,
			LastOffset:       0,
			LastLineNo:       0,
		}
		if existing := cm.Get(file); existing != nil {
			cp.SourceGeneration = existing.SourceGeneration + 1
		}
		if err := cm.Save(ctx, cp); err != nil {
			w.logger.Error("save rotation checkpoint failed", "file", file, "error", err)
		}
	}

	cp := cm.Get(file)
	if src.FileParser != nil {
		w.processWholeFile(ctx, src, file, fi, cp)
		return
	}

	var offset int64
	var lineNo int
	var checkpointOffset int64
	var initialState lineParserState
	emitReplay := true
	if cp != nil {
		offset = cp.LastOffset
		lineNo = cp.LastLineNo
		checkpointOffset = cp.LastOffset
		// Nothing new to read
		if fi.Size() <= offset {
			return
		}

		checkpointState, stateOK := decodeLineParserCheckpointState(cp.StateJSON, w.logger)
		if stateOK && checkpointState.ReplayStartLineNo > 0 &&
			checkpointState.ReplayStartLineNo <= cp.LastLineNo &&
			checkpointState.ReplayStartOffset <= cp.LastOffset {
			offset = checkpointState.ReplayStartOffset
			lineNo = checkpointState.ReplayStartLineNo - 1
			initialState = checkpointState.ReplayState.clone()
		} else {
			offset, lineNo = replayStartFromPrefix(file, offset, lineNo, incrementalReplayLines, w.logger)
			// Legacy checkpoints do not know the parser state at the replay
			// boundary. Use the replay window only as context, then emit new
			// rows; subsequent checkpoints persist exact replay state.
			emitReplay = false
		}
	}

	f, err := os.Open(file)
	if err != nil {
		w.logger.Error("open file failed", "file", file, "error", err)
		return
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			w.logger.Error("seek failed", "file", file, "offset", offset, "error", err)
			return
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, scannerMaxTokenBytes), scannerMaxTokenBytes)

	var allEvents []NormalizedEvent
	var replayLines []replayLine

	for scanner.Scan() {
		lineNo++
		lineBytes := scanner.Bytes()
		lineLen := int64(len(lineBytes)) + 1 // +1 for newline
		replayLines = appendReplayLine(replayLines, replayLine{offset: offset, lineNo: lineNo}, incrementalReplayLines)

		if len(lineBytes) == 0 {
			offset += lineLen
			continue
		}

		events, err := src.Parser(lineBytes, file, lineNo, offset)
		if err != nil {
			w.logger.Warn("parse error", "file", file, "line", lineNo, "error", err)
			// Record capture error
			ce := &models.CaptureError{
				ID:              genID(),
				SourceName:      src.Name,
				SourceFile:      file,
				SourceLineNo:    lineNo,
				SourceOffset:    offset,
				ErrorClass:      "parse_error",
				ErrorMessage:    err.Error(),
				ContextFragment: truncate(string(lineBytes), 500),
			}
			if err := w.store.InsertCaptureError(ctx, *ce); err != nil {
				w.logger.Error("record capture error failed", "error", err)
			}
			offset += lineLen
			continue
		}

		for i := range events {
			events[i].SourceName = firstNonEmpty(events[i].SourceName, src.Name)
			events[i].Runtime = firstNonEmpty(events[i].Runtime, src.Runtime)
			events[i].Provider = firstNonEmpty(events[i].Provider, src.Provider)
			events[i].Format = firstNonEmpty(events[i].Format, src.Format)
			if cp != nil {
				events[i].SourceGeneration = cp.SourceGeneration
			}
		}
		allEvents = append(allEvents, events...)
		offset += lineLen
	}
	if err := scanner.Err(); err != nil {
		w.logger.Error("scan file failed", "file", file, "error", err)
		return
	}

	// Propagate model from context events to events without a model. On safe
	// incremental replays, seed from the parser state captured at the replay
	// boundary.
	PropagateModelWithInitial(allEvents, initialState.Models)

	// Deduplicate tokens across JSONL lines from the same API call
	// before sending to the batcher.
	allEvents = DeduplicateTokensWithInitial(allEvents, initialState.TokenUsageTotals)

	nextState := buildLineParserCheckpointState(initialState, allEvents, replayLines)

	for _, evt := range allEvents {
		if cp != nil && !emitReplay && evt.SourceOffset < checkpointOffset {
			continue
		}
		w.eventCh <- BatchEvent{Insert: &InsertEvent{Normalized: evt}}
	}

	// Save checkpoint after processing
	newCP := &models.Checkpoint{
		SourceName:  src.Name,
		SourceFile:  file,
		SourceInode: fileInode(fi),
		LastOffset:  offset,
		LastLineNo:  lineNo,
		StateJSON:   encodeLineParserCheckpointState(nextState, w.logger),
	}
	if cp != nil {
		newCP.SourceGeneration = cp.SourceGeneration
	}
	if err := cm.Save(ctx, newCP); err != nil {
		w.logger.Error("save checkpoint failed", "file", file, "error", err)
	}
}

func replayStartFromPrefix(file string, limitOffset int64, limitLineNo int, overlapLines int, logger *slog.Logger) (int64, int) {
	if limitOffset <= 0 || overlapLines <= 0 {
		return limitOffset, limitLineNo
	}
	lines := tailLinesBeforeOffset(file, limitOffset, limitLineNo, overlapLines, incrementalReplayBytes, logger)
	if len(lines) == 0 {
		return limitOffset, limitLineNo
	}
	return lines[0].offset, lines[0].lineNo - 1
}

func tailLinesBeforeOffset(file string, limitOffset int64, limitLineNo int, overlapLines int, maxReadBytes int64, logger *slog.Logger) []replayLine {
	if limitOffset <= 0 || overlapLines <= 0 || maxReadBytes <= 0 {
		return nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()

	readStart := limitOffset - maxReadBytes
	if readStart < 0 {
		readStart = 0
	}
	if readStart > 0 {
		if _, err := f.Seek(readStart-1, io.SeekStart); err != nil {
			if logger != nil {
				logger.Warn("seek replay prefix failed", "file", file, "offset", readStart-1, "error", err)
			}
			return nil
		}
		var prev [1]byte
		if _, err := io.ReadFull(f, prev[:]); err != nil {
			if logger != nil {
				logger.Warn("read replay prefix failed", "file", file, "offset", readStart-1, "error", err)
			}
			return nil
		}
		if _, err := f.Seek(readStart, io.SeekStart); err != nil {
			if logger != nil {
				logger.Warn("seek replay prefix failed", "file", file, "offset", readStart, "error", err)
			}
			return nil
		}
		if prev[0] != '\n' {
			reader := bufio.NewReader(f)
			skipped, err := reader.ReadBytes('\n')
			readStart += int64(len(skipped))
			if err != nil {
				if err != io.EOF && logger != nil {
					logger.Warn("read replay prefix failed", "file", file, "error", err)
				}
				return nil
			}
			if _, err := f.Seek(readStart, io.SeekStart); err != nil {
				if logger != nil {
					logger.Warn("seek replay prefix failed", "file", file, "offset", readStart, "error", err)
				}
				return nil
			}
		}
	} else if _, err := f.Seek(0, io.SeekStart); err != nil {
		if logger != nil {
			logger.Warn("seek replay prefix failed", "file", file, "error", err)
		}
		return nil
	}

	reader := bufio.NewReader(f)
	offset := readStart
	var lines []replayLine
	var completeLineCount int
	for offset < limitOffset {
		lineStart := offset
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineLen := int64(len(line))
			if offset+lineLen > limitOffset {
				break
			}
			offset += lineLen
			completeLineCount++
			line = bytes.TrimSuffix(line, []byte("\n"))
			line = bytes.TrimSuffix(line, []byte("\r"))
			if len(line) < scannerMaxTokenBytes {
				lines = appendReplayLine(lines, replayLine{
					offset: lineStart,
					line:   line,
					seq:    completeLineCount,
				}, overlapLines)
			}
		}
		if err != nil {
			if err != io.EOF && logger != nil {
				logger.Warn("scan prefix failed", "file", file, "error", err)
			}
			break
		}
	}
	for i := range lines {
		lines[i].lineNo = limitLineNo - completeLineCount + lines[i].seq
	}
	return lines
}

func appendReplayLine(lines []replayLine, line replayLine, limit int) []replayLine {
	if limit <= 0 {
		return nil
	}
	lines = append(lines, line)
	if len(lines) > limit {
		copy(lines, lines[1:])
		lines = lines[:limit]
	}
	return lines
}

func decodeLineParserCheckpointState(raw string, logger *slog.Logger) (lineParserCheckpointState, bool) {
	if raw == "" {
		return lineParserCheckpointState{}, false
	}
	var state lineParserCheckpointState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		if logger != nil {
			logger.Warn("decode line parser checkpoint state failed", "error", err)
		}
		return lineParserCheckpointState{}, false
	}
	if state.Version != 1 {
		return lineParserCheckpointState{}, false
	}
	return state, true
}

func encodeLineParserCheckpointState(state lineParserCheckpointState, logger *slog.Logger) string {
	state.Version = 1
	payload, err := json.Marshal(state)
	if err != nil {
		if logger != nil {
			logger.Warn("encode line parser checkpoint state failed", "error", err)
		}
		return ""
	}
	return string(payload)
}

func buildLineParserCheckpointState(initial lineParserState, events []NormalizedEvent, replayLines []replayLine) lineParserCheckpointState {
	if len(replayLines) == 0 {
		return lineParserCheckpointState{
			Version:     1,
			ReplayState: initial.clone(),
		}
	}

	replayStart := replayLines[0]
	replayState := initial.clone()
	for _, event := range events {
		if event.SourceOffset >= replayStart.offset {
			continue
		}
		replayState.apply(event)
	}

	return lineParserCheckpointState{
		Version:           1,
		ReplayStartOffset: replayStart.offset,
		ReplayStartLineNo: replayStart.lineNo,
		ReplayState:       replayState,
	}
}

func (s lineParserState) clone() lineParserState {
	return lineParserState{
		Models:           cloneStringMap(s.Models),
		TokenUsageTotals: cloneStringMap(s.TokenUsageTotals),
	}
}

func (s *lineParserState) apply(event NormalizedEvent) {
	if event.SessionID == "" {
		return
	}
	if event.Model != "" {
		if s.Models == nil {
			s.Models = make(map[string]string)
		}
		s.Models[event.SessionID] = event.Model
	}
	if event.PayloadType == "token_count" && event.TokenUsageTotalKey != "" {
		if s.TokenUsageTotals == nil {
			s.TokenUsageTotals = make(map[string]string)
		}
		s.TokenUsageTotals[event.SessionID] = event.TokenUsageTotalKey
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (w *Watcher) processWholeFile(ctx context.Context, src WatchSource, file string, fi os.FileInfo, cp *models.Checkpoint) {
	events, err := src.FileParser(file)
	if err != nil {
		w.logger.Warn("parse error", "file", file, "error", err)
		ce := &models.CaptureError{
			ID:              genID(),
			SourceName:      src.Name,
			SourceFile:      file,
			ErrorClass:      "parse_error",
			ErrorMessage:    err.Error(),
			ContextFragment: file,
		}
		if err := w.store.InsertCaptureError(ctx, *ce); err != nil {
			w.logger.Error("record capture error failed", "error", err)
		}
		return
	}

	for i := range events {
		events[i].SourceName = firstNonEmpty(events[i].SourceName, src.Name)
		events[i].Runtime = firstNonEmpty(events[i].Runtime, src.Runtime)
		events[i].Provider = firstNonEmpty(events[i].Provider, src.Provider)
		events[i].Format = firstNonEmpty(events[i].Format, src.Format)
		if cp != nil {
			events[i].SourceGeneration = cp.SourceGeneration
		}
	}
	PropagateModel(events)
	events = DeduplicateTokens(events)

	for _, evt := range events {
		w.eventCh <- BatchEvent{Insert: &InsertEvent{Normalized: evt}}
	}

	newCP := &models.Checkpoint{
		SourceName:  src.Name,
		SourceFile:  file,
		SourceInode: fileInode(fi),
		// Whole-file parsers intentionally replay complete, mutable session
		// stores. Stable per-row event IDs let ClickHouse replace prior rows.
		LastOffset: 0,
		LastLineNo: 0,
	}
	if cp != nil {
		newCP.SourceGeneration = cp.SourceGeneration
	}
	if err := w.checkpoints[src.Name].Save(ctx, newCP); err != nil {
		w.logger.Error("save checkpoint failed", "file", file, "error", err)
	}
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
