//go:build e2e

package web

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/johnnygreco/beacon/internal/search"
)

// E2ESearchBackend is the test-mode search backend contract used by the
// Playwright server harness. It intentionally mirrors the production searcher
// methods consumed by page handlers.
type E2ESearchBackend interface {
	Search(ctx context.Context, q search.SearchQuery) ([]search.SearchResult, error)
	Browse(ctx context.Context, q search.SearchQuery) ([]search.SearchResult, error)
}

// NewHandlersForE2E wires production page handlers to a deterministic backend
// without requiring ClickHouse. The e2e build tag keeps this out of normal
// Beacon builds.
func NewHandlersForE2E(db *sql.DB, searcher E2ESearchBackend, logger *slog.Logger) *Handlers {
	return &Handlers{db: db, searcher: searcher, logger: logger}
}
