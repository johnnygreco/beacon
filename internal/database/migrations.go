package database

import "context"

func (db *DB) migrate() error {
	ddl := []string{
		// Core tables
		`CREATE TABLE IF NOT EXISTS actors (
			id VARCHAR PRIMARY KEY,
			user_id VARCHAR,
			org_team VARCHAR,
			machine_id VARCHAR,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id VARCHAR PRIMARY KEY,
			actor_id VARCHAR,
			source VARCHAR,
			started_at TIMESTAMP DEFAULT current_timestamp,
			ended_at TIMESTAMP,
			cwd VARCHAR,
			git_repo VARCHAR,
			total_cost DOUBLE DEFAULT 0.0
		)`,

		`CREATE TABLE IF NOT EXISTS turns (
			id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			turn_number INTEGER,
			user_prompt VARCHAR,
			started_at TIMESTAMP DEFAULT current_timestamp,
			ended_at TIMESTAMP,
			input_tokens BIGINT DEFAULT 0,
			output_tokens BIGINT DEFAULT 0,
			cache_read BIGINT DEFAULT 0,
			cache_create BIGINT DEFAULT 0,
			cost_usd DOUBLE DEFAULT 0.0
		)`,

		`CREATE TABLE IF NOT EXISTS model_calls (
			id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			turn_id VARCHAR,
			model VARCHAR,
			provider VARCHAR,
			input_tokens BIGINT DEFAULT 0,
			output_tokens BIGINT DEFAULT 0,
			cache_read BIGINT DEFAULT 0,
			cache_create BIGINT DEFAULT 0,
			duration_ms BIGINT DEFAULT 0,
			status_code INTEGER DEFAULT 200,
			cost_usd DOUBLE DEFAULT 0.0,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		`CREATE TABLE IF NOT EXISTS tool_calls (
			id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			turn_id VARCHAR,
			tool_name VARCHAR,
			input VARCHAR,
			output VARCHAR,
			success BOOLEAN DEFAULT true,
			duration_ms BIGINT DEFAULT 0,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		`CREATE TABLE IF NOT EXISTS api_errors (
			id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			turn_id VARCHAR,
			error_code VARCHAR,
			error_class VARCHAR,
			message VARCHAR,
			provider VARCHAR,
			retry_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		`CREATE TABLE IF NOT EXISTS context_snapshots (
			id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			turn_id VARCHAR,
			tokens_in_context BIGINT DEFAULT 0,
			max_tokens BIGINT DEFAULT 0,
			headroom BIGINT DEFAULT 0,
			compaction_event BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		`CREATE TABLE IF NOT EXISTS documents (
			id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			turn_id VARCHAR,
			doc_type VARCHAR,
			content VARCHAR,
			embedding FLOAT[],
			embedding_model VARCHAR,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		`CREATE TABLE IF NOT EXISTS raw_events (
			id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			source VARCHAR,
			event_type VARCHAR,
			payload VARCHAR,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		// Analytical views
		`CREATE OR REPLACE VIEW tokens_per_minute AS
		SELECT
			time_bucket(INTERVAL '1 minute', created_at) AS minute,
			SUM(input_tokens) AS total_input,
			SUM(output_tokens) AS total_output,
			SUM(input_tokens + output_tokens) AS total_tokens,
			SUM(cost_usd) AS total_cost,
			COUNT(*) AS call_count
		FROM model_calls
		GROUP BY minute
		ORDER BY minute DESC`,

		`CREATE OR REPLACE VIEW tool_success_rates AS
		SELECT
			tool_name,
			COUNT(*) AS total_calls,
			SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes,
			SUM(CASE WHEN NOT success THEN 1 ELSE 0 END) AS failures,
			ROUND(100.0 * SUM(CASE WHEN success THEN 1 ELSE 0 END) / COUNT(*), 2) AS success_rate,
			AVG(duration_ms) AS avg_duration_ms
		FROM tool_calls
		GROUP BY tool_name
		ORDER BY total_calls DESC`,

		`CREATE OR REPLACE VIEW session_summaries AS
		SELECT
			s.id,
			s.source,
			s.started_at,
			s.ended_at,
			s.cwd,
			s.git_repo,
			s.total_cost,
			COUNT(DISTINCT t.id) AS turn_count,
			COALESCE(SUM(mc.input_tokens + mc.output_tokens), 0) AS total_tokens,
			COALESCE(SUM(mc.cost_usd), 0) AS computed_cost,
			(SELECT COUNT(*) FROM api_errors ae WHERE ae.session_id = s.id) AS error_count
		FROM sessions s
		LEFT JOIN turns t ON t.session_id = s.id
		LEFT JOIN model_calls mc ON mc.session_id = s.id
		GROUP BY s.id, s.source, s.started_at, s.ended_at, s.cwd, s.git_repo, s.total_cost`,

		`CREATE OR REPLACE VIEW hourly_costs AS
		SELECT
			time_bucket(INTERVAL '1 hour', created_at) AS hour,
			provider,
			model,
			SUM(cost_usd) AS total_cost,
			SUM(input_tokens) AS total_input,
			SUM(output_tokens) AS total_output,
			COUNT(*) AS call_count
		FROM model_calls
		GROUP BY hour, provider, model
		ORDER BY hour DESC`,
	}

	for _, stmt := range ddl {
		if _, err := db.writeConn.ExecContext(context.Background(), stmt); err != nil {
			return err
		}
	}
	return nil
}
