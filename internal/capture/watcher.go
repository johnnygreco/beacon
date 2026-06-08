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
	"sync/atomic"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/redaction"
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
	PendingLineOffset int64           `json:"pending_line_offset,omitempty"`
	PendingLineNo     int             `json:"pending_line_no,omitempty"`
	PendingEventIndex int             `json:"pending_event_index,omitempty"`
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

type captureErrorStore interface {
	InsertCaptureError(ctx context.Context, err models.CaptureError) error
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
	sources            []WatchSource
	eventCh            chan<- BatchEvent
	store              captureErrorStore
	logger             *slog.Logger
	debounceDelay      time.Duration
	reconcileInterval  time.Duration
	backfillOnStart    bool
	backfillWorkers    int
	checkpoints        map[string]*CheckpointManager
	redactor           *redaction.Policy
	backpressuredSends atomic.Uint64
}

type WatcherOption func(*Watcher)

func WithWatcherRedactionPolicy(policy *redaction.Policy) WatcherOption {
	return func(w *Watcher) {
		if policy != nil {
			w.redactor = policy
		}
	}
}

// NewWatcher creates a new JSONL file watcher.
func NewWatcher(sources []WatchSource, eventCh chan<- BatchEvent, ch *store.Store, logger *slog.Logger, debounce, reconcile time.Duration, backfillOnStart bool, backfillWorkers int, options ...WatcherOption) *Watcher {
	cps := make(map[string]*CheckpointManager)
	for _, src := range sources {
		cps[src.Name] = NewCheckpointManager(ch, src.Name)
	}
	if backfillWorkers <= 0 {
		backfillWorkers = 4
	}
	w := &Watcher{
		sources:           sources,
		eventCh:           eventCh,
		store:             ch,
		logger:            logger,
		debounceDelay:     debounce,
		reconcileInterval: reconcile,
		backfillOnStart:   backfillOnStart,
		backfillWorkers:   backfillWorkers,
		checkpoints:       cps,
		redactor:          redaction.DefaultPolicy(),
	}
	for _, option := range options {
		option(w)
	}
	return w
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
		w.logger.Warn("failed to watch dir", "dir", w.redactionPolicy().RedactPath(dir), "error", err)
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
		// processFiles owns these bounded backfill workers and waits for all of
		// them before returning; each worker exits when ctx is cancelled or the
		// jobs channel is closed.
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
			w.logger.Warn("glob failed", "pattern", w.redactionPolicy().RedactPath(glob), "error", err)
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
		w.logger.Info("file rotation detected, resetting checkpoint", "file", w.redactionPolicy().RedactPath(file))
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
		if err := w.saveCheckpoint(ctx, cm, cp); err != nil {
			w.logger.Error("save rotation checkpoint failed", "file", w.redactionPolicy().RedactPath(file), "error", err)
		}
	}

	cp := cm.Get(file)
	result, err := ReadSourceFile(ctx, src, file, cp, w.logger, w.redactionPolicy())
	if err != nil {
		w.logger.Error("read source file failed", "file", w.redactionPolicy().RedactPath(file), "error", err)
		return
	}
	for _, errRow := range RedactCaptureErrors(result.CaptureErrors, w.redactionPolicy()) {
		if err := w.store.InsertCaptureError(ctx, errRow); err != nil {
			w.logger.Error("record capture error failed", "error", err)
		}
	}
	for _, evt := range result.Events {
		if !w.sendEvent(ctx, BatchEvent{Insert: &InsertEvent{Normalized: evt}}) {
			return
		}
	}
	if result.Checkpoint == nil {
		return
	}
	if err := w.saveCheckpoint(ctx, cm, result.Checkpoint); err != nil {
		w.logger.Error("save checkpoint failed", "file", w.redactionPolicy().RedactPath(file), "error", err)
	}
}

func (w *Watcher) redactionPolicy() *redaction.Policy {
	if w != nil && w.redactor != nil {
		return w.redactor
	}
	return redaction.DefaultPolicy()
}

func (w *Watcher) saveCheckpoint(ctx context.Context, cm *CheckpointManager, cp *models.Checkpoint) error {
	if cp == nil {
		return nil
	}
	protected := RedactCheckpoint(*cp, w.redactionPolicy())
	return cm.SaveProtected(ctx, cp, protected)
}

func replayStartFromPrefix(file string, limitOffset int64, limitLineNo int, overlapLines int, logger *slog.Logger, policies ...*redaction.Policy) (int64, int) {
	if limitOffset <= 0 || overlapLines <= 0 {
		return limitOffset, limitLineNo
	}
	lines := tailLinesBeforeOffset(file, limitOffset, limitLineNo, overlapLines, incrementalReplayBytes, logger, policies...)
	if len(lines) == 0 {
		return limitOffset, limitLineNo
	}
	return lines[0].offset, lines[0].lineNo - 1
}

func tailLinesBeforeOffset(file string, limitOffset int64, limitLineNo int, overlapLines int, maxReadBytes int64, logger *slog.Logger, policies ...*redaction.Policy) []replayLine {
	if limitOffset <= 0 || overlapLines <= 0 || maxReadBytes <= 0 {
		return nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	logFile := redactionPolicyFromArgs(policies...).RedactPath(file)

	readStart := limitOffset - maxReadBytes
	if readStart < 0 {
		readStart = 0
	}
	if readStart > 0 {
		if _, err := f.Seek(readStart-1, io.SeekStart); err != nil {
			if logger != nil {
				logger.Warn("seek replay prefix failed", "file", logFile, "offset", readStart-1, "error", err)
			}
			return nil
		}
		var prev [1]byte
		if _, err := io.ReadFull(f, prev[:]); err != nil {
			if logger != nil {
				logger.Warn("read replay prefix failed", "file", logFile, "offset", readStart-1, "error", err)
			}
			return nil
		}
		if _, err := f.Seek(readStart, io.SeekStart); err != nil {
			if logger != nil {
				logger.Warn("seek replay prefix failed", "file", logFile, "offset", readStart, "error", err)
			}
			return nil
		}
		if prev[0] != '\n' {
			reader := bufio.NewReader(f)
			skipped, err := reader.ReadBytes('\n')
			readStart += int64(len(skipped))
			if err != nil {
				if err != io.EOF && logger != nil {
					logger.Warn("read replay prefix failed", "file", logFile, "error", err)
				}
				return nil
			}
			if _, err := f.Seek(readStart, io.SeekStart); err != nil {
				if logger != nil {
					logger.Warn("seek replay prefix failed", "file", logFile, "offset", readStart, "error", err)
				}
				return nil
			}
		}
	} else if _, err := f.Seek(0, io.SeekStart); err != nil {
		if logger != nil {
			logger.Warn("seek replay prefix failed", "file", logFile, "error", err)
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
				logger.Warn("scan prefix failed", "file", logFile, "error", err)
			}
			break
		}
	}
	for i := range lines {
		lines[i].lineNo = limitLineNo - completeLineCount + lines[i].seq
	}
	return lines
}

func redactionPolicyFromArgs(policies ...*redaction.Policy) *redaction.Policy {
	if len(policies) > 0 && policies[0] != nil {
		return policies[0]
	}
	return redaction.DefaultPolicy()
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
		return lineParserCheckpointState{Version: 1}
	}

	replayStart := replayLines[0]
	replaySessionIDs := sessionIDsAtOrAfterOffset(events, replayStart.offset)
	replayState := initial.filter(replaySessionIDs)
	for _, event := range events {
		if event.SourceOffset >= replayStart.offset {
			continue
		}
		if _, ok := replaySessionIDs[event.SessionID]; !ok {
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

func sessionIDsAtOrAfterOffset(events []NormalizedEvent, offset int64) map[string]struct{} {
	out := make(map[string]struct{})
	for _, event := range events {
		if event.SourceOffset < offset || event.SessionID == "" {
			continue
		}
		out[event.SessionID] = struct{}{}
	}
	return out
}

func (s lineParserState) clone() lineParserState {
	return lineParserState{
		Models:           cloneStringMap(s.Models),
		TokenUsageTotals: cloneStringMap(s.TokenUsageTotals),
	}
}

func (s lineParserState) filter(sessionIDs map[string]struct{}) lineParserState {
	if len(sessionIDs) == 0 {
		return lineParserState{}
	}
	return lineParserState{
		Models:           filterStringMap(s.Models, sessionIDs),
		TokenUsageTotals: filterStringMap(s.TokenUsageTotals, sessionIDs),
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

func filterStringMap(in map[string]string, allowed map[string]struct{}) map[string]string {
	if len(in) == 0 || len(allowed) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, value := range in {
		if key == "" || value == "" {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func applyWatchSourceMetadata(evt *NormalizedEvent, src WatchSource, cp *models.Checkpoint) {
	evt.SourceName = src.Name
	evt.Runtime = firstNonEmpty(evt.Runtime, src.Runtime)
	evt.Provider = firstNonEmpty(evt.Provider, src.Provider)
	evt.Format = firstNonEmpty(evt.Format, src.Format)
	if cp != nil {
		evt.SourceGeneration = cp.SourceGeneration
	}
}

func (w *Watcher) sendEvent(ctx context.Context, evt BatchEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case w.eventCh <- evt:
		return true
	default:
	}

	start := time.Now()
	blocked := w.backpressuredSends.Add(1)
	select {
	case <-ctx.Done():
		return false
	case w.eventCh <- evt:
		if w.logger != nil {
			w.logger.Debug("capture event send delayed by batcher backpressure",
				"duration", time.Since(start),
				"delayed_total", blocked,
			)
		}
		return true
	}
}

// BackpressuredSendCount returns watcher sends that encountered full batcher
// input capacity. These sends are delayed, not dropped, unless the context is
// cancelled while waiting.
func (w *Watcher) BackpressuredSendCount() uint64 {
	return w.backpressuredSends.Load()
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
