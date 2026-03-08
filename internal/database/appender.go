package database

import (
	"context"
	"database/sql"

	"github.com/johnnygreco/beacon/internal/models"
)

func InsertEvent(ctx context.Context, db *DB, e *models.Event) error {
	_, err := db.writeConn.ExecContext(ctx,
		`INSERT OR IGNORE INTO events (
			event_uid, session_id, session_date, source_name, provider,
			event_kind, payload_type, actor_role, timestamp,
			text_content, text_preview, tool_name, model,
			input_tokens, output_tokens, cache_read_tokens, cache_create_tokens,
			duration_ms, cost_usd, error_code, error_message,
			event_version, payload_json, cwd, source_file, source_line_no, source_offset
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.EventUID, e.SessionID, e.SessionDate, e.SourceName, e.Provider,
		e.EventKind, e.PayloadType, e.ActorRole, e.Timestamp,
		e.TextContent, e.TextPreview, e.ToolName, e.Model,
		e.InputTokens, e.OutputTokens, e.CacheReadTokens, e.CacheCreateTokens,
		e.DurationMs, e.CostUSD, e.ErrorCode, e.ErrorMessage,
		e.EventVersion, e.PayloadJSON, e.CWD, e.SourceFile, e.SourceLineNo, e.SourceOffset,
	)
	return err
}

func InsertEventLink(ctx context.Context, db *DB, el *models.EventLink) error {
	_, err := db.writeConn.ExecContext(ctx,
		`INSERT OR IGNORE INTO event_links (event_uid, linked_event_uid, link_type)
		 VALUES (?, ?, ?)`,
		el.EventUID, el.LinkedEventUID, el.LinkType,
	)
	return err
}

func InsertToolIO(ctx context.Context, db *DB, tio *models.ToolIO) error {
	_, err := db.writeConn.ExecContext(ctx,
		`INSERT OR IGNORE INTO tool_io (event_uid, tool_name, tool_phase, input_json, output_json, input_preview, output_preview)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tio.EventUID, tio.ToolName, tio.ToolPhase, tio.InputJSON, tio.OutputJSON, tio.InputPreview, tio.OutputPreview,
	)
	return err
}

func InsertIngestError(ctx context.Context, db *DB, ie *models.IngestError) error {
	_, err := db.writeConn.ExecContext(ctx,
		`INSERT INTO ingest_errors (id, source_file, source_line_no, error_class, error_message, context_fragment)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ie.ID, ie.SourceFile, ie.SourceLineNo, ie.ErrorClass, ie.ErrorMessage, ie.ContextFragment,
	)
	return err
}

func UpsertCheckpoint(ctx context.Context, db *DB, cp *models.Checkpoint) error {
	_, err := db.writeConn.ExecContext(ctx,
		`INSERT OR REPLACE INTO ingest_checkpoints (source_name, source_file, source_inode, source_generation, last_offset, last_line_no, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, current_timestamp)`,
		cp.SourceName, cp.SourceFile, cp.SourceInode, cp.SourceGeneration, cp.LastOffset, cp.LastLineNo,
	)
	return err
}

func LoadCheckpoints(ctx context.Context, db *sql.DB, sourceName string) (map[string]*models.Checkpoint, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT source_name, source_file, source_inode, source_generation, last_offset, last_line_no
		 FROM ingest_checkpoints WHERE source_name = ?`, sourceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*models.Checkpoint)
	for rows.Next() {
		cp := &models.Checkpoint{}
		if err := rows.Scan(&cp.SourceName, &cp.SourceFile, &cp.SourceInode, &cp.SourceGeneration, &cp.LastOffset, &cp.LastLineNo); err != nil {
			continue
		}
		result[cp.SourceFile] = cp
	}
	return result, nil
}

func ResetSchema(ctx context.Context, db *DB) error {
	drops := []string{
		`DROP VIEW IF EXISTS v_session_summary`,
		`DROP VIEW IF EXISTS v_conversation_trace`,
		`DROP VIEW IF EXISTS v_tokens_per_minute`,
		`DROP VIEW IF EXISTS v_tool_stats`,
		`DROP VIEW IF EXISTS v_tokens_by_model`,
		`DROP TABLE IF EXISTS event_links`,
		`DROP TABLE IF EXISTS tool_io`,
		`DROP TABLE IF EXISTS ingest_errors`,
		`DROP TABLE IF EXISTS ingest_checkpoints`,
		`DROP TABLE IF EXISTS events`,
	}
	for _, stmt := range drops {
		if _, err := db.writeConn.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return db.migrate()
}
