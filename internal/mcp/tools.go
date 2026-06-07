package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/usage"
)

const defaultOpenContextWindow = 3
const defaultListSessionsLimit = 20
const maxListSessionsLimit = 100

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "search_sessions",
			"description": "Search Beacon's precomputed activity index. Returns structured session and event IDs.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":       map[string]any{"type": "string", "description": "Search query text"},
					"limit":       nullableType("integer", "Max results (default 25)"),
					"session_id":  nullableType("string", "Filter to a Beacon session ID"),
					"event_kinds": map[string]any{"type": []string{"array", "null"}, "items": map[string]any{"type": "string"}, "description": "Filter by event kinds"},
				},
				"required":             []string{"query", "limit", "session_id", "event_kinds"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "open",
			"description": "Retrieve a specific Beacon event with surrounding context from the same session. Pass the event_id returned by search_sessions.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"event_id": map[string]any{"type": "string", "description": "Beacon event ID returned by search_sessions, e.g. event:<uid>"},
					"before":   nullableType("integer", "Number of events before target (defaults to server context window)"),
					"after":    nullableType("integer", "Number of events after target (defaults to server context window)"),
				},
				"required":             []string{"event_id", "before", "after"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_sessions",
			"description": "List recent AI agent sessions with summary statistics.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":               nullableType("integer", "Max sessions (default 20, max 100)"),
					"since":               nullableType("string", "Only sessions that started at or after this RFC3339 timestamp"),
					"until":               nullableType("string", "Only sessions that started at or before this RFC3339 timestamp"),
					"source_name":         nullableType("string", "Filter by Beacon capture source, such as codex or claude"),
					"model":               nullableType("string", "Filter by session last model"),
					"provider":            nullableType("string", "Filter by provider"),
					"working_dir":         nullableType("string", "Filter by session working directory"),
					"active_during_since": nullableType("string", "Only sessions active at or after this RFC3339 timestamp"),
					"active_during_until": nullableType("string", "Only sessions active at or before this RFC3339 timestamp"),
					"cursor":              nullableType("string", "Opaque pagination cursor returned by a prior list_sessions call"),
				},
				"required":             []string{"limit", "since", "until", "source_name", "model", "provider", "working_dir", "active_during_since", "active_during_until", "cursor"},
				"additionalProperties": false,
			},
		},
		{
			"name":        "usage_summary",
			"description": "Summarize Beacon token usage with event-level time-window accounting.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"since":       nullableType("string", "Start of the usage window as RFC3339, now, or now-duration such as now-24h (default now-24h)"),
					"until":       nullableType("string", "End of the usage window as RFC3339 or now (default now)"),
					"window_mode": nullableType("string", "Usage window mode (default event_timestamp)"),
					"token_mode":  nullableType("string", "Selected total mode: io_only or include_cache (default io_only)"),
					"source_name": nullableType("string", "Filter by Beacon capture source, such as codex or claude"),
					"model":       nullableType("string", "Filter by model"),
					"provider":    nullableType("string", "Filter by provider"),
					"working_dir": nullableType("string", "Filter by working directory"),
					"group_by":    map[string]any{"type": []string{"array", "null"}, "items": map[string]any{"type": "string"}, "description": "Group results by source_name, provider, model, session_id, or working_dir"},
					"limit":       nullableType("integer", "Max grouped results (default 10, max 100)"),
				},
				"required":             []string{"since", "until", "window_mode", "token_mode", "source_name", "model", "provider", "working_dir", "group_by", "limit"},
				"additionalProperties": false,
			},
		},
	}
}

func nullableType(schemaType, description string) map[string]any {
	return map[string]any{"type": []string{schemaType, "null"}, "description": description}
}

func readOnlyToolAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  true,
		"openWorldHint":   false,
	}
}

func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case "search_sessions":
		return s.toolSearch(ctx, args)
	case "open":
		return s.toolOpen(ctx, args)
	case "list_sessions":
		return s.toolListSessions(ctx, args)
	case "usage_summary":
		return s.toolUsageSummary(ctx, args)
	default:
		return "", userToolError("unknown tool: %s", name)
	}
}

func (s *Server) toolSearch(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query      string   `json:"query"`
		Limit      int      `json:"limit"`
		SessionID  string   `json:"session_id"`
		EventKinds []string `json:"event_kinds"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	if params.Query == "" {
		return "", userToolError("query is required")
	}
	if params.Limit <= 0 {
		params.Limit = 25
	}

	backend, err := s.toolBackend(ctx)
	if err != nil {
		return "", err
	}
	if backend.Searcher == nil {
		return "", internalToolError("search unavailable", fmt.Errorf("search backend is not configured"))
	}
	results, err := backend.Searcher.Search(ctx, search.SearchQuery{
		Query:          params.Query,
		Limit:          params.Limit,
		SessionID:      stripBeaconPrefix(params.SessionID, "session:"),
		EventKinds:     params.EventKinds,
		ExcludeMCPSelf: true,
		SkipQueryLog:   true,
	})
	if err != nil {
		return "", internalToolError("search failed", err)
	}

	return FormatSearchResults(results), nil
}

func (s *Server) toolOpen(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		EventID string `json:"event_id"`
		Before  int    `json:"before"`
		After   int    `json:"after"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	eventUID := stripBeaconPrefix(params.EventID, "event:")
	if eventUID == "" {
		return "", userToolError("event_id is required")
	}
	if params.Before <= 0 {
		params.Before = s.defaultContextWindow()
	}
	if params.After <= 0 {
		params.After = s.defaultContextWindow()
	}

	backend, err := s.toolBackend(ctx)
	if err != nil {
		return "", err
	}
	if backend.DB == nil {
		return "", internalToolError("database unavailable", fmt.Errorf("database backend is not configured"))
	}

	// Use a window query to fetch only the target event and its surrounding context,
	// avoiding a full session scan for sessions with thousands of events.
	rows, err := backend.DB.QueryContext(ctx,
		`WITH target AS (
		    SELECT event_uid,
		           argMax(session_id, captured_at) AS target_session_id,
		           argMax(timestamp, captured_at) AS timestamp
		    FROM activity_events
		    WHERE event_uid = ?
		    GROUP BY event_uid
		 ),
		 session_events AS (
		    SELECT event_uid,
		           argMax(ae.session_id, captured_at) AS event_session_id,
		           argMax(event_kind, captured_at) AS event_kind,
		           argMax(actor_role, captured_at) AS actor_role,
		           argMax(text_preview, captured_at) AS text_preview,
		           argMax(tool_name, captured_at) AS tool_name,
		           argMax(model, captured_at) AS model,
		           argMax(input_tokens, captured_at) + argMax(output_tokens, captured_at) AS tokens,
		           argMax(timestamp, captured_at) AS timestamp
		    FROM activity_events AS ae
		    WHERE ae.session_id IN (SELECT target_session_id FROM target)
		    GROUP BY event_uid
		 ),
		 numbered AS (
		    SELECT e.event_uid, e.event_kind, COALESCE(e.actor_role, '') AS actor_role,
		           COALESCE(e.text_preview, '') AS text_preview,
		           COALESCE(e.tool_name, '') AS tool_name, COALESCE(e.model, '') AS model,
		           e.tokens, e.timestamp,
			   ROW_NUMBER() OVER (ORDER BY e.timestamp, e.event_uid) AS rn
		    FROM session_events e, target t
		    WHERE e.event_session_id = t.target_session_id
		 )
		 SELECT n.event_uid, n.event_kind, n.actor_role, n.text_preview,
		        n.tool_name, n.model, n.tokens, n.timestamp
		 FROM numbered n, (SELECT rn FROM numbered WHERE event_uid = ?) t
		 WHERE n.rn BETWEEN t.rn - ? AND t.rn + ?
		 ORDER BY n.rn`,
		eventUID, eventUID, params.Before, params.After)
	if err != nil {
		return "", internalToolError("failed to open event context", err)
	}
	defer rows.Close()

	var window []contextEvent
	targetIdx := -1
	for rows.Next() {
		var e contextEvent
		if err := rows.Scan(&e.EventUID, &e.EventKind, &e.ActorRole, &e.TextPreview, &e.ToolName, &e.Model, &e.Tokens, &e.Timestamp); err != nil {
			return "", internalToolError("failed to open event context", fmt.Errorf("scan context event: %w", err))
		}
		if e.EventUID == eventUID {
			targetIdx = len(window)
		}
		window = append(window, e)
	}
	if err := rows.Err(); err != nil {
		return "", internalToolError("failed to open event context", fmt.Errorf("reading context events: %w", err))
	}

	if targetIdx == -1 {
		return "", userToolError("event not found: %s", eventUID)
	}

	return FormatOpenContext(window, targetIdx), nil
}

func (s *Server) toolListSessions(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Limit             int    `json:"limit"`
		Since             string `json:"since"`
		Until             string `json:"until"`
		SourceName        string `json:"source_name"`
		Model             string `json:"model"`
		Provider          string `json:"provider"`
		WorkingDir        string `json:"working_dir"`
		ActiveDuringSince string `json:"active_during_since"`
		ActiveDuringUntil string `json:"active_during_until"`
		Cursor            string `json:"cursor"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return "", userToolError("invalid arguments")
		}
	}
	if params.Limit <= 0 {
		params.Limit = defaultListSessionsLimit
	}
	if params.Limit > maxListSessionsLimit {
		params.Limit = maxListSessionsLimit
	}

	backend, err := s.toolBackend(ctx)
	if err != nil {
		return "", err
	}
	if backend.DB == nil {
		return "", internalToolError("database unavailable", fmt.Errorf("database backend is not configured"))
	}

	where, filterArgs, offset, err := listSessionsFilterSQL(listSessionsParams{
		Since:             params.Since,
		Until:             params.Until,
		SourceName:        params.SourceName,
		Model:             params.Model,
		Provider:          params.Provider,
		WorkingDir:        params.WorkingDir,
		ActiveDuringSince: params.ActiveDuringSince,
		ActiveDuringUntil: params.ActiveDuringUntil,
		Cursor:            params.Cursor,
	})
	if err != nil {
		return "", err
	}

	var totalMatching int64
	if err := backend.DB.QueryRowContext(ctx, `SELECT count() FROM `+mcpSessionProjectionSQL+` WHERE `+where, filterArgs...).Scan(&totalMatching); err != nil {
		return "", internalToolError("failed to list sessions", fmt.Errorf("count sessions: %w", err))
	}

	queryArgs := append(append([]any{}, filterArgs...), params.Limit, offset)
	rows, err := backend.DB.QueryContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), COALESCE(provider, ''), started_at, ended_at,
		        event_count, turn_count, total_tokens, tool_call_count, mcp_call_count, error_count,
		        COALESCE(last_model, ''), COALESCE(working_dir, '')
		 FROM `+mcpSessionProjectionSQL+`
		 WHERE `+where+`
		 ORDER BY started_at DESC, session_id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return "", internalToolError("failed to list sessions", err)
	}
	defer rows.Close()

	var sessions []sessionInfo
	for rows.Next() {
		var s sessionInfo
		if err := rows.Scan(&s.SessionID, &s.SourceName, &s.Provider, &s.StartedAt, &s.EndedAt,
			&s.EventCount, &s.TurnCount, &s.TotalTokens, &s.ToolCallCount, &s.MCPCallCount, &s.ErrorCount, &s.LastModel, &s.WorkingDir); err != nil {
			return "", internalToolError("failed to list sessions", fmt.Errorf("scan session: %w", err))
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return "", internalToolError("failed to list sessions", fmt.Errorf("reading sessions: %w", err))
	}

	nextOffset := offset + len(sessions)
	resultComplete := int64(nextOffset) >= totalMatching
	nextCursor := ""
	if !resultComplete {
		nextCursor = "offset:" + strconv.Itoa(nextOffset)
	}
	return FormatSessionList(sessions, sessionListMetadata{
		ResultCount:        len(sessions),
		TotalMatchingCount: totalMatching,
		Limit:              params.Limit,
		Cursor:             strings.TrimSpace(params.Cursor),
		ResultComplete:     resultComplete,
		NextCursor:         nextCursor,
	}), nil
}

func (s *Server) toolUsageSummary(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Since      string   `json:"since"`
		Until      string   `json:"until"`
		WindowMode string   `json:"window_mode"`
		TokenMode  string   `json:"token_mode"`
		SourceName string   `json:"source_name"`
		Model      string   `json:"model"`
		Provider   string   `json:"provider"`
		WorkingDir string   `json:"working_dir"`
		GroupBy    []string `json:"group_by"`
		Limit      int      `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return "", userToolError("invalid arguments")
		}
	}

	backend, err := s.toolBackend(ctx)
	if err != nil {
		return "", err
	}
	if backend.DB == nil {
		return "", internalToolError("database unavailable", fmt.Errorf("database backend is not configured"))
	}

	result, err := usage.Summarize(ctx, backend.DB, usage.Request{
		Since:      params.Since,
		Until:      params.Until,
		WindowMode: params.WindowMode,
		TokenMode:  params.TokenMode,
		SourceName: params.SourceName,
		Model:      params.Model,
		Provider:   params.Provider,
		WorkingDir: params.WorkingDir,
		GroupBy:    params.GroupBy,
		Limit:      params.Limit,
	}, time.Now())
	if err != nil {
		if usage.IsUserError(err) {
			return "", userToolError("%s", err.Error())
		}
		return "", internalToolError("failed to summarize usage", err)
	}
	return FormatUsageSummary(result), nil
}

var mcpSessionProjectionSQL = mcpSessionProjectionSubquery("")

func mcpSessionProjectionSubquery(where string) string {
	if where != "" {
		where = "WHERE " + where
	}
	return `(SELECT
		sp.session_id AS session_id,
		argMax(sp.source_name, sp.updated_at) AS source_name,
		argMax(sp.provider, sp.updated_at) AS provider,
		argMax(sp.started_at, sp.updated_at) AS started_at,
		argMax(sp.ended_at, sp.updated_at) AS ended_at,
		argMax(sp.event_count, sp.updated_at) AS event_count,
		argMax(sp.turn_count, sp.updated_at) AS turn_count,
		argMax(sp.total_tokens, sp.updated_at) AS total_tokens,
		argMax(sp.tool_call_count, sp.updated_at) AS tool_call_count,
		argMax(sp.mcp_call_count, sp.updated_at) AS mcp_call_count,
		argMax(sp.error_count, sp.updated_at) AS error_count,
		argMax(sp.last_model, sp.updated_at) AS last_model,
		argMax(sp.working_dir, sp.updated_at) AS working_dir
	FROM session_projection AS sp ` + where + `
	GROUP BY sp.session_id)`
}

type listSessionsParams struct {
	Since             string
	Until             string
	SourceName        string
	Model             string
	Provider          string
	WorkingDir        string
	ActiveDuringSince string
	ActiveDuringUntil string
	Cursor            string
}

func listSessionsFilterSQL(params listSessionsParams) (string, []any, int, error) {
	clauses := []string{"1 = 1"}
	args := []any{}

	addTimeFilter := func(raw, name, clause string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return userToolError("invalid %s timestamp", name)
		}
		clauses = append(clauses, clause)
		args = append(args, t)
		return nil
	}

	if err := addTimeFilter(params.Since, "since", "started_at >= ?"); err != nil {
		return "", nil, 0, err
	}
	if err := addTimeFilter(params.Until, "until", "started_at <= ?"); err != nil {
		return "", nil, 0, err
	}
	if err := addTimeFilter(params.ActiveDuringSince, "active_during_since", "ended_at >= ?"); err != nil {
		return "", nil, 0, err
	}
	if err := addTimeFilter(params.ActiveDuringUntil, "active_during_until", "started_at <= ?"); err != nil {
		return "", nil, 0, err
	}

	if source := strings.TrimSpace(params.SourceName); source != "" {
		clauses = append(clauses, "source_name = ?")
		args = append(args, source)
	}
	if model := strings.TrimSpace(params.Model); model != "" {
		clauses = append(clauses, "last_model = ?")
		args = append(args, model)
	}
	if provider := strings.TrimSpace(params.Provider); provider != "" {
		clauses = append(clauses, "provider = ?")
		args = append(args, provider)
	}
	if workingDir := strings.TrimSpace(params.WorkingDir); workingDir != "" {
		clauses = append(clauses, "working_dir = ?")
		args = append(args, workingDir)
	}

	offset, err := parseListSessionsCursor(params.Cursor)
	if err != nil {
		return "", nil, 0, err
	}
	return strings.Join(clauses, " AND "), args, offset, nil
}

func parseListSessionsCursor(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	raw = strings.TrimPrefix(raw, "offset:")
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		return 0, userToolError("invalid cursor")
	}
	return offset, nil
}

func stripBeaconPrefix(id, prefix string) string {
	if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return id
}
