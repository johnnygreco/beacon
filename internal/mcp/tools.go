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
const maxOpenContextWindow = 25
const maxSearchSessionsLimit = 100
const defaultListSessionsLimit = 20
const maxListSessionsLimit = 100

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "search_sessions",
			"description": "Search Beacon's unified fleet activity index. Returns structured session/event IDs, fleet provenance, and open_ref values.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":         map[string]any{"type": "string", "description": "Search query text"},
					"limit":         nullableType("integer", "Max results (default 25, max 100)"),
					"session_id":    nullableType("string", "Filter to a Beacon session ID"),
					"event_kinds":   map[string]any{"type": []string{"array", "null"}, "items": map[string]any{"type": "string"}, "description": "Filter by event kinds"},
					"node_id":       nullableType("string", "Filter by collector node ID"),
					"node_ids":      nullableArrayType("string", "Filter by collector node IDs"),
					"collector_id":  nullableType("string", "Filter by collector ID"),
					"collector_ids": nullableArrayType("string", "Filter by collector IDs"),
					"source_id":     nullableType("string", "Filter by source ID"),
					"source_ids":    nullableArrayType("string", "Filter by source IDs"),
					"source_name":   nullableType("string", "Filter by source name"),
					"source_names":  nullableArrayType("string", "Filter by source names"),
					"runtime":       nullableType("string", "Filter by agent runtime"),
					"runtimes":      nullableArrayType("string", "Filter by agent runtimes"),
					"project_key":   nullableType("string", "Filter by project key"),
					"project_keys":  nullableArrayType("string", "Filter by project keys"),
				},
				"required":             append([]string{"query", "limit", "session_id", "event_kinds"}, scopeRequiredProperties()...),
				"additionalProperties": false,
			},
		},
		{
			"name":        "open",
			"description": "Retrieve a Beacon event or returned open_ref with surrounding scoped context from the same session.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": mergeSchemaProperties(map[string]any{
					"event_id":   map[string]any{"type": []string{"string", "null"}, "description": "Beacon event ID returned by search_sessions, e.g. event:<uid>"},
					"session_id": nullableType("string", "Beacon session ID for session/latest anchors"),
					"anchor":     nullableType("string", "Open anchor when event_id is omitted, such as latest"),
					"open_ref":   map[string]any{"type": []string{"object", "null"}, "description": "open_ref object returned by Beacon MCP tools"},
					"before":     nullableType("integer", "Number of events before target (default server context window, max 25)"),
					"after":      nullableType("integer", "Number of events after target (default server context window, max 25)"),
				}, scopeSchemaProperties()),
				"required":             append([]string{"event_id", "session_id", "anchor", "open_ref", "before", "after"}, scopeRequiredProperties()...),
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_agents",
			"description": "List fleet agent/source rollups across enrolled collectors, nodes, runtimes, and projects.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": mergeSchemaProperties(map[string]any{
					"limit": nullableType("integer", "Max grouped agents (default 50, max 200)"),
				}, scopeSchemaProperties()),
				"required":             append([]string{"limit"}, scopeRequiredProperties()...),
				"additionalProperties": false,
			},
		},
		{
			"name":        "list_sessions",
			"description": "List recent AI agent sessions across the unified fleet with summary statistics and open_ref values.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": mergeSchemaProperties(map[string]any{
					"limit":               nullableType("integer", "Max sessions (default 20, max 100)"),
					"since":               nullableType("string", "Only sessions that started at or after this RFC3339 timestamp"),
					"until":               nullableType("string", "Only sessions that started at or before this RFC3339 timestamp"),
					"model":               nullableType("string", "Filter by session last model"),
					"provider":            nullableType("string", "Filter by provider"),
					"working_dir":         nullableType("string", "Filter by session working directory"),
					"active_during_since": nullableType("string", "Only sessions active at or after this RFC3339 timestamp"),
					"active_during_until": nullableType("string", "Only sessions active at or before this RFC3339 timestamp"),
					"cursor":              nullableType("string", "Opaque pagination cursor returned by a prior list_sessions call"),
				}, scopeSchemaProperties()),
				"required":             append([]string{"limit", "since", "until", "model", "provider", "working_dir", "active_during_since", "active_during_until", "cursor"}, scopeRequiredProperties()...),
				"additionalProperties": false,
			},
		},
		{
			"name":        "usage_summary",
			"description": "Summarize Beacon token usage with event-level time-window accounting.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": mergeSchemaProperties(map[string]any{
					"since":       nullableType("string", "Start of the usage window as RFC3339, now, or now-duration such as now-24h (default now-24h)"),
					"until":       nullableType("string", "End of the usage window as RFC3339 or now (default now)"),
					"window_mode": nullableType("string", "Usage window mode (default event_timestamp)"),
					"token_mode":  nullableType("string", "Selected total mode: io_only or include_cache (default io_only)"),
					"model":       nullableType("string", "Filter by model"),
					"provider":    nullableType("string", "Filter by provider"),
					"working_dir": nullableType("string", "Filter by working directory"),
					"group_by":    map[string]any{"type": []string{"array", "null"}, "items": map[string]any{"type": "string"}, "description": "Group results by source_name, provider, model, session_id, working_dir, node_id, collector_id, source_id, runtime, or project_key"},
					"limit":       nullableType("integer", "Max grouped results (default 10, max 100)"),
				}, scopeSchemaProperties()),
				"required":             append([]string{"since", "until", "window_mode", "token_mode", "model", "provider", "working_dir", "group_by", "limit"}, scopeRequiredProperties()...),
				"additionalProperties": false,
			},
		},
	}
}

func nullableType(schemaType, description string) map[string]any {
	return map[string]any{"type": []string{schemaType, "null"}, "description": description}
}

func nullableArrayType(itemType, description string) map[string]any {
	return map[string]any{"type": []string{"array", "null"}, "items": map[string]any{"type": itemType}, "description": description}
}

func scopeSchemaProperties() map[string]any {
	return map[string]any{
		"node_id":       nullableType("string", "Filter by collector node ID"),
		"node_ids":      nullableArrayType("string", "Filter by collector node IDs"),
		"collector_id":  nullableType("string", "Filter by collector ID"),
		"collector_ids": nullableArrayType("string", "Filter by collector IDs"),
		"source_id":     nullableType("string", "Filter by source ID"),
		"source_ids":    nullableArrayType("string", "Filter by source IDs"),
		"source_name":   nullableType("string", "Filter by source name"),
		"source_names":  nullableArrayType("string", "Filter by source names"),
		"runtime":       nullableType("string", "Filter by agent runtime"),
		"runtimes":      nullableArrayType("string", "Filter by agent runtimes"),
		"project_key":   nullableType("string", "Filter by project key"),
		"project_keys":  nullableArrayType("string", "Filter by project keys"),
	}
}

func mergeSchemaProperties(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func scopeRequiredProperties() []string {
	return []string{"node_id", "node_ids", "collector_id", "collector_ids", "source_id", "source_ids", "source_name", "source_names", "runtime", "runtimes", "project_key", "project_keys"}
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
	case "list_agents":
		return s.toolListAgents(ctx, args)
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
		scopeArgs
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
	if params.Limit > maxSearchSessionsLimit {
		params.Limit = maxSearchSessionsLimit
	}

	backend, err := s.toolBackend(ctx)
	if err != nil {
		return "", err
	}
	if backend.Searcher == nil {
		return "", internalToolError("search unavailable", fmt.Errorf("search backend is not configured"))
	}
	scope, metadata := s.effectiveScope(ctx, params.scopeArgs.filters())
	query := search.SearchQuery{
		Query:          params.Query,
		Limit:          params.Limit,
		SessionID:      stripBeaconPrefix(params.SessionID, "session:"),
		EventKinds:     params.EventKinds,
		ExcludeMCPSelf: true,
		SkipQueryLog:   true,
	}
	scope.applyToSearchQuery(&query)
	results, err := backend.Searcher.Search(ctx, query)
	if err != nil {
		return "", internalToolError("search failed", err)
	}

	return FormatSearchResults(results, metadata), nil
}

func (s *Server) toolOpen(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		EventID   string   `json:"event_id"`
		SessionID string   `json:"session_id"`
		Anchor    string   `json:"anchor"`
		OpenRef   *openRef `json:"open_ref"`
		Before    int      `json:"before"`
		After     int      `json:"after"`
		scopeArgs
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	eventUID, sessionID, anchor, refScope, err := resolveOpenTarget(params.EventID, params.SessionID, params.Anchor, params.OpenRef)
	if err != nil {
		return "", err
	}
	before, after, err := normalizeOpenContextWindow(params.Before, params.After, s.defaultContextWindow())
	if err != nil {
		return "", err
	}

	backend, err := s.toolBackend(ctx)
	if err != nil {
		return "", err
	}
	if backend.DB == nil {
		return "", internalToolError("database unavailable", fmt.Errorf("database backend is not configured"))
	}

	requestedScope := intersectScopes(params.scopeArgs.filters(), refScope)
	scope, metadata := s.effectiveScope(ctx, requestedScope)
	targetWhere := "ae.event_uid = ?"
	targetArgs := []any{eventUID}
	if eventUID == "" {
		targetWhere = "ae.session_id = ?"
		targetArgs = []any{sessionID}
	}
	targetScopeClause, targetScopeArgs := scope.eventSQLAndClause("ae", "ae.cwd")
	sessionScopeClause, sessionScopeArgs := scope.eventSQLAndClause("ae", "ae.cwd")
	queryArgs := make([]any, 0, len(targetArgs)+len(targetScopeArgs)+len(sessionScopeArgs)+2)
	queryArgs = append(queryArgs, targetArgs...)
	queryArgs = append(queryArgs, targetScopeArgs...)
	queryArgs = append(queryArgs, sessionScopeArgs...)
	queryArgs = append(queryArgs, before, after)

	// Use a window query to fetch only the target event and its surrounding context,
	// avoiding a full session scan for sessions with thousands of events.
	rows, err := backend.DB.QueryContext(ctx,
		`WITH target AS (
		    SELECT event_uid, target_session_id, timestamp
		    FROM (
			    SELECT event_uid,
			           argMax(ae.session_id, captured_at) AS target_session_id,
			           argMax(ae.timestamp, captured_at) AS timestamp
			    FROM activity_events AS ae
			    WHERE `+targetWhere+targetScopeClause+`
			    GROUP BY event_uid
		    )
		    ORDER BY timestamp DESC, event_uid DESC
		    LIMIT 1
		 ),
		 session_events AS (
		    SELECT event_uid,
		           argMax(ae.session_id, captured_at) AS event_session_id,
		           argMax(ae.node_id, captured_at) AS node_id,
		           argMax(ae.collector_id, captured_at) AS collector_id,
		           argMax(ae.source_id, captured_at) AS source_id,
		           argMax(ae.source_name, captured_at) AS source_name,
		           argMax(ae.runtime, captured_at) AS runtime,
		           argMax(event_kind, captured_at) AS event_kind,
		           argMax(actor_role, captured_at) AS actor_role,
		           argMax(text_preview, captured_at) AS text_preview,
		           argMax(tool_name, captured_at) AS tool_name,
		           argMax(model, captured_at) AS model,
		           argMax(input_tokens, captured_at) + argMax(output_tokens, captured_at) AS tokens,
		           argMax(timestamp, captured_at) AS timestamp,
		           argMax(cwd, captured_at) AS cwd
		    FROM activity_events AS ae
		    WHERE ae.session_id IN (SELECT target_session_id FROM target)`+sessionScopeClause+`
		    GROUP BY event_uid
		 ),
		 numbered AS (
		    SELECT e.event_uid, e.event_session_id, COALESCE(e.node_id, '') AS node_id,
		           COALESCE(e.collector_id, '') AS collector_id,
		           COALESCE(e.source_id, '') AS source_id,
		           COALESCE(e.source_name, '') AS source_name,
		           COALESCE(e.runtime, '') AS runtime,
		           `+projectKeyExpr("e.cwd")+` AS project_key,
		           COALESCE(e.cwd, '') AS project_path,
		           e.event_kind, COALESCE(e.actor_role, '') AS actor_role,
		           COALESCE(e.text_preview, '') AS text_preview,
		           COALESCE(e.tool_name, '') AS tool_name, COALESCE(e.model, '') AS model,
		           e.tokens, e.timestamp,
			   ROW_NUMBER() OVER (ORDER BY e.timestamp, e.event_uid) AS rn
		    FROM session_events e, target t
		    WHERE e.event_session_id = t.target_session_id
		 )
		 SELECT n.event_uid, n.event_session_id, n.node_id, n.collector_id, n.source_id,
		        n.source_name, n.runtime, n.project_key, n.project_path, n.event_kind,
		        n.actor_role, n.text_preview, n.tool_name, n.model, n.tokens, n.timestamp,
		        if(n.event_uid IN (SELECT event_uid FROM target), 1, 0) AS target
		 FROM numbered n, (SELECT rn FROM numbered WHERE event_uid IN (SELECT event_uid FROM target)) t
		 WHERE n.rn BETWEEN t.rn - ? AND t.rn + ?
		 ORDER BY n.rn`,
		queryArgs...)
	if err != nil {
		return "", internalToolError("failed to open event context", err)
	}
	defer rows.Close()

	var window []contextEvent
	targetIdx := -1
	for rows.Next() {
		var e contextEvent
		var target uint8
		if err := rows.Scan(&e.EventUID, &e.SessionID, &e.NodeID, &e.CollectorID, &e.SourceID, &e.SourceName, &e.Runtime, &e.ProjectKey, &e.ProjectPath, &e.EventKind, &e.ActorRole, &e.TextPreview, &e.ToolName, &e.Model, &e.Tokens, &e.Timestamp, &target); err != nil {
			return "", internalToolError("failed to open event context", fmt.Errorf("scan context event: %w", err))
		}
		if target != 0 {
			targetIdx = len(window)
		}
		window = append(window, e)
	}
	if err := rows.Err(); err != nil {
		return "", internalToolError("failed to open event context", fmt.Errorf("reading context events: %w", err))
	}

	if targetIdx == -1 {
		if metadata.AuthScopeApplied || hasScopeFilters(scope) {
			return "", userToolError("forbidden")
		}
		if sessionID != "" {
			return "", userToolError("session anchor not found: %s#%s", sessionID, anchor)
		}
		return "", userToolError("event not found: %s", eventUID)
	}

	return FormatOpenContext(window, targetIdx, metadata), nil
}

func normalizeOpenContextWindow(before, after, defaultWindow int) (int, int, error) {
	defaultWindow = clampOpenContextWindow(defaultWindow)
	if before <= 0 {
		before = defaultWindow
	}
	if after <= 0 {
		after = defaultWindow
	}
	if before > maxOpenContextWindow {
		return 0, 0, userToolError("before must be <= %d", maxOpenContextWindow)
	}
	if after > maxOpenContextWindow {
		return 0, 0, userToolError("after must be <= %d", maxOpenContextWindow)
	}
	return before, after, nil
}

func clampOpenContextWindow(events int) int {
	if events < 0 {
		return defaultOpenContextWindow
	}
	if events > maxOpenContextWindow {
		return maxOpenContextWindow
	}
	return events
}

func (s *Server) toolListAgents(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Limit int `json:"limit"`
		scopeArgs
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return "", userToolError("invalid arguments")
		}
	}
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	backend, err := s.toolBackend(ctx)
	if err != nil {
		return "", err
	}
	if backend.DB == nil {
		return "", internalToolError("database unavailable", fmt.Errorf("database backend is not configured"))
	}

	scope, metadata := s.effectiveScope(ctx, params.scopeArgs.filters())
	sessionSource, sourceArgs := mcpSessionProjectionSource(scope)
	scopeClause, scopeArgs := scope.sqlAndClause("")
	groupSource := `(
		SELECT COALESCE(NULLIF(node_id, ''), 'local') AS node_id,
		       COALESCE(collector_id, '') AS collector_id,
		       COALESCE(source_id, '') AS source_id,
		       COALESCE(source_name, '') AS source_name,
		       COALESCE(runtime, '') AS runtime,
		       COALESCE(project_key, '') AS project_key,
		       COALESCE(project_path, '') AS project_path,
		       count() AS session_count,
		       sum(event_count) AS event_count,
		       sum(total_tokens) AS total_tokens,
		       max(started_at) AS last_started_at,
		       max(ended_at) AS last_ended_at,
		       argMax(session_id, greatest(started_at, ended_at)) AS latest_session_id
		FROM ` + sessionSource + `
		WHERE 1 = 1` + scopeClause + `
		GROUP BY node_id, collector_id, source_id, source_name, runtime, project_key, project_path
	)`
	groupArgs := append(append([]any{}, sourceArgs...), scopeArgs...)
	var totalMatching int64
	if err := backend.DB.QueryRowContext(ctx, `SELECT count() FROM `+groupSource, groupArgs...).Scan(&totalMatching); err != nil {
		return "", internalToolError("failed to list agents", fmt.Errorf("count agents: %w", err))
	}
	queryArgs := append(append([]any{}, groupArgs...), params.Limit)
	rows, err := backend.DB.QueryContext(ctx,
		`SELECT node_id, collector_id, source_id, source_name, runtime, project_key, project_path,
		        session_count, event_count, total_tokens, last_started_at, last_ended_at, latest_session_id
		 FROM `+groupSource+`
		 ORDER BY last_ended_at DESC, last_started_at DESC, node_id, collector_id, source_id, project_key
		 LIMIT ?`,
		queryArgs...)
	if err != nil {
		return "", internalToolError("failed to list agents", err)
	}
	defer rows.Close()

	var agents []agentInfo
	for rows.Next() {
		var a agentInfo
		if err := rows.Scan(&a.NodeID, &a.CollectorID, &a.SourceID, &a.SourceName, &a.Runtime, &a.ProjectKey, &a.ProjectPath, &a.SessionCount, &a.EventCount, &a.TotalTokens, &a.LastStartedAt, &a.LastEndedAt, &a.LatestSessionID); err != nil {
			return "", internalToolError("failed to list agents", fmt.Errorf("scan agent: %w", err))
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return "", internalToolError("failed to list agents", fmt.Errorf("reading agents: %w", err))
	}
	return FormatAgentList(agents, sessionListMetadata{
		ResultCount:        len(agents),
		TotalMatchingCount: totalMatching,
		Limit:              params.Limit,
		ResultComplete:     int64(len(agents)) >= totalMatching,
		NextCursor:         "",
	}, metadata), nil
}

func resolveOpenTarget(eventID, sessionID, anchor string, ref *openRef) (string, string, string, ScopeFilters, error) {
	var refScope ScopeFilters
	if ref != nil {
		if ref.EventID != "" {
			eventID = ref.EventID
		}
		if ref.SessionID != "" {
			sessionID = ref.SessionID
		}
		if ref.Anchor != "" {
			anchor = ref.Anchor
		}
		if ref.Scope != nil {
			refScope = normalizeScopeFilters(*ref.Scope)
		}
	}
	eventUID := stripBeaconPrefix(eventID, "event:")
	sessionID = stripBeaconPrefix(sessionID, "session:")
	anchor = strings.TrimSpace(anchor)
	if eventUID != "" {
		return eventUID, "", "", refScope, nil
	}
	if sessionID == "" {
		return "", "", "", ScopeFilters{}, userToolError("event_id or open_ref is required")
	}
	if anchor == "" {
		anchor = "latest"
	}
	if anchor != "latest" {
		return "", "", "", ScopeFilters{}, userToolError("unsupported open anchor: %s", anchor)
	}
	return "", sessionID, anchor, refScope, nil
}

func (s *Server) toolListSessions(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Limit             int    `json:"limit"`
		Since             string `json:"since"`
		Until             string `json:"until"`
		Model             string `json:"model"`
		Provider          string `json:"provider"`
		WorkingDir        string `json:"working_dir"`
		ActiveDuringSince string `json:"active_during_since"`
		ActiveDuringUntil string `json:"active_during_until"`
		Cursor            string `json:"cursor"`
		scopeArgs
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
	scope, metadata := s.effectiveScope(ctx, params.scopeArgs.filters())
	sessionSource, sourceArgs := mcpSessionProjectionSource(scope)

	where, filterArgs, offset, err := listSessionsFilterSQL(listSessionsParams{
		Since:             params.Since,
		Until:             params.Until,
		Model:             params.Model,
		Provider:          params.Provider,
		WorkingDir:        params.WorkingDir,
		ActiveDuringSince: params.ActiveDuringSince,
		ActiveDuringUntil: params.ActiveDuringUntil,
		Cursor:            params.Cursor,
		Scope:             scope,
	})
	if err != nil {
		return "", err
	}

	var totalMatching int64
	queryFilterArgs := append(append([]any{}, sourceArgs...), filterArgs...)
	if err := backend.DB.QueryRowContext(ctx, `SELECT count() FROM `+sessionSource+` WHERE `+where, queryFilterArgs...).Scan(&totalMatching); err != nil {
		return "", internalToolError("failed to list sessions", fmt.Errorf("count sessions: %w", err))
	}

	queryArgs := append(append([]any{}, queryFilterArgs...), params.Limit, offset)
	rows, err := backend.DB.QueryContext(ctx,
		`SELECT session_id, COALESCE(node_id, ''), COALESCE(collector_id, ''), COALESCE(source_id, ''),
		        COALESCE(source_name, ''), COALESCE(runtime, ''), COALESCE(project_key, ''), COALESCE(project_path, ''),
		        COALESCE(provider, ''), started_at, ended_at,
		        event_count, turn_count, total_tokens, tool_call_count, mcp_call_count, error_count,
		        COALESCE(last_model, ''), COALESCE(working_dir, '')
		 FROM `+sessionSource+`
		 WHERE `+where+`
		 ORDER BY started_at DESC, session_id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return "", internalToolError("failed to list sessions", err)
	}
	defer rows.Close()

	var sessions []sessionInfo
	for rows.Next() {
		var s sessionInfo
		if err := rows.Scan(&s.SessionID, &s.NodeID, &s.CollectorID, &s.SourceID, &s.SourceName, &s.Runtime, &s.ProjectKey, &s.ProjectPath, &s.Provider, &s.StartedAt, &s.EndedAt,
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
	}, metadata), nil
}

func (s *Server) toolUsageSummary(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Since      string   `json:"since"`
		Until      string   `json:"until"`
		WindowMode string   `json:"window_mode"`
		TokenMode  string   `json:"token_mode"`
		Model      string   `json:"model"`
		Provider   string   `json:"provider"`
		WorkingDir string   `json:"working_dir"`
		GroupBy    []string `json:"group_by"`
		Limit      int      `json:"limit"`
		scopeArgs
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
	scope, metadata := s.effectiveScope(ctx, params.scopeArgs.filters())
	if scope.denyAll {
		scope.NodeIDs = []string{scopeImpossibleValue}
		scope.denyAll = false
	}

	result, err := usage.Summarize(ctx, backend.DB, usage.Request{
		Since:        params.Since,
		Until:        params.Until,
		WindowMode:   params.WindowMode,
		TokenMode:    params.TokenMode,
		SourceNames:  scope.SourceNames,
		NodeIDs:      scope.NodeIDs,
		CollectorIDs: scope.CollectorIDs,
		SourceIDs:    scope.SourceIDs,
		Runtimes:     scope.Runtimes,
		ProjectKeys:  scope.ProjectKeys,
		Model:        params.Model,
		Provider:     params.Provider,
		WorkingDir:   params.WorkingDir,
		GroupBy:      params.GroupBy,
		Limit:        params.Limit,
	}, time.Now())
	if err != nil {
		if usage.IsUserError(err) {
			return "", userToolError("%s", err.Error())
		}
		return "", internalToolError("failed to summarize usage", err)
	}
	return FormatUsageSummary(result, metadata), nil
}

var mcpSessionProjectionSQL = mcpSessionProjectionSubquery("")

func mcpSessionProjectionSubquery(where string) string {
	if where != "" {
		where = "WHERE " + where
	}
	return `(SELECT
		sp.session_id AS session_id,
		argMax(sp.node_id, sp.updated_at) AS node_id,
		argMax(sp.collector_id, sp.updated_at) AS collector_id,
		argMax(sp.source_id, sp.updated_at) AS source_id,
		argMax(sp.source_name, sp.updated_at) AS source_name,
		argMax(sp.runtime, sp.updated_at) AS runtime,
		argMax(sp.project_key, sp.updated_at) AS project_key,
		argMax(sp.project_path, sp.updated_at) AS project_path,
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

func mcpSQLWhereClause(where string) string {
	if strings.TrimSpace(where) == "" {
		return ""
	}
	return " WHERE " + where
}

func mcpLatestActivityEventsSubquery(where string) string {
	return `(SELECT event_uid,
	               argMax(session_id, captured_at) AS session_id,
	               argMax(node_id, captured_at) AS node_id,
	               argMax(collector_id, captured_at) AS collector_id,
	               argMax(source_id, captured_at) AS source_id,
	               argMax(source_name, captured_at) AS source_name,
	               argMax(runtime, captured_at) AS runtime,
	               argMax(provider, captured_at) AS provider,
	               argMax(timestamp, captured_at) AS timestamp,
	               argMax(event_kind, captured_at) AS event_kind,
	               argMax(actor_role, captured_at) AS actor_role,
	               argMax(tool_name, captured_at) AS tool_name,
	               argMax(model, captured_at) AS model,
	               argMax(input_tokens, captured_at) AS input_tokens,
	               argMax(output_tokens, captured_at) AS output_tokens,
	               argMax(cwd, captured_at) AS cwd
	        FROM activity_events AS ae` + mcpSQLWhereClause(where) + `
	        GROUP BY event_uid)`
}

func mcpSessionProjectFallbackSubquery(where string) string {
	if strings.TrimSpace(where) == "" {
		where = "ae.session_id != ''"
	}
	return `(SELECT session_id,
		       if(project_count = 1, any_project_key, '') AS project_key,
		       project_count
		FROM (
			SELECT session_id,
			       uniqExactIf(project_key, project_key != '') AS project_count,
			       anyIf(project_key, project_key != '') AS any_project_key
			FROM (
				SELECT session_id,
				       ` + projectKeyExpr("cwd") + ` AS project_key
				FROM (
					SELECT event_uid,
					       argMax(session_id, captured_at) AS session_id,
					       argMax(cwd, captured_at) AS cwd
					FROM activity_events AS ae` + mcpSQLWhereClause(where) + `
					GROUP BY event_uid
				)
				WHERE session_id != ''
			)
			GROUP BY session_id
		))`
}

func mcpSessionProjectionSource(scope ScopeFilters) (string, []any) {
	if len(compactScopeValues(scope.ProjectKeys)) == 0 {
		return mcpSessionProjectionSQL, nil
	}
	scopeClause, scopeArgs := scope.eventAndSessionProjectSQLAndClause("e", "e.cwd", "s")
	eventProjectExpr := projectKeyExpr("e.cwd")
	scopedProjectExpr := "COALESCE(NULLIF(" + eventProjectExpr + ", ''), if(COALESCE(s.project_count, 0) <= 1, NULLIF(s.project_key, ''), ''))"
	return `(SELECT
		e.session_id AS session_id,
		argMaxIf(e.node_id, e.timestamp, e.node_id != '') AS node_id,
		argMaxIf(e.collector_id, e.timestamp, e.collector_id != '') AS collector_id,
		argMaxIf(e.source_id, e.timestamp, e.source_id != '') AS source_id,
		argMaxIf(e.source_name, e.timestamp, e.source_name != '') AS source_name,
		argMaxIf(e.runtime, e.timestamp, e.runtime != '') AS runtime,
		argMaxIf(` + scopedProjectExpr + `, e.timestamp, ` + scopedProjectExpr + ` != '') AS project_key,
		argMaxIf(e.cwd, e.timestamp, e.cwd != '') AS project_path,
		argMaxIf(e.provider, e.timestamp, e.provider != '') AS provider,
		minIf(e.timestamp, e.timestamp > toDateTime64(0, 3, 'UTC')) AS started_at,
		maxIf(e.timestamp, e.timestamp > toDateTime64(0, 3, 'UTC')) AS ended_at,
		count() AS event_count,
		uniqExactIf(e.event_uid, e.event_kind = 'message' AND e.actor_role = 'user') AS turn_count,
		sum(e.input_tokens + e.output_tokens) AS total_tokens,
		countIf(e.event_kind = 'tool_call') AS tool_call_count,
		countIf(e.event_kind = 'tool_call' AND startsWith(e.tool_name, 'mcp__')) AS mcp_call_count,
		countIf(e.event_kind IN ('error', 'tool_error')) AS error_count,
		argMaxIf(e.model, e.timestamp, e.model != '') AS last_model,
		argMaxIf(e.cwd, e.timestamp, e.cwd != '') AS working_dir
	FROM ` + mcpLatestActivityEventsSubquery("ae.session_id != ''") + ` AS e
	LEFT JOIN ` + mcpSessionProjectFallbackSubquery("") + ` AS s ON s.session_id = e.session_id
	WHERE 1 = 1` + scopeClause + `
	GROUP BY e.session_id)`, scopeArgs
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
	Scope             ScopeFilters
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
	if scopeClause, scopeArgs := params.Scope.sqlAndClause(""); scopeClause != "" {
		clauses = append(clauses, strings.TrimPrefix(scopeClause, " AND "))
		args = append(args, scopeArgs...)
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
