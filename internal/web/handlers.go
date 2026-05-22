package web

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/views"
	"github.com/johnnygreco/beacon/internal/views/pages"
	"github.com/johnnygreco/beacon/internal/views/partials"
)

// Handlers serves HTML page routes rendered with templ.
type Handlers struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewHandlers creates page handlers.
func NewHandlers(db *sql.DB, _ *search.Searcher, logger *slog.Logger) *Handlers {
	return &Handlers{db: db, logger: logger}
}

// Dashboard renders the dashboard shell; data loads through JSON APIs.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if err := pages.Dashboard(views.DashboardData{}).Render(r.Context(), w); err != nil {
		h.logger.Debug("render dashboard failed", "error", err)
	}
	h.logger.Debug("Dashboard handler complete", "duration", time.Since(start))
}

// Sessions redirects to dashboard (sessions are shown on the dashboard).
func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

// SessionDetail renders a single session detail page.
// The conversation trace is loaded lazily via SessionConversation.
func (h *Handlers) SessionDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	data, err := QuerySessionDetail(r.Context(), h.db, id)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	if err := pages.SessionDetail(data).Render(r.Context(), w); err != nil {
		h.logger.Debug("render session detail failed", "error", err)
	}
}

// SessionConversation returns the conversation trace partial for lazy loading.
func (h *Handlers) SessionConversation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := chi.URLParam(r, "id")
	chatTurns, turns := QuerySessionConversation(r.Context(), h.db, id)
	childSessions := QueryChildSessions(r.Context(), h.db, id)
	ctx := views.ChatContext{ChildSessions: childSessions}
	if err := partials.SessionConversationWithContext(chatTurns, turns, ctx).Render(r.Context(), w); err != nil {
		h.logger.Debug("render conversation failed", "error", err)
	}
	eventCount := 0
	for _, t := range turns {
		eventCount += len(t.Events)
	}
	h.logger.Debug("SessionConversation handler complete", "session_id", id, "events", eventCount, "duration", time.Since(start))
}

// Health is a lightweight endpoint for connectivity checks.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Search redirects legacy search links to the dashboard table search.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/#dashboard-search", http.StatusFound)
}
