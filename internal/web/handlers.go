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
	db       *sql.DB
	searcher *search.Searcher
	logger   *slog.Logger
	updater  *Updater
}

// NewHandlers creates page handlers.
func NewHandlers(db *sql.DB, searcher *search.Searcher, logger *slog.Logger, updater *Updater) *Handlers {
	return &Handlers{db: db, searcher: searcher, logger: logger, updater: updater}
}

// Dashboard renders the main dashboard page with live metrics.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	var data views.DashboardData
	if snap := h.updater.Snapshot(); snap != nil {
		data = *snap
	} else {
		data = QueryDashboardData(r.Context(), h.db)
	}
	if err := pages.Dashboard(data).Render(r.Context(), w); err != nil {
		h.logger.Debug("render dashboard failed", "error", err)
	}
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
	id := chi.URLParam(r, "id")
	chatTurns, turns := QuerySessionConversation(r.Context(), h.db, id)
	childSessions := QueryChildSessions(r.Context(), h.db, id)
	ctx := views.ChatContext{ChildSessions: childSessions}
	if err := partials.SessionConversationWithContext(chatTurns, turns, ctx).Render(r.Context(), w); err != nil {
		h.logger.Debug("render conversation failed", "error", err)
	}
}

// Health is a lightweight endpoint for connectivity checks.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Search renders the search page (results load via HTMX).
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	if err := pages.Search(nil).Render(r.Context(), w); err != nil {
		h.logger.Debug("render search failed", "error", err)
	}
}

// SearchResults handles HTMX partial requests for search results.
func (h *Handlers) SearchResults(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		if err := partials.SearchResults(nil).Render(r.Context(), w); err != nil {
			h.logger.Debug("render search results failed", "error", err)
		}
		return
	}

	sq := search.SearchQuery{
		Query:     query,
		Limit:     20,
		SessionID: r.URL.Query().Get("session_id"),
	}
	if ek := r.URL.Query().Get("event_kind"); ek != "" {
		sq.EventKinds = []string{ek}
	}
	if v := r.URL.Query().Get("range"); v != "" {
		now := time.Now()
		switch v {
		case "1h":
			sq.FromTime = now.Add(-1 * time.Hour)
		case "24h":
			sq.FromTime = now.Add(-24 * time.Hour)
		case "7d":
			sq.FromTime = now.Add(-7 * 24 * time.Hour)
		case "30d":
			sq.FromTime = now.Add(-30 * 24 * time.Hour)
		}
	}

	results, err := h.searcher.Search(r.Context(), sq)
	if err != nil {
		h.logger.Error("search failed", "error", err)
		if err := partials.SearchResults(nil).Render(r.Context(), w); err != nil {
			h.logger.Debug("render search results failed", "error", err)
		}
		return
	}

	var viewResults []views.SearchResult
	for _, sr := range results {
		viewResults = append(viewResults, views.SearchResult{
			EventUID:  sr.EventUID,
			SessionID: sr.SessionID,
			EventKind: sr.EventKind,
			Snippet:   sr.TextPreview,
			Score:     sr.Score,
			Timestamp: sr.Timestamp,
		})
	}

	if err := partials.SearchResults(viewResults).Render(r.Context(), w); err != nil {
		h.logger.Debug("render search results failed", "error", err)
	}
}
