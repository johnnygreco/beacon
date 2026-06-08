package store

import (
	"context"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func (s *Store) insertRawRecords(ctx context.Context, records []models.RawRecord) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO raw_records (
		record_uid, event_uid, node_id, collector_id, source_id,
		source_name, runtime, provider, format, source_file,
		source_line_no, source_offset, source_generation, session_id,
		raw_session_id, raw_event_id, source_event_index, batch_id,
		control_plane_epoch, payload_digest, redaction_status, redaction_version,
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
			r.EventUID,
			r.NodeID,
			r.CollectorID,
			r.SourceID,
			r.SourceName,
			r.Runtime,
			r.Provider,
			r.Format,
			r.SourceFile,
			uint32(nonNegativeInt(r.SourceLineNo)),
			uint64(nonNegativeInt64(r.SourceOffset)),
			uint32(nonNegativeInt(r.SourceGeneration)),
			r.SessionID,
			r.RawSessionID,
			r.RawEventID,
			r.SourceEventIndex,
			r.BatchID,
			r.ControlPlaneEpoch,
			r.PayloadDigest,
			r.RedactionStatus,
			r.RedactionVersion,
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
		event_uid, session_id, raw_session_id, parent_session_id, raw_parent_session_id,
		node_id, collector_id, source_id, source_name, runtime, provider,
		format, event_kind, payload_type, actor_role, timestamp, text_content, text_preview,
		tool_name, tool_use_id, model, input_tokens, output_tokens,
		cache_read_tokens, cache_create_tokens, duration_ms, cost_usd,
		error_code, error_message, event_version, payload_json, cwd,
		source_file, source_line_no, source_offset, source_generation,
		raw_event_id, source_event_index, batch_id, control_plane_epoch,
		payload_digest, redaction_status, redaction_version, captured_at
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
			e.RawSessionID,
			e.ParentSessionID,
			e.RawParentSessionID,
			e.NodeID,
			e.CollectorID,
			e.SourceID,
			e.SourceName,
			firstNonEmpty(e.Runtime, runtimeForSource(e.SourceName)),
			e.Provider,
			firstNonEmpty(e.Format, models.FormatJSONL),
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
			e.RawEventID,
			e.SourceEventIndex,
			e.BatchID,
			e.ControlPlaneEpoch,
			e.PayloadDigest,
			e.RedactionStatus,
			e.RedactionVersion,
			now,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertEventLinks(ctx context.Context, links []models.EventLink) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO event_links (
		event_uid, linked_event_uid, link_type, link_scope, resolution_status,
		session_id, raw_session_id, linked_session_id, raw_linked_session_id,
		raw_linked_event_id, collector_id, source_id, batch_id, control_plane_epoch,
		captured_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, link := range links {
		if err := batch.Append(
			link.EventUID,
			link.LinkedEventUID,
			link.LinkType,
			link.LinkScope,
			link.ResolutionStatus,
			link.SessionID,
			link.RawSessionID,
			link.LinkedSessionID,
			link.RawLinkedSessionID,
			link.RawLinkedEventID,
			link.CollectorID,
			link.SourceID,
			link.BatchID,
			link.ControlPlaneEpoch,
			now,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertToolPayloads(ctx context.Context, payloads []models.ToolPayload) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO tool_payloads (
		event_uid, collector_id, source_id, tool_name, tool_phase, input_json, output_json,
		input_preview, output_preview, batch_id, control_plane_epoch, payload_digest,
		redaction_status, redaction_version, captured_at
	)`)
	if err != nil {
		return err
	}
	defer batch.Close()
	now := time.Now().UTC()
	for _, p := range payloads {
		if err := batch.Append(
			p.EventUID,
			p.CollectorID,
			p.SourceID,
			p.ToolName,
			p.ToolPhase,
			p.InputJSON,
			p.OutputJSON,
			p.InputPreview,
			p.OutputPreview,
			p.BatchID,
			p.ControlPlaneEpoch,
			p.PayloadDigest,
			p.RedactionStatus,
			p.RedactionVersion,
			now,
		); err != nil {
			return err
		}
	}
	return batch.Send()
}

func (s *Store) insertCaptureErrors(ctx context.Context, errors []models.CaptureError) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO capture_errors (
			id, node_id, collector_id, source_id, source_name, source_file,
			source_line_no, source_offset, batch_id, control_plane_epoch,
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
			e.NodeID,
			e.CollectorID,
			e.SourceID,
			e.SourceName,
			e.SourceFile,
			uint32(nonNegativeInt(e.SourceLineNo)),
			uint64(nonNegativeInt64(e.SourceOffset)),
			e.BatchID,
			e.ControlPlaneEpoch,
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

func (s *Store) insertSearchDocuments(ctx context.Context, docs []models.SearchDocument) error {
	batch, err := s.native.PrepareBatch(ctx, `INSERT INTO search_documents (
		event_uid, session_id, node_id, collector_id, source_id, source_name,
		runtime, format, project_key, project_path, event_kind, timestamp,
		text_preview, tool_name, model, provider, searchable_text, document_len,
		updated_at
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
			doc.NodeID,
			doc.CollectorID,
			doc.SourceID,
			doc.SourceName,
			doc.Runtime,
			doc.Format,
			doc.ProjectKey,
			doc.ProjectPath,
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
		token, event_uid, session_id, node_id, collector_id, source_id,
		source_name, runtime, format, project_key, project_path, event_kind,
		timestamp, term_frequency, document_len, text_preview, tool_name, model,
		provider, updated_at
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
			p.NodeID,
			p.CollectorID,
			p.SourceID,
			p.SourceName,
			p.Runtime,
			p.Format,
			p.ProjectKey,
			p.ProjectPath,
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
