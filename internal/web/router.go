package web

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/johnnygreco/beacon/internal/sse"
)

// NewRouter creates the chi router with all routes registered.
func NewRouter(
	staticFS fs.FS,
	broker *sse.Broker,
	handlers *Handlers,
	apiHandlers *APIHandlers,
) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// Static files
	fileServer := http.FileServer(http.FS(staticFS))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Page routes (templ rendered)
	r.Get("/", handlers.Dashboard)
	r.Get("/sessions", handlers.Sessions)
	r.Get("/sessions/{id}", handlers.SessionDetail)
	r.Get("/sessions/{id}/conversation", handlers.SessionConversation)
	r.Get("/search", handlers.Search)
	r.Get("/search/results", handlers.SearchResults)
	r.Get("/health", handlers.Health)

	// SSE endpoints
	r.Get("/sse/dashboard", broker.DashboardHandler)
	r.Get("/sse/session/{id}", broker.SessionHandler)

	// JSON API endpoints
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", handlers.Health)
		r.Get("/status", apiHandlers.GetMetrics)
		r.Get("/analytics", apiHandlers.GetTokensByModel)
		r.Get("/metrics", apiHandlers.GetMetrics)
		r.Get("/sessions", apiHandlers.GetSessions)
		r.Get("/dashboard/sessions", apiHandlers.GetDashboardSessions)
		r.Get("/dashboard/activity", apiHandlers.GetActivity)
		r.Get("/dashboard/charts", apiHandlers.GetDashboardCharts)
		r.Get("/sessions/{id}", apiHandlers.GetSessionDetail)
		r.Get("/sessions/{id}/subagents", apiHandlers.GetSessionSubagents)
		r.Get("/sessions/{id}/events", apiHandlers.GetSessionEvents)
		r.Get("/events/{event_id}", apiHandlers.GetEvent)
		r.Get("/tool-payloads/{event_id}", apiHandlers.GetToolPayload)
		r.Get("/search", apiHandlers.SearchEvents)
		r.Get("/tokens-per-minute", apiHandlers.GetTokensPerMinute)
		r.Get("/tool-stats", apiHandlers.GetToolStats)
		r.Get("/tokens-by-model", apiHandlers.GetTokensByModel)
	})

	return r
}
