package ingestion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/technodrome-ai/technodrome/internal/database"
	"github.com/technodrome-ai/technodrome/internal/models"
)

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
	var updates []UpdateEvent

	flush := func() {
		if len(inserts) == 0 && len(updates) == 0 {
			return
		}
		b.flushInserts(ctx, inserts)
		b.flushUpdates(ctx, updates)
		inserts = inserts[:0]
		updates = updates[:0]

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
			if evt.Update != nil {
				updates = append(updates, *evt.Update)
			}
			if len(inserts)+len(updates) >= b.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (b *Batcher) flushInserts(ctx context.Context, events []NormalizedEvent) {
	for _, evt := range events {
		id := genID()
		switch evt.EventType {
		case "user_prompt":
			if evt.SessionID != "" {
				database.InsertSession(ctx, b.db, &models.Session{
					ID:        evt.SessionID,
					Source:    evt.Source,
					StartedAt: evt.Timestamp,
					CWD:       evt.CWD,
					GitRepo:   evt.GitRepo,
				})
			}
			if evt.TurnID == "" {
				evt.TurnID = id
			}
			database.InsertTurn(ctx, b.db, &models.Turn{
				ID:         evt.TurnID,
				SessionID:  evt.SessionID,
				TurnNumber: evt.TurnNumber,
				UserPrompt: evt.UserPrompt,
				StartedAt:  evt.Timestamp,
			})
			if evt.DocContent != "" {
				database.InsertDocument(ctx, b.db, &models.Document{
					ID:        genID(),
					SessionID: evt.SessionID,
					TurnID:    evt.TurnID,
					DocType:   evt.DocType,
					Content:   evt.DocContent,
					CreatedAt: evt.Timestamp,
				})
			}

		case "api_request":
			cost := evt.CostUSD
			if cost == 0 {
				cost = CalcCost(evt.Model, evt.InputTokens, evt.OutputTokens, b.defaultInput, b.defaultOutput)
			}
			database.InsertModelCall(ctx, b.db, &models.ModelCall{
				ID:           id,
				SessionID:    evt.SessionID,
				TurnID:       evt.TurnID,
				Model:        evt.Model,
				Provider:     evt.Provider,
				InputTokens:  evt.InputTokens,
				OutputTokens: evt.OutputTokens,
				CacheRead:    evt.CacheRead,
				CacheCreate:  evt.CacheCreate,
				DurationMs:   evt.DurationMs,
				StatusCode:   evt.StatusCode,
				CostUSD:      cost,
				CreatedAt:    evt.Timestamp,
			})

		case "tool_use":
			database.InsertToolCall(ctx, b.db, &models.ToolCall{
				ID:        id,
				SessionID: evt.SessionID,
				TurnID:    evt.TurnID,
				ToolName:  evt.ToolName,
				Input:     evt.ToolInput,
				CreatedAt: evt.Timestamp,
			})

		case "tool_result":
			database.InsertToolCall(ctx, b.db, &models.ToolCall{
				ID:         id,
				SessionID:  evt.SessionID,
				TurnID:     evt.TurnID,
				ToolName:   evt.ToolName,
				Output:     evt.ToolOutput,
				Success:    evt.ToolSuccess,
				DurationMs: evt.DurationMs,
				CreatedAt:  evt.Timestamp,
			})
			if evt.DocContent != "" {
				database.InsertDocument(ctx, b.db, &models.Document{
					ID:        genID(),
					SessionID: evt.SessionID,
					TurnID:    evt.TurnID,
					DocType:   evt.DocType,
					Content:   evt.DocContent,
					CreatedAt: evt.Timestamp,
				})
			}

		case "api_error":
			database.InsertApiError(ctx, b.db, &models.ApiError{
				ID:         id,
				SessionID:  evt.SessionID,
				TurnID:     evt.TurnID,
				ErrorCode:  evt.ErrorCode,
				ErrorClass: evt.ErrorClass,
				Message:    evt.ErrorMsg,
				Provider:   evt.Provider,
				RetryCount: evt.RetryCount,
				CreatedAt:  evt.Timestamp,
			})

		case "context_snapshot":
			headroom := evt.MaxTokens - evt.TokensInContext
			if headroom < 0 {
				headroom = 0
			}
			database.InsertContextSnapshot(ctx, b.db, &models.ContextSnapshot{
				ID:              id,
				SessionID:       evt.SessionID,
				TurnID:          evt.TurnID,
				TokensInContext: evt.TokensInContext,
				MaxTokens:       evt.MaxTokens,
				Headroom:        headroom,
				CompactionEvent: evt.CompactionEvent,
				CreatedAt:       evt.Timestamp,
			})

		default:
			database.InsertRawEvent(ctx, b.db, &models.RawEvent{
				ID:        id,
				SessionID: evt.SessionID,
				Source:    evt.Source,
				EventType: evt.EventType,
				Payload:   evt.RawPayload,
				CreatedAt: evt.Timestamp,
			})
		}
	}
}

func (b *Batcher) flushUpdates(ctx context.Context, updates []UpdateEvent) {
	for _, u := range updates {
		if _, err := b.db.WriteConn().ExecContext(ctx, u.SQL, u.Args...); err != nil {
			b.logger.Error("update failed", "table", u.Table, "id", u.ID, "error", err)
		}
	}
}

func genID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
