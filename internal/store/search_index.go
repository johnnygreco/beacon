package store

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/textindex"
)

const searchIndexVersionMarker = "__beacon_search_index_v2__"

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
		projectPath, projectKey := searchProject(event.CWD)
		preview := searchPreview(event, payloadByEvent[event.EventUID])
		doc := models.SearchDocument{
			EventUID:       event.EventUID,
			SessionID:      event.SessionID,
			NodeID:         event.NodeID,
			CollectorID:    event.CollectorID,
			SourceID:       event.SourceID,
			SourceName:     event.SourceName,
			Runtime:        event.Runtime,
			Format:         event.Format,
			ProjectKey:     projectKey,
			ProjectPath:    projectPath,
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
				NodeID:         event.NodeID,
				CollectorID:    event.CollectorID,
				SourceID:       event.SourceID,
				SourceName:     event.SourceName,
				Runtime:        event.Runtime,
				Format:         event.Format,
				ProjectKey:     projectKey,
				ProjectPath:    projectPath,
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

func eventUIDs(events []models.Event) []string {
	if len(events) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(events))
	result := make([]string, 0, len(events))
	for _, event := range events {
		uid := strings.TrimSpace(event.EventUID)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		result = append(result, uid)
	}
	return result
}

func searchProject(cwd string) (string, string) {
	projectPath := strings.TrimRight(strings.TrimSpace(cwd), "/")
	if projectPath == "" {
		return "", ""
	}
	if idx := strings.Index(projectPath, "/.claude/worktrees/"); idx >= 0 {
		projectPath = strings.TrimRight(projectPath[:idx], "/")
	}
	key := path.Base(projectPath)
	if key == "." || key == "/" {
		key = ""
	}
	return projectPath, key
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
	if event.EventKind == models.EventKindToolResult || event.EventKind == models.EventKindToolError {
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
	return event.ToolName != "" || strings.HasPrefix(event.EventKind, models.EventKindToolPrefix)
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
		event.PayloadJSON,
		payload.InputPreview,
		payload.OutputPreview,
		payload.InputJSON,
		payload.OutputJSON,
	}
	text := strings.TrimSpace(strings.Join(parts, " "))
	if text == "" {
		return ""
	}
	return truncateString(searchIndexVersionMarker+" "+text, 4096)
}

func (s *Store) RefreshOutdatedSearchIndex(ctx context.Context) (int, bool, error) {
	if s == nil || s.DB == nil || s.native == nil {
		return 0, false, nil
	}
	var staleEvents uint64
	if err := s.DB.QueryRowContext(ctx, `SELECT count()
		FROM (
			SELECT event_uid, max(captured_at) AS latest_event_captured_at
			FROM activity_events
			WHERE session_id != ''
			GROUP BY event_uid
		) AS events
		LEFT JOIN (
			SELECT event_uid AS doc_event_uid,
			       updated_at AS doc_updated_at,
			       searchable_text,
			       document_len AS doc_document_len
			FROM search_documents FINAL
		) AS docs ON events.event_uid = docs.doc_event_uid
		LEFT JOIN (
			SELECT p.event_uid AS posting_event_uid,
			       sum(p.term_frequency) AS current_posting_terms,
			       max(p.updated_at) AS latest_posting_updated_at
			FROM (SELECT * FROM search_postings FINAL) AS p
			INNER JOIN (
				SELECT event_uid, updated_at
				FROM search_documents FINAL
			) AS d ON d.event_uid = p.event_uid
			WHERE p.updated_at >= d.updated_at
			GROUP BY p.event_uid
		) AS postings ON events.event_uid = postings.posting_event_uid
		LEFT JOIN (
			SELECT event_uid AS payload_event_uid,
			       max(captured_at) AS latest_payload_captured_at
			FROM tool_payloads
			GROUP BY event_uid
		) AS payloads ON events.event_uid = payloads.payload_event_uid
		WHERE docs.doc_event_uid = ''
		   OR position(docs.searchable_text, ?) = 0
		   OR postings.posting_event_uid = ''
		   OR postings.current_posting_terms != docs.doc_document_len
		   OR postings.latest_posting_updated_at < docs.doc_updated_at
		   OR events.latest_event_captured_at > docs.doc_updated_at
		   OR (payloads.payload_event_uid != '' AND payloads.latest_payload_captured_at > docs.doc_updated_at)`,
		searchIndexVersionMarker).Scan(&staleEvents); err != nil {
		return 0, false, err
	}
	if staleEvents == 0 {
		return 0, false, nil
	}
	count, err := s.RefreshSearchIndex(ctx, 0)
	return count, true, err
}

func (s *Store) RefreshSearchIndex(ctx context.Context, batchSize int) (int, error) {
	return s.refreshSearchIndex(ctx, batchSize, "ae.session_id != ''", nil, nil)
}

func (s *Store) RefreshSearchIndexForSessions(ctx context.Context, ids []string, batchSize int) (int, error) {
	ids = uniqStrings(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	where := "ae.session_id != '' AND ae.session_id IN (" + placeholders(len(ids)) + ")"
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return s.refreshSearchIndex(ctx, batchSize, where, args, ids)
}

func (s *Store) RefreshSearchIndexForEvents(ctx context.Context, events []models.Event, batchSize int) (int, error) {
	uids := eventUIDs(events)
	if len(uids) == 0 {
		return 0, nil
	}
	where := "ae.session_id != '' AND ae.event_uid IN (" + placeholders(len(uids)) + ")"
	args := make([]any, 0, len(uids))
	for _, uid := range uids {
		args = append(args, uid)
	}
	return s.refreshSearchIndex(ctx, batchSize, where, args, sessionIDs(events))
}

func (s *Store) refreshSearchIndex(ctx context.Context, batchSize int, where string, args []any, projectSessionIDs []string) (int, error) {
	if s == nil || s.DB == nil || s.native == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = defaultProjectionRefreshBatch
	}
	sessionProjectsCTE := `SELECT
				session_id,
				argMaxIf(cwd, timestamp, cwd != '') AS session_project_path
			FROM latest_events
			GROUP BY session_id`
	if len(projectSessionIDs) > 0 {
		projectArgs := make([]any, 0, len(projectSessionIDs))
		for _, id := range projectSessionIDs {
			projectArgs = append(projectArgs, id)
		}
		sessionProjectsCTE = `SELECT
				session_id,
				argMaxIf(cwd, timestamp, cwd != '') AS session_project_path
			FROM (
				SELECT
					event_uid,
					argMax(session_id, captured_at) AS session_id,
					argMax(timestamp, captured_at) AS timestamp,
					argMax(cwd, captured_at) AS cwd
				FROM activity_events AS sp
				WHERE sp.session_id IN (` + placeholders(len(projectSessionIDs)) + `)
				GROUP BY event_uid
			)
			GROUP BY session_id`
		args = append(args, projectArgs...)
	}
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`WITH latest_events AS (
			SELECT
				event_uid,
				argMax(session_id, captured_at) AS session_id,
				argMax(node_id, captured_at) AS node_id,
				argMax(collector_id, captured_at) AS collector_id,
				argMax(source_id, captured_at) AS source_id,
				argMax(source_name, captured_at) AS source_name,
				argMax(runtime, captured_at) AS runtime,
				argMax(format, captured_at) AS format,
				argMax(provider, captured_at) AS provider,
				argMax(event_kind, captured_at) AS event_kind,
				argMax(payload_type, captured_at) AS payload_type,
				argMax(actor_role, captured_at) AS actor_role,
				argMax(timestamp, captured_at) AS timestamp,
				argMax(text_content, captured_at) AS text_content,
				argMax(text_preview, captured_at) AS text_preview,
				argMax(tool_name, captured_at) AS tool_name,
				argMax(model, captured_at) AS model,
				argMax(error_code, captured_at) AS error_code,
				argMax(error_message, captured_at) AS error_message,
				argMax(payload_json, captured_at) AS payload_json,
				argMax(cwd, captured_at) AS cwd
			FROM activity_events AS ae
			WHERE %s
			GROUP BY event_uid
		),
		session_projects AS (
			%s
		)
		SELECT
			e.event_uid,
			e.session_id,
			e.node_id,
			e.collector_id,
			e.source_id,
			e.source_name,
			e.runtime,
			e.format,
			e.provider,
			e.event_kind,
			e.payload_type,
			e.actor_role,
			e.timestamp,
			e.text_content,
			e.text_preview,
			e.tool_name,
			e.model,
			e.error_code,
			e.error_message,
			e.payload_json,
			if(e.cwd != '', e.cwd, sp.session_project_path)
		FROM latest_events AS e
		LEFT JOIN session_projects AS sp ON sp.session_id = e.session_id
		ORDER BY e.session_id, e.event_uid`, where, sessionProjectsCTE), args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	events := make([]models.Event, 0, batchSize)
	flush := func() error {
		if len(events) == 0 {
			return nil
		}
		payloads, err := s.searchIndexPayloads(ctx, events)
		if err != nil {
			return fmt.Errorf("query tool payloads: %w", err)
		}
		docs, postings := buildSearchRows(events, payloads)
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
		total += len(events)
		events = events[:0]
		return nil
	}

	for rows.Next() {
		var event models.Event
		if err := rows.Scan(
			&event.EventUID,
			&event.SessionID,
			&event.NodeID,
			&event.CollectorID,
			&event.SourceID,
			&event.SourceName,
			&event.Runtime,
			&event.Format,
			&event.Provider,
			&event.EventKind,
			&event.PayloadType,
			&event.ActorRole,
			&event.Timestamp,
			&event.TextContent,
			&event.TextPreview,
			&event.ToolName,
			&event.Model,
			&event.ErrorCode,
			&event.ErrorMessage,
			&event.PayloadJSON,
			&event.CWD,
		); err != nil {
			return total, err
		}
		events = append(events, event)
		if len(events) >= batchSize {
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

func (s *Store) searchIndexPayloads(ctx context.Context, events []models.Event) ([]models.ToolPayload, error) {
	if len(events) == 0 {
		return nil, nil
	}
	placeholders := placeholders(len(events))
	args := make([]any, len(events))
	for i, event := range events {
		args[i] = event.EventUID
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT
			event_uid,
			argMax(tool_name, captured_at),
			argMax(tool_phase, captured_at),
			argMax(input_json, captured_at),
			argMax(output_json, captured_at),
			argMax(input_preview, captured_at),
			argMax(output_preview, captured_at)
		FROM tool_payloads
		WHERE event_uid IN (`+placeholders+`)
		GROUP BY event_uid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payloads []models.ToolPayload
	for rows.Next() {
		var payload models.ToolPayload
		if err := rows.Scan(
			&payload.EventUID,
			&payload.ToolName,
			&payload.ToolPhase,
			&payload.InputJSON,
			&payload.OutputJSON,
			&payload.InputPreview,
			&payload.OutputPreview,
		); err != nil {
			return payloads, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, rows.Err()
}
