package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/technodrome-ai/technodrome/internal/ingestion"
	"github.com/technodrome-ai/technodrome/internal/sse"
)

// NewRouter creates the chi router with all routes registered.
func NewRouter(
	otlpHandler *ingestion.OTLPHandler,
	broker *sse.Broker,
	handlers *Handlers,
	apiHandlers *APIHandlers,
) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// Static files
	fs := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	// Page routes (templ rendered)
	r.Get("/", handlers.Dashboard)
	r.Get("/sessions", handlers.Sessions)
	r.Get("/sessions/{id}", handlers.SessionDetail)
	r.Get("/search", handlers.Search)
	r.Get("/search/results", handlers.SearchResults)

	// OTLP ingestion endpoints
	r.Post("/v1/logs", otlpHandler.HandleLogs)
	r.Post("/v1/metrics", otlpHandler.HandleMetrics)

	// SSE endpoints
	r.Get("/sse/dashboard", broker.DashboardHandler)
	r.Get("/sse/session/{id}", broker.SessionHandler)

	// JSON API endpoints
	r.Route("/api", func(r chi.Router) {
		r.Get("/metrics", apiHandlers.GetMetrics)
		r.Get("/sessions", apiHandlers.GetSessions)
		r.Get("/sessions/{id}", apiHandlers.GetSessionDetail)
		r.Get("/search", apiHandlers.SearchDocuments)
		r.Get("/tokens-per-minute", apiHandlers.GetTokensPerMinute)
		r.Get("/tool-stats", apiHandlers.GetToolStats)
		r.Get("/hourly-costs", apiHandlers.GetHourlyCosts)
	})

	return r
}
