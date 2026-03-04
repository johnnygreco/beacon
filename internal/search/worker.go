package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/technodrome-ai/technodrome/internal/ingestion"
)

// Worker polls for documents without embeddings and generates them.
type Worker struct {
	db       *sql.DB
	provider EmbeddingProvider
	eventCh  chan<- ingestion.BatchEvent
	logger   *slog.Logger
	interval time.Duration
}

// NewWorker creates a new embedding worker.
func NewWorker(db *sql.DB, provider EmbeddingProvider, eventCh chan<- ingestion.BatchEvent, logger *slog.Logger) *Worker {
	return &Worker{
		db:       db,
		provider: provider,
		eventCh:  eventCh,
		logger:   logger,
		interval: 10 * time.Second,
	}
}

// Run starts the embedding worker loop.
func (w *Worker) Run(ctx context.Context) {
	if w.provider == nil {
		w.logger.Info("no embedding provider configured, embedding worker disabled")
		return
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processUnembedded(ctx)
		}
	}
}

func (w *Worker) processUnembedded(ctx context.Context) {
	rows, err := w.db.QueryContext(ctx,
		`SELECT id, content FROM documents
		 WHERE embedding IS NULL AND content != '' AND LENGTH(content) > 10
		 LIMIT 50`,
	)
	if err != nil {
		w.logger.Error("failed to query unembedded documents", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			continue
		}

		// Truncate very long content for embedding
		if len(content) > 8000 {
			content = content[:8000]
		}

		embedding, err := w.provider.Embed(ctx, content)
		if err != nil {
			w.logger.Warn("failed to embed document", "id", id, "error", err)
			continue
		}

		// Send update through batcher to serialize writes
		w.eventCh <- ingestion.BatchEvent{
			Update: &ingestion.UpdateEvent{
				Table: "documents",
				ID:    id,
				SQL:   fmt.Sprintf(`UPDATE documents SET embedding = %s::FLOAT[], embedding_model = $1 WHERE id = $2`, formatEmbedding(embedding)),
				Args:  []any{w.provider.ModelName(), id},
			},
		}
	}
}
