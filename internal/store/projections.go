package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	sessionEndProjectionPredicate = "event_kind = 'session_end'"
	validEventTimestampPredicate  = "timestamp > toDateTime64(0, 3, 'UTC')"
	defaultProjectionRefreshBatch = 500
)

func (s *Store) RefreshSessionProjections(ctx context.Context, ids []string) error {
	ids = uniqStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	placeholders := placeholders(len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	query := sessionProjectionInsertSQL(placeholders)
	_, err := s.DB.ExecContext(ctx, query, args...)
	return err
}

func sessionProjectionInsertSQL(placeholders string) string {
	return fmt.Sprintf(`INSERT INTO session_projection
		SELECT
			session_id,
			node_id,
			collector_id,
			source_id,
			source_name,
			runtime,
			provider,
			format,
			%[4]s AS project_key,
			working_dir AS project_path,
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
			if(has_session_end > 0, 'completed', 'active') AS completion_state,
			total_cost_usd,
			cost_event_count,
			if(cost_event_count > 0, 'event_cost_usd', 'none') AS cost_provenance,
			toUInt16(least(error_count * 100 + tool_error_count * 50 + mcp_error_count * 50, 65535)) AS attention_score,
			arrayFilter(reason -> reason != '', [
				if(error_count > 0, 'errors', ''),
				if(tool_error_count > 0, 'tool_errors', ''),
				if(mcp_error_count > 0, 'mcp_errors', '')
			]) AS attention_reasons,
			'' AS archive_reason,
			toDateTime64(0, 3, 'UTC') AS archived_at,
			now64(3) AS updated_at
		FROM (
			SELECT
				projected_session_id AS session_id,
				argMaxIf(node_id, timestamp, node_id != '') AS node_id,
				argMaxIf(collector_id, timestamp, collector_id != '') AS collector_id,
				argMaxIf(source_id, timestamp, source_id != '') AS source_id,
				argMax(source_name, timestamp) AS source_name,
				argMax(runtime, timestamp) AS runtime,
				argMax(provider, timestamp) AS provider,
				argMax(format, timestamp) AS format,
				if(countIf(%[1]s) > 0, minIf(timestamp, %[1]s), min(timestamp)) AS started_at,
				if(countIf(%[1]s) > 0, maxIf(timestamp, %[1]s), max(timestamp)) AS ended_at,
				count() AS event_count,
				uniqExactIf(event_uid, event_kind = 'message' AND actor_role = 'user') AS turn_count,
				sum(input_tokens) AS total_input_tokens,
				sum(output_tokens) AS total_output_tokens,
				sum(cache_read_tokens) AS total_cache_read_tokens,
				sum(cache_create_tokens) AS total_cache_create_tokens,
				sum(input_tokens + output_tokens) AS total_tokens,
				countIf(event_kind = 'tool_call') AS tool_call_count,
				countIf(event_kind = 'tool_call' AND startsWith(tool_name, 'mcp__')) AS mcp_call_count,
				countIf(event_kind IN ('error', 'tool_error')) AS error_count,
				countIf(event_kind = 'tool_error') AS tool_error_count,
				countIf(event_kind = 'tool_error' AND startsWith(tool_name, 'mcp__')) AS mcp_error_count,
				argMaxIf(model, timestamp, model != '') AS last_model,
				argMaxIf(cwd, timestamp, cwd != '') AS working_dir,
				argMaxIf(parent_session_id, timestamp, parent_session_id != '') AS parent_session_id,
				max(if(%[2]s, 1, 0)) AS has_session_end,
				sum(cost_usd) AS total_cost_usd,
				countIf(cost_usd != 0) AS cost_event_count
			FROM (
				SELECT event_uid,
				       argMax(session_id, captured_at) AS projected_session_id,
				       argMax(node_id, captured_at) AS node_id,
				       argMax(collector_id, captured_at) AS collector_id,
				       argMax(source_id, captured_at) AS source_id,
				       argMax(source_name, captured_at) AS source_name,
				       argMax(runtime, captured_at) AS runtime,
				       argMax(provider, captured_at) AS provider,
				       argMax(format, captured_at) AS format,
				       argMax(timestamp, captured_at) AS timestamp,
				       argMax(event_kind, captured_at) AS event_kind,
				       argMax(payload_type, captured_at) AS payload_type,
				       argMax(actor_role, captured_at) AS actor_role,
				       argMax(input_tokens, captured_at) AS input_tokens,
				       argMax(output_tokens, captured_at) AS output_tokens,
				       argMax(cache_read_tokens, captured_at) AS cache_read_tokens,
				       argMax(cache_create_tokens, captured_at) AS cache_create_tokens,
				       argMax(tool_name, captured_at) AS tool_name,
				       argMax(model, captured_at) AS model,
				       argMax(cwd, captured_at) AS cwd,
				       argMax(parent_session_id, captured_at) AS parent_session_id,
				       argMax(cost_usd, captured_at) AS cost_usd
				FROM activity_events
				WHERE session_id IN (%[3]s)
				GROUP BY event_uid
			)
			GROUP BY projected_session_id
		)`, validEventTimestampPredicate, sessionEndProjectionPredicate, placeholders, projectKeySQL("working_dir"))
}

func (s *Store) RefreshAllProjections(ctx context.Context, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = defaultProjectionRefreshBatch
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT session_id FROM activity_events WHERE session_id != ''`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	batch := make([]string, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.refreshProjections(ctx, batch); err != nil {
			return err
		}
		total += len(batch)
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return total, err
		}
		batch = append(batch, id)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

func (s *Store) refreshProjections(ctx context.Context, ids []string) error {
	if err := s.RefreshSessionProjections(ctx, ids); err != nil {
		return fmt.Errorf("refresh session projections: %w", err)
	}
	if err := s.RefreshAnalyticsProjections(ctx, ids); err != nil {
		return fmt.Errorf("refresh analytics projections: %w", err)
	}
	return nil
}

func (s *Store) RefreshOutdatedProjections(ctx context.Context) (int, bool, error) {
	if s == nil || s.DB == nil {
		return 0, false, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT events.session_id
		FROM (
			SELECT projected_session_id AS session_id,
			       count() AS event_count,
			       max(latest_captured_at) AS latest_captured_at
			FROM (
				SELECT event_uid,
				       argMax(session_id, captured_at) AS projected_session_id,
				       max(captured_at) AS latest_captured_at
				FROM activity_events
				WHERE session_id != ''
				GROUP BY event_uid
			)
			WHERE projected_session_id != ''
			GROUP BY projected_session_id
		) AS events
		LEFT JOIN (
			SELECT session_id AS projected_session_id,
			       event_count AS projected_event_count,
			       updated_at AS projected_updated_at
			FROM session_projection FINAL
			WHERE session_id != ''
		) AS projected ON events.session_id = projected.projected_session_id
		WHERE projected.projected_session_id = ''
		   OR events.event_count != projected.projected_event_count
		   OR events.latest_captured_at > projected.projected_updated_at`)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()

	total := 0
	batch := make([]string, 0, defaultProjectionRefreshBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.refreshProjections(ctx, batch); err != nil {
			return err
		}
		total += len(batch)
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return total, total > 0, err
		}
		batch = append(batch, id)
		if len(batch) >= defaultProjectionRefreshBatch {
			if err := flush(); err != nil {
				return total, total > 0, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, total > 0, err
	}
	if err := flush(); err != nil {
		return total, total > 0, err
	}
	return total, total > 0, nil
}

func (s *Store) RefreshAnalyticsProjections(ctx context.Context, ids []string) error {
	ids = uniqStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	placeholders := placeholders(len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	query := analyticsProjectionInsertSQL(placeholders)
	insertArgs := append([]any{projectionRefreshID()}, args...)
	insertArgs = append(insertArgs, args...)
	_, err := s.DB.ExecContext(ctx, query, insertArgs...)
	return err
}

func projectionRefreshID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func analyticsProjectionInsertSQL(placeholders string) string {
	return fmt.Sprintf(`INSERT INTO analytics_projection
		SELECT
			projected_session_id AS session_id,
			node_id,
			collector_id,
			source_id,
			source_name,
			runtime,
			format,
			%[2]s AS project_key,
			project_path,
			toStartOfMinute(timestamp) AS minute,
			provider,
			model,
			tool_name,
			event_kind,
			count() AS event_count,
			countIf(input_tokens + output_tokens > 0) AS call_count,
			countIf(event_kind = 'tool_call') AS tool_call_count,
			countIf(event_kind = 'tool_result') AS tool_result_count,
			sum(input_tokens) AS input_tokens_sum,
			sum(output_tokens) AS output_tokens_sum,
			sum(cache_read_tokens) AS cache_read_tokens_sum,
			sum(cache_create_tokens) AS cache_create_tokens_sum,
			sum(input_tokens + output_tokens) AS total_tokens_sum,
			sum(duration_ms) AS duration_ms_total,
			sum(cost_usd) AS cost_usd_sum,
			? AS refresh_id,
			now64(9) AS updated_at
		FROM (
			SELECT latest_events.*,
			       if(cwd != '', cwd, COALESCE(NULLIF(sp.project_path, ''), '')) AS project_path
			FROM (
				SELECT event_uid,
				       argMax(session_id, captured_at) AS projected_session_id,
				       argMax(node_id, captured_at) AS node_id,
				       argMax(collector_id, captured_at) AS collector_id,
				       argMax(source_id, captured_at) AS source_id,
				       argMax(source_name, captured_at) AS source_name,
				       argMax(runtime, captured_at) AS runtime,
				       argMax(format, captured_at) AS format,
				       argMax(provider, captured_at) AS provider,
				       argMax(timestamp, captured_at) AS timestamp,
				       argMax(event_kind, captured_at) AS event_kind,
				       argMax(tool_name, captured_at) AS tool_name,
				       argMax(model, captured_at) AS model,
				       argMax(input_tokens, captured_at) AS input_tokens,
				       argMax(output_tokens, captured_at) AS output_tokens,
				       argMax(cache_read_tokens, captured_at) AS cache_read_tokens,
				       argMax(cache_create_tokens, captured_at) AS cache_create_tokens,
				       argMax(duration_ms, captured_at) AS duration_ms,
				       argMax(cost_usd, captured_at) AS cost_usd,
				       argMax(cwd, captured_at) AS cwd
				FROM activity_events
				WHERE session_id IN (%[1]s)
				GROUP BY event_uid
			) AS latest_events
			LEFT JOIN `+sessionProjectFallbackSQL(placeholders)+` AS sp ON sp.session_id = latest_events.projected_session_id
		)
		GROUP BY projected_session_id, node_id, collector_id, source_id, source_name, runtime, format, project_key, project_path, minute, provider, model, tool_name, event_kind`, placeholders, projectKeySQL("project_path"))
}

func projectKeySQL(pathExpr string) string {
	return fmt.Sprintf(`if(%[1]s = '', '',
				replaceRegexpOne(
					if(position(%[1]s, '/.claude/worktrees/') > 0,
						substring(%[1]s, 1, position(%[1]s, '/.claude/worktrees/') - 1),
						replaceRegexpOne(%[1]s, '/+$', '')
					),
					'^.*/',
					''
				)
			)`, pathExpr)
}
