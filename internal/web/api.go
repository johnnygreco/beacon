package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/views"
)

const completedSessionEventSearchLimit = 5000

type apiSearcher interface {
	Search(ctx context.Context, q search.SearchQuery) ([]search.SearchResult, error)
	Browse(ctx context.Context, q search.SearchQuery) ([]search.SearchResult, error)
}

// APIHandlers serves JSON API endpoints.
type APIHandlers struct {
	db       *sql.DB
	searcher apiSearcher
	logger   *slog.Logger
}

// NewAPIHandlers creates API handlers.
func NewAPIHandlers(db *sql.DB, searcher *search.Searcher, logger *slog.Logger) *APIHandlers {
	var backend apiSearcher
	if searcher != nil {
		backend = searcher
	}
	return &APIHandlers{db: db, searcher: backend, logger: logger}
}

type apiErrorResponse struct {
	Error string `json:"error"`
}

func (a *APIHandlers) log() *slog.Logger {
	if a != nil && a.logger != nil {
		return a.logger
	}
	return slog.Default()
}

func (a *APIHandlers) jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		a.log().Debug("json response write failed", "error", err)
	}
}

func (a *APIHandlers) jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(apiErrorResponse{Error: msg}); err != nil {
		a.log().Debug("json error response write failed", "error", err)
	}
}

func (a *APIHandlers) badRequest(w http.ResponseWriter, err error) {
	a.jsonError(w, err.Error(), http.StatusBadRequest)
}

func (a *APIHandlers) internalError(w http.ResponseWriter, publicMessage string, err error) {
	a.log().Error(publicMessage, "error", err)
	a.jsonError(w, publicMessage, http.StatusInternalServerError)
}

func (a *APIHandlers) logSkippedRow(handler string, err error) {
	a.log().Warn(handler+" row skipped", "error", err)
}

// GetMetrics returns current dashboard metrics.
func (a *APIHandlers) GetMetrics(w http.ResponseWriter, r *http.Request) {
	var totalSessions, activeCount, toolCalls, mcpCalls int
	var inputTokens, outputTokens int64
	activeCutoff := time.Now().Add(-idleThreshold)

	if err := a.db.QueryRowContext(r.Context(),
		`SELECT count(),
		        countIf(`+activeSessionPredicate()+`),
		        COALESCE(SUM(total_input_tokens), 0),
		        COALESCE(SUM(total_output_tokens), 0),
		        COALESCE(SUM(tool_call_count), 0),
		        COALESCE(SUM(mcp_call_count), 0)
		 FROM `+sessionProjectionSQL, activeCutoff, activeCutoff,
	).Scan(&totalSessions, &activeCount, &inputTokens, &outputTokens, &toolCalls, &mcpCalls); err != nil {
		a.internalError(w, "failed to query metrics", err)
		return
	}

	metrics := []APIMetricData{
		{Label: "Total Sessions", Value: float64(totalSessions), Unit: "sessions"},
		{Label: "Active Sessions", Value: float64(activeCount), Unit: "sessions"},
		{Label: "Input Tokens", Value: float64(inputTokens), Unit: "tokens"},
		{Label: "Output Tokens", Value: float64(outputTokens), Unit: "tokens"},
		{Label: "Tool Calls", Value: float64(toolCalls), Unit: "calls"},
		{Label: "MCP Calls", Value: float64(mcpCalls), Unit: "calls"},
	}

	a.jsonResponse(w, metrics)
}

// GetSessions returns session summaries.
func (a *APIHandlers) GetSessions(w http.ResponseWriter, r *http.Request) {
	req, err := parseSessionsAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}

	now := time.Now()
	activeCutoff := now.Add(-idleThreshold)
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT `+sessionSummaryColumnsWithReopenedFlag()+`
		 FROM `+sessionProjectionSQL+`
		 ORDER BY started_at DESC
		 LIMIT ?`, activeCutoff, req.Limit)
	if err != nil {
		a.internalError(w, "failed to query sessions", err)
		return
	}
	defer rows.Close()

	sessions := make([]APISessionSummary, 0)
	for rows.Next() {
		s, err := scanSessionSummaryIncludingReopened(rows, now)
		if err != nil {
			a.logSkippedRow("sessions", err)
			continue
		}
		sessions = append(sessions, apiSessionSummaryFromView(s))
	}
	if err := rows.Err(); err != nil {
		a.internalError(w, "failed to query sessions", err)
		return
	}
	a.jsonResponse(w, sessions)
}

// GetDashboardSessions returns dashboard session rows as JSON for client-side rendering.
func (a *APIHandlers) GetDashboardSessions(w http.ResponseWriter, r *http.Request) {
	req, err := parseDashboardSessionsAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}

	var sessions []views.SessionSummary
	hasMore := false
	switch req.State {
	case "active":
		sessions = QueryActiveSessions(r.Context(), a.db)
	default:
		eventSessionIDs, err := a.completedSessionContentSearchSessionIDs(r.Context(), req.Query, req.Range, req.SessionID)
		if err != nil {
			a.internalError(w, "search failed", err)
			return
		}
		sessions, hasMore = queryCompletedSessionsFiltered(r.Context(), a.db, parseRange(req.Range), req.Offset, req.Limit, req.Query, eventSessionIDs, req.SortKey, req.SortAsc, req.SessionID)
		req.State = "completed"
	}

	items := make([]APISessionSummary, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, apiSessionSummaryFromView(session))
	}
	a.jsonResponse(w, APIDashboardSessionsResponse{
		State:   req.State,
		Range:   req.Range,
		Query:   req.Query,
		Offset:  req.Offset,
		Limit:   req.Limit,
		HasMore: hasMore,
		Items:   items,
	})
}

func (a *APIHandlers) completedSessionEventSearchSessionIDs(ctx context.Context, query string) ([]string, error) {
	return a.completedSessionContentSearchSessionIDs(ctx, query, "", "")
}

func (a *APIHandlers) completedSessionContentSearchSessionIDs(ctx context.Context, query, rangeVal, sessionIDPrefix string) ([]string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	var allIDs []string
	var firstErr error
	if a.searcher != nil {
		sq := search.SearchQuery{
			Query:     query,
			Limit:     completedSessionEventSearchLimit,
			SessionID: sessionIDPrefix,
		}
		if t := parseRange(rangeVal); t != nil {
			sq.FromTime = *t
		}
		results, err := a.searcher.Search(ctx, sq)
		if err != nil {
			firstErr = err
			a.log().Debug("indexed session content search failed", "error", err)
		} else {
			indexedIDs := searchResultSessionIDs(results)
			if len(indexedIDs) > 0 {
				return indexedIDs, nil
			}
		}
	}
	dbSucceeded := false
	dbIDs, err := queryCompletedSessionContentMatchIDs(ctx, a.db, parseRange(rangeVal), query, sessionIDPrefix, completedSessionEventSearchLimit)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		a.log().Debug("database session content search failed", "error", err)
	} else {
		dbSucceeded = a.db != nil
		allIDs = append(allIDs, dbIDs...)
	}
	ids := uniqueSessionIDs(allIDs)
	if len(ids) > 0 || dbSucceeded || firstErr == nil {
		return ids, nil
	}
	return nil, firstErr
}

func searchResultSessionIDs(results []search.SearchResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.SessionID)
	}
	return uniqueSessionIDs(ids)
}

func uniqueSessionIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

// GetDashboardSearch returns event and session-metadata search results for the dashboard table.
func (a *APIHandlers) GetDashboardSearch(w http.ResponseWriter, r *http.Request) {
	req, err := parseDashboardSearchAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}
	if !req.active() {
		a.jsonResponse(w, APIDashboardSearchResponse{
			State: "idle",
			Sort:  req.SortBy,
			Limit: req.Limit,
			Items: []APIDashboardSearchResult{},
		})
		return
	}
	if a.searcher == nil && req.EventKind != "session" {
		a.jsonResponse(w, APIDashboardSearchResponse{
			State:     "unavailable",
			Query:     req.Query,
			Range:     req.Range,
			EventKind: req.EventKind,
			SessionID: req.SessionID,
			Sort:      req.SortBy,
			Limit:     req.Limit,
			Items:     []APIDashboardSearchResult{},
		})
		return
	}

	items := make([]APIDashboardSearchResult, 0)
	seenSessions := make(map[string]struct{})
	if req.EventKind != "session" {
		sq := search.SearchQuery{
			Query:      req.Query,
			Limit:      req.Limit + 1,
			SessionID:  req.SessionID,
			EventKinds: dashboardSearchEventKinds(req.EventKind),
			SortBy:     req.SortBy,
		}
		if t := parseRange(req.Range); t != nil {
			sq.FromTime = *t
		}

		var results []search.SearchResult
		if req.Query == "" {
			results, err = a.searcher.Browse(r.Context(), sq)
		} else {
			results, err = a.searcher.Search(r.Context(), sq)
		}
		if err != nil {
			a.internalError(w, "search failed", err)
			return
		}

		sessionMeta := a.dashboardSearchSessionMeta(r.Context(), searchResultSessionIDs(results))
		items = make([]APIDashboardSearchResult, 0, len(results))
		seenSessions = make(map[string]struct{}, len(results))
		for _, result := range results {
			if result.SessionID != "" {
				seenSessions[result.SessionID] = struct{}{}
			}
			meta := sessionMeta[result.SessionID]
			items = append(items, APIDashboardSearchResult{
				ResultType:   "event",
				EventUID:     result.EventUID,
				SessionID:    result.SessionID,
				EventKind:    result.EventKind,
				Snippet:      dashboardSearchSnippet(result),
				ToolName:     result.ToolName,
				Provider:     result.Provider,
				Model:        result.Model,
				Score:        result.Score,
				Timestamp:    result.Timestamp,
				RelativeTime: views.RelativeTime(result.Timestamp),
				SessionTitle: meta.title,
				WorkingDir:   meta.workingDir,
			})
		}
	}
	hasMore := len(items) > req.Limit
	if req.EventKind == "session" || (req.Query != "" && req.EventKind == "") {
		var eventSessionIDs []string
		if req.EventKind == "session" {
			eventSessionIDs, err = a.completedSessionContentSearchSessionIDs(r.Context(), req.Query, req.Range, req.SessionID)
			if err != nil {
				a.internalError(w, "search failed", err)
				return
			}
		}
		sessionItems, sessionHasMore := a.dashboardSearchSessionMetadataResults(r.Context(), req.Query, req.Range, req.SessionID, req.SortBy, seenSessions, eventSessionIDs, req.Limit+1)
		if sessionHasMore {
			hasMore = true
		}
		items = append(items, sessionItems...)
	}
	dashboardSortSearchItems(items, req.SortBy)
	if len(items) > req.Limit {
		hasMore = true
		items = items[:req.Limit]
	}

	a.jsonResponse(w, APIDashboardSearchResponse{
		State:     "ready",
		Query:     req.Query,
		Range:     req.Range,
		EventKind: req.EventKind,
		SessionID: req.SessionID,
		Sort:      req.SortBy,
		Limit:     req.Limit,
		HasMore:   hasMore,
		Items:     items,
	})
}

func dashboardSearchEventKinds(eventKind string) []string {
	switch eventKind {
	case models.EventKindError:
		return []string{models.EventKindError, models.EventKindToolError}
	case "", "event", "session":
		return nil
	default:
		return []string{eventKind}
	}
}

func dashboardSearchSnippet(result search.SearchResult) string {
	snippet := result.TextPreview
	if result.EventKind == models.EventKindToolCall && result.ToolName != "" {
		raw := strings.TrimPrefix(snippet, result.ToolName+": ")
		if raw == result.ToolName || raw == "" {
			return ""
		}
		if formatted := formatToolCallSnippet(result.ToolName, raw); formatted != "" {
			return formatted
		}
	}
	return snippet
}

func (a *APIHandlers) dashboardSearchSessionMetadataResults(ctx context.Context, query, rangeVal, sessionIDPrefix, sortBy string, seenSessions map[string]struct{}, eventSessionIDs []string, limit int) ([]APIDashboardSearchResult, bool) {
	if a.db == nil || limit <= 0 {
		return nil, false
	}
	fetchLimit := limit + len(seenSessions) + 1
	sortKey, sortAsc := dashboardSearchMetadataSort(sortBy)
	sessions, storeHasMore := queryCompletedSessionsFiltered(ctx, a.db, parseRange(rangeVal), 0, fetchLimit, query, eventSessionIDs, sortKey, sortAsc, sessionIDPrefix)
	items := make([]APIDashboardSearchResult, 0, min(limit, len(sessions)))
	for _, session := range sessions {
		if _, ok := seenSessions[session.ID]; ok {
			continue
		}
		items = append(items, dashboardSearchSessionResult(session))
		if len(items) > limit {
			return items[:limit], true
		}
	}
	return items, storeHasMore
}

func dashboardSearchMetadataSort(sortBy string) (string, bool) {
	switch sortBy {
	case "oldest":
		return "ended", true
	default:
		return "ended", false
	}
}

func dashboardSortSearchItems(items []APIDashboardSearchResult, sortBy string) {
	switch sortBy {
	case "newest":
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Timestamp.After(items[j].Timestamp)
		})
	case "oldest":
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Timestamp.Before(items[j].Timestamp)
		})
	}
}

func dashboardSearchSessionResult(session views.SessionSummary) APIDashboardSearchResult {
	fields := []string{views.SessionTitle(session, false), session.WorkingDir, session.Provider, session.ActiveModel}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			parts = append(parts, field)
		}
	}
	snippet := "Session metadata"
	if len(parts) > 0 {
		snippet += ": " + strings.Join(parts, " | ")
	}
	return APIDashboardSearchResult{
		ResultType:   "session",
		SessionID:    session.ID,
		EventKind:    "session",
		Snippet:      snippet,
		Provider:     session.Provider,
		Model:        session.ActiveModel,
		Timestamp:    session.EndedAt,
		RelativeTime: views.RelativeTime(session.EndedAt),
		SessionTitle: views.SessionTitle(session, false),
		WorkingDir:   session.WorkingDir,
	}
}

type dashboardSearchSessionInfo struct {
	title      string
	workingDir string
}

func (a *APIHandlers) dashboardSearchSessionMeta(ctx context.Context, ids []string) map[string]dashboardSearchSessionInfo {
	meta := make(map[string]dashboardSearchSessionInfo, len(ids))
	if a.db == nil || len(ids) == 0 {
		return meta
	}
	args := make([]any, len(ids))
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		placeholders[i] = "?"
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), started_at, COALESCE(working_dir, '')
		 FROM session_projection FINAL
		 WHERE session_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		a.log().Warn("dashboard search session metadata query failed", "error", err)
		return meta
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID, source, workingDir string
		var startedAt time.Time
		if err := rows.Scan(&sessionID, &source, &startedAt, &workingDir); err != nil {
			a.logSkippedRow("dashboard search session metadata", err)
			continue
		}
		summary := views.SessionSummary{
			ID:         sessionID,
			Actor:      source,
			StartedAt:  startedAt,
			WorkingDir: workingDir,
		}
		meta[sessionID] = dashboardSearchSessionInfo{
			title:      views.SessionTitle(summary, false),
			workingDir: workingDir,
		}
	}
	if err := rows.Err(); err != nil {
		a.log().Warn("dashboard search session metadata rows failed", "error", err)
	}
	return meta
}

// GetSessionSubagents returns child sessions for a parent session as JSON.
func (a *APIHandlers) GetSessionSubagents(w http.ResponseWriter, r *http.Request) {
	parentID := chi.URLParam(r, "id")
	sessions := QueryChildSessions(r.Context(), a.db, parentID)
	items := make([]APISessionSummary, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, apiSessionSummaryFromView(session))
	}
	a.jsonResponse(w, items)
}

// GetActivity returns recent activity items as JSON for client-side rendering.
func (a *APIHandlers) GetActivity(w http.ResponseWriter, r *http.Request) {
	req := parseActivityAPIRequest(r.URL.Query())
	items := QueryRecentActivityFilteredByKind(r.Context(), a.db, req.Since, req.EventKinds)
	result := make([]APIActivityItem, 0, len(items))
	for _, item := range items {
		result = append(result, APIActivityItem{
			ID:           item.ID,
			Type:         item.Type,
			Summary:      item.Summary,
			SessionID:    item.SessionID,
			Provider:     item.Provider,
			Timestamp:    item.Timestamp,
			RelativeTime: views.RelativeTime(item.Timestamp),
		})
	}
	a.jsonResponse(w, result)
}

// GetDashboardCharts returns the dashboard chart payloads as JSON.
func (a *APIHandlers) GetDashboardCharts(w http.ResponseWriter, r *http.Request) {
	req := parseDashboardChartsAPIRequest(r.URL.Query())
	tokenCumulative, modelActivity := QueryDashboardModelAnalytics(r.Context(), a.db, parseRange(req.Range), req.Range)

	a.jsonResponse(w, APIDashboardCharts{
		Range:           req.Range,
		TokenCumulative: tokenCumulative,
		ModelActivity:   modelActivity,
	})
}

// GetSessionDetail returns detailed info for a single session.
func (a *APIHandlers) GetSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	data, err := QuerySessionDetail(r.Context(), a.db, id)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			a.internalError(w, "failed to query session detail", err)
			return
		}
		a.jsonError(w, "session not found", http.StatusNotFound)
		return
	}
	a.jsonResponse(w, apiSessionDetailFromView(data))
}

// GetSessionEvents returns bounded event detail for a session.
func (a *APIHandlers) GetSessionEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	req, err := parseSessionEventsAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}

	rows, err := a.db.QueryContext(r.Context(),
		`WITH session_events AS (
			SELECT event_uid, session_id, event_kind, payload_type, actor_role,
			       timestamp, text_preview, tool_name, tool_use_id, model,
			       tokens, duration_ms
			FROM (
				SELECT event_uid,
				       argMax(ae.session_id, captured_at) AS session_id,
				       argMax(event_kind, captured_at) AS event_kind,
				       argMax(payload_type, captured_at) AS payload_type,
				       argMax(actor_role, captured_at) AS actor_role,
				       argMax(timestamp, captured_at) AS timestamp,
				       argMax(text_preview, captured_at) AS text_preview,
				       argMax(tool_name, captured_at) AS tool_name,
				       argMax(tool_use_id, captured_at) AS tool_use_id,
				       argMax(model, captured_at) AS model,
				       argMax(input_tokens, captured_at) + argMax(output_tokens, captured_at) AS tokens,
				       argMax(duration_ms, captured_at) AS duration_ms
				FROM activity_events AS ae
				WHERE ae.session_id = ?
				GROUP BY event_uid
			)
			ORDER BY timestamp, event_uid
			LIMIT ? OFFSET ?
		 ),
		 payload_previews AS (
			SELECT event_uid,
			       argMax(input_preview, captured_at) AS input_preview,
			       argMax(output_preview, captured_at) AS output_preview
			FROM tool_payloads
			WHERE event_uid IN (SELECT event_uid FROM session_events)
			GROUP BY event_uid
		 )
		 SELECT e.event_uid, e.session_id, e.event_kind, e.payload_type, e.actor_role,
		        e.timestamp, e.text_preview, e.tool_name, e.tool_use_id, e.model,
		        e.tokens, e.duration_ms,
		        COALESCE(p.input_preview, ''), COALESCE(p.output_preview, '')
		 FROM session_events e
		 LEFT JOIN payload_previews p ON e.event_uid = p.event_uid
		 ORDER BY e.timestamp, e.event_uid`, id, req.Limit, req.Offset)
	if err != nil {
		a.internalError(w, "failed to query session events", err)
		return
	}
	defer rows.Close()

	events := make([]APISessionEvent, 0)
	for rows.Next() {
		var e APISessionEvent
		if err := rows.Scan(&e.EventUID, &e.SessionID, &e.EventKind, &e.PayloadType, &e.ActorRole,
			&e.Timestamp, &e.TextPreview, &e.ToolName, &e.ToolUseID, &e.Model, &e.Tokens, &e.DurationMs,
			&e.InputPreview, &e.OutputPreview); err != nil {
			a.logSkippedRow("session events", err)
			continue
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		a.internalError(w, "failed to query session events", err)
		return
	}
	a.jsonResponse(w, events)
}

// GetEvent returns bounded detail for one event.
func (a *APIHandlers) GetEvent(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "event_id")
	var e APISessionEvent
	err := a.db.QueryRowContext(r.Context(),
		`WITH latest_event AS (
			SELECT event_uid,
			       argMax(session_id, captured_at) AS session_id,
			       argMax(event_kind, captured_at) AS event_kind,
			       argMax(payload_type, captured_at) AS payload_type,
			       argMax(actor_role, captured_at) AS actor_role,
			       argMax(timestamp, captured_at) AS timestamp,
			       argMax(text_preview, captured_at) AS text_preview,
			       argMax(tool_name, captured_at) AS tool_name,
			       argMax(tool_use_id, captured_at) AS tool_use_id,
			       argMax(model, captured_at) AS model,
			       argMax(input_tokens, captured_at) + argMax(output_tokens, captured_at) AS tokens,
			       argMax(duration_ms, captured_at) AS duration_ms
			FROM activity_events
			WHERE event_uid = ?
			GROUP BY event_uid
		 ),
		 payload_previews AS (
			SELECT event_uid,
			       argMax(input_preview, captured_at) AS input_preview,
			       argMax(output_preview, captured_at) AS output_preview
			FROM tool_payloads
			WHERE event_uid = ?
			GROUP BY event_uid
		 )
		 SELECT e.event_uid, e.session_id, e.event_kind, e.payload_type, e.actor_role,
		        e.timestamp, e.text_preview, e.tool_name, e.tool_use_id, e.model,
		        e.tokens, e.duration_ms,
		        COALESCE(p.input_preview, ''), COALESCE(p.output_preview, '')
		 FROM latest_event e
		 LEFT JOIN payload_previews p ON e.event_uid = p.event_uid
		 LIMIT 1`, eventID, eventID).Scan(&e.EventUID, &e.SessionID, &e.EventKind, &e.PayloadType, &e.ActorRole,
		&e.Timestamp, &e.TextPreview, &e.ToolName, &e.ToolUseID, &e.Model, &e.Tokens, &e.DurationMs,
		&e.InputPreview, &e.OutputPreview)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			a.internalError(w, "failed to query event", err)
			return
		}
		a.jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	a.jsonResponse(w, e)
}

// GetToolPayload returns large tool input/output lazily.
func (a *APIHandlers) GetToolPayload(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "event_id")
	var p APIToolPayload
	err := a.db.QueryRowContext(r.Context(),
		`SELECT event_uid,
		        argMax(tool_name, captured_at),
		        argMax(tool_phase, captured_at),
		        argMax(input_json, captured_at),
		        argMax(output_json, captured_at),
		        argMax(input_preview, captured_at),
		        argMax(output_preview, captured_at)
		 FROM tool_payloads
		 WHERE event_uid = ?
		 GROUP BY event_uid`, eventID).Scan(&p.EventUID, &p.ToolName, &p.ToolPhase, &p.InputJSON, &p.OutputJSON, &p.InputPreview, &p.OutputPreview)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			a.internalError(w, "failed to query tool payload", err)
			return
		}
		a.jsonError(w, "payload not found", http.StatusNotFound)
		return
	}
	a.jsonResponse(w, p)
}

// SearchEvents performs keyword search.
func (a *APIHandlers) SearchEvents(w http.ResponseWriter, r *http.Request) {
	req, err := parseSearchEventsAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}
	if a.searcher == nil {
		a.jsonError(w, "search unavailable", http.StatusServiceUnavailable)
		return
	}

	results, err := a.searcher.Search(r.Context(), search.SearchQuery{Query: req.Query, Limit: req.Limit})
	if err != nil {
		a.internalError(w, "search failed", err)
		return
	}
	a.jsonResponse(w, results)
}

// GetTokensPerMinute returns time-series token data with breakdown.
func (a *APIHandlers) GetTokensPerMinute(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT minute, total_input, total_output, total_cache_read, tokens_total, call_count FROM (
			SELECT minute,
			       sum(input_tokens) AS total_input,
			       sum(output_tokens) AS total_output,
			       sum(cache_read_tokens) AS total_cache_read,
			       sum(total_tokens) AS tokens_total,
			       sum(call_count) AS call_count
			FROM `+analyticsProjectionSQL+`
			GROUP BY minute
			ORDER BY minute DESC
			LIMIT 60
		 ) ORDER BY minute ASC`)
	if err != nil {
		a.internalError(w, "failed to query token data", err)
		return
	}
	defer rows.Close()

	points := make([]APITokensPerMinute, 0, 60)
	for rows.Next() {
		var p APITokensPerMinute
		var minute time.Time
		if err := rows.Scan(&minute, &p.InputTokens, &p.OutputTokens, &p.CacheReadTokens, &p.TotalTokens, &p.CallCount); err != nil {
			a.logSkippedRow("tokens per minute", err)
			continue
		}
		p.Minute = minute.UTC().Format(time.RFC3339)
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		a.internalError(w, "failed to query token data", err)
		return
	}
	a.jsonResponse(w, points)
}

// GetToolStats returns tool usage statistics.
func (a *APIHandlers) GetToolStats(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT tool_name,
		        sum(tool_call_count) AS calls,
		        sum(tool_result_count) AS results,
		        sum(event_count) AS total,
		        if(sum(tool_call_count) > 0, sumIf(duration_ms_sum, event_kind = 'tool_call') / sum(tool_call_count), 0) AS avg_duration_ms
		 FROM `+analyticsProjectionSQL+`
		 WHERE tool_name != ''
		 GROUP BY tool_name
		 ORDER BY total DESC`)
	if err != nil {
		a.internalError(w, "failed to query tool stats", err)
		return
	}
	defer rows.Close()

	var stats []APIToolStats
	for rows.Next() {
		var s APIToolStats
		if err := rows.Scan(&s.ToolName, &s.Calls, &s.Results, &s.Total, &s.AvgDurationMs); err != nil {
			a.logSkippedRow("tool stats", err)
			continue
		}
		s.IsMCP = models.IsMCPTool(s.ToolName)
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		a.internalError(w, "failed to query tool stats", err)
		return
	}
	a.jsonResponse(w, stats)
}

// GetTokensByModel returns token usage broken down by model.
func (a *APIHandlers) GetTokensByModel(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(),
		`SELECT model,
		        sum(input_tokens) AS total_input,
		        sum(output_tokens) AS total_output,
		        sum(cache_read_tokens) AS total_cache_read,
		        sum(cache_create_tokens) AS total_cache_create,
		        sum(total_tokens) AS tokens_total,
		        sum(call_count) AS call_count
		 FROM `+analyticsProjectionSQL+`
		 WHERE model != '' AND model != '<synthetic>'
		 GROUP BY model
		 ORDER BY tokens_total DESC`)
	if err != nil {
		a.internalError(w, "failed to query model tokens", err)
		return
	}
	defer rows.Close()

	var items []APITokensByModel
	for rows.Next() {
		var m APITokensByModel
		if err := rows.Scan(&m.Model, &m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheCreateTokens, &m.TotalTokens, &m.CallCount); err != nil {
			a.logSkippedRow("tokens by model", err)
			continue
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		a.internalError(w, "failed to query model tokens", err)
		return
	}
	a.jsonResponse(w, items)
}

func apiSessionSummaryFromView(s views.SessionSummary) APISessionSummary {
	children := make([]APISessionSummary, 0, len(s.ChildSessions))
	for _, child := range s.ChildSessions {
		children = append(children, apiSessionSummaryFromView(child))
	}
	return APISessionSummary{
		ID:                s.ID,
		Title:             views.SessionTitle(s, false),
		Source:            s.Actor,
		Provider:          s.Provider,
		Status:            s.Status,
		StartedAt:         s.StartedAt,
		EndedAt:           s.EndedAt,
		Duration:          s.Duration,
		TurnCount:         s.TurnCount,
		TotalTokens:       s.TotalTokens,
		InputTokens:       s.InputTokens,
		OutputTokens:      s.OutputTokens,
		CacheReadTokens:   s.CacheReadTokens,
		CacheCreateTokens: s.CacheCreateTokens,
		ToolCallCount:     s.ToolCallCount,
		MCPCallCount:      s.MCPCallCount,
		ErrorCount:        s.ErrorCount,
		LastModel:         s.ActiveModel,
		WorkingDir:        s.WorkingDir,
		ParentSessionID:   s.ParentSessionID,
		HasSessionEnd:     s.HasSessionEnd,
		SubagentCount:     s.SubagentCount,
		ChildSessions:     children,
	}
}

func apiSessionDetailFromView(data views.SessionDetailData) APISessionDetail {
	return APISessionDetail{Session: apiSessionSummaryFromView(data.Session)}
}
