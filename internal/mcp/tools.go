package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
)

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "search",
			"description": "Search across all monitored AI agent conversations using BM25 full-text search. Returns ranked results with text previews.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":        map[string]any{"type": "string", "description": "Search query text"},
					"limit":        map[string]any{"type": "integer", "description": "Max results (default 25)"},
					"session_id":   map[string]any{"type": "string", "description": "Filter to a specific session"},
					"event_kinds":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Filter by event kinds"},
					"exclude_self": map[string]any{"type": "boolean", "description": "Exclude beacon's own events (default true)"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "open",
			"description": "Retrieve a specific event with surrounding context from the same session. Shows events before and after the target.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"event_uid": map[string]any{"type": "string", "description": "Event UID to open"},
					"before":    map[string]any{"type": "integer", "description": "Number of events before target (default 3)"},
					"after":     map[string]any{"type": "integer", "description": "Number of events after target (default 3)"},
				},
				"required": []string{"event_uid"},
			},
		},
		{
			"name":        "list_sessions",
			"description": "List recent AI agent sessions with summary statistics.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "Max sessions (default 20)"},
					"since": map[string]any{"type": "string", "description": "Only sessions after this ISO8601 timestamp"},
				},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case "search":
		return s.toolSearch(ctx, args)
	case "open":
		return s.toolOpen(ctx, args)
	case "list_sessions":
		return s.toolListSessions(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) toolSearch(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Query       string   `json:"query"`
		Limit       int      `json:"limit"`
		SessionID   string   `json:"session_id"`
		EventKinds  []string `json:"event_kinds"`
		ExcludeSelf *bool    `json:"exclude_self"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.Limit <= 0 {
		params.Limit = 25
	}

	excludeSelf := true
	if params.ExcludeSelf != nil {
		excludeSelf = *params.ExcludeSelf
	}

	results, err := s.searcher.Search(ctx, search.SearchQuery{
		Query:          params.Query,
		Limit:          params.Limit,
		SessionID:      params.SessionID,
		EventKinds:     params.EventKinds,
		ExcludeMCPSelf: excludeSelf,
	})
	if err != nil {
		return "", err
	}

	return FormatSearchResults(results), nil
}

func (s *Server) toolOpen(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		EventUID string `json:"event_uid"`
		Before   int    `json:"before"`
		After    int    `json:"after"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if params.EventUID == "" {
		return "", fmt.Errorf("event_uid is required")
	}
	if params.Before <= 0 {
		params.Before = 3
	}
	if params.After <= 0 {
		params.After = 3
	}

	// Use a window query to fetch only the target event and its surrounding context,
	// avoiding a full session scan for sessions with thousands of events.
	rows, err := s.db.QueryContext(ctx,
		`WITH target AS (
		    SELECT session_id, timestamp, event_uid
		    FROM events WHERE event_uid = $1
		 ),
		 numbered AS (
		    SELECT e.event_uid, e.event_kind, COALESCE(e.actor_role, '') AS actor_role,
		           COALESCE(e.text_preview, '') AS text_preview,
		           COALESCE(e.tool_name, '') AS tool_name, COALESCE(e.model, '') AS model,
		           e.input_tokens + e.output_tokens AS tokens, e.timestamp,
		           ROW_NUMBER() OVER (ORDER BY e.timestamp, e.event_uid) AS rn
		    FROM events e, target t
		    WHERE e.session_id = t.session_id
		 )
		 SELECT n.event_uid, n.event_kind, n.actor_role, n.text_preview,
		        n.tool_name, n.model, n.tokens, n.timestamp
		 FROM numbered n, (SELECT rn FROM numbered WHERE event_uid = $1) t
		 WHERE n.rn BETWEEN t.rn - $2 AND t.rn + $3
		 ORDER BY n.rn`,
		params.EventUID, params.Before, params.After)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var window []contextEvent
	targetIdx := -1
	for rows.Next() {
		var e contextEvent
		if err := rows.Scan(&e.EventUID, &e.EventKind, &e.ActorRole, &e.TextPreview, &e.ToolName, &e.Model, &e.Tokens, &e.Timestamp); err != nil {
			continue
		}
		if e.EventUID == params.EventUID {
			targetIdx = len(window)
		}
		window = append(window, e)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("reading context events: %w", err)
	}

	if targetIdx == -1 {
		return "", fmt.Errorf("event not found: %s", params.EventUID)
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
			return "", fmt.Errorf("invalid arguments: %w", err)
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
			return "", fmt.Errorf("invalid since timestamp: %w", parseErr)
		}
		rows, err = s.db.QueryContext(ctx,
			`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
			        event_count, turn_count, total_tokens, tool_call_count, mcp_call_count, error_count, COALESCE(last_model, '')
			 FROM v_session_summary
			 WHERE started_at >= $1
			 ORDER BY started_at DESC LIMIT $2`, since, params.Limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
			        event_count, turn_count, total_tokens, tool_call_count, mcp_call_count, error_count, COALESCE(last_model, '')
			 FROM v_session_summary
			 ORDER BY started_at DESC LIMIT $1`, params.Limit)
	}
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var sessions []sessionInfo
	for rows.Next() {
		var s sessionInfo
		if err := rows.Scan(&s.SessionID, &s.SourceName, &s.StartedAt, &s.EndedAt,
			&s.EventCount, &s.TurnCount, &s.TotalTokens, &s.ToolCallCount, &s.MCPCallCount, &s.ErrorCount, &s.LastModel); err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("reading sessions: %w", err)
	}

	return FormatSessionList(sessions), nil
}
