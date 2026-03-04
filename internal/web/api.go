package web

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

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

	a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sessions`).Scan(&totalSessions)
	a.db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM model_calls`).Scan(&totalTokens)
	a.db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(cost_usd), 0) FROM model_calls`).Scan(&totalCost)

	var activeCount int
	a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sessions WHERE ended_at IS NULL`).Scan(&activeCount)

	metrics := []MetricData{
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
		`SELECT id, source, started_at, ended_at, cwd, git_repo, total_cost, turn_count, total_tokens, error_count
		 FROM session_summaries
		 ORDER BY started_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		var s SessionSummary
		var endedAt sql.NullTime
		if err := rows.Scan(&s.ID, &s.Source, &s.StartedAt, &endedAt, &s.CWD, &s.GitRepo, &s.TotalCost, &s.TurnCount, &s.TotalTokens, &s.ErrorCount); err != nil {
			continue
		}
		if endedAt.Valid {
			s.Duration = endedAt.Time.Sub(s.StartedAt).String()
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

	detail := SessionDetail{}

	// Session info
	var endedAt sql.NullTime
	err := a.db.QueryRowContext(r.Context(),
		`SELECT id, source, started_at, ended_at, cwd, git_repo, total_cost, turn_count, total_tokens, error_count
		 FROM session_summaries WHERE id = $1`, id,
	).Scan(&detail.Session.ID, &detail.Session.Source, &detail.Session.StartedAt, &endedAt,
		&detail.Session.CWD, &detail.Session.GitRepo, &detail.Session.TotalCost,
		&detail.Session.TurnCount, &detail.Session.TotalTokens, &detail.Session.ErrorCount)
	if err != nil {
		jsonError(w, "session not found", http.StatusNotFound)
		return
	}
	if endedAt.Valid {
		detail.Session.Duration = endedAt.Time.Sub(detail.Session.StartedAt).String()
	}

	// Turns
	turnRows, _ := a.db.QueryContext(r.Context(),
		`SELECT id, turn_number, user_prompt, started_at, input_tokens, output_tokens, cost_usd
		 FROM turns WHERE session_id = $1 ORDER BY turn_number`, id)
	if turnRows != nil {
		defer turnRows.Close()
		for turnRows.Next() {
			var t TurnSummary
			turnRows.Scan(&t.ID, &t.TurnNumber, &t.Prompt, &t.StartedAt, &t.InputTokens, &t.OutputTokens, &t.CostUSD)
			detail.Turns = append(detail.Turns, t)
		}
	}

	// Model calls
	mcRows, _ := a.db.QueryContext(r.Context(),
		`SELECT model, provider, input_tokens, output_tokens, duration_ms, cost_usd, created_at
		 FROM model_calls WHERE session_id = $1 ORDER BY created_at`, id)
	if mcRows != nil {
		defer mcRows.Close()
		for mcRows.Next() {
			var mc ModelCallView
			mcRows.Scan(&mc.Model, &mc.Provider, &mc.InputTokens, &mc.OutputTokens, &mc.DurationMs, &mc.CostUSD, &mc.Timestamp)
			detail.ModelCalls = append(detail.ModelCalls, mc)
		}
	}

	// Tool calls
	tcRows, _ := a.db.QueryContext(r.Context(),
		`SELECT tool_name, success, duration_ms, created_at
		 FROM tool_calls WHERE session_id = $1 ORDER BY created_at`, id)
	if tcRows != nil {
		defer tcRows.Close()
		for tcRows.Next() {
			var tc ToolCallView
			tcRows.Scan(&tc.ToolName, &tc.Success, &tc.DurationMs, &tc.Timestamp)
			detail.ToolCalls = append(detail.ToolCalls, tc)
		}
	}

	// Errors
	errRows, _ := a.db.QueryContext(r.Context(),
		`SELECT error_code, error_class, message, provider, created_at
		 FROM api_errors WHERE session_id = $1 ORDER BY created_at`, id)
	if errRows != nil {
		defer errRows.Close()
		for errRows.Next() {
			var e ErrorView
			errRows.Scan(&e.ErrorCode, &e.ErrorClass, &e.Message, &e.Provider, &e.Timestamp)
			detail.Errors = append(detail.Errors, e)
		}
	}

	// Context snapshots
	csRows, _ := a.db.QueryContext(r.Context(),
		`SELECT created_at, tokens_in_context, max_tokens, compaction_event
		 FROM context_snapshots WHERE session_id = $1 ORDER BY created_at`, id)
	if csRows != nil {
		defer csRows.Close()
		for csRows.Next() {
			var cp ContextPoint
			csRows.Scan(&cp.Timestamp, &cp.TokensInContext, &cp.MaxTokens, &cp.CompactionEvent)
			detail.Context = append(detail.Context, cp)
		}
	}

	jsonResponse(w, detail)
}

// SearchDocuments performs keyword + semantic search.
func (a *APIHandlers) SearchDocuments(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		jsonError(w, "missing query parameter 'q'", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	results, err := a.searcher.Search(r.Context(), query, limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to JSON-friendly format
	var jsonResults []SearchResult
	for _, sr := range results {
		jsonResults = append(jsonResults, SearchResult{
			DocumentID: sr.DocumentID,
			SessionID:  sr.SessionID,
			DocType:    sr.DocType,
			Content:    sr.Content,
			Snippet:    sr.Snippet,
			Score:      sr.Score,
			Source:     sr.Source,
			CreatedAt:  sr.CreatedAt,
		})
	}
	jsonResponse(w, jsonResults)
}

// GetTokensPerMinute returns time-series token data.
func (a *APIHandlers) GetTokensPerMinute(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT minute, total_input, total_output, total_tokens, total_cost, call_count
		 FROM tokens_per_minute LIMIT 60`)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var points []map[string]any
	for rows.Next() {
		var m map[string]any = make(map[string]any)
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

// GetToolStats returns tool success rate statistics.
func (a *APIHandlers) GetToolStats(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT tool_name, total_calls, successes, failures, success_rate, avg_duration_ms
		 FROM tool_success_rates`)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var stats []map[string]any
	for rows.Next() {
		m := make(map[string]any)
		var name string
		var total, successes, failures int
		var rate, avgDur float64
		rows.Scan(&name, &total, &successes, &failures, &rate, &avgDur)
		m["tool_name"] = name
		m["total_calls"] = total
		m["successes"] = successes
		m["failures"] = failures
		m["success_rate"] = rate
		m["avg_duration_ms"] = avgDur
		stats = append(stats, m)
	}
	jsonResponse(w, stats)
}

// GetHourlyCosts returns hourly cost breakdown.
func (a *APIHandlers) GetHourlyCosts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT hour, provider, model, total_cost, total_input, total_output, call_count
		 FROM hourly_costs LIMIT 168`) // 7 days
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
