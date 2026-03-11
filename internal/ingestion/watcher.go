package ingestion

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/models"
)

// WatchSource defines a source to watch for JSONL files.
type WatchSource struct {
	Name     string
	Provider string
	Globs    []string
	Parser   func(line []byte, file string, lineNo int, offset int64) ([]NormalizedEvent, error)
}

// Watcher monitors JSONL files and sends parsed events to the batcher.
type Watcher struct {
	sources           []WatchSource
	eventCh           chan<- BatchEvent
	db                *database.DB
	logger            *slog.Logger
	debounceDelay     time.Duration
	reconcileInterval time.Duration
	checkpoints       map[string]*CheckpointManager
}

// NewWatcher creates a new JSONL file watcher.
func NewWatcher(sources []WatchSource, eventCh chan<- BatchEvent, db *database.DB, logger *slog.Logger, debounce, reconcile time.Duration) *Watcher {
	cps := make(map[string]*CheckpointManager)
	for _, src := range sources {
		cps[src.Name] = NewCheckpointManager(db, db.ReadPool, src.Name)
	}
	return &Watcher{
		sources:           sources,
		eventCh:           eventCh,
		db:                db,
		logger:            logger,
		debounceDelay:     debounce,
		reconcileInterval: reconcile,
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
	for _, src := range w.sources {
		files := sourceFiles[src.Name]
		w.logger.Info("backfill source", "name", src.Name, "files", len(files))
		for _, f := range files {
			w.processFile(ctx, src, f)
		}
		w.logger.Info("backfill source complete", "name", src.Name)
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
			if !watchedDirs[dir] {
				if err := fsw.Add(dir); err != nil {
					w.logger.Warn("failed to watch dir", "dir", dir, "error", err)
				} else {
					watchedDirs[dir] = true
				}
			}
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
				if strings.HasSuffix(event.Name, ".jsonl") {
					pending[event.Name] = time.Now()
				}
				// If new directory, watch it
				if event.Has(fsnotify.Create) {
					if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
						if !watchedDirs[event.Name] {
							if err := fsw.Add(event.Name); err != nil {
								w.logger.Warn("failed to watch new dir", "dir", event.Name, "error", err)
							} else {
								watchedDirs[event.Name] = true
							}
						}
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
				for _, f := range files {
					dir := filepath.Dir(f)
					if !watchedDirs[dir] {
						if err := fsw.Add(dir); err == nil {
							watchedDirs[dir] = true
						}
					}
					w.processFile(ctx, src, f)
				}
			}
		}
	}
}

func (w *Watcher) findSource(file string) *WatchSource {
	for _, src := range w.sources {
		for _, glob := range src.Globs {
			matched, _ := doublestar.PathMatch(glob, file)
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
			// Record ingest error
			ie := &models.IngestError{
				ID:              genID(),
				SourceFile:      file,
				SourceLineNo:    lineNo,
				ErrorClass:      "parse_error",
				ErrorMessage:    err.Error(),
				ContextFragment: truncate(string(lineBytes), 500),
			}
			if err := database.InsertIngestError(ctx, w.db, ie); err != nil {
				w.logger.Error("record ingest error failed", "error", err)
			}
			offset += lineLen
			continue
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
		SourceName: src.Name,
		SourceFile: file,
		SourceInode: fileInode(fi),
		LastOffset: offset,
		LastLineNo: lineNo,
	}
	if cp != nil {
		newCP.SourceGeneration = cp.SourceGeneration
	}
	if err := cm.Save(ctx, newCP); err != nil {
		w.logger.Error("save checkpoint failed", "file", file, "error", err)
	}
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
