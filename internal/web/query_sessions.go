package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

// QueryActiveSessions returns active session summaries only.
// Active sessions are those with recent activity and no definitive end signal.
func QueryActiveSessions(ctx context.Context, db *sql.DB) []views.SessionSummary {
	return queryActiveSessions(ctx, db, 0)
}

func QueryActiveSessionsLimited(ctx context.Context, db *sql.DB, limit int) []views.SessionSummary {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	return queryActiveSessions(ctx, db, limit)
}

func queryActiveSessions(ctx context.Context, db *sql.DB, limit int) []views.SessionSummary {
	now := time.Now()
	// Use Go's time.Now() (UTC-aware) instead of SQL current_timestamp to avoid
	// timezone mismatch — stored timestamps are UTC but current_timestamp is local.
	cutoff := now.Add(-idleThreshold)

	// Fetch active sessions: recent activity AND not explicitly ended.
	query := `SELECT ` + sessionSummaryColumns + `
		 FROM ` + sessionProjectionSQL + `
		 WHERE ended_at >= ?
		   AND COALESCE(has_session_end, 0) = 0
		 ORDER BY ended_at DESC`
	args := []any{cutoff}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	activeRows, err := db.QueryContext(ctx,
		query, args...)
	if err != nil {
		logQueryError("active sessions", err)
		return nil
	}
	defer activeRows.Close()
	var active []views.SessionSummary
	for activeRows.Next() {
		s, err := scanSessionSummary(activeRows, now)
		if err != nil {
			logQueryScanError("active sessions", err)
			continue
		}
		active = append(active, s)
	}
	if err := activeRows.Err(); err != nil {
		logQueryError("active sessions rows", err)
		return nil
	}

	attachActiveSessionContextUsage(ctx, db, active)
	return views.GroupActiveSessions(active)
}

type activeContextSnapshot struct {
	Tokens    int64
	Window    int64
	HasTokens bool
	HasWindow bool
}

func attachActiveSessionContextUsage(ctx context.Context, db *sql.DB, sessions []views.SessionSummary) {
	if len(sessions) == 0 {
		return
	}
	placeholders := make([]string, 0, len(sessions))
	args := make([]any, 0, len(sessions))
	for _, session := range sessions {
		if session.ID == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, session.ID)
	}
	if len(args) == 0 {
		return
	}

	tokenCondition := `(payload_type = 'token_count' OR (input_tokens + output_tokens + cache_read_tokens + cache_create_tokens) > 0)`
	rawContextCondition := `(position(payload_json, '"last_token_usage"') > 0 OR position(payload_json, '"usage"') > 0 OR position(payload_json, '"total_tokens"') > 0 OR position(payload_json, '"model_context_window"') > 0 OR position(payload_json, '"context_window_tokens"') > 0 OR position(payload_json, '"context_tokens"') > 0 OR position(payload_json, '"window_tokens"') > 0 OR position(payload_json, '"used_tokens"') > 0)`
	query := `SELECT session_id,
		       argMaxIf(model, timestamp, ` + tokenCondition + `) AS token_model,
		       argMaxIf(payload_json, timestamp, ` + tokenCondition + `) AS token_payload,
		       argMaxIf(input_tokens, timestamp, ` + tokenCondition + `) AS input_tokens,
		       argMaxIf(output_tokens, timestamp, ` + tokenCondition + `) AS output_tokens,
		       argMaxIf(cache_read_tokens, timestamp, ` + tokenCondition + `) AS cache_read_tokens,
		       argMaxIf(cache_create_tokens, timestamp, ` + tokenCondition + `) AS cache_create_tokens,
		       argMaxIf(payload_json, timestamp, ` + rawContextCondition + `) AS context_payload
		FROM activity_events FINAL
		WHERE session_id IN (` + strings.Join(placeholders, ",") + `)
		  AND (` + tokenCondition + ` OR ` + rawContextCondition + `)
		GROUP BY session_id`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		logQueryError("active context usage", err)
		return
	}
	defer rows.Close()

	bySession := make(map[string]activeContextSnapshot, len(sessions))
	for rows.Next() {
		var sessionID, tokenModel, tokenPayload, windowPayload string
		var inputTokens, outputTokens, cacheReadTokens, cacheCreateTokens int64
		if err := rows.Scan(&sessionID, &tokenModel, &tokenPayload,
			&inputTokens, &outputTokens, &cacheReadTokens, &cacheCreateTokens, &windowPayload); err != nil {
			logQueryScanError("active context usage", err)
			continue
		}
		bySession[sessionID] = activeContextUsageFromLatestEvent(tokenModel, tokenPayload, windowPayload,
			inputTokens, outputTokens, cacheReadTokens, cacheCreateTokens)
	}
	if err := rows.Err(); err != nil {
		logQueryError("active context usage rows", err)
		return
	}

	for i := range sessions {
		snapshot, ok := bySession[sessions[i].ID]
		if !ok {
			continue
		}
		if snapshot.HasTokens {
			sessions[i].ContextTokens = snapshot.Tokens
			sessions[i].ContextEstimate = false
		}
		if snapshot.HasWindow {
			sessions[i].ContextWindowTokens = snapshot.Window
		}
	}
}

func activeContextUsageFromLatestEvent(model, tokenPayload, windowPayload string, inputTokens, outputTokens, cacheReadTokens, cacheCreateTokens int64) activeContextSnapshot {
	snapshot := rawPayloadContextUsage(tokenPayload)
	if !snapshot.HasTokens {
		contextSnapshot := rawPayloadContextUsage(windowPayload)
		if contextSnapshot.HasTokens {
			snapshot.Tokens = contextSnapshot.Tokens
			snapshot.HasTokens = true
		}
		if !snapshot.HasWindow && contextSnapshot.HasWindow {
			snapshot.Window = contextSnapshot.Window
			snapshot.HasWindow = true
		}
	}
	if !snapshot.HasTokens {
		tokens := inputTokens + outputTokens + cacheReadTokens + cacheCreateTokens
		if tokens > 0 {
			snapshot.Tokens = tokens
			snapshot.HasTokens = true
		}
	}
	if !snapshot.HasWindow {
		windowSnapshot := rawPayloadContextUsage(windowPayload)
		if windowSnapshot.HasWindow {
			snapshot.Window = windowSnapshot.Window
			snapshot.HasWindow = true
		}
	}
	if !snapshot.HasWindow {
		if window := views.ContextWindowTokensForModel(model); window > 0 {
			snapshot.Window = window
			snapshot.HasWindow = true
		}
	}
	return snapshot
}

func rawPayloadContextUsage(raw string) activeContextSnapshot {
	var snapshot activeContextSnapshot
	if strings.TrimSpace(raw) == "" {
		return snapshot
	}
	var root map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return snapshot
	}
	payload := jsonObject(root["payload"])
	info := jsonObject(payload["info"])
	usage := jsonObject(info["last_token_usage"])
	if len(usage) == 0 {
		usage = jsonObject(payload["usage"])
	}
	if len(usage) == 0 {
		usage = jsonObject(root["usage"])
	}
	contextPayload := jsonObject(payload["context"])
	if tokens := firstPositiveInt64(jsonNumber(usage["total_tokens"]), jsonNumber(usage["context_tokens"]), jsonNumber(usage["used_tokens"])); tokens > 0 {
		snapshot.Tokens = tokens
		snapshot.HasTokens = true
	} else {
		cacheRead := firstPositiveInt64(jsonNumber(usage["cache_read_input_tokens"]), jsonNumber(usage["cache_read_tokens"]))
		// OpenAI/Codex cached_input_tokens is already included in input_tokens.
		if jsonNumber(usage["cached_input_tokens"]) > 0 {
			cacheRead = 0
		}
		cacheCreate := firstPositiveInt64(jsonNumber(usage["cache_creation_input_tokens"]), jsonNumber(usage["cache_create_tokens"]), jsonNumber(usage["cache_write_input_tokens"]), jsonNumber(usage["cache_write_tokens"]))
		tokens = jsonNumber(usage["input_tokens"]) + jsonNumber(usage["output_tokens"]) + cacheRead + cacheCreate
		if tokens > 0 {
			snapshot.Tokens = tokens
			snapshot.HasTokens = true
		}
	}
	window := firstPositiveInt64(
		jsonNumber(info["model_context_window"]),
		jsonNumber(usage["model_context_window"]),
		jsonNumber(usage["context_window_tokens"]),
		jsonNumber(payload["model_context_window"]),
		jsonNumber(payload["context_window_tokens"]),
		jsonNumber(payload["context_window"]),
		jsonNumber(payload["max_context_tokens"]),
		jsonNumber(contextPayload["model_context_window"]),
		jsonNumber(contextPayload["window_tokens"]),
		jsonNumber(contextPayload["window"]),
		jsonNumber(contextPayload["context_window_tokens"]),
		jsonNumber(root["context_window_tokens"]),
		jsonNumber(root["context_window"]),
		jsonNumber(root["model_context_window"]),
	)
	if window > 0 {
		snapshot.Window = window
		snapshot.HasWindow = true
	}
	if !snapshot.HasTokens {
		tokens := firstPositiveInt64(
			jsonNumber(payload["context_tokens"]),
			jsonNumber(contextPayload["tokens"]),
			jsonNumber(contextPayload["used_tokens"]),
			jsonNumber(root["context_tokens"]),
		)
		if tokens > 0 {
			snapshot.Tokens = tokens
			snapshot.HasTokens = true
		}
	}
	return snapshot
}

func jsonObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func jsonNumber(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case uint64:
		return int64(n)
	case uint:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		text := strings.TrimSpace(n)
		if text == "" {
			return 0
		}
		i, err := strconv.ParseInt(text, 10, 64)
		if err == nil {
			return i
		}
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0
		}
		return int64(f)
	}
	return 0
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// QueryDashboardSessions returns session summaries split into active and completed.
// Completed sessions are fetched separately with LIMIT+1 to determine hasMore.
func QueryDashboardSessions(ctx context.Context, db *sql.DB) (active, completed []views.SessionSummary, hasMore bool) {
	active = QueryActiveSessions(ctx, db)
	// Fetch completed sessions with LIMIT+1 for hasMore detection
	completed, hasMore = QueryCompletedSessions(ctx, db, nil, 0, defaultSessionPageSize)
	return active, completed, hasMore
}

// QueryCompletedSessions returns paginated completed sessions with optional time filter.
// Only returns parent sessions (excludes subagents); subagent counts are attached.
func QueryCompletedSessions(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int) ([]views.SessionSummary, bool) {
	return QueryCompletedSessionsFiltered(ctx, db, since, offset, limit, "", nil, "ended", false)
}

// QueryCompletedSessionsFiltered returns paginated completed sessions with optional
// time and text filters. Search matches session metadata plus session IDs
// discovered by the tokenized event search path.
func QueryCompletedSessionsFiltered(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int, searchText string, eventSessionIDs []string, sortKey string, sortAsc bool) ([]views.SessionSummary, bool) {
	return queryCompletedSessionsFiltered(ctx, db, since, offset, limit, searchText, eventSessionIDs, sortKey, sortAsc, "")
}

func queryCompletedSessionsFiltered(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int, searchText string, eventSessionIDs []string, sortKey string, sortAsc bool, sessionIDPrefix string) ([]views.SessionSummary, bool) {
	cutoff := time.Now().Add(-idleThreshold)
	query := `SELECT ` + sessionSummaryColumns + `
		 FROM ` + sessionProjectionSQL + `
		 WHERE (ended_at < ?
		    OR COALESCE(has_session_end, 0) = 1)
		   AND (parent_session_id = '' OR parent_session_id IS NULL)`
	args := []any{cutoff}
	if since != nil {
		query += " AND ended_at >= ?"
		args = append(args, *since)
	}
	if clause, prefixArgs := completedSessionIDPrefixClause(sessionIDPrefix); clause != "" {
		query += clause
		args = append(args, prefixArgs...)
	}
	if searchText = strings.TrimSpace(searchText); searchText != "" {
		clause, searchArgs := completedSessionSearchClause(searchText, eventSessionIDs)
		query += clause
		args = append(args, searchArgs...)
	}
	query += completedSessionsOrderBy(sortKey, sortAsc)
	if limit <= 0 {
		limit = defaultSessionPageSize
	}
	if offset < 0 {
		offset = 0
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit+1, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		logQueryError("completed sessions", err)
		return nil, false
	}
	defer rows.Close()

	now := time.Now()
	var sessions []views.SessionSummary
	for rows.Next() {
		s, err := scanSessionSummary(rows, now)
		if err != nil {
			logQueryScanError("completed sessions", err)
			continue
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		logQueryError("completed sessions rows", err)
		return nil, false
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	attachSubagentCounts(ctx, db, sessions)
	return sessions, hasMore
}

func completedSessionIDPrefixClause(prefix string) (string, []any) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil
	}
	return " AND positionCaseInsensitive(session_id, ?) = 1", []any{prefix}
}

func completedSessionsOrderBy(sortKey string, asc bool) string {
	direction := "DESC"
	if asc {
		direction = "ASC"
	}
	var expr string
	switch sortKey {
	case "name":
		expr = "lower(COALESCE(NULLIF(replaceRegexpOne(if(position(COALESCE(working_dir, ''), '/.claude/worktrees/') > 0, substring(COALESCE(working_dir, ''), 1, position(COALESCE(working_dir, ''), '/.claude/worktrees/') - 1), replaceRegexpOne(COALESCE(working_dir, ''), '/+$', '')), '^.*/', ''), ''), NULLIF(source_name, ''), session_id))"
	case "provider":
		expr = "lower(COALESCE(provider, ''))"
	case "model":
		expr = "lower(COALESCE(last_model, ''))"
	case "tokens":
		expr = "COALESCE(total_tokens, 0)"
	case "turns":
		expr = "COALESCE(turn_count, 0)"
	case "tools":
		expr = "COALESCE(tool_call_count, 0)"
	case "duration":
		expr = "dateDiff('second', started_at, ended_at)"
	case "project":
		expr = "lower(COALESCE(working_dir, ''))"
	case "id":
		expr = "session_id"
	case "ended", "":
		expr = "ended_at"
	default:
		expr = "ended_at"
		direction = "DESC"
	}
	return " ORDER BY " + expr + " " + direction + ", ended_at DESC, session_id DESC"
}

func completedSessionSearchClause(searchText string, eventSessionIDs []string) (string, []any) {
	columns := []string{
		"session_id",
		"COALESCE(source_name, '')",
		"COALESCE(provider, '')",
		"COALESCE(last_model, '')",
		"COALESCE(working_dir, '')",
	}
	terms := make([]string, 0, len(columns)+1)
	args := make([]any, 0, len(columns)+len(eventSessionIDs))
	for _, col := range columns {
		terms = append(terms, "positionCaseInsensitive("+col+", ?) > 0")
		args = append(args, searchText)
	}
	if len(eventSessionIDs) > 0 {
		terms = append(terms, "session_id IN ("+strings.TrimRight(strings.Repeat("?,", len(eventSessionIDs)), ",")+")")
		for _, id := range eventSessionIDs {
			args = append(args, id)
		}
	}
	return ` AND (` + strings.Join(terms, " OR ") + `)`, args
}

// attachSubagentCounts queries subagent counts for the given sessions and attaches them.
func attachSubagentCounts(ctx context.Context, db *sql.DB, sessions []views.SessionSummary) {
	if len(sessions) == 0 {
		return
	}
	// Build placeholders for the parent IDs on this page
	placeholders := make([]string, len(sessions))
	args := make([]any, len(sessions))
	for i, s := range sessions {
		placeholders[i] = "?"
		args[i] = s.ID
	}
	query := `SELECT parent_session_id, COUNT(*)
		 FROM ` + sessionProjectionSQL + `
		 WHERE parent_session_id IN (` + strings.Join(placeholders, ",") + `)
		 GROUP BY parent_session_id`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		logQueryError("subagent counts", err)
		return
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var parentID string
		var count int
		if err := rows.Scan(&parentID, &count); err != nil {
			logQueryScanError("subagent counts", err)
			continue
		}
		counts[parentID] = count
	}
	if err := rows.Err(); err != nil {
		logQueryError("subagent counts rows", err)
	}
	for i := range sessions {
		sessions[i].SubagentCount = counts[sessions[i].ID]
	}
}

// QueryChildSessions returns subagent sessions spawned from a parent session.
func QueryChildSessions(ctx context.Context, db *sql.DB, parentID string) []views.SessionSummary {
	rows, err := db.QueryContext(ctx,
		`SELECT `+sessionSummaryColumns+`
		 FROM `+sessionProjectionSQL+`
		 WHERE parent_session_id = ?
		 ORDER BY started_at ASC`, parentID)
	if err != nil {
		logQueryError("child sessions", err)
		return nil
	}
	defer rows.Close()

	now := time.Now()
	var children []views.SessionSummary
	for rows.Next() {
		s, err := scanSessionSummary(rows, now)
		if err != nil {
			logQueryScanError("child sessions", err)
			continue
		}
		children = append(children, s)
	}
	if err := rows.Err(); err != nil {
		logQueryError("child sessions rows", err)
		return nil
	}
	return children
}

// scanSessionSummary scans a row from session_projection into a SessionSummary.
func scanSessionSummary(scanner interface{ Scan(dest ...any) error }, now time.Time) (views.SessionSummary, error) {
	var s views.SessionSummary
	var source, model string
	var startedAt, endedAt time.Time
	var hasSessionEnd int
	err := scanner.Scan(&s.ID, &source, &startedAt, &endedAt,
		&s.TurnCount, &s.TotalTokens, &s.InputTokens, &s.OutputTokens,
		&s.CacheReadTokens, &s.CacheCreateTokens,
		&s.ToolCallCount, &s.MCPCallCount, &s.ErrorCount, &model, &s.WorkingDir,
		&s.ParentSessionID, &hasSessionEnd, &s.Provider)
	if err != nil {
		return s, err
	}
	s.Actor = source
	s.ActiveModel = model
	s.HasSessionEnd = hasSessionEnd > 0
	setSessionTiming(&s, startedAt, endedAt, now)
	return s, nil
}
