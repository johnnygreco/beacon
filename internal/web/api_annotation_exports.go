package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

const (
	annotatedTraceIndexSchema  = "beacon.annotated_traces.index.v1"
	annotatedTraceExportSchema = "beacon.annotated_traces.export.v1"
	annotatedTraceScanBatch    = 100
)

type annotatedTraceGroup struct {
	session           APISessionSummary
	counts            APIAnnotationCounts
	firstAnnotationAt time.Time
	lastAnnotationAt  time.Time
	targets           map[string]APIAnnotatedTargetSummary
	annotations       []APITraceAnnotation
}

func (a *APIHandlers) ListAnnotatedTraces(w http.ResponseWriter, r *http.Request) {
	req, err := parseAnnotatedTracesAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}
	var scopeMetadata APIScopeMetadata
	req.Scope, scopeMetadata = scopeForRequest(r.Context(), req.Scope)
	if _, err := annotationFilterFromAnnotatedRequest(req, req.Limit, req.Offset); err != nil {
		a.badRequest(w, err)
		return
	}
	groups, hasMore, err := a.collectAnnotatedTraceGroups(r.Context(), r, req, false)
	if err != nil {
		a.annotationExportError(w, "failed to query annotated traces", err)
		return
	}
	items := make([]APIAnnotatedTraceSummary, 0, len(groups))
	for _, group := range groups {
		items = append(items, APIAnnotatedTraceSummary{
			Session:           group.session,
			Counts:            group.counts,
			FirstAnnotationAt: group.firstAnnotationAt,
			LastAnnotationAt:  group.lastAnnotationAt,
			Targets:           sortedAnnotatedTargets(group.targets),
		})
	}
	a.jsonResponse(w, APIAnnotatedTracesResponse{
		Schema:         annotatedTraceIndexSchema,
		Scope:          scopeMetadata,
		IncludeDeleted: req.IncludeDeleted,
		Offset:         req.Offset,
		Limit:          req.Limit,
		HasMore:        hasMore,
		Items:          items,
	})
}

func (a *APIHandlers) ExportAnnotatedTraces(w http.ResponseWriter, r *http.Request) {
	req, err := parseAnnotatedTracesAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}
	var scopeMetadata APIScopeMetadata
	req.Scope, scopeMetadata = scopeForRequest(r.Context(), req.Scope)
	if _, err := annotationFilterFromAnnotatedRequest(req, req.Limit, req.Offset); err != nil {
		a.badRequest(w, err)
		return
	}
	groups, hasMore, err := a.collectAnnotatedTraceGroups(r.Context(), r, req, true)
	if err != nil {
		a.annotationExportError(w, "failed to export annotated traces", err)
		return
	}
	traces := make([]APIAnnotatedTraceExport, 0, len(groups))
	warnings := []string{}
	for _, group := range groups {
		events, truncated, err := a.querySessionEventsForExport(r.Context(), group.session.ID, req.EventLimit, req.Scope)
		if err != nil {
			a.annotationExportError(w, "failed to export annotated traces", err)
			return
		}
		if truncated {
			warnings = append(warnings, fmt.Sprintf("session %s events truncated at event_limit=%d", group.session.ID, req.EventLimit))
		}
		traces = append(traces, APIAnnotatedTraceExport{
			Session:        group.session,
			Counts:         group.counts,
			Annotations:    group.annotations,
			Events:         events,
			EventLimit:     req.EventLimit,
			EventTruncated: truncated,
		})
	}
	a.jsonResponse(w, APIAnnotatedTraceExportResponse{
		Schema:         annotatedTraceExportSchema,
		ExportedAt:     time.Now().UTC(),
		Scope:          scopeMetadata,
		IncludeDeleted: req.IncludeDeleted,
		Offset:         req.Offset,
		Limit:          req.Limit,
		EventLimit:     req.EventLimit,
		HasMore:        hasMore,
		Traces:         traces,
		Warnings:       warnings,
	})
}

func (a *APIHandlers) collectAnnotatedTraceGroups(ctx context.Context, r *http.Request, req annotatedTracesAPIRequest, includeAnnotations bool) ([]annotatedTraceGroup, bool, error) {
	collected := []annotatedTraceGroup{}
	scanOffset := 0
	for {
		filter, err := annotationFilterFromAnnotatedRequest(req, annotatedTraceScanBatch, scanOffset)
		if err != nil {
			return nil, false, err
		}
		candidates, err := store.ListTraceAnnotationSessionSummaries(ctx, a.db, filter)
		if err != nil {
			return nil, false, err
		}
		if len(candidates) == 0 {
			break
		}
		scanOffset += len(candidates)
		sessionIDs := annotationSessionIDs(candidates)
		sessions, err := a.querySessionSummariesByID(ctx, sessionIDs, req.Scope)
		if err != nil {
			return nil, false, err
		}
		annotations, err := a.listTraceAnnotationsForSessionBatch(ctx, sessionIDs, req)
		if err != nil {
			return nil, false, err
		}
		visible := make(map[string][]models.TraceAnnotation, len(sessionIDs))
		for _, annotation := range annotations {
			if err := a.ensureAnnotationTargetInScope(r, annotation, req.Scope); err != nil {
				if errors.Is(err, store.ErrAnnotationTargetNotFound) {
					continue
				}
				return nil, false, err
			}
			if _, ok := sessions[annotation.SessionID]; !ok {
				continue
			}
			visible[annotation.SessionID] = append(visible[annotation.SessionID], annotation)
		}
		for _, candidate := range candidates {
			session, ok := sessions[candidate.SessionID]
			if !ok {
				continue
			}
			sessionAnnotations := visible[candidate.SessionID]
			if len(sessionAnnotations) == 0 {
				continue
			}
			group := annotatedTraceGroup{session: session, targets: map[string]APIAnnotatedTargetSummary{}}
			for _, annotation := range sessionAnnotations {
				addAnnotationToTraceGroup(&group, annotation, includeAnnotations)
			}
			collected = append(collected, group)
		}
		if len(candidates) < annotatedTraceScanBatch {
			break
		}
	}
	sort.Slice(collected, func(i, j int) bool {
		if collected[i].lastAnnotationAt.Equal(collected[j].lastAnnotationAt) {
			return collected[i].session.ID < collected[j].session.ID
		}
		return collected[i].lastAnnotationAt.After(collected[j].lastAnnotationAt)
	})
	hasMore := len(collected) > req.Offset+req.Limit
	if req.Offset >= len(collected) {
		return []annotatedTraceGroup{}, hasMore, nil
	}
	end := req.Offset + req.Limit
	if end > len(collected) {
		end = len(collected)
	}
	return collected[req.Offset:end], hasMore, nil
}

func (a *APIHandlers) listTraceAnnotationsForSessionBatch(ctx context.Context, sessionIDs []string, req annotatedTracesAPIRequest) ([]models.TraceAnnotation, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	annotations := []models.TraceAnnotation{}
	for offset := 0; ; {
		page, err := store.ListTraceAnnotations(ctx, a.db, store.AnnotationFilter{
			SessionIDs:     sessionIDs,
			TargetType:     req.TargetType,
			EventUID:       req.EventUID,
			AuthorType:     req.AuthorType,
			Source:         req.Source,
			Category:       req.Category,
			Outcome:        req.Outcome,
			Label:          req.Label,
			NeedsFollowup:  req.NeedsFollowup,
			IncludeDeleted: req.IncludeDeleted,
			Limit:          maxAnnotationsAPILimit,
			Offset:         offset,
		})
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, page...)
		if len(page) < maxAnnotationsAPILimit {
			break
		}
		offset += len(page)
	}
	return annotations, nil
}

func annotationFilterFromAnnotatedRequest(req annotatedTracesAPIRequest, limit, offset int) (store.AnnotationFilter, error) {
	targetType := strings.TrimSpace(strings.ToLower(req.TargetType))
	switch targetType {
	case "", models.AnnotationTargetSession, models.AnnotationTargetMessage, models.AnnotationTargetEvent:
	default:
		return store.AnnotationFilter{}, fmt.Errorf("target_type must be session, message, or event")
	}
	return store.AnnotationFilter{
		TargetType:     targetType,
		SessionID:      strings.TrimSpace(req.SessionID),
		EventUID:       strings.TrimSpace(req.EventUID),
		AuthorType:     strings.TrimSpace(req.AuthorType),
		Source:         strings.TrimSpace(req.Source),
		Category:       strings.TrimSpace(req.Category),
		Outcome:        strings.TrimSpace(req.Outcome),
		Label:          strings.TrimSpace(req.Label),
		NeedsFollowup:  req.NeedsFollowup,
		IncludeDeleted: req.IncludeDeleted,
		Limit:          limit,
		Offset:         offset,
	}, nil
}

func (a *APIHandlers) querySessionSummariesByID(ctx context.Context, ids []string, scope APIScopeFilters) (map[string]APISessionSummary, error) {
	out := make(map[string]APISessionSummary, len(ids))
	ids = compactScopeValues(ids)
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	for i := range ids {
		placeholders[i] = "?"
	}
	now := time.Now()
	cutoff := now.Add(-idleThreshold)
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope("", scope)
	sessionScope := scope.withoutProjectKeys()
	scopeClause, scopeArgs := sessionScope.sqlAndClause("")
	args := reopenedFlagArgs(scope, cutoff)
	args = append(args, sourceArgs...)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, scopeArgs...)
	rows, err := a.db.QueryContext(ctx, `SELECT `+sessionSummaryColumnsWithReopenedFlagScoped(scope)+`
		FROM `+sessionSource+`
		WHERE session_id IN (`+strings.Join(placeholders, ",")+`)`+scopeClause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		session, err := scanSessionSummaryIncludingReopened(rows, now)
		if err != nil {
			a.logSkippedRow("annotated trace session summary", err)
			continue
		}
		out[session.ID] = apiSessionSummaryFromView(session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *APIHandlers) querySessionEventsForExport(ctx context.Context, sessionID string, eventLimit int, scope APIScopeFilters) ([]APISessionEvent, bool, error) {
	sessionScope := scope.withoutProjectKeys()
	sessionScopeClause := ""
	sessionScopeArgs := []any{}
	scopedSessionSQL := "SELECT ? AS session_id"
	if len(compactScopeValues(scope.ProjectKeys)) == 0 {
		sessionScopeClause, sessionScopeArgs = sessionScope.sqlAndClause("")
		scopedSessionSQL = `SELECT session_id
			FROM session_projection FINAL
			WHERE session_id = ?` + sessionScopeClause + `
			LIMIT 1`
	}
	args := []any{sessionID}
	args = append(args, sessionScopeArgs...)
	eventScopeClause, eventScopeArgs := scope.eventAndSessionProjectSQLAndClause("e", "e.cwd", "s")
	args = append(args, eventScopeArgs...)
	args = append(args, eventLimit+1)
	rows, err := a.db.QueryContext(ctx,
		`WITH scoped_session AS (
			`+scopedSessionSQL+`
		 ),
		 session_events AS (
			SELECT e.event_uid, e.session_id, e.event_kind, e.payload_type, e.actor_role,
			       e.timestamp, e.text_preview, e.tool_name, e.tool_use_id, e.model,
			       e.input_tokens + e.output_tokens AS tokens, e.duration_ms
			FROM `+latestActivityEventsSubquery("ae.session_id IN (SELECT session_id FROM scoped_session)")+` AS e
			LEFT JOIN `+sessionProjectFallbackSubquery("ae.session_id IN (SELECT session_id FROM scoped_session)")+` AS s ON s.session_id = e.session_id
			WHERE 1 = 1`+eventScopeClause+`
			ORDER BY timestamp, event_uid
			LIMIT ?
		 ),
		 payload_previews AS (
			SELECT event_uid,
			       argMax(input_preview, captured_at) AS input_preview,
			       argMax(output_preview, captured_at) AS output_preview
			FROM tool_payloads
			WHERE event_uid IN (SELECT event_uid FROM session_events)
			GROUP BY event_uid
		 )
		 SELECT e.event_uid, e.session_id, e.event_kind, e.payload_type, e.actor_role,
		        e.timestamp, e.text_preview, e.tool_name, e.tool_use_id, e.model,
		        e.tokens, e.duration_ms,
		        COALESCE(p.input_preview, ''), COALESCE(p.output_preview, '')
		 FROM session_events e
		 LEFT JOIN payload_previews p ON e.event_uid = p.event_uid
		 ORDER BY e.timestamp, e.event_uid`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	events := make([]APISessionEvent, 0, eventLimit)
	for rows.Next() {
		var event APISessionEvent
		if err := rows.Scan(&event.EventUID, &event.SessionID, &event.EventKind, &event.PayloadType, &event.ActorRole,
			&event.Timestamp, &event.TextPreview, &event.ToolName, &event.ToolUseID, &event.Model, &event.Tokens, &event.DurationMs,
			&event.InputPreview, &event.OutputPreview); err != nil {
			a.logSkippedRow("annotated trace events", err)
			continue
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(events) > eventLimit
	if truncated {
		events = events[:eventLimit]
	}
	return events, truncated, nil
}

func annotationSessionIDs(summaries []store.TraceAnnotationSessionSummary) []string {
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		ids = append(ids, summary.SessionID)
	}
	return ids
}

func addAnnotationToTraceGroup(group *annotatedTraceGroup, annotation models.TraceAnnotation, includeAnnotation bool) {
	apiAnnotation := apiTraceAnnotationFromModel(annotation)
	group.counts.AnnotationCount++
	switch annotation.TargetType {
	case models.AnnotationTargetSession:
		group.counts.SessionAnnotationCount++
	case models.AnnotationTargetMessage:
		group.counts.MessageAnnotationCount++
	case models.AnnotationTargetEvent:
		group.counts.EventAnnotationCount++
	}
	if annotation.NeedsFollowup {
		group.counts.NeedsFollowupCount++
	}
	if group.firstAnnotationAt.IsZero() || annotation.CreatedAt.Before(group.firstAnnotationAt) {
		group.firstAnnotationAt = annotation.CreatedAt
	}
	if group.lastAnnotationAt.IsZero() || annotation.UpdatedAt.After(group.lastAnnotationAt) {
		group.lastAnnotationAt = annotation.UpdatedAt
	}
	targetKey := annotation.TargetType + "\x00" + annotation.EventUID
	target := group.targets[targetKey]
	if target.TargetType == "" {
		target.TargetType = annotation.TargetType
		target.EventUID = annotation.EventUID
		target.FirstAnnotationAt = annotation.CreatedAt
		target.LastAnnotationAt = annotation.UpdatedAt
	}
	target.AnnotationCount++
	if annotation.CreatedAt.Before(target.FirstAnnotationAt) {
		target.FirstAnnotationAt = annotation.CreatedAt
	}
	if annotation.UpdatedAt.After(target.LastAnnotationAt) {
		target.LastAnnotationAt = annotation.UpdatedAt
	}
	group.targets[targetKey] = target
	if includeAnnotation {
		group.annotations = append(group.annotations, apiAnnotation)
	}
}

func sortedAnnotatedTargets(targets map[string]APIAnnotatedTargetSummary) []APIAnnotatedTargetSummary {
	out := make([]APIAnnotatedTargetSummary, 0, len(targets))
	for _, target := range targets {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TargetType != out[j].TargetType {
			return out[i].TargetType < out[j].TargetType
		}
		return out[i].EventUID < out[j].EventUID
	})
	return out
}

func (a *APIHandlers) annotationExportError(w http.ResponseWriter, publicMessage string, err error) {
	var validation *models.AnnotationValidationError
	switch {
	case err == nil:
		return
	case errors.As(err, &validation):
		a.jsonError(w, validation.Message, http.StatusBadRequest)
	case errors.Is(err, store.ErrAnnotationTargetNotFound):
		a.jsonError(w, "annotation target not found", http.StatusNotFound)
	default:
		a.internalError(w, publicMessage, err)
	}
}
