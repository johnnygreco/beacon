package web

import (
	"fmt"
	"strings"
	"time"
)

func latestActivityEventsSubquery(where string) string {
	return `(SELECT event_uid,
	               argMax(session_id, captured_at) AS session_id,
	               argMax(parent_session_id, captured_at) AS parent_session_id,
	               argMax(node_id, captured_at) AS node_id,
	               argMax(collector_id, captured_at) AS collector_id,
	               argMax(source_id, captured_at) AS source_id,
	               argMax(source_name, captured_at) AS source_name,
	               argMax(runtime, captured_at) AS runtime,
	               argMax(format, captured_at) AS format,
	               argMax(provider, captured_at) AS provider,
	               argMax(timestamp, captured_at) AS timestamp,
	               argMax(event_kind, captured_at) AS event_kind,
	               argMax(payload_type, captured_at) AS payload_type,
	               argMax(actor_role, captured_at) AS actor_role,
	               argMax(text_content, captured_at) AS text_content,
	               argMax(text_preview, captured_at) AS text_preview,
	               argMax(tool_name, captured_at) AS tool_name,
	               argMax(tool_use_id, captured_at) AS tool_use_id,
	               argMax(model, captured_at) AS model,
	               argMax(input_tokens, captured_at) AS input_tokens,
	               argMax(output_tokens, captured_at) AS output_tokens,
	               argMax(cache_read_tokens, captured_at) AS cache_read_tokens,
	               argMax(cache_create_tokens, captured_at) AS cache_create_tokens,
	               argMax(duration_ms, captured_at) AS duration_ms,
	               argMax(cost_usd, captured_at) AS cost_usd,
	               argMax(error_code, captured_at) AS error_code,
	               argMax(error_message, captured_at) AS error_message,
	               argMax(cwd, captured_at) AS cwd,
	               max(captured_at) AS latest_captured_at
	        FROM activity_events AS ae ` + sqlWhereClause(where) + `
	        GROUP BY event_uid)`
}

func recentActivityEventsSubquery(where string) string {
	return recentActivityEventsJoinedSubquery(where, "", where)
}

func recentActivityEventsJoinedSubquery(where, join, candidateWhere string) string {
	candidateFilter := ""
	if strings.TrimSpace(candidateWhere) != "" {
		candidateFilter = `WHERE event_uid IN (
				SELECT DISTINCT ae.event_uid
				FROM activity_events AS ae ` + sqlWhereClause(candidateWhere) + `
			)`
	}
	return fmt.Sprintf(`(SELECT event_uid,
	               session_id,
	               node_id,
	               collector_id,
	               source_id,
	               source_name,
	               runtime,
	               provider,
	               timestamp,
	               event_kind,
	               actor_role,
	               text_preview,
	               tool_name,
	               error_code,
	               error_message,
	               cwd
	        FROM (
			SELECT event_uid,
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
			       argMax(text_preview, captured_at) AS text_preview,
			       argMax(tool_name, captured_at) AS tool_name,
			       argMax(error_code, captured_at) AS error_code,
			       argMax(error_message, captured_at) AS error_message,
			       argMax(cwd, captured_at) AS cwd
			FROM activity_events
			`+candidateFilter+`
			GROUP BY event_uid
		) AS ae `+join+` `+sqlWhereClause(where)+`
		ORDER BY timestamp DESC
		LIMIT %d)`, recentActivityCandidates)
}

func sqlWhereClause(where string) string {
	if strings.TrimSpace(where) == "" {
		return ""
	}
	return "WHERE " + where
}

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func sessionProjectionSubquery(where string) string {
	return `(SELECT
		session_id,
		node_id,
		collector_id,
		source_id,
		source_name,
		runtime,
		provider,
		format,
		project_key,
		project_path,
		started_at,
		ended_at,
		event_count,
		turn_count,
		total_input_tokens,
		total_output_tokens,
		total_cache_read_tokens,
		total_cache_create_tokens,
		total_tokens,
		tool_call_count,
		mcp_call_count,
		error_count,
		last_model,
		working_dir,
		parent_session_id,
		has_session_end,
		completion_state,
		total_cost_usd,
		cost_event_count,
		cost_provenance,
		attention_score,
		attention_reasons,
		archive_reason,
		archived_at
	FROM session_projection FINAL ` + sqlWhereClause(where) + `)`
}

func sessionProjectFallbackSubquery(where string) string {
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
					FROM activity_events AS ae ` + sqlWhereClause(where) + `
					GROUP BY event_uid
				)
				WHERE session_id != ''
			)
			GROUP BY session_id
		))`
}

func sessionProjectionSubqueryForScope(where string, scope APIScopeFilters) (string, []any) {
	return sessionProjectionSubqueryForScopeWithPrefilter(where, "", nil, scope)
}

func sessionProjectionSubqueryForScopeWithPrefilter(where, eventSessionWhere string, eventSessionArgs []any, scope APIScopeFilters) (string, []any) {
	if len(compactScopeValues(scope.ProjectKeys)) == 0 {
		return sessionProjectionSubquery(where), nil
	}
	scopeClause, scopeArgs := scope.eventAndSessionProjectSQLAndClause("e", "e.cwd", "s")
	latestWhere := "ae.session_id != ''"
	if strings.TrimSpace(eventSessionWhere) != "" {
		latestWhere += " AND " + eventSessionWhere
	}
	scopedProjectKeyExpr := projectKeyExpr("sa.scoped_working_dir")
	return `(SELECT *
	FROM (WITH scoped_activity AS (
		SELECT e.session_id AS session_id,
		       argMaxIf(e.node_id, e.timestamp, e.node_id != '') AS node_id,
		       argMaxIf(e.collector_id, e.timestamp, e.collector_id != '') AS collector_id,
		       argMaxIf(e.source_id, e.timestamp, e.source_id != '') AS source_id,
		       argMaxIf(e.source_name, e.timestamp, e.source_name != '') AS source_name,
		       argMaxIf(e.runtime, e.timestamp, e.runtime != '') AS runtime,
		       argMaxIf(e.provider, e.timestamp, e.provider != '') AS provider,
		       argMaxIf(e.format, e.timestamp, e.format != '') AS format,
		       argMaxIf(e.cwd, e.timestamp, e.cwd != '') AS scoped_working_dir,
		       minIf(e.timestamp, e.timestamp > toDateTime64(0, 3, 'UTC')) AS started_at,
		       maxIf(e.timestamp, e.timestamp > toDateTime64(0, 3, 'UTC')) AS ended_at,
		       argMax(e.event_kind, e.timestamp) AS latest_event_kind,
		       count() AS event_count,
		       uniqExactIf(e.event_uid, e.event_kind = 'message' AND e.actor_role = 'user') AS turn_count,
		       sum(e.input_tokens) AS total_input_tokens,
		       sum(e.output_tokens) AS total_output_tokens,
		       sum(e.cache_read_tokens) AS total_cache_read_tokens,
		       sum(e.cache_create_tokens) AS total_cache_create_tokens,
		       sum(e.input_tokens + e.output_tokens) AS total_tokens,
		       countIf(e.event_kind = 'tool_call') AS tool_call_count,
		       countIf(e.event_kind = 'tool_call' AND startsWith(e.tool_name, 'mcp__')) AS mcp_call_count,
		       countIf(e.event_kind IN ('error', 'tool_error')) AS error_count,
		       countIf(e.event_kind = 'tool_error') AS tool_error_count,
		       countIf(e.event_kind = 'tool_error' AND startsWith(e.tool_name, 'mcp__')) AS mcp_error_count,
		       argMaxIf(e.model, e.timestamp, e.model != '') AS last_model,
		       sum(e.cost_usd) AS total_cost_usd,
		       countIf(e.cost_usd != 0) AS cost_event_count
		FROM ` + latestActivityEventsSubquery(latestWhere) + ` AS e
		LEFT JOIN ` + sessionProjectFallbackSubquery("") + ` AS s ON s.session_id = e.session_id
		WHERE 1 = 1` + scopeClause + `
		GROUP BY e.session_id
	)
	SELECT
		sp.session_id AS session_id,
		sa.node_id AS node_id,
		sa.collector_id AS collector_id,
		sa.source_id AS source_id,
		sa.source_name AS source_name,
		sa.runtime AS runtime,
		sa.provider AS provider,
		sa.format AS format,
		COALESCE(NULLIF(` + scopedProjectKeyExpr + `, ''), sp.project_key) AS project_key,
		COALESCE(NULLIF(sa.scoped_working_dir, ''), sp.project_path) AS project_path,
		sa.started_at AS started_at,
		sa.ended_at AS ended_at,
		sa.event_count AS event_count,
		sa.turn_count AS turn_count,
		sa.total_input_tokens AS total_input_tokens,
		sa.total_output_tokens AS total_output_tokens,
		sa.total_cache_read_tokens AS total_cache_read_tokens,
		sa.total_cache_create_tokens AS total_cache_create_tokens,
		sa.total_tokens AS total_tokens,
		sa.tool_call_count AS tool_call_count,
		sa.mcp_call_count AS mcp_call_count,
		sa.error_count AS error_count,
		sa.last_model AS last_model,
		COALESCE(NULLIF(sa.scoped_working_dir, ''), sp.working_dir) AS working_dir,
		sp.parent_session_id AS parent_session_id,
		if(sa.latest_event_kind = 'session_end', 1, 0) AS has_session_end,
		if(sa.latest_event_kind = 'session_end', 'completed', 'active') AS completion_state,
		sa.total_cost_usd AS total_cost_usd,
		sa.cost_event_count AS cost_event_count,
		if(sa.cost_event_count > 0, 'event_cost_usd', 'none') AS cost_provenance,
		toUInt16(least(sa.error_count * 100 + sa.tool_error_count * 50 + sa.mcp_error_count * 50, 65535)) AS attention_score,
		arrayFilter(reason -> reason != '', [
			if(sa.error_count > 0, 'errors', ''),
			if(sa.tool_error_count > 0, 'tool_errors', ''),
			if(sa.mcp_error_count > 0, 'mcp_errors', '')
		]) AS attention_reasons,
		'' AS archive_reason,
		toDateTime64(0, 3, 'UTC') AS archived_at
	FROM ` + sessionProjectionSubquery("") + ` AS sp
	INNER JOIN scoped_activity AS sa ON sa.session_id = sp.session_id
	) AS scoped_sessions ` + sqlWhereClause(where) + `)`, append(append([]any{}, eventSessionArgs...), scopeArgs...)
}

func reopenedSessionIDsSubquery() string {
	return `(SELECT session_id
	        FROM (
			SELECT ae.event_uid,
			       argMax(ae.session_id, ae.captured_at) AS session_id,
			       argMax(ae.timestamp, ae.captured_at) AS timestamp,
			       argMax(ae.event_kind, ae.captured_at) AS event_kind
			FROM activity_events AS ae
			PREWHERE ae.timestamp >= ?
			WHERE ae.session_id != ''
			  AND ae.timestamp > toDateTime64(0, 3, 'UTC')
			GROUP BY ae.event_uid
	        )
	        GROUP BY session_id
	        HAVING maxIf(timestamp, event_kind != 'session_end') > maxIf(timestamp, event_kind = 'session_end'))`
}

func reopenedSessionPredicate() string {
	return `session_id IN ` + reopenedSessionIDsSubquery()
}

func activeSessionPredicate() string {
	return `(ended_at >= ? AND (COALESCE(has_session_end, 0) = 0 OR ` + reopenedSessionPredicate() + `))`
}

func activeSessionPredicateScoped(scope APIScopeFilters) string {
	if len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return `(ended_at >= ? AND COALESCE(has_session_end, 0) = 0)`
	}
	return activeSessionPredicate()
}

func activeSessionPredicateArgs(scope APIScopeFilters, cutoff time.Time) []any {
	if len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return []any{cutoff}
	}
	return []any{cutoff, cutoff}
}

func completedSessionPredicate() string {
	return completedSessionPredicateFor("ended_at", "has_session_end", "session_id")
}

func completedSessionPredicateScoped(scope APIScopeFilters) string {
	return completedSessionPredicateForScope("ended_at", "has_session_end", "session_id", scope)
}

func completedSessionPredicateFor(endedAtExpr, hasSessionEndExpr, sessionIDExpr string) string {
	return `(` + endedAtExpr + ` < ? OR (COALESCE(` + hasSessionEndExpr + `, 0) = 1 AND NOT (` + sessionIDExpr + ` IN ` + reopenedSessionIDsSubquery() + `)))`
}

func completedSessionPredicateForScope(endedAtExpr, hasSessionEndExpr, sessionIDExpr string, scope APIScopeFilters) string {
	if len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return `(` + endedAtExpr + ` < ? OR COALESCE(` + hasSessionEndExpr + `, 0) = 1)`
	}
	return completedSessionPredicateFor(endedAtExpr, hasSessionEndExpr, sessionIDExpr)
}

func completedSessionPredicateArgs(scope APIScopeFilters, cutoff time.Time) []any {
	if len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return []any{cutoff}
	}
	return []any{cutoff, cutoff}
}

func sessionSummaryColumnsWithReopenedFlag() string {
	return sessionSummaryColumns + `, if(` + reopenedSessionPredicate() + `, 1, 0)`
}

func sessionSummaryColumnsWithReopenedFlagScoped(scope APIScopeFilters) string {
	if len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return sessionSummaryColumns + `, 0`
	}
	return sessionSummaryColumnsWithReopenedFlag()
}

func reopenedFlagArgs(scope APIScopeFilters, cutoff time.Time) []any {
	if len(compactScopeValues(scope.ProjectKeys)) > 0 {
		return nil
	}
	return []any{cutoff}
}

var analyticsProjectionSQL = analyticsProjectionSubquery("")

func analyticsProjectionSubquery(where string) string {
	return analyticsProjectionSubqueryWithLatestWhere(where, "")
}

func analyticsProjectionSubqueryWithLatestWhere(where, latestWhere string) string {
	apWhere := sqlWhereClause(latestWhere)
	latestRefreshWhere := "session_id != ''"
	if strings.TrimSpace(latestWhere) != "" {
		latestRefreshWhere += " AND " + latestWhere
	}
	return `(SELECT
		session_id,
		node_id,
		collector_id,
		source_id,
		source_name,
		runtime,
		format,
		project_key,
		project_path,
		minute,
		provider,
		model,
		tool_name,
		event_kind,
		event_count,
		call_count,
		tool_call_count,
		tool_result_count,
		input_tokens,
		output_tokens,
		cache_read_tokens,
		cache_create_tokens,
		total_tokens,
		duration_ms_sum,
		cost_usd_sum
	FROM (
		SELECT ap.session_id,
		       ap.node_id,
		       ap.collector_id,
		       ap.source_id,
		       ap.source_name,
		       ap.runtime,
		       ap.format,
		       ap.project_key,
		       ap.project_path,
		       ap.minute,
		       ap.provider,
		       ap.model,
		       ap.tool_name,
		       ap.event_kind,
		       ap.event_count,
		       ap.call_count,
		       ap.tool_call_count,
		       ap.tool_result_count,
		       ap.input_tokens,
		       ap.output_tokens,
		       ap.cache_read_tokens,
		       ap.cache_create_tokens,
		       ap.total_tokens,
		       ap.duration_ms_sum,
		       ap.cost_usd_sum
		FROM (
			SELECT *
			FROM analytics_projection FINAL ` + apWhere + `
		) AS ap
		INNER JOIN (
			SELECT session_id, argMax(refresh_id, updated_at) AS refresh_id
			FROM analytics_projection FINAL
			WHERE ` + latestRefreshWhere + `
			GROUP BY session_id
		) AS latest ON latest.session_id = ap.session_id AND latest.refresh_id = ap.refresh_id
	) ` + sqlWhereClause(where) + `)`
}

// sessionSummaryColumns is the shared SELECT column list for session projection queries.
const sessionSummaryColumns = `session_id,
		COALESCE(node_id, ''), COALESCE(collector_id, ''), COALESCE(source_id, ''),
		COALESCE(source_name, ''), COALESCE(runtime, ''), COALESCE(provider, ''),
		COALESCE(format, ''), COALESCE(project_key, ''), COALESCE(project_path, ''),
		started_at, ended_at,
		COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
		COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
		COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
		COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
		COALESCE(error_count, 0),
		COALESCE(last_model, ''),
		COALESCE(working_dir, ''),
		COALESCE(parent_session_id, ''),
		COALESCE(has_session_end, 0),
		COALESCE(completion_state, ''),
		COALESCE(total_cost_usd, 0),
		COALESCE(cost_event_count, 0),
		COALESCE(cost_provenance, ''),
		COALESCE(attention_score, 0),
		attention_reasons,
		COALESCE(archive_reason, ''),
		archived_at`
