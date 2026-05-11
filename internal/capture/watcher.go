package capture

import (
	"bufio"
	"context"
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
	if cp != nil {
		offset = cp.LastOffset
		lineNo = cp.LastLineNo
		// Nothing new to read
		if fi.Size() <= offset {
			return
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
	scanner.Buffer(make([]byte, 0, 4<<20), 4<<20) // 4MB buffer

	var allEvents []NormalizedEvent

	for scanner.Scan() {
		lineNo++
		lineBytes := scanner.Bytes()
		lineLen := int64(len(lineBytes)) + 1 // +1 for newline

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

	// Propagate model from context events to events without a model.
	// Codex puts the model on turn_context events but not on token_count
	// or tool events, so we forward-fill the model within the file.
	PropagateModel(allEvents)

	// Deduplicate tokens across JSONL lines from the same API call
	// before sending to the batcher.
	allEvents = DeduplicateTokens(allEvents)

	for _, evt := range allEvents {
		w.eventCh <- BatchEvent{Insert: &InsertEvent{Normalized: evt}}
	}

	// Save checkpoint after processing
	newCP := &models.Checkpoint{
		SourceName:  src.Name,
		SourceFile:  file,
		SourceInode: fileInode(fi),
		LastOffset:  offset,
		LastLineNo:  lineNo,
	}
	if cp != nil {
		newCP.SourceGeneration = cp.SourceGeneration
	}
	if err := cm.Save(ctx, newCP); err != nil {
		w.logger.Error("save checkpoint failed", "file", file, "error", err)
	}
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
