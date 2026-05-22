//go:build e2e

package web

import (
	"database/sql"
	"log/slog"
)

// NewHandlersForE2E wires production page handlers to a deterministic backend
// without requiring ClickHouse. The e2e build tag keeps this out of normal
// Beacon builds.
func NewHandlersForE2E(db *sql.DB, logger *slog.Logger) *Handlers {
	return &Handlers{db: db, logger: logger}
}
