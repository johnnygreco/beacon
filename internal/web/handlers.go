package web

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
		if err := partials.SearchResultsWithCount(nil, -1, false).Render(r.Context(), w); err != nil {
			h.logger.Debug("render search results failed", "error", err)
		}
		return
	}

	// Determine page size from user-selected limit.
	pageSize := defaultSearchPageSize
	if lim, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && lim > 0 && lim <= 500 {
		pageSize = lim
	}

	// Fetch one extra to detect if there are more results.
	sq := search.SearchQuery{
		Query:     query,
		Limit:     pageSize + 1,
		SessionID: r.URL.Query().Get("session_id"),
	}
	if ek := r.URL.Query().Get("event_kind"); ek != "" {
		sq.EventKinds = []string{ek}
	}
	if t := parseRange(r.URL.Query().Get("range")); t != nil {
		sq.FromTime = *t
	}

	results, err := h.searcher.Search(r.Context(), sq)
	if err != nil {
		h.logger.Error("search failed", "error", err)
		if err := partials.SearchResultsWithCount(nil, 0, false).Render(r.Context(), w); err != nil {
			h.logger.Debug("render search results failed", "error", err)
		}
		return
	}

	hasMore := len(results) > pageSize
	if hasMore {
		results = results[:pageSize]
	}

	var viewResults []views.SearchResult
	for _, sr := range results {
		snippet := sr.TextPreview
		// For tool_call events, extract a human-readable preview from
		// the raw JSON input, and strip the redundant tool name prefix.
		if sr.EventKind == "tool_call" && sr.ToolName != "" {
			// Strip "ToolName: " prefix if present
			raw := strings.TrimPrefix(snippet, sr.ToolName+": ")
			if raw == sr.ToolName || raw == "" {
				snippet = ""
			} else if formatted := formatToolCallSnippet(sr.ToolName, raw); formatted != "" {
				snippet = formatted
			}
		}
		viewResults = append(viewResults, views.SearchResult{
			EventUID:  sr.EventUID,
			SessionID: sr.SessionID,
			EventKind: sr.EventKind,
			Snippet:   snippet,
			ToolName:  sr.ToolName,
			Score:     sr.Score,
			Timestamp: sr.Timestamp,
		})
	}

	if err := partials.SearchResultsWithCount(viewResults, len(viewResults), hasMore).Render(r.Context(), w); err != nil {
		h.logger.Debug("render search results failed", "error", err)
	}
}

// DashboardSessions returns paginated completed sessions as an HTMX partial.
func (h *Handlers) DashboardSessions(w http.ResponseWriter, r *http.Request) {
	rangeVal := r.URL.Query().Get("range")
	since := parseRange(rangeVal)
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	sessions, hasMore := QueryCompletedSessions(r.Context(), h.db, since, offset, defaultSessionPageSize)

	if err := partials.CompletedSessionListPaginated(sessions, hasMore, rangeVal, offset, defaultSessionPageSize).Render(r.Context(), w); err != nil {
		h.logger.Debug("render dashboard sessions failed", "error", err)
	}
}

// DashboardSubagentSessions returns subagent rows for a parent session (HTMX partial).
func (h *Handlers) DashboardSubagentSessions(w http.ResponseWriter, r *http.Request) {
	parentID := chi.URLParam(r, "id")
	if parentID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	subagents := QueryChildSessions(r.Context(), h.db, parentID)
	if err := partials.CompletedSubagentRows(subagents, parentID).Render(r.Context(), w); err != nil {
		h.logger.Debug("render subagent sessions failed", "error", err)
	}
}

// formatToolCallSnippet extracts a human-readable preview from raw tool input JSON.
// The JSON may be truncated (input_preview is capped at 320 chars), so we also
// try regex extraction when json.Unmarshal fails.
func formatToolCallSnippet(toolName, rawJSON string) string {
	if rawJSON == "" || rawJSON == toolName {
		return ""
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &params); err != nil {
		// Truncated JSON — try to extract the most relevant field with string matching.
		return extractJSONField(toolName, rawJSON)
	}
	// Pick the most meaningful field for each tool type.
	switch toolName {
	case "Bash":
		if cmd, ok := params["command"].(string); ok {
			if desc, ok := params["description"].(string); ok && desc != "" {
				return desc + " — " + truncateStr(cmd, 120)
			}
			return truncateStr(cmd, 200)
		}
	case "Read", "Edit", "Write":
		if fp, ok := params["file_path"].(string); ok {
			return fp
		}
	case "Glob", "Grep":
		if p, ok := params["pattern"].(string); ok {
			return p
		}
	case "Agent":
		if p, ok := params["prompt"].(string); ok {
			return truncateStr(p, 200)
		}
	}
	// Generic: show file_path or command if present.
	for _, key := range []string{"file_path", "command", "pattern", "prompt", "description"} {
		if v, ok := params[key].(string); ok && v != "" {
			return truncateStr(v, 200)
		}
	}
	return truncateStr(rawJSON, 200)
}

// extractJSONField extracts a key field from possibly-truncated JSON using string matching.
func extractJSONField(toolName, raw string) string {
	// Determine which field to look for based on tool type.
	keys := []string{"file_path", "command", "pattern", "prompt", "description"}
	for _, key := range keys {
		prefix := `"` + key + `":"`
		idx := strings.Index(raw, prefix)
		if idx < 0 {
			continue
		}
		start := idx + len(prefix)
		// Find the closing quote, handling escaped quotes.
		end := start
		for end < len(raw) {
			if raw[end] == '\\' {
				end += 2
				continue
			}
			if raw[end] == '"' {
				break
			}
			end++
		}
		if end > start {
			val := raw[start:end]
			// Unescape common JSON escapes
			val = strings.ReplaceAll(val, `\"`, `"`)
			val = strings.ReplaceAll(val, `\\`, `\`)
			return truncateStr(val, 200)
		}
	}
	return truncateStr(raw, 200)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// DashboardActivity returns activity items as an HTMX partial (24h window, no pagination).
func (h *Handlers) DashboardActivity(w http.ResponseWriter, r *http.Request) {
	rangeVal := r.URL.Query().Get("range")
	since := parseRange(rangeVal)
	// Default to 24h if no range specified
	if since == nil {
		t := time.Now().Add(-24 * time.Hour)
		since = &t
	}

	items, _ := QueryRecentActivityFiltered(r.Context(), h.db, since, 0, activityTimelineLimit)

	if err := partials.ActivityTimelineFull(items).Render(r.Context(), w); err != nil {
		h.logger.Debug("render dashboard activity failed", "error", err)
	}
}
