package web

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/search"
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
	var totalSessions, activeCount, toolCalls, mcpCalls int
	var inputTokens, outputTokens int64

	a.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT session_id),
		        COUNT(DISTINCT CASE WHEN timestamp > current_timestamp - INTERVAL '1 hour' THEN session_id END),
		        COALESCE(SUM(input_tokens), 0),
		        COALESCE(SUM(output_tokens), 0),
		        COUNT(CASE WHEN event_kind = 'tool_call' THEN 1 END),
		        COUNT(CASE WHEN event_kind = 'tool_call' AND tool_name LIKE 'mcp__%' THEN 1 END)
		 FROM events`,
	).Scan(&totalSessions, &activeCount, &inputTokens, &outputTokens, &toolCalls, &mcpCalls)

	metrics := []APIMetricData{
		{Label: "Total Sessions", Value: float64(totalSessions), Unit: "sessions"},
		{Label: "Active Sessions", Value: float64(activeCount), Unit: "sessions"},
		{Label: "Input Tokens", Value: float64(inputTokens), Unit: "tokens"},
		{Label: "Output Tokens", Value: float64(outputTokens), Unit: "tokens"},
		{Label: "Tool Calls", Value: float64(toolCalls), Unit: "calls"},
		{Label: "MCP Calls", Value: float64(mcpCalls), Unit: "calls"},
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
		        turn_count, total_tokens, total_input_tokens, total_output_tokens,
		        total_cache_read_tokens, total_cache_create_tokens,
		        tool_call_count, mcp_call_count, error_count, COALESCE(last_model, '')
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
		if err := rows.Scan(&s.ID, &s.Source, &s.StartedAt, &endedAt,
			&s.TurnCount, &s.TotalTokens, &s.InputTokens, &s.OutputTokens,
			&s.CacheReadTokens, &s.CacheCreateTokens,
			&s.ToolCallCount, &s.MCPCallCount, &s.ErrorCount, &s.LastModel); err != nil {
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
	data.ChatTurns, data.Turns = QuerySessionConversation(r.Context(), a.db, id)
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

// GetTokensPerMinute returns time-series token data with breakdown.
func (a *APIHandlers) GetTokensPerMinute(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT minute, total_input, total_output, total_cache_read, total_tokens, call_count
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
		var input, output, cacheRead, total int64
		var count int
		rows.Scan(&minute, &input, &output, &cacheRead, &total, &count)
		m["minute"] = minute
		m["input_tokens"] = input
		m["output_tokens"] = output
		m["cache_read_tokens"] = cacheRead
		m["total_tokens"] = total
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
		m["is_mcp"] = models.IsMCPTool(name)
		stats = append(stats, m)
	}
	jsonResponse(w, stats)
}

// GetTokensByModel returns token usage broken down by model.
func (a *APIHandlers) GetTokensByModel(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT model, total_input, total_output, total_cache_read, total_cache_create, total_tokens, call_count
		 FROM v_tokens_by_model`)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		m := make(map[string]any)
		var model string
		var input, output, cacheRead, cacheCreate, total int64
		var count int
		rows.Scan(&model, &input, &output, &cacheRead, &cacheCreate, &total, &count)
		m["model"] = model
		m["input_tokens"] = input
		m["output_tokens"] = output
		m["cache_read_tokens"] = cacheRead
		m["cache_create_tokens"] = cacheCreate
		m["total_tokens"] = total
		m["call_count"] = count
		items = append(items, m)
	}
	jsonResponse(w, items)
}
