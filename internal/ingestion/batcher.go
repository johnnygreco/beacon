package ingestion

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/models"
)

const previewMaxLen = 320

// Batcher accumulates events and flushes them to DuckDB in batches.
type Batcher struct {
	eventCh  chan BatchEvent
	db       *database.DB
	notify   func() // called after each flush to notify SSE broker
	logger   *slog.Logger

	batchSize     int
	flushInterval time.Duration
	defaultInput  float64
	defaultOutput float64
}

// NewBatcher creates a new batcher.
func NewBatcher(db *database.DB, batchSize int, flushInterval time.Duration, defaultInput, defaultOutput float64, notify func(), logger *slog.Logger) *Batcher {
	return &Batcher{
		eventCh:       make(chan BatchEvent, batchSize*2),
		db:            db,
		notify:        notify,
		logger:        logger,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		defaultInput:  defaultInput,
		defaultOutput: defaultOutput,
	}
}

// EventCh returns the channel to send events to.
func (b *Batcher) EventCh() chan<- BatchEvent {
	return b.eventCh
}

// Run starts the batcher loop. It blocks until ctx is cancelled.
func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	var inserts []NormalizedEvent

	flush := func() {
		if len(inserts) == 0 {
			return
		}
		b.flushInserts(ctx, inserts)
		inserts = inserts[:0]

		if b.notify != nil {
			b.notify()
		}
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case evt := <-b.eventCh:
			if evt.Insert != nil {
				inserts = append(inserts, evt.Insert.Normalized)
			}
			if len(inserts) >= b.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (b *Batcher) flushInserts(ctx context.Context, events []NormalizedEvent) {
	start := time.Now()

	tx, err := b.db.WriteConn().BeginTx(ctx, nil)
	if err != nil {
		b.logger.Error("begin tx failed", "error", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	for _, evt := range events {
		uid := eventUID(evt.SourceFile, evt.SourceLineNo, evt.SourceOffset, evt.RawPayload)

		// Calculate cost if not provided
		cost := evt.CostUSD
		if cost == 0 && (evt.InputTokens > 0 || evt.OutputTokens > 0) {
			cost = CalcCost(evt.Model, evt.InputTokens, evt.OutputTokens, b.defaultInput, b.defaultOutput)
		}

		// Build text preview
		preview := truncate(evt.TextContent, previewMaxLen)

		event := &models.Event{
			EventUID:          uid,
			SessionID:         evt.SessionID,
			ParentSessionID:   evt.ParentSessionID,
			SourceName:        evt.SourceName,
			Provider:          evt.Provider,
			EventKind:         evt.EventKind,
			PayloadType:       evt.PayloadType,
			ActorRole:         evt.ActorRole,
			Timestamp:         evt.Timestamp,
			TextContent:       evt.TextContent,
			TextPreview:       preview,
			ToolName:          evt.ToolName,
			ToolUseID:         evt.ToolUseID,
			Model:             evt.Model,
			InputTokens:       evt.InputTokens,
			OutputTokens:      evt.OutputTokens,
			CacheReadTokens:   evt.CacheReadTokens,
			CacheCreateTokens: evt.CacheCreateTokens,
			DurationMs:        evt.DurationMs,
			CostUSD:           cost,
			ErrorCode:         evt.ErrorCode,
			ErrorMessage:      evt.ErrorMessage,
			EventVersion:      1,
			PayloadJSON:       evt.RawPayload,
			CWD:              evt.CWD,
			SourceFile:        evt.SourceFile,
			SourceLineNo:      evt.SourceLineNo,
			SourceOffset:      evt.SourceOffset,
		}

		if err := database.InsertEventTx(ctx, tx, event); err != nil {
			b.logger.Error("insert event failed", "uid", uid, "source", evt.SourceName, "kind", evt.EventKind, "error", err)
			continue
		}

		// Insert event links if parent UUID exists
		if evt.ParentUUID != "" {
			el := &models.EventLink{
				EventUID:       uid,
				LinkedEventUID: evt.ParentUUID,
				LinkType:       "parent",
			}
			if err := database.InsertEventLinkTx(ctx, tx, el); err != nil {
				b.logger.Error("insert event link failed", "error", err)
			}
		}

		// Insert tool I/O for tool_call/tool_result
		if evt.ToolPhase != "" && (evt.ToolInput != "" || evt.ToolOutput != "") {
			tio := &models.ToolIO{
				EventUID:      uid,
				ToolName:      evt.ToolName,
				ToolPhase:     evt.ToolPhase,
				InputJSON:     evt.ToolInput,
				OutputJSON:    evt.ToolOutput,
				InputPreview:  truncate(evt.ToolInput, previewMaxLen),
				OutputPreview: truncate(evt.ToolOutput, previewMaxLen),
			}
			if err := database.InsertToolIOTx(ctx, tx, tio); err != nil {
				b.logger.Error("insert tool io failed", "error", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		b.logger.Error("commit tx failed", "error", err)
		return
	}
	committed = true
	b.logger.Debug("flushInserts complete", "rows", len(events), "duration", time.Since(start))
}

// eventUID generates a deterministic UID for idempotent replay.
func eventUID(sourceFile string, lineNo int, offset int64, contentHash string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%s", sourceFile, lineNo, offset, contentHash)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func genID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
