package capture

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

const previewMaxLen = 320

// Batcher accumulates events and flushes them to ClickHouse in table batches.
// Its input channel is sized to twice the configured batch size and applies
// backpressure to watcher sends when full; capture does not drop parsed events
// under bursty ingest.
type Batcher struct {
	eventCh chan BatchEvent
	store   *store.Store
	notify  func([]string) // called after each flush to notify SSE broker
	logger  *slog.Logger

	batchSize     int
	flushInterval time.Duration
	defaultInput  float64
	defaultOutput float64
}

// NewBatcher creates a new batcher.
func NewBatcher(ch *store.Store, batchSize int, flushInterval time.Duration, defaultInput, defaultOutput float64, notify func([]string), logger *slog.Logger) *Batcher {
	return &Batcher{
		eventCh:       make(chan BatchEvent, batchSize*2),
		store:         ch,
		notify:        notify,
		logger:        logger,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		defaultInput:  defaultInput,
		defaultOutput: defaultOutput,
	}
}

// EventCh returns the channel to send events to. Senders should select on their
// context when writing so cancellation can break out of batcher backpressure.
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
		sessionIDs := changedSessionIDs(inserts)
		b.flushInserts(ctx, inserts)
		inserts = inserts[:0]

		if b.notify != nil {
			b.notify(sessionIDs)
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

func changedSessionIDs(events []NormalizedEvent) []string {
	seen := make(map[string]struct{}, len(events))
	ids := make([]string, 0, len(events))
	for _, evt := range events {
		if evt.SessionID == "" {
			continue
		}
		if _, ok := seen[evt.SessionID]; ok {
			continue
		}
		seen[evt.SessionID] = struct{}{}
		ids = append(ids, evt.SessionID)
	}
	return ids
}

func (b *Batcher) flushInserts(ctx context.Context, events []NormalizedEvent) {
	start := time.Now()

	batch := buildInsertRowBatch(events, b.defaultInput, b.defaultOutput)

	if err := b.store.Flush(ctx, batch); err != nil {
		b.logger.Error("clickhouse flush failed", "error", err, "rows", len(events))
		return
	}
	b.logger.Debug("flushInserts complete", "rows", len(events), "duration", time.Since(start))
}

func buildInsertRowBatch(events []NormalizedEvent, defaultInput, defaultOutput float64) store.RowBatch {
	var batch store.RowBatch
	eventOrdinals := make(map[string]int, len(events))

	for _, evt := range events {
		ordinalKey := eventUIDOrdinalKey(evt)
		ordinal := eventOrdinals[ordinalKey]
		eventOrdinals[ordinalKey] = ordinal + 1
		uid := eventUID(evt.SourceFile, evt.SourceLineNo, evt.SourceOffset, evt.SourceGeneration, evt.RawPayload, ordinal)

		cost := evt.CostUSD
		if cost == 0 && (evt.InputTokens > 0 || evt.OutputTokens > 0) {
			cost = CalcCost(evt.Model, evt.InputTokens, evt.OutputTokens, defaultInput, defaultOutput)
		}

		preview := truncate(evt.TextContent, previewMaxLen)
		event := &models.Event{
			EventUID:          uid,
			SessionID:         evt.SessionID,
			ParentSessionID:   evt.ParentSessionID,
			SourceName:        evt.SourceName,
			Runtime:           evt.Runtime,
			Provider:          evt.Provider,
			Format:            evt.Format,
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
			CWD:               evt.CWD,
			SourceFile:        evt.SourceFile,
			SourceLineNo:      evt.SourceLineNo,
			SourceOffset:      evt.SourceOffset,
			SourceGeneration:  evt.SourceGeneration,
		}

		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(*event))
		batch.ActivityEvents = append(batch.ActivityEvents, *event)

		if evt.ParentUUID != "" {
			batch.EventLinks = append(batch.EventLinks, models.EventLink{
				EventUID:       uid,
				LinkedEventUID: evt.ParentUUID,
				LinkType:       "parent",
			})
		}

		if evt.ToolPhase != "" && (evt.ToolInput != "" || evt.ToolOutput != "") {
			payload := models.ToolPayload{
				EventUID:      uid,
				ToolName:      evt.ToolName,
				ToolPhase:     evt.ToolPhase,
				InputJSON:     evt.ToolInput,
				OutputJSON:    evt.ToolOutput,
				InputPreview:  truncate(evt.ToolInput, previewMaxLen),
				OutputPreview: truncate(evt.ToolOutput, previewMaxLen),
			}
			batch.ToolPayloads = append(batch.ToolPayloads, payload)
		}
	}
	return batch
}

// eventUID generates a deterministic UID for idempotent replay.
func eventUIDOrdinalKey(evt NormalizedEvent) string {
	return fmt.Sprintf("%s|%d|%d|%d|%s", evt.SourceFile, evt.SourceLineNo, evt.SourceOffset, evt.SourceGeneration, evt.RawPayload)
}

func eventUID(sourceFile string, lineNo int, offset int64, generation int, contentHash string, ordinal int) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%d|%s", sourceFile, lineNo, offset, generation, contentHash)
	if ordinal > 0 {
		fmt.Fprintf(h, "|%d", ordinal)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func genID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
