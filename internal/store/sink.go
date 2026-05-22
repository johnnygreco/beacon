package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/textindex"
)

const (
	sessionEndProjectionPredicate = "event_kind = 'session_end' OR (event_kind = 'event_msg' AND payload_type = 'last-prompt')"
	validEventTimestampPredicate  = "timestamp > toDateTime64(0, 3, 'UTC')"
	defaultProjectionRefreshBatch = 500
)

type RowBatch struct {
	RawRecords     []models.RawRecord
	ActivityEvents []models.Event
	EventLinks     []models.EventLink
	ToolPayloads   []models.ToolPayload
	CaptureErrors  []models.CaptureError
	Checkpoints    []models.Checkpoint
}

func (s *Store) Flush(ctx context.Context, rows RowBatch) error {
	if len(rows.RawRecords) > 0 {
		if err := s.insertRawRecords(ctx, rows.RawRecords); err != nil {
			return fmt.Errorf("insert raw records: %w", err)
		}
	}
	if len(rows.ActivityEvents) > 0 {
		if err := s.insertActivityEvents(ctx, rows.ActivityEvents); err != nil {
			return fmt.Errorf("insert activity events: %w", err)
		}
	}
	if len(rows.EventLinks) > 0 {
		if err := s.insertEventLinks(ctx, rows.EventLinks); err != nil {
			return fmt.Errorf("insert event links: %w", err)
		}
	}
	if len(rows.ToolPayloads) > 0 {
		if err := s.insertToolPayloads(ctx, rows.ToolPayloads); err != nil {
			return fmt.Errorf("insert tool payloads: %w", err)
		}
	}
	if len(rows.CaptureErrors) > 0 {
		if err := s.insertCaptureErrors(ctx, rows.CaptureErrors); err != nil {
			return fmt.Errorf("insert capture errors: %w", err)
		}
	}
	if len(rows.Checkpoints) > 0 {
		if err := s.insertCheckpoints(ctx, rows.Checkpoints); err != nil {
			return fmt.Errorf("insert checkpoints: %w", err)
		}
	}
	if len(rows.ActivityEvents) > 0 {
		docs, postings := buildSearchRows(rows.ActivityEvents, rows.ToolPayloads)
		if len(docs) > 0 {
			if err := s.insertSearchDocuments(ctx, docs); err != nil {
				return fmt.Errorf("insert search documents: %w", err)
			}
		}
		if len(postings) > 0 {
			if err := s.insertSearchPostings(ctx, postings); err != nil {
				return fmt.Errorf("insert search postings: %w", err)
			}
		}
		ids := sessionIDs(rows.ActivityEvents)
		if err := s.RefreshSessionProjections(ctx, ids); err != nil {
			return fmt.Errorf("refresh session projections: %w", err)
		}
		if err := s.RefreshAnalyticsProjections(ctx, ids); err != nil {
			return fmt.Errorf("refresh analytics projections: %w", err)
		}
	}
	return nil
}

func (s *Store) InsertCaptureError(ctx context.Context, errRow models.CaptureError) error {
	return s.insertCaptureErrors(ctx, []models.CaptureError{errRow})
}

func (s *Store) UpsertCheckpoint(ctx context.Context, cp models.Checkpoint) error {
	return s.insertCheckpoints(ctx, []models.Checkpoint{cp})
}

func (s *Store) LoadCheckpoints(ctx context.Context, sourceName string) (map[string]*models.Checkpoint, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT source_file,
		        argMax(source_inode, updated_at),
		        argMax(source_generation, updated_at),
		        argMax(last_offset, updated_at),
		        argMax(last_line_no, updated_at),
		        argMax(state_json, updated_at)
		 FROM capture_checkpoints
		 WHERE source_name = ?
		 GROUP BY source_file`, sourceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*models.Checkpoint)
	for rows.Next() {
		var sourceFile string
		var inode, offset uint64
		var generation, lineNo uint32
		var stateJSON string
		if err := rows.Scan(&sourceFile, &inode, &generation, &offset, &lineNo, &stateJSON); err != nil {
			continue
		}
		result[sourceFile] = &models.Checkpoint{
			SourceName:       sourceName,
			SourceFile:       sourceFile,
			SourceInode:      int64(inode),
			SourceGeneration: int(generation),
			LastOffset:       int64(offset),
			LastLineNo:       int(lineNo),
			StateJSON:        stateJSON,
		}
	}
	return result, rows.Err()
}

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

	query := fmt.Sprintf(`INSERT INTO analytics_projection
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
	_, err := s.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) insertRawRecords(ctx context.Context, records []models.RawRecord) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO raw_records (
		record_uid, source_name, runtime, provider, format, source_file,
		source_line_no, source_offset, source_generation, session_id,
		payload_json, captured_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, r := range records {
		capturedAt := nonZeroTime(r.CapturedAt, now)
		if err := batch.Append(
			r.RecordUID,
			r.SourceName,
			r.Runtime,
			r.Provider,
			r.Format,
			r.SourceFile,
			uint32(nonNegativeInt(r.SourceLineNo)),
			uint64(nonNegativeInt64(r.SourceOffset)),
			uint32(nonNegativeInt(r.SourceGeneration)),
			r.SessionID,
			r.PayloadJSON,
			capturedAt,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertActivityEvents(ctx context.Context, events []models.Event) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO activity_events (
		event_uid, session_id, parent_session_id, source_name, runtime, provider,
		format, event_kind, payload_type, actor_role, timestamp, text_content, text_preview,
		tool_name, tool_use_id, model, input_tokens, output_tokens,
		cache_read_tokens, cache_create_tokens, duration_ms, cost_usd,
		error_code, error_message, event_version, payload_json, cwd,
		source_file, source_line_no, source_offset, source_generation, captured_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, e := range events {
		if err := batch.Append(
			e.EventUID,
			e.SessionID,
			e.ParentSessionID,
			e.SourceName,
			firstNonEmpty(e.Runtime, runtimeForSource(e.SourceName)),
			e.Provider,
			firstNonEmpty(e.Format, "jsonl"),
			e.EventKind,
			e.PayloadType,
			e.ActorRole,
			nonZeroTime(e.Timestamp, time.Unix(0, 0).UTC()),
			e.TextContent,
			e.TextPreview,
			e.ToolName,
			e.ToolUseID,
			e.Model,
			uint64(nonNegativeInt64(e.InputTokens)),
			uint64(nonNegativeInt64(e.OutputTokens)),
			uint64(nonNegativeInt64(e.CacheReadTokens)),
			uint64(nonNegativeInt64(e.CacheCreateTokens)),
			uint64(nonNegativeInt64(e.DurationMs)),
			e.CostUSD,
			e.ErrorCode,
			e.ErrorMessage,
			uint16(nonNegativeInt(e.EventVersion)),
			e.PayloadJSON,
			e.CWD,
			e.SourceFile,
			uint32(nonNegativeInt(e.SourceLineNo)),
			uint64(nonNegativeInt64(e.SourceOffset)),
			uint32(nonNegativeInt(e.SourceGeneration)),
			now,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertEventLinks(ctx context.Context, links []models.EventLink) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO event_links (
		event_uid, linked_event_uid, link_type, captured_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, link := range links {
		if err := batch.Append(link.EventUID, link.LinkedEventUID, link.LinkType, now); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertToolPayloads(ctx context.Context, payloads []models.ToolPayload) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO tool_payloads (
		event_uid, tool_name, tool_phase, input_json, output_json,
		input_preview, output_preview, captured_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, p := range payloads {
		if err := batch.Append(
			p.EventUID,
			p.ToolName,
			p.ToolPhase,
			p.InputJSON,
			p.OutputJSON,
			p.InputPreview,
			p.OutputPreview,
			now,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertCaptureErrors(ctx context.Context, errors []models.CaptureError) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO capture_errors (
		id, source_name, source_file, source_line_no, source_offset,
		error_class, error_message, context_fragment, created_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, e := range errors {
		if err := batch.Append(
			e.ID,
			e.SourceName,
			e.SourceFile,
			uint32(nonNegativeInt(e.SourceLineNo)),
			uint64(nonNegativeInt64(e.SourceOffset)),
			e.ErrorClass,
			e.ErrorMessage,
			e.ContextFragment,
			nonZeroTime(e.CreatedAt, now),
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertCheckpoints(ctx context.Context, checkpoints []models.Checkpoint) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO capture_checkpoints (
		source_name, source_file, source_inode, source_generation,
		last_offset, last_line_no, state_json, updated_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, cp := range checkpoints {
		if err := batch.Append(
			cp.SourceName,
			cp.SourceFile,
			uint64(nonNegativeInt64(cp.SourceInode)),
			uint32(nonNegativeInt(cp.SourceGeneration)),
			uint64(nonNegativeInt64(cp.LastOffset)),
			uint32(nonNegativeInt(cp.LastLineNo)),
			cp.StateJSON,
			now,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertSearchDocuments(ctx context.Context, docs []models.SearchDocument) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO search_documents (
		event_uid, session_id, event_kind, timestamp, text_preview, tool_name,
		model, provider, searchable_text, document_len, updated_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, doc := range docs {
		if err := batch.Append(
			doc.EventUID,
			doc.SessionID,
			doc.EventKind,
			nonZeroTime(doc.Timestamp, time.Unix(0, 0).UTC()),
			doc.TextPreview,
			doc.ToolName,
			doc.Model,
			doc.Provider,
			doc.SearchableText,
			uint32(nonNegativeInt(doc.DocumentLength)),
			nonZeroTime(doc.UpdatedAt, now),
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertSearchPostings(ctx context.Context, postings []models.SearchPosting) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO search_postings (
		token, event_uid, session_id, event_kind, timestamp, term_frequency,
		document_len, text_preview, tool_name, model, provider, updated_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, p := range postings {
		if err := batch.Append(
			p.Token,
			p.EventUID,
			p.SessionID,
			p.EventKind,
			nonZeroTime(p.Timestamp, time.Unix(0, 0).UTC()),
			uint32(nonNegativeInt(p.TermFrequency)),
			uint32(nonNegativeInt(p.DocumentLength)),
			p.TextPreview,
			p.ToolName,
			p.Model,
			p.Provider,
			nonZeroTime(p.UpdatedAt, now),
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func buildSearchRows(events []models.Event, payloads []models.ToolPayload) ([]models.SearchDocument, []models.SearchPosting) {
	payloadByEvent := make(map[string]models.ToolPayload, len(payloads))
	for _, payload := range payloads {
		payloadByEvent[payload.EventUID] = payload
	}

	now := time.Now().UTC()
	var docs []models.SearchDocument
	var postings []models.SearchPosting
	for _, event := range events {
		text := searchableText(event, payloadByEvent[event.EventUID])
		tokens := textindex.Tokenize(text)
		if len(tokens) == 0 {
			continue
		}
		preview := searchPreview(event, payloadByEvent[event.EventUID])
		doc := models.SearchDocument{
			EventUID:       event.EventUID,
			SessionID:      event.SessionID,
			EventKind:      event.EventKind,
			Timestamp:      event.Timestamp,
			TextPreview:    preview,
			ToolName:       event.ToolName,
			Model:          event.Model,
			Provider:       event.Provider,
			SearchableText: text,
			DocumentLength: len(tokens),
			UpdatedAt:      now,
		}
		docs = append(docs, doc)
		for token, frequency := range textindex.Frequencies(tokens) {
			postings = append(postings, models.SearchPosting{
				Token:          token,
				EventUID:       event.EventUID,
				SessionID:      event.SessionID,
				EventKind:      event.EventKind,
				Timestamp:      event.Timestamp,
				TermFrequency:  frequency,
				DocumentLength: len(tokens),
				TextPreview:    preview,
				ToolName:       event.ToolName,
				Model:          event.Model,
				Provider:       event.Provider,
				UpdatedAt:      now,
			})
		}
	}
	return docs, postings
}

func searchPreview(event models.Event, payload models.ToolPayload) string {
	eventPreview := strings.TrimSpace(event.TextPreview)
	if !isToolEvent(event) {
		return eventPreview
	}
	if eventPreview != "" && eventPreview != strings.TrimSpace(event.ToolName) {
		return truncateString(eventPreview, 512)
	}
	candidates := []string{payload.InputPreview, payload.OutputPreview}
	if event.EventKind == "tool_result" || event.EventKind == "tool_error" {
		candidates = []string{payload.OutputPreview, payload.InputPreview}
	}
	for _, candidate := range candidates {
		preview := strings.TrimSpace(candidate)
		if preview != "" {
			return truncateString(preview, 512)
		}
	}
	return eventPreview
}

func isToolEvent(event models.Event) bool {
	return event.ToolName != "" || strings.HasPrefix(event.EventKind, "tool_")
}

func searchableText(event models.Event, payload models.ToolPayload) string {
	parts := []string{
		event.EventKind,
		event.PayloadType,
		event.ActorRole,
		event.TextContent,
		event.TextPreview,
		event.ToolName,
		event.Model,
		event.ErrorCode,
		event.ErrorMessage,
		payload.InputPreview,
		payload.OutputPreview,
	}
	text := strings.Join(parts, " ")
	return truncateString(strings.TrimSpace(text), 4096)
}

func NewRawRecord(event models.Event) models.RawRecord {
	capturedAt := time.Now().UTC()
	return models.RawRecord{
		RecordUID:        recordUID(event.SourceFile, event.SourceLineNo, event.SourceOffset, event.SourceGeneration, event.PayloadJSON),
		SourceName:       event.SourceName,
		Runtime:          firstNonEmpty(event.Runtime, runtimeForSource(event.SourceName)),
		Provider:         event.Provider,
		Format:           firstNonEmpty(event.Format, "jsonl"),
		SourceFile:       event.SourceFile,
		SourceLineNo:     event.SourceLineNo,
		SourceOffset:     event.SourceOffset,
		SourceGeneration: event.SourceGeneration,
		SessionID:        event.SessionID,
		PayloadJSON:      event.PayloadJSON,
		CapturedAt:       capturedAt,
	}
}

func recordUID(sourceFile string, lineNo int, offset int64, generation int, payload string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%d|%s", sourceFile, lineNo, offset, generation, payload)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func runtimeForSource(source string) string {
	switch source {
	case "claude":
		return "claude-code"
	case "codex":
		return "codex"
	default:
		return source
	}
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func sessionIDs(events []models.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if event.SessionID != "" {
			ids = append(ids, event.SessionID)
		}
	}
	return ids
}

func uniqStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func nonZeroTime(t time.Time, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback.UTC()
	}
	return t.UTC()
}

func nonNegativeInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func nonNegativeInt64(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
