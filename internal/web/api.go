package web

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/technodrome-ai/technodrome/internal/search"
)

// APIHandlers serves JSON API endpoints.
type APIHandlers struct {
	db       *sql.DB
	searcher *search.Searcher
	logger   *slog.Logger
}

// NewAPIHandlers creates API handlers.
func NewAPIHandlers(db *sql.DB, searcher *search.Searcher, logger *slog.Logger) *APIHandlers {
	return &APIHandlers{db: db, searcher: searcher, logger: logger}
}

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// GetMetrics returns current dashboard metrics.
func (a *APIHandlers) GetMetrics(w http.ResponseWriter, r *http.Request) {
	var totalSessions int
	var totalTokens int64
	var totalCost float64
	var activeCount int

	a.db.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT session_id) FROM events`).Scan(&totalSessions)
	a.db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM events`).Scan(&totalTokens)
	a.db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(cost_usd), 0) FROM events`).Scan(&totalCost)
	a.db.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT session_id) FROM events WHERE timestamp > current_timestamp - INTERVAL '1 hour'`).Scan(&activeCount)

	metrics := []APIMetricData{
		{Label: "Total Sessions", Value: float64(totalSessions), Unit: "sessions"},
		{Label: "Active Sessions", Value: float64(activeCount), Unit: "sessions"},
		{Label: "Total Tokens", Value: float64(totalTokens), Unit: "tokens"},
		{Label: "Total Cost", Value: totalCost, Unit: "USD"},
	}

	jsonResponse(w, metrics)
}

// GetSessions returns session summaries.
func (a *APIHandlers) GetSessions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	rows, err := a.db.QueryContext(r.Context(),
		`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
		        total_cost, turn_count, total_tokens, error_count, COALESCE(last_model, '')
		 FROM v_session_summary
		 ORDER BY started_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sessions []APISessionSummary
	for rows.Next() {
		var s APISessionSummary
		var endedAt time.Time
		if err := rows.Scan(&s.ID, &s.Source, &s.StartedAt, &endedAt, &s.TotalCost, &s.TurnCount, &s.TotalTokens, &s.ErrorCount, &s.LastModel); err != nil {
			continue
		}
		if !endedAt.IsZero() && endedAt.After(s.StartedAt) {
			s.Duration = endedAt.Sub(s.StartedAt).String()
		} else {
			s.Duration = "active"
		}
		sessions = append(sessions, s)
	}
	jsonResponse(w, sessions)
}

// GetSessionDetail returns detailed info for a single session.
func (a *APIHandlers) GetSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	data, err := QuerySessionDetail(r.Context(), a.db, id)
	if err != nil {
		jsonError(w, "session not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, data)
}

// SearchEvents performs keyword search.
func (a *APIHandlers) SearchEvents(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		jsonError(w, "missing query parameter 'q'", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	results, err := a.searcher.LegacySearch(r.Context(), query, limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, results)
}

// GetTokensPerMinute returns time-series token data.
func (a *APIHandlers) GetTokensPerMinute(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT minute, total_input, total_output, total_tokens, total_cost, call_count
		 FROM v_tokens_per_minute LIMIT 60`)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var points []map[string]any
	for rows.Next() {
		m := make(map[string]any)
		var minute string
		var input, output, total int64
		var cost float64
		var count int
		rows.Scan(&minute, &input, &output, &total, &cost, &count)
		m["minute"] = minute
		m["input_tokens"] = input
		m["output_tokens"] = output
		m["total_tokens"] = total
		m["cost"] = cost
		m["call_count"] = count
		points = append(points, m)
	}
	jsonResponse(w, points)
}

// GetToolStats returns tool usage statistics.
func (a *APIHandlers) GetToolStats(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT tool_name, calls, results, total, avg_duration_ms FROM v_tool_stats`)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var stats []map[string]any
	for rows.Next() {
		m := make(map[string]any)
		var name string
		var calls, results, total int
		var avgDur float64
		rows.Scan(&name, &calls, &results, &total, &avgDur)
		m["tool_name"] = name
		m["calls"] = calls
		m["results"] = results
		m["total"] = total
		m["avg_duration_ms"] = avgDur
		stats = append(stats, m)
	}
	jsonResponse(w, stats)
}

// GetHourlyCosts returns hourly cost breakdown.
func (a *APIHandlers) GetHourlyCosts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT hour, COALESCE(provider, ''), COALESCE(model, ''), total_cost, total_input, total_output, call_count
		 FROM v_hourly_costs LIMIT 168`)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var costs []map[string]any
	for rows.Next() {
		m := make(map[string]any)
		var hour, provider, model string
		var cost float64
		var input, output int64
		var count int
		rows.Scan(&hour, &provider, &model, &cost, &input, &output, &count)
		m["hour"] = hour
		m["provider"] = provider
		m["model"] = model
		m["total_cost"] = cost
		m["total_input"] = input
		m["total_output"] = output
		m["call_count"] = count
		costs = append(costs, m)
	}
	jsonResponse(w, costs)
}
