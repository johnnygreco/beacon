package web

import (
	"fmt"
	"strings"
)

func latestActivityEventsSubquery(where string) string {
	return `(SELECT event_uid,
	               argMax(session_id, captured_at) AS session_id,
	               argMax(parent_session_id, captured_at) AS parent_session_id,
	               argMax(source_name, captured_at) AS source_name,
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
	return fmt.Sprintf(`(SELECT event_uid,
	               argMax(session_id, captured_at) AS session_id,
	               argMax(provider, captured_at) AS provider,
	               argMax(timestamp, captured_at) AS timestamp,
	               argMax(event_kind, captured_at) AS event_kind,
	               argMax(actor_role, captured_at) AS actor_role,
	               argMax(text_preview, captured_at) AS text_preview,
	               argMax(tool_name, captured_at) AS tool_name,
	               argMax(error_code, captured_at) AS error_code,
	               argMax(error_message, captured_at) AS error_message
	        FROM (
			SELECT event_uid,
			       session_id,
			       provider,
			       timestamp,
			       event_kind,
			       actor_role,
			       text_preview,
			       tool_name,
			       error_code,
			       error_message,
			       captured_at
			FROM activity_events AS ae `+sqlWhereClause(where)+`
			ORDER BY timestamp DESC
			LIMIT %d
	        )
	        GROUP BY event_uid)`, recentActivityCandidates)
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

var sessionProjectionSQL = sessionProjectionSubquery("")

func sessionProjectionSubquery(where string) string {
	return `(SELECT
		session_id,
		source_name,
		provider,
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
		has_session_end
	FROM session_projection FINAL ` + sqlWhereClause(where) + `)`
}

func reopenedSessionIDsSubquery() string {
	return `(SELECT session_id
	        FROM (
			SELECT event_uid,
			       argMax(session_id, captured_at) AS session_id,
			       argMax(timestamp, captured_at) AS timestamp,
			       argMax(event_kind, captured_at) AS event_kind
			FROM activity_events
			PREWHERE timestamp >= ?
			WHERE session_id != ''
			  AND timestamp > toDateTime64(0, 3, 'UTC')
			GROUP BY event_uid
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

func completedSessionPredicate() string {
	return `(ended_at < ? OR (COALESCE(has_session_end, 0) = 1 AND NOT (` + reopenedSessionPredicate() + `)))`
}

func sessionSummaryColumnsWithReopenedFlag() string {
	return sessionSummaryColumns + `, if(` + reopenedSessionPredicate() + `, 1, 0)`
}

var analyticsProjectionSQL = analyticsProjectionSubquery("")

func analyticsProjectionSubquery(where string) string {
	return `(SELECT
		session_id,
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
		duration_ms_sum
	FROM analytics_projection FINAL ` + sqlWhereClause(where) + `)`
}

// sessionSummaryColumns is the shared SELECT column list for session projection queries.
const sessionSummaryColumns = `session_id, COALESCE(source_name, ''), started_at, ended_at,
		COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
		COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
		COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
		COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
		COALESCE(error_count, 0),
		COALESCE(last_model, ''),
		COALESCE(working_dir, ''),
		COALESCE(parent_session_id, ''),
		COALESCE(has_session_end, 0),
		COALESCE(provider, '')`
