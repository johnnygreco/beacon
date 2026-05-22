package store

import (
	"context"
	"fmt"
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
			projected_session_id AS session_id,
			argMax(source_name, timestamp) AS source_name,
			argMax(provider, timestamp) AS provider,
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
			argMaxIf(model, timestamp, model != '') AS last_model,
			argMaxIf(cwd, timestamp, cwd != '') AS working_dir,
			argMaxIf(parent_session_id, timestamp, parent_session_id != '') AS parent_session_id,
			max(if(%[2]s, 1, 0)) AS has_session_end,
			now64(3) AS updated_at
		FROM (
			SELECT event_uid,
			       argMax(session_id, captured_at) AS projected_session_id,
			       argMax(source_name, captured_at) AS source_name,
			       argMax(provider, captured_at) AS provider,
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
			       argMax(parent_session_id, captured_at) AS parent_session_id
			FROM activity_events
			WHERE session_id IN (%[3]s)
			GROUP BY event_uid
		)
		GROUP BY projected_session_id`, validEventTimestampPredicate, sessionEndProjectionPredicate, placeholders)
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
		if err := s.RefreshSessionProjections(ctx, batch); err != nil {
			return fmt.Errorf("refresh session projections: %w", err)
		}
		if err := s.RefreshAnalyticsProjections(ctx, batch); err != nil {
			return fmt.Errorf("refresh analytics projections: %w", err)
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
	_, err := s.DB.ExecContext(ctx, query, args...)
	return err
}

func analyticsProjectionInsertSQL(placeholders string) string {
	return fmt.Sprintf(`INSERT INTO analytics_projection
		SELECT
			projected_session_id AS session_id,
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
			now64(3) AS updated_at
		FROM (
			SELECT event_uid,
			       argMax(session_id, captured_at) AS projected_session_id,
			       argMax(provider, captured_at) AS provider,
			       argMax(timestamp, captured_at) AS timestamp,
			       argMax(event_kind, captured_at) AS event_kind,
			       argMax(tool_name, captured_at) AS tool_name,
			       argMax(model, captured_at) AS model,
			       argMax(input_tokens, captured_at) AS input_tokens,
			       argMax(output_tokens, captured_at) AS output_tokens,
			       argMax(cache_read_tokens, captured_at) AS cache_read_tokens,
			       argMax(cache_create_tokens, captured_at) AS cache_create_tokens,
			       argMax(duration_ms, captured_at) AS duration_ms
			FROM activity_events
			WHERE session_id IN (%s)
			GROUP BY event_uid
		)
		GROUP BY projected_session_id, minute, provider, model, tool_name, event_kind`, placeholders)
}
