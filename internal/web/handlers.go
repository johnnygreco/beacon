package web

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/technodrome-ai/technodrome/internal/search"
	"github.com/technodrome-ai/technodrome/internal/views"
	"github.com/technodrome-ai/technodrome/internal/views/pages"
	"github.com/technodrome-ai/technodrome/internal/views/partials"
)

// Handlers serves HTML page routes rendered with templ.
type Handlers struct {
	db       *sql.DB
	searcher *search.Searcher
	logger   *slog.Logger
}

// NewHandlers creates page handlers.
func NewHandlers(db *sql.DB, searcher *search.Searcher, logger *slog.Logger) *Handlers {
	return &Handlers{db: db, searcher: searcher, logger: logger}
}

// Dashboard renders the main dashboard page with live metrics.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	data := QueryDashboardData(r.Context(), h.db)
	pages.Dashboard(data).Render(r.Context(), w)
}

// Sessions redirects to dashboard (sessions are shown on the dashboard).
func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

// SessionDetail renders a single session detail page.
func (h *Handlers) SessionDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	data, err := QuerySessionDetail(r.Context(), h.db, id)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	pages.SessionDetail(data).Render(r.Context(), w)
}

// Search renders the search page (results load via HTMX).
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	pages.Search(nil).Render(r.Context(), w)
}

// SearchResults handles HTMX partial requests for search results.
func (h *Handlers) SearchResults(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		partials.SearchResults(nil).Render(r.Context(), w)
		return
	}

	results, err := h.searcher.Search(r.Context(), query, 20)
	if err != nil {
		h.logger.Error("search failed", "error", err)
		partials.SearchResults(nil).Render(r.Context(), w)
		return
	}

	// Convert search.Result to views.SearchResult for templ rendering
	var viewResults []views.SearchResult
	for _, sr := range results {
		viewResults = append(viewResults, views.SearchResult{
			DocumentID: sr.DocumentID,
			SessionID:  sr.SessionID,
			Content:    sr.Content,
			Snippet:    sr.Snippet,
			Score:      sr.Score,
			MatchType:  "hybrid",
			Timestamp:  sr.CreatedAt,
		})
	}

	partials.SearchResults(viewResults).Render(r.Context(), w)
}
