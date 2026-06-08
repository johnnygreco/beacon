package web

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/textindex"
	"github.com/johnnygreco/beacon/internal/views"
)

// QueryActiveSessions returns active session summaries only.
// Active sessions are those with recent activity and no definitive end signal.
func QueryActiveSessions(ctx context.Context, db *sql.DB) []views.SessionSummary {
	return queryActiveSessions(ctx, db, 0, APIScopeFilters{})
}

func QueryActiveSessionsLimited(ctx context.Context, db *sql.DB, limit int) []views.SessionSummary {
	return QueryActiveSessionsLimitedScoped(ctx, db, limit, APIScopeFilters{})
}

func QueryActiveSessionsLimitedScoped(ctx context.Context, db *sql.DB, limit int, scope APIScopeFilters) []views.SessionSummary {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	return queryActiveSessions(ctx, db, limit, scope)
}

func QueryActiveSessionsScoped(ctx context.Context, db *sql.DB, scope APIScopeFilters) []views.SessionSummary {
	return queryActiveSessions(ctx, db, 0, scope)
}

func queryActiveSessions(ctx context.Context, db *sql.DB, limit int, scope APIScopeFilters) []views.SessionSummary {
	now := time.Now()
	// Use Go's time.Now() (UTC-aware) instead of SQL current_timestamp to avoid
	// timezone mismatch — stored timestamps are UTC but current_timestamp is local.
	cutoff := now.Add(-idleThreshold)

	// Fetch active sessions: recent activity that is not terminally ended.
	// A historical session_end does not keep a session completed after newer
	// non-end activity arrives for the same session.
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	query := `SELECT ` + sessionSummaryColumns + `
		 FROM ` + sessionSource + `
		 WHERE ` + activeSessionPredicateScoped(scope) + `
		`
	args := append([]any{}, sourceArgs...)
	args = append(args, activeSessionPredicateArgs(scope, cutoff)...)
	sessionScope := scope.withoutProjectKeys()
	if clause, scopeArgs := sessionScope.sqlAndClause(""); clause != "" {
		query += clause
		args = append(args, scopeArgs...)
	}
	query += ` ORDER BY ended_at DESC`
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
		markSessionReopened(&s, now)
		active = append(active, s)
	}
	if err := activeRows.Err(); err != nil {
		logQueryError("active sessions rows", err)
		return nil
	}

	return views.GroupActiveSessions(active)
}

// QueryDashboardSessions returns session summaries split into active and completed.
// Completed sessions are fetched separately with LIMIT+1 to determine hasMore.
func QueryDashboardSessions(ctx context.Context, db *sql.DB) (active, completed []views.SessionSummary, hasMore bool) {
	active = QueryActiveSessions(ctx, db)
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
	return queryCompletedSessionsFiltered(ctx, db, since, offset, limit, searchText, eventSessionIDs, sortKey, sortAsc, "", APIScopeFilters{})
}

func queryCompletedSessionsFiltered(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int, searchText string, eventSessionIDs []string, sortKey string, sortAsc bool, sessionIDPrefix string, scope APIScopeFilters) ([]views.SessionSummary, bool) {
	cutoff := time.Now().Add(-idleThreshold)
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	query := `SELECT ` + sessionSummaryColumns + `
		 FROM ` + sessionSource + `
		 WHERE ` + completedSessionPredicateScoped(scope) + `
		   AND (parent_session_id = '' OR parent_session_id IS NULL)`
	args := append([]any{}, sourceArgs...)
	args = append(args, completedSessionPredicateArgs(scope, cutoff)...)
	if since != nil {
		query += " AND ended_at >= ?"
		args = append(args, *since)
	}
	if clause, prefixArgs := completedSessionIDPrefixClause(sessionIDPrefix); clause != "" {
		query += clause
		args = append(args, prefixArgs...)
	}
	sessionScope := scope.withoutProjectKeys()
	if clause, scopeArgs := sessionScope.sqlAndClause(""); clause != "" {
		query += clause
		args = append(args, scopeArgs...)
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
	attachSubagentCounts(ctx, db, sessions, scope)
	return sessions, hasMore
}

func completedSessionIDPrefixClause(prefix string) (string, []any) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil
	}
	return " AND positionCaseInsensitive(session_id, ?) = 1", []any{prefix}
}

func queryCompletedSessionContentMatchIDs(ctx context.Context, db *sql.DB, since *time.Time, searchText, sessionIDPrefix string, limit int, scope APIScopeFilters) ([]string, error) {
	searchText = strings.TrimSpace(searchText)
	if db == nil || searchText == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = completedSessionEventSearchLimit
	}
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	query := `SELECT e.session_id
		 FROM (
			SELECT *
			FROM activity_events FINAL
		 ) AS e
		 INNER JOIN ` + sessionSource + ` AS s ON s.session_id = e.session_id
		 LEFT JOIN ` + sessionProjectFallbackSubquery("") + ` AS sj ON sj.session_id = e.session_id
		 LEFT JOIN (
			SELECT *
			FROM tool_payloads FINAL
		 ) AS tp ON tp.event_uid = e.event_uid
		 WHERE (s.parent_session_id = '' OR s.parent_session_id IS NULL)
		   AND e.session_id != ''`
	args := append([]any{}, sourceArgs...)
	cutoff := time.Now().Add(-idleThreshold)
	query += ` AND ` + completedSessionPredicateForScope("s.ended_at", "s.has_session_end", "s.session_id", scope)
	args = append(args, completedSessionPredicateArgs(scope, cutoff)...)
	if since != nil {
		query += " AND s.ended_at >= ?"
		args = append(args, *since)
	}
	if sessionIDPrefix = strings.TrimSpace(sessionIDPrefix); sessionIDPrefix != "" {
		query += " AND positionCaseInsensitive(s.session_id, ?) = 1"
		args = append(args, sessionIDPrefix)
	}
	sessionScope := scope.withoutProjectKeys()
	if clause, scopeArgs := sessionScope.sqlAndClause("s"); clause != "" {
		query += clause
		args = append(args, scopeArgs...)
	}
	if clause, eventScopeArgs := scope.eventAndSessionProjectSQLAndClause("e", "e.cwd", "sj"); clause != "" {
		query += clause
		args = append(args, eventScopeArgs...)
	}
	contentColumns := []string{
		"COALESCE(s.session_id, '')",
		"COALESCE(s.source_name, '')",
		"COALESCE(s.provider, '')",
		"COALESCE(s.last_model, '')",
		"COALESCE(s.working_dir, '')",
		"COALESCE(e.text_content, '')",
		"COALESCE(e.text_preview, '')",
		"COALESCE(e.tool_name, '')",
		"COALESCE(e.error_code, '')",
		"COALESCE(e.error_message, '')",
		"COALESCE(e.cwd, '')",
		"COALESCE(e.payload_json, '')",
		"COALESCE(tp.input_preview, '')",
		"COALESCE(tp.output_preview, '')",
		"COALESCE(tp.input_json, '')",
		"COALESCE(tp.output_json, '')",
	}
	for _, term := range completedSessionContentSearchTerms(searchText) {
		clauses := make([]string, 0, len(contentColumns))
		for _, column := range contentColumns {
			clauses = append(clauses, "positionCaseInsensitive("+column+", ?) > 0")
			args = append(args, term)
		}
		query += " AND (" + strings.Join(clauses, " OR ") + ")"
	}
	query += " GROUP BY e.session_id ORDER BY max(s.ended_at) DESC, e.session_id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func completedSessionContentSearchTerms(searchText string) []string {
	tokens := textindex.Tokenize(searchText)
	if len(tokens) == 0 {
		return []string{searchText}
	}
	terms := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		terms = append(terms, token)
	}
	if len(terms) == 0 {
		return []string{searchText}
	}
	return terms
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
func attachSubagentCounts(ctx context.Context, db *sql.DB, sessions []views.SessionSummary, scope APIScopeFilters) {
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
		 FROM `
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	query += sessionSource + `
		 WHERE parent_session_id IN (` + strings.Join(placeholders, ",") + `)`
	args = append(sourceArgs, args...)
	sessionScope := scope.withoutProjectKeys()
	if clause, scopeArgs := sessionScope.sqlAndClause(""); clause != "" {
		query += clause
		args = append(args, scopeArgs...)
	}
	query += ` GROUP BY parent_session_id`
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
	return QueryChildSessionsScoped(ctx, db, parentID, APIScopeFilters{})
}

func QueryChildSessionsScoped(ctx context.Context, db *sql.DB, parentID string, scope APIScopeFilters) []views.SessionSummary {
	now := time.Now()
	activeCutoff := now.Add(-idleThreshold)
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	query := `SELECT ` + sessionSummaryColumnsWithReopenedFlagScoped(scope) + `
		 FROM ` + sessionSource + `
		 WHERE parent_session_id = ?`
	args := reopenedFlagArgs(scope, activeCutoff)
	args = append(args, sourceArgs...)
	args = append(args, parentID)
	sessionScope := scope.withoutProjectKeys()
	if clause, scopeArgs := sessionScope.sqlAndClause(""); clause != "" {
		query += clause
		args = append(args, scopeArgs...)
	}
	query += ` ORDER BY started_at ASC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		logQueryError("child sessions", err)
		return nil
	}
	defer rows.Close()

	var children []views.SessionSummary
	for rows.Next() {
		s, err := scanSessionSummaryIncludingReopened(rows, now)
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

func markSessionReopened(s *views.SessionSummary, now time.Time) {
	if s == nil || !s.HasSessionEnd {
		return
	}
	s.HasSessionEnd = false
	s.CompletionState = "active"
	setSessionTiming(s, s.StartedAt, s.EndedAt, now)
}

// scanSessionSummary scans a row from session_projection into a SessionSummary.
func scanSessionSummary(scanner interface{ Scan(dest ...any) error }, now time.Time) (views.SessionSummary, error) {
	return scanSessionSummaryBase(scanner, now, nil)
}

func scanSessionSummaryIncludingReopened(scanner interface{ Scan(dest ...any) error }, now time.Time) (views.SessionSummary, error) {
	var reopened int
	s, err := scanSessionSummaryBase(scanner, now, &reopened)
	if err != nil {
		return s, err
	}
	if reopened > 0 {
		markSessionReopened(&s, now)
	}
	return s, nil
}

func scanSessionSummaryBase(scanner interface{ Scan(dest ...any) error }, now time.Time, reopened *int) (views.SessionSummary, error) {
	var s views.SessionSummary
	var source, model string
	var startedAt, endedAt time.Time
	var hasSessionEnd int
	var attentionScore int
	dest := []any{&s.ID, &s.NodeID, &s.CollectorID, &s.SourceID, &source, &s.Runtime, &s.Provider,
		&s.Format, &s.ProjectKey, &s.ProjectPath, &startedAt, &endedAt,
		&s.TurnCount, &s.TotalTokens, &s.InputTokens, &s.OutputTokens,
		&s.CacheReadTokens, &s.CacheCreateTokens,
		&s.ToolCallCount, &s.MCPCallCount, &s.ErrorCount, &model, &s.WorkingDir,
		&s.ParentSessionID, &hasSessionEnd, &s.CompletionState, &s.TotalCostUSD,
		&s.CostEventCount, &s.CostProvenance, &attentionScore, &s.AttentionReasons,
		&s.ArchiveReason, &s.ArchivedAt}
	if reopened != nil {
		dest = append(dest, reopened)
	}
	err := scanner.Scan(dest...)
	if err != nil {
		return s, err
	}
	s.Actor = source
	s.ActiveModel = model
	s.HasSessionEnd = hasSessionEnd > 0
	s.AttentionScore = attentionScore
	if s.ProjectPath == "" {
		s.ProjectPath = s.WorkingDir
	}
	if !s.ArchivedAt.After(time.Unix(0, 0).UTC()) {
		s.ArchivedAt = time.Time{}
	}
	setSessionTiming(&s, startedAt, endedAt, now)
	return s, nil
}
