package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

var validIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var tableNames = []string{
	"raw_records",
	"activity_events",
	"event_links",
	"tool_payloads",
	"capture_errors",
	"capture_checkpoints",
	"capture_heartbeats",
	"session_projection",
	"analytics_projection",
	"search_documents",
	"search_postings",
	"search_query_log",
}

func Migrate(ctx context.Context, db *sql.DB, database string) error {
	database = cleanIdent(database)
	for _, stmt := range Schema(database) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func Reset(ctx context.Context, db *sql.DB, database string) error {
	database = cleanIdent(database)
	for i := len(tableNames) - 1; i >= 0; i-- {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", database, tableNames[i])); err != nil {
			return err
		}
	}
	return Migrate(ctx, db, database)
}

func Schema(database string) []string {
	database = cleanIdent(database)
	db := func(table string) string { return database + "." + table }

	return []string{
		`CREATE DATABASE IF NOT EXISTS ` + database,

		`CREATE TABLE IF NOT EXISTS ` + db("raw_records") + ` (
			record_uid String,
			source_name LowCardinality(String),
			runtime LowCardinality(String),
			provider LowCardinality(String),
			format LowCardinality(String),
			source_file String,
			source_line_no UInt32,
			source_offset UInt64,
			source_generation UInt32,
			session_id String,
			payload_json String CODEC(ZSTD(3)),
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(captured_at)
		ORDER BY (source_name, source_file, source_generation, source_offset, source_line_no, record_uid)`,

		`CREATE TABLE IF NOT EXISTS ` + db("activity_events") + ` (
			event_uid String,
			session_id String,
			parent_session_id String,
			source_name LowCardinality(String),
			runtime LowCardinality(String),
			provider LowCardinality(String),
			format LowCardinality(String),
			event_kind LowCardinality(String),
			payload_type LowCardinality(String),
			actor_role LowCardinality(String),
			timestamp DateTime64(3, 'UTC'),
			text_content String CODEC(ZSTD(3)),
			text_preview String,
			tool_name LowCardinality(String),
			tool_use_id String,
			model LowCardinality(String),
			input_tokens UInt64,
			output_tokens UInt64,
			cache_read_tokens UInt64,
			cache_create_tokens UInt64,
			duration_ms UInt64,
			cost_usd Float64,
			error_code LowCardinality(String),
			error_message String,
			event_version UInt16,
			payload_json String CODEC(ZSTD(3)),
			cwd String,
			source_file String,
			source_line_no UInt32,
			source_offset UInt64,
			source_generation UInt32,
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3),
			INDEX idx_session session_id TYPE bloom_filter(0.01) GRANULARITY 2,
			INDEX idx_tool tool_name TYPE set(256) GRANULARITY 4
		)
		ENGINE = ReplacingMergeTree(captured_at)
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (session_id, timestamp, event_uid)`,

		`CREATE TABLE IF NOT EXISTS ` + db("event_links") + ` (
			event_uid String,
			linked_event_uid String,
			link_type LowCardinality(String),
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(captured_at)
		ORDER BY (event_uid, linked_event_uid, link_type)`,

		`CREATE TABLE IF NOT EXISTS ` + db("tool_payloads") + ` (
			event_uid String,
			tool_name LowCardinality(String),
			tool_phase LowCardinality(String),
			input_json String CODEC(ZSTD(3)),
			output_json String CODEC(ZSTD(3)),
			input_preview String,
			output_preview String,
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(captured_at)
		ORDER BY event_uid`,

		`CREATE TABLE IF NOT EXISTS ` + db("capture_errors") + ` (
			id String,
			source_name LowCardinality(String),
			source_file String,
			source_line_no UInt32,
			source_offset UInt64,
			error_class LowCardinality(String),
			error_message String,
			context_fragment String,
			created_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = MergeTree
		ORDER BY (source_name, source_file, source_line_no, id)`,

		`CREATE TABLE IF NOT EXISTS ` + db("capture_checkpoints") + ` (
			source_name LowCardinality(String),
			source_file String,
			source_inode UInt64,
			source_generation UInt32,
			last_offset UInt64,
			last_line_no UInt32,
			state_json String DEFAULT '',
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY (source_name, source_file)`,

		`ALTER TABLE ` + db("capture_checkpoints") + ` ADD COLUMN IF NOT EXISTS state_json String DEFAULT ''`,

		`CREATE TABLE IF NOT EXISTS ` + db("capture_heartbeats") + ` (
			source_name LowCardinality(String),
			queue_depth UInt32,
			active_files UInt32,
			append_to_visible_ms UInt64,
			created_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = MergeTree
		ORDER BY (created_at, source_name)`,

		`CREATE TABLE IF NOT EXISTS ` + db("session_projection") + ` (
			session_id String,
			source_name LowCardinality(String),
			provider LowCardinality(String),
			started_at DateTime64(3, 'UTC'),
			ended_at DateTime64(3, 'UTC'),
			event_count UInt64,
			turn_count UInt64,
			total_input_tokens UInt64,
			total_output_tokens UInt64,
			total_cache_read_tokens UInt64,
			total_cache_create_tokens UInt64,
			total_tokens UInt64,
			tool_call_count UInt64,
			mcp_call_count UInt64,
			error_count UInt64,
			last_model LowCardinality(String),
			working_dir String,
			parent_session_id String,
			has_session_end UInt8,
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY session_id`,

		`CREATE TABLE IF NOT EXISTS ` + db("analytics_projection") + ` (
			session_id String,
			minute DateTime64(3, 'UTC'),
			provider LowCardinality(String),
			model LowCardinality(String),
			tool_name LowCardinality(String),
			event_kind LowCardinality(String),
			event_count UInt64,
			call_count UInt64,
			tool_call_count UInt64,
			tool_result_count UInt64,
			input_tokens UInt64,
			output_tokens UInt64,
			cache_read_tokens UInt64,
			cache_create_tokens UInt64,
			total_tokens UInt64,
			duration_ms_sum UInt64,
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(updated_at)
		PARTITION BY toYYYYMM(minute)
		ORDER BY (session_id, minute, provider, model, tool_name, event_kind)`,

		`CREATE TABLE IF NOT EXISTS ` + db("search_documents") + ` (
			event_uid String,
			session_id String,
			event_kind LowCardinality(String),
			timestamp DateTime64(3, 'UTC'),
			text_preview String,
			tool_name LowCardinality(String),
			model LowCardinality(String),
			provider LowCardinality(String),
			searchable_text String CODEC(ZSTD(3)),
			document_len UInt32,
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3),
			INDEX idx_search_text searchable_text TYPE tokenbf_v1(8192, 3, 0) GRANULARITY 4
		)
		ENGINE = ReplacingMergeTree(updated_at)
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY event_uid`,

		`CREATE TABLE IF NOT EXISTS ` + db("search_postings") + ` (
			token String,
			event_uid String,
			session_id String,
			event_kind LowCardinality(String),
			timestamp DateTime64(3, 'UTC'),
			term_frequency UInt32,
			document_len UInt32,
			text_preview String,
			tool_name LowCardinality(String),
			model LowCardinality(String),
			provider LowCardinality(String),
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY (token, event_uid)`,

		`CREATE TABLE IF NOT EXISTS ` + db("search_query_log") + ` (
			query String,
			normalized_terms Array(String),
			result_count UInt32,
			duration_ms UInt64,
			created_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = MergeTree
		ORDER BY created_at`,
	}
}

func cleanIdent(v string) string {
	v = strings.TrimSpace(v)
	if validIdent.MatchString(v) {
		return v
	}
	return DefaultDatabase
}
