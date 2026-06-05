package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
)

const defaultOpenContextWindow = 3

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
					"limit": nullableType("integer", "Max sessions (default 20)"),
					"since": nullableType("string", "Only sessions after this ISO8601 timestamp"),
				},
				"required":             []string{"limit", "since"},
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

	if s.searcher == nil {
		return "", internalToolError("search unavailable", fmt.Errorf("search backend is not configured"))
	}
	results, err := s.searcher.Search(ctx, search.SearchQuery{
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

	// Use a window query to fetch only the target event and its surrounding context,
	// avoiding a full session scan for sessions with thousands of events.
	rows, err := s.db.QueryContext(ctx,
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
		Limit int    `json:"limit"`
		Since string `json:"since"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return "", userToolError("invalid arguments")
		}
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}

	var rows *sql.Rows
	var err error

	if params.Since != "" {
		since, parseErr := time.Parse(time.RFC3339, params.Since)
		if parseErr != nil {
			return "", userToolError("invalid since timestamp")
		}
		rows, err = s.db.QueryContext(ctx,
			`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
			        event_count, turn_count, total_tokens, tool_call_count, mcp_call_count, error_count, COALESCE(last_model, '')
			 FROM `+mcpSessionProjectionSubquery("sp.started_at >= ?")+`
			 ORDER BY started_at DESC LIMIT ?`, since, params.Limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
			        event_count, turn_count, total_tokens, tool_call_count, mcp_call_count, error_count, COALESCE(last_model, '')
			 FROM `+mcpSessionProjectionSQL+`
			 ORDER BY started_at DESC LIMIT ?`, params.Limit)
	}
	if err != nil {
		return "", internalToolError("failed to list sessions", err)
	}
	defer rows.Close()

	var sessions []sessionInfo
	for rows.Next() {
		var s sessionInfo
		if err := rows.Scan(&s.SessionID, &s.SourceName, &s.StartedAt, &s.EndedAt,
			&s.EventCount, &s.TurnCount, &s.TotalTokens, &s.ToolCallCount, &s.MCPCallCount, &s.ErrorCount, &s.LastModel); err != nil {
			return "", internalToolError("failed to list sessions", fmt.Errorf("scan session: %w", err))
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return "", internalToolError("failed to list sessions", fmt.Errorf("reading sessions: %w", err))
	}

	return FormatSessionList(sessions), nil
}

var mcpSessionProjectionSQL = mcpSessionProjectionSubquery("")

func mcpSessionProjectionSubquery(where string) string {
	if where != "" {
		where = "WHERE " + where
	}
	return `(SELECT
		sp.session_id AS session_id,
		argMax(sp.source_name, sp.updated_at) AS source_name,
		argMax(sp.started_at, sp.updated_at) AS started_at,
		argMax(sp.ended_at, sp.updated_at) AS ended_at,
		argMax(sp.event_count, sp.updated_at) AS event_count,
		argMax(sp.turn_count, sp.updated_at) AS turn_count,
		argMax(sp.total_tokens, sp.updated_at) AS total_tokens,
		argMax(sp.tool_call_count, sp.updated_at) AS tool_call_count,
		argMax(sp.mcp_call_count, sp.updated_at) AS mcp_call_count,
		argMax(sp.error_count, sp.updated_at) AS error_count,
		argMax(sp.last_model, sp.updated_at) AS last_model
	FROM session_projection AS sp ` + where + `
	GROUP BY sp.session_id)`
}

func stripBeaconPrefix(id, prefix string) string {
	if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
		return id[len(prefix):]
	}
	return id
}
