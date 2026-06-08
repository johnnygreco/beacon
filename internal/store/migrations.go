package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

const (
	CurrentSchemaVersion = 6

	schemaVersionTable = "schema_version"
	schemaVersionRowID = 1
)

var validIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var dataTableNames = []string{
	"raw_records",
	"activity_events",
	"event_links",
	"tool_payloads",
	"capture_errors",
	"capture_checkpoints",
	"capture_heartbeats",
	"ingest_batches",
	"session_projection",
	"analytics_projection",
	"search_documents",
	"search_postings",
	"search_query_log",
}

var tableNames = append([]string{schemaVersionTable}, dataTableNames...)

func Migrate(ctx context.Context, db *sql.DB, database string) error {
	database = cleanIdent(database)
	if _, err := db.ExecContext(ctx, `CREATE DATABASE IF NOT EXISTS `+database); err != nil {
		return err
	}
	state, err := inspectSchemaState(ctx, db, database)
	if err != nil {
		return err
	}
	if state.hasVersionRow {
		switch state.version {
		case 2:
			if err := migrateSchemaV2ToV6(ctx, db, database); err != nil {
				return err
			}
		case 3:
			if err := migrateSchemaV3ToV6(ctx, db, database); err != nil {
				return err
			}
		case 4:
			if err := migrateSchemaV4ToV6(ctx, db, database); err != nil {
				return err
			}
		case 5:
			if err := migrateSchemaV5ToV6(ctx, db, database); err != nil {
				return err
			}
		}
		state, err = inspectSchemaState(ctx, db, database)
		if err != nil {
			return err
		}
	}
	if err := validateSchemaState(state, database, true); err != nil {
		return err
	}
	for _, stmt := range Schema(database) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return writeSchemaVersion(ctx, db, database)
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

// DetectSchemaVersion returns Beacon's recorded ClickHouse schema version.
func DetectSchemaVersion(ctx context.Context, db *sql.DB, database string) (int, bool, error) {
	state, err := inspectSchemaState(ctx, db, cleanIdent(database))
	if err != nil {
		return 0, false, err
	}
	return state.version, state.hasVersionRow, nil
}

// RequireCurrentSchema fails when the configured database is missing Beacon's
// current schema marker or contains a version this build does not support.
func RequireCurrentSchema(ctx context.Context, db *sql.DB, database string) error {
	database = cleanIdent(database)
	state, err := inspectSchemaState(ctx, db, database)
	if err != nil {
		return err
	}
	return validateSchemaState(state, database, false)
}

type schemaState struct {
	hasVersionTable bool
	hasVersionRow   bool
	hasOwnedTables  bool
	version         int
}

type UnsupportedSchemaError struct {
	Database       string
	Detail         string
	CurrentVersion int
}

func (e *UnsupportedSchemaError) Error() string {
	return fmt.Sprintf(
		"unsupported Beacon ClickHouse schema in database %q (%s); this build supports schema version %d. Run `beacon db reset --force` to drop and recreate Beacon tables.",
		e.Database,
		e.Detail,
		e.CurrentVersion,
	)
}

func inspectSchemaState(ctx context.Context, db *sql.DB, database string) (schemaState, error) {
	var state schemaState
	rows, err := db.QueryContext(ctx, `SELECT name FROM system.tables WHERE database = ?`, database)
	if err != nil {
		return state, err
	}
	defer rows.Close()

	ownedDataTables := make(map[string]bool, len(dataTableNames))
	for _, table := range dataTableNames {
		ownedDataTables[table] = true
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return state, err
		}
		if name == schemaVersionTable {
			state.hasVersionTable = true
		}
		if ownedDataTables[name] {
			state.hasOwnedTables = true
		}
	}
	if err := rows.Err(); err != nil {
		return state, err
	}

	if state.hasVersionTable {
		version, ok, err := readSchemaVersion(ctx, db, database)
		if err != nil {
			return state, err
		}
		state.version = version
		state.hasVersionRow = ok
	}
	return state, nil
}

func validateSchemaState(state schemaState, database string, allowEmpty bool) error {
	switch {
	case state.hasVersionRow && state.version == CurrentSchemaVersion:
		return nil
	case state.hasVersionRow:
		return &UnsupportedSchemaError{
			Database:       database,
			Detail:         fmt.Sprintf("found version %d", state.version),
			CurrentVersion: CurrentSchemaVersion,
		}
	case state.hasVersionTable:
		return &UnsupportedSchemaError{
			Database:       database,
			Detail:         "schema_version table has no version row",
			CurrentVersion: CurrentSchemaVersion,
		}
	case state.hasOwnedTables:
		return &UnsupportedSchemaError{
			Database:       database,
			Detail:         "legacy Beacon tables are missing schema_version",
			CurrentVersion: CurrentSchemaVersion,
		}
	case allowEmpty:
		return nil
	default:
		return fmt.Errorf("Beacon ClickHouse schema is not initialized in database %q; run `beacon db migrate` or `beacon db reset --force`", database)
	}
}

func readSchemaVersion(ctx context.Context, db *sql.DB, database string) (int, bool, error) {
	var rows uint64
	var version uint32
	err := db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(), if(count() = 0, toUInt32(0), argMax(version, updated_at)) FROM %s.%s WHERE id = ?`, database, schemaVersionTable),
		schemaVersionRowID,
	).Scan(&rows, &version)
	if err != nil {
		return 0, false, err
	}
	return int(version), rows > 0, nil
}

func writeSchemaVersion(ctx context.Context, db *sql.DB, database string) error {
	_, err := db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s.%s (id, version) VALUES (?, ?)`, database, schemaVersionTable),
		schemaVersionRowID,
		CurrentSchemaVersion,
	)
	return err
}

func migrateSchemaV2ToV6(ctx context.Context, db *sql.DB, database string) error {
	for _, stmt := range []string{
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.capture_checkpoints`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.capture_errors`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.capture_heartbeats`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.ingest_batches`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.session_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.analytics_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_documents`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_postings`, database),
		captureErrorsSchema(database),
		captureCheckpointsSchema(database),
		captureHeartbeatsSchema(database),
		ingestBatchesSchema(database),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("advance schema v2 to v6: %w", err)
		}
	}
	return writeSchemaVersion(ctx, db, database)
}

func migrateSchemaV3ToV6(ctx context.Context, db *sql.DB, database string) error {
	for _, stmt := range []string{
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.capture_checkpoints`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.capture_errors`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.capture_heartbeats`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.ingest_batches`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.session_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.analytics_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_documents`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_postings`, database),
		captureErrorsSchema(database),
		captureCheckpointsSchema(database),
		captureHeartbeatsSchema(database),
		ingestBatchesSchema(database),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("advance schema v3 to v6: %w", err)
		}
	}
	return writeSchemaVersion(ctx, db, database)
}

func migrateSchemaV4ToV6(ctx context.Context, db *sql.DB, database string) error {
	for _, stmt := range []string{
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.capture_heartbeats`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.session_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.analytics_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_documents`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_postings`, database),
		captureHeartbeatsSchema(database),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("advance schema v4 to v6: %w", err)
		}
	}
	return writeSchemaVersion(ctx, db, database)
}

func migrateSchemaV5ToV6(ctx context.Context, db *sql.DB, database string) error {
	for _, stmt := range []string{
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.session_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.analytics_projection`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_documents`, database),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s.search_postings`, database),
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("advance schema v5 to v6: %w", err)
		}
	}
	return writeSchemaVersion(ctx, db, database)
}

func Schema(database string) []string {
	database = cleanIdent(database)
	db := func(table string) string { return database + "." + table }

	return []string{
		`CREATE DATABASE IF NOT EXISTS ` + database,

		`CREATE TABLE IF NOT EXISTS ` + db(schemaVersionTable) + ` (
			id UInt8,
			version UInt32,
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY id`,

		`CREATE TABLE IF NOT EXISTS ` + db("raw_records") + ` (
			record_uid String,
			event_uid String,
			node_id String,
			collector_id String,
			source_id String,
			source_name LowCardinality(String),
			runtime LowCardinality(String),
			provider LowCardinality(String),
			format LowCardinality(String),
			source_file String,
			source_line_no UInt32,
			source_offset UInt64,
			source_generation UInt32,
			session_id String,
			raw_session_id String,
			raw_event_id String,
			source_event_index UInt64,
			batch_id String,
			control_plane_epoch String,
			payload_digest String,
			redaction_status LowCardinality(String),
			redaction_version String,
			payload_json String CODEC(ZSTD(3)),
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(captured_at)
		ORDER BY (collector_id, source_id, source_file, source_generation, source_event_index, record_uid)`,

		`CREATE TABLE IF NOT EXISTS ` + db("activity_events") + ` (
			event_uid String,
			session_id String,
			raw_session_id String,
			parent_session_id String,
			raw_parent_session_id String,
			node_id String,
			collector_id String,
			source_id String,
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
			raw_event_id String,
			source_event_index UInt64,
			batch_id String,
			control_plane_epoch String,
			payload_digest String,
			redaction_status LowCardinality(String),
			redaction_version String,
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3),
			INDEX idx_session session_id TYPE bloom_filter(0.01) GRANULARITY 2,
			INDEX idx_collector collector_id TYPE bloom_filter(0.01) GRANULARITY 2,
			INDEX idx_source source_id TYPE bloom_filter(0.01) GRANULARITY 2,
			INDEX idx_tool tool_name TYPE set(256) GRANULARITY 4
		)
		ENGINE = ReplacingMergeTree(captured_at)
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (session_id, collector_id, source_id, timestamp, event_uid)`,

		`CREATE TABLE IF NOT EXISTS ` + db("event_links") + ` (
			event_uid String,
			linked_event_uid String,
			link_type LowCardinality(String),
			link_scope LowCardinality(String),
			resolution_status LowCardinality(String),
			session_id String,
			raw_session_id String,
			linked_session_id String,
			raw_linked_session_id String,
			raw_linked_event_id String,
			collector_id String,
			source_id String,
			batch_id String,
			control_plane_epoch String,
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(captured_at)
		ORDER BY (collector_id, source_id, event_uid, link_type, raw_linked_session_id, raw_linked_event_id)`,

		`CREATE TABLE IF NOT EXISTS ` + db("tool_payloads") + ` (
			event_uid String,
			collector_id String,
			source_id String,
			tool_name LowCardinality(String),
			tool_phase LowCardinality(String),
			input_json String CODEC(ZSTD(3)),
			output_json String CODEC(ZSTD(3)),
			input_preview String,
			output_preview String,
			batch_id String,
			control_plane_epoch String,
			payload_digest String,
			redaction_status LowCardinality(String),
			redaction_version String,
			captured_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(captured_at)
		ORDER BY (event_uid, collector_id, source_id)`,

		captureErrorsSchema(database),

		captureCheckpointsSchema(database),

		captureHeartbeatsSchema(database),

		ingestBatchesSchema(database),

		`CREATE TABLE IF NOT EXISTS ` + db("session_projection") + ` (
			session_id String,
			node_id String,
			collector_id String,
			source_id String,
			source_name LowCardinality(String),
			runtime LowCardinality(String),
			provider LowCardinality(String),
			format LowCardinality(String),
			project_key String,
			project_path String,
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
			completion_state LowCardinality(String),
			total_cost_usd Float64,
			cost_event_count UInt64,
			cost_provenance LowCardinality(String),
			attention_score UInt16,
			attention_reasons Array(String),
			archive_reason LowCardinality(String),
			archived_at DateTime64(3, 'UTC'),
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY session_id`,

		`CREATE TABLE IF NOT EXISTS ` + db("analytics_projection") + ` (
			session_id String,
			node_id String,
			collector_id String,
			source_id String,
			source_name LowCardinality(String),
			runtime LowCardinality(String),
			format LowCardinality(String),
			project_key String,
			project_path String,
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
			cost_usd_sum Float64,
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
		)
		ENGINE = ReplacingMergeTree(updated_at)
		PARTITION BY toYYYYMM(minute)
		ORDER BY (collector_id, source_id, project_key, session_id, minute, provider, model, tool_name, event_kind)`,

		`CREATE TABLE IF NOT EXISTS ` + db("search_documents") + ` (
			event_uid String,
			session_id String,
			node_id String,
			collector_id String,
			source_id String,
			source_name LowCardinality(String),
			runtime LowCardinality(String),
			format LowCardinality(String),
			project_key String,
			project_path String,
			event_kind LowCardinality(String),
			timestamp DateTime64(3, 'UTC'),
			text_preview String,
			tool_name LowCardinality(String),
			model LowCardinality(String),
			provider LowCardinality(String),
			searchable_text String CODEC(ZSTD(3)),
			document_len UInt32,
			updated_at DateTime64(3, 'UTC') DEFAULT now64(3),
			INDEX idx_search_text searchable_text TYPE tokenbf_v1(8192, 3, 0) GRANULARITY 4,
			INDEX idx_collector collector_id TYPE bloom_filter(0.01) GRANULARITY 2,
			INDEX idx_source source_id TYPE bloom_filter(0.01) GRANULARITY 2,
			INDEX idx_project project_key TYPE set(1024) GRANULARITY 4
		)
		ENGINE = ReplacingMergeTree(updated_at)
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY event_uid`,

		`CREATE TABLE IF NOT EXISTS ` + db("search_postings") + ` (
			token String,
			event_uid String,
			session_id String,
			node_id String,
			collector_id String,
			source_id String,
			source_name LowCardinality(String),
			runtime LowCardinality(String),
			format LowCardinality(String),
			project_key String,
			project_path String,
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
		ORDER BY (token, collector_id, source_id, project_key, event_uid)`,

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

func captureCheckpointsSchema(database string) string {
	db := cleanIdent(database) + ".capture_checkpoints"
	return `CREATE TABLE IF NOT EXISTS ` + db + ` (
			node_id String,
			collector_id String,
			source_id String,
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
			ORDER BY (collector_id, source_id, source_name, source_file)`
}

func captureErrorsSchema(database string) string {
	db := cleanIdent(database) + ".capture_errors"
	return `CREATE TABLE IF NOT EXISTS ` + db + ` (
				id String,
				node_id String,
				collector_id String,
				source_id String,
				source_name LowCardinality(String),
				source_file String,
				source_line_no UInt32,
				source_offset UInt64,
				batch_id String,
				control_plane_epoch String,
				error_class LowCardinality(String),
				error_message String,
				context_fragment String,
				created_at DateTime64(3, 'UTC') DEFAULT now64(3)
			)
			ENGINE = ReplacingMergeTree(created_at)
				ORDER BY (collector_id, batch_id, id)`
}

func captureHeartbeatsSchema(database string) string {
	db := cleanIdent(database) + ".capture_heartbeats"
	return `CREATE TABLE IF NOT EXISTS ` + db + ` (
				node_id String,
				collector_id String,
				source_id String,
				source_name LowCardinality(String),
				control_plane_epoch String,
				status LowCardinality(String),
				queue_depth UInt32,
				spool_bytes UInt64,
				active_files UInt32,
				error_count UInt64,
				last_event_at Nullable(DateTime64(3, 'UTC')),
				append_to_visible_ms UInt64,
				created_at DateTime64(3, 'UTC') DEFAULT now64(3)
			)
			ENGINE = ReplacingMergeTree(created_at)
			ORDER BY (collector_id, source_id, created_at)`
}

func ingestBatchesSchema(database string) string {
	db := cleanIdent(database) + ".ingest_batches"
	return `CREATE TABLE IF NOT EXISTS ` + db + ` (
			collector_id String,
			batch_id String,
			node_id String,
			sequence UInt64,
			control_plane_epoch String,
			payload_digest String,
				redaction_version String,
				created_at DateTime64(3, 'UTC'),
				received_at DateTime64(3, 'UTC'),
				state_version UInt64,
				event_count UInt64,
				raw_count UInt64,
			tool_payload_count UInt64,
			checkpoint_count UInt64,
			status LowCardinality(String),
			error_message String,
				committed_at Nullable(DateTime64(3, 'UTC')),
				updated_at DateTime64(3, 'UTC') DEFAULT now64(3)
			)
			ENGINE = MergeTree
			ORDER BY (collector_id, batch_id, state_version)`
}

func cleanIdent(v string) string {
	v = strings.TrimSpace(v)
	if validIdent.MatchString(v) {
		return v
	}
	return DefaultDatabase
}
