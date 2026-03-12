package database

import "context"

func (db *DB) migrate() error {
	ddl := []string{
		// Drop old tables and views from v1 schema
		`DROP TABLE IF EXISTS actors`,
		`DROP TABLE IF EXISTS sessions`,
		`DROP TABLE IF EXISTS turns`,
		`DROP TABLE IF EXISTS model_calls`,
		`DROP TABLE IF EXISTS tool_calls`,
		`DROP TABLE IF EXISTS api_errors`,
		`DROP TABLE IF EXISTS context_snapshots`,
		`DROP TABLE IF EXISTS documents`,
		`DROP TABLE IF EXISTS raw_events`,
		`DROP VIEW IF EXISTS tokens_per_minute`,
		`DROP VIEW IF EXISTS tool_success_rates`,
		`DROP VIEW IF EXISTS session_summaries`,
		`DROP VIEW IF EXISTS hourly_costs`,

		// Core event table
		`CREATE TABLE IF NOT EXISTS events (
			event_uid VARCHAR PRIMARY KEY,
			session_id VARCHAR NOT NULL,
			session_date DATE,
			source_name VARCHAR,
			provider VARCHAR,
			event_kind VARCHAR NOT NULL,
			payload_type VARCHAR,
			actor_role VARCHAR,
			timestamp TIMESTAMP NOT NULL,
			text_content VARCHAR,
			text_preview VARCHAR,
			tool_name VARCHAR,
			model VARCHAR,
			input_tokens BIGINT DEFAULT 0,
			output_tokens BIGINT DEFAULT 0,
			cache_read_tokens BIGINT DEFAULT 0,
			cache_create_tokens BIGINT DEFAULT 0,
			duration_ms BIGINT DEFAULT 0,
			cost_usd DOUBLE DEFAULT 0.0,
			error_code VARCHAR,
			error_message VARCHAR,
			event_version INTEGER DEFAULT 1,
			payload_json VARCHAR,
			cwd VARCHAR,
			source_file VARCHAR,
			source_line_no INTEGER,
			source_offset BIGINT,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		// Add cwd column if not exists (for existing databases)
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS cwd VARCHAR`,

		// Add parent_session_id column for subagent support
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS parent_session_id VARCHAR DEFAULT ''`,

		// Add tool_use_id column for matching tool_call/tool_result pairs
		`ALTER TABLE events ADD COLUMN IF NOT EXISTS tool_use_id VARCHAR DEFAULT ''`,

		`CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_kind ON events(event_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session_kind ON events(session_id, event_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_events_kind_ts ON events(event_kind, timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_parent_session ON events(parent_session_id)`,

		// Parent-child relationships
		`CREATE TABLE IF NOT EXISTS event_links (
			event_uid VARCHAR NOT NULL,
			linked_event_uid VARCHAR NOT NULL,
			link_type VARCHAR NOT NULL,
			PRIMARY KEY (event_uid, linked_event_uid, link_type)
		)`,

		// Separated tool I/O
		`CREATE TABLE IF NOT EXISTS tool_io (
			event_uid VARCHAR PRIMARY KEY,
			tool_name VARCHAR,
			tool_phase VARCHAR,
			input_json VARCHAR,
			output_json VARCHAR,
			input_preview VARCHAR,
			output_preview VARCHAR
		)`,

		// Ingest error tracking
		`CREATE TABLE IF NOT EXISTS ingest_errors (
			id VARCHAR PRIMARY KEY,
			source_file VARCHAR,
			source_line_no INTEGER,
			error_class VARCHAR,
			error_message VARCHAR,
			context_fragment VARCHAR,
			created_at TIMESTAMP DEFAULT current_timestamp
		)`,

		// Checkpoint table for crash recovery
		`CREATE TABLE IF NOT EXISTS ingest_checkpoints (
			source_name VARCHAR,
			source_file VARCHAR,
			source_inode BIGINT,
			source_generation INTEGER DEFAULT 0,
			last_offset BIGINT DEFAULT 0,
			last_line_no INTEGER DEFAULT 0,
			updated_at TIMESTAMP DEFAULT current_timestamp,
			PRIMARY KEY (source_name, source_file)
		)`,

		// Session-level aggregates
		`CREATE OR REPLACE VIEW v_session_summary AS
		SELECT
			session_id, source_name,
			COALESCE(MIN(CASE WHEN timestamp > '2000-01-01' THEN timestamp END), MIN(timestamp)) AS started_at,
			COALESCE(MAX(CASE WHEN timestamp > '2000-01-01' THEN timestamp END), MIN(timestamp)) AS ended_at,
			COUNT(*) AS event_count,
			COUNT(DISTINCT CASE WHEN event_kind = 'message' AND actor_role = 'user' THEN event_uid END) AS turn_count,
			COALESCE(SUM(input_tokens), 0) AS total_input_tokens,
			COALESCE(SUM(output_tokens), 0) AS total_output_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS total_cache_read_tokens,
			COALESCE(SUM(cache_create_tokens), 0) AS total_cache_create_tokens,
			COALESCE(SUM(input_tokens + output_tokens), 0) AS total_tokens,
			COUNT(CASE WHEN event_kind = 'tool_call' THEN 1 END) AS tool_call_count,
			COUNT(CASE WHEN event_kind = 'tool_call' AND tool_name LIKE 'mcp__%' THEN 1 END) AS mcp_call_count,
			COUNT(CASE WHEN event_kind = 'error' THEN 1 END) AS error_count,
			COALESCE(MAX(model), '') AS last_model,
			COALESCE(MAX(cwd), '') AS working_dir,
			COALESCE(MAX(parent_session_id), '') AS parent_session_id,
			MAX(CASE WHEN (event_kind = 'session_end' AND payload_type = 'last-prompt')
			           OR (event_kind = 'event_msg' AND payload_type = 'last-prompt')
			         THEN 1 ELSE 0 END) AS has_session_end,
			COALESCE(MAX(provider), '') AS provider
		FROM events GROUP BY session_id, source_name`,

		// Conversation trace with turn sequencing
		`CREATE OR REPLACE VIEW v_conversation_trace AS
		SELECT
			e.*,
			ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS event_order,
			SUM(CASE WHEN event_kind = 'message' AND actor_role = 'user' THEN 1 ELSE 0 END)
				OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS turn_seq
		FROM events e`,

		// Time-series token usage (excludes zero-timestamp events)
		`CREATE OR REPLACE VIEW v_tokens_per_minute AS
		SELECT
			time_bucket(INTERVAL '1 minute', timestamp) AS minute,
			SUM(input_tokens) AS total_input,
			SUM(output_tokens) AS total_output,
			SUM(cache_read_tokens) AS total_cache_read,
			SUM(cache_create_tokens) AS total_cache_create,
			SUM(input_tokens + output_tokens) AS total_tokens,
			COUNT(CASE WHEN input_tokens + output_tokens > 0 THEN 1 END) AS call_count
		FROM events WHERE timestamp > '2000-01-01' GROUP BY minute ORDER BY minute DESC`,

		// Tool usage stats
		`CREATE OR REPLACE VIEW v_tool_stats AS
		SELECT
			tool_name,
			COUNT(CASE WHEN event_kind = 'tool_call' THEN 1 END) AS calls,
			COUNT(CASE WHEN event_kind = 'tool_result' THEN 1 END) AS results,
			COUNT(*) AS total,
			AVG(duration_ms) AS avg_duration_ms
		FROM events
		WHERE tool_name IS NOT NULL AND tool_name != ''
		GROUP BY tool_name ORDER BY total DESC`,

		// Token usage by model
		`CREATE OR REPLACE VIEW v_tokens_by_model AS
		SELECT
			COALESCE(model, 'unknown') AS model,
			SUM(input_tokens) AS total_input,
			SUM(output_tokens) AS total_output,
			SUM(cache_read_tokens) AS total_cache_read,
			SUM(cache_create_tokens) AS total_cache_create,
			SUM(input_tokens + output_tokens) AS total_tokens,
			COUNT(CASE WHEN input_tokens + output_tokens > 0 THEN 1 END) AS call_count
		FROM events
		WHERE model IS NOT NULL AND model != '' AND model != '<synthetic>'
		GROUP BY model ORDER BY total_tokens DESC`,
	}

	// Load FTS extension (errors ignored — may already be installed/loaded)
	db.writeConn.ExecContext(context.Background(), "INSTALL fts") //nolint:errcheck
	db.writeConn.ExecContext(context.Background(), "LOAD fts")    //nolint:errcheck

	for _, stmt := range ddl {
		if _, err := db.writeConn.ExecContext(context.Background(), stmt); err != nil {
			return err
		}
	}
	return nil
}
