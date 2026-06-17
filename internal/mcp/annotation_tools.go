package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

const (
	defaultAnnotationListLimit = 200
	maxAnnotationListLimit     = 500
)

type annotationTargetInput struct {
	TargetType string
	SessionID  string
	EventUID   string
	MessageUID string
	RefScope   ScopeFilters
}

type annotationToolTarget struct {
	TargetType string
	SessionID  string
	EventUID   string
}

func (s *Server) toolCreateAnnotation(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		TargetType    string   `json:"target_type"`
		SessionID     string   `json:"session_id"`
		MessageID     string   `json:"message_id"`
		EventID       string   `json:"event_id"`
		OpenRef       *openRef `json:"open_ref"`
		AuthorID      string   `json:"author_id"`
		AuthorName    string   `json:"author_name"`
		Category      string   `json:"category"`
		Outcome       string   `json:"outcome"`
		QualityScore  int      `json:"quality_score"`
		Confidence    int      `json:"confidence"`
		NeedsFollowup bool     `json:"needs_followup"`
		Labels        []string `json:"labels"`
		Note          string   `json:"note"`
		MetadataJSON  string   `json:"metadata_json"`
		scopeArgs
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	backend, err := s.annotationBackend(ctx)
	if err != nil {
		return "", err
	}
	target, metadata, err := s.resolveAnnotationWriteTarget(ctx, backend.DB, params.TargetType, params.SessionID, params.EventID, params.MessageID, params.OpenRef, params.scopeArgs.filters())
	if err != nil {
		return "", err
	}
	annotation, err := store.CreateTraceAnnotation(ctx, backend.DB, models.TraceAnnotation{
		TargetType:    target.TargetType,
		SessionID:     target.SessionID,
		EventUID:      target.EventUID,
		AuthorType:    models.AnnotationAuthorAgent,
		AuthorID:      params.AuthorID,
		AuthorName:    params.AuthorName,
		Source:        models.AnnotationSourceMCP,
		Category:      params.Category,
		Outcome:       params.Outcome,
		QualityScore:  params.QualityScore,
		Confidence:    params.Confidence,
		NeedsFollowup: params.NeedsFollowup,
		Labels:        params.Labels,
		Note:          params.Note,
		MetadataJSON:  params.MetadataJSON,
	})
	if err != nil {
		return "", annotationToolError("failed to create annotation", err)
	}
	return FormatAnnotationResult("create_annotation", annotation, metadata), nil
}

func (s *Server) toolUpdateAnnotation(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		AnnotationID  string    `json:"annotation_id"`
		Category      *string   `json:"category"`
		Outcome       *string   `json:"outcome"`
		QualityScore  *int      `json:"quality_score"`
		Confidence    *int      `json:"confidence"`
		NeedsFollowup *bool     `json:"needs_followup"`
		Labels        *[]string `json:"labels"`
		Note          *string   `json:"note"`
		MetadataJSON  *string   `json:"metadata_json"`
		scopeArgs
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	params.AnnotationID = strings.TrimSpace(params.AnnotationID)
	if params.AnnotationID == "" {
		return "", userToolError("annotation_id is required")
	}
	backend, err := s.annotationBackend(ctx)
	if err != nil {
		return "", err
	}
	current, err := store.GetTraceAnnotation(ctx, backend.DB, params.AnnotationID, false)
	if err != nil {
		return "", annotationToolError("failed to update annotation", err)
	}
	metadata, err := s.ensureAnnotationToolTargetInScope(ctx, backend.DB, current, params.scopeArgs.filters())
	if err != nil {
		return "", err
	}
	annotation, err := store.UpdateTraceAnnotation(ctx, backend.DB, current.AnnotationID, store.AnnotationUpdate{
		Category:      params.Category,
		Outcome:       params.Outcome,
		QualityScore:  params.QualityScore,
		Confidence:    params.Confidence,
		NeedsFollowup: params.NeedsFollowup,
		Labels:        params.Labels,
		Note:          params.Note,
		MetadataJSON:  params.MetadataJSON,
	})
	if err != nil {
		return "", annotationToolError("failed to update annotation", err)
	}
	return FormatAnnotationResult("update_annotation", annotation, metadata), nil
}

func (s *Server) toolListAnnotations(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		TargetType     string   `json:"target_type"`
		SessionID      string   `json:"session_id"`
		MessageID      string   `json:"message_id"`
		EventID        string   `json:"event_id"`
		OpenRef        *openRef `json:"open_ref"`
		IncludeDeleted bool     `json:"include_deleted"`
		Limit          int      `json:"limit"`
		Offset         int      `json:"offset"`
		scopeArgs
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	backend, err := s.annotationBackend(ctx)
	if err != nil {
		return "", err
	}
	filter, metadata, err := s.resolveAnnotationListFilter(ctx, backend.DB, params.TargetType, params.SessionID, params.EventID, params.MessageID, params.OpenRef, params.scopeArgs.filters())
	if err != nil {
		return "", err
	}
	filter.IncludeDeleted = params.IncludeDeleted
	filter.Limit = normalizeAnnotationListLimit(params.Limit)
	if params.Offset > 0 {
		filter.Offset = params.Offset
	}
	visibleAnnotations, resultComplete, err := s.listAnnotationsVisiblePage(ctx, backend.DB, filter, metadata)
	if err != nil {
		return "", annotationToolError("failed to list annotations", err)
	}
	return FormatAnnotationList(visibleAnnotations, AnnotationListMetadata{
		ResultCount:    len(visibleAnnotations),
		Limit:          filter.Limit,
		Offset:         filter.Offset,
		ResultComplete: resultComplete,
	}, metadata), nil
}

func (s *Server) toolGetAnnotation(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		AnnotationID   string `json:"annotation_id"`
		IncludeDeleted bool   `json:"include_deleted"`
		scopeArgs
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	params.AnnotationID = strings.TrimSpace(params.AnnotationID)
	if params.AnnotationID == "" {
		return "", userToolError("annotation_id is required")
	}
	backend, err := s.annotationBackend(ctx)
	if err != nil {
		return "", err
	}
	annotation, err := store.GetTraceAnnotation(ctx, backend.DB, params.AnnotationID, params.IncludeDeleted)
	if err != nil {
		return "", annotationToolError("failed to get annotation", err)
	}
	metadata, err := s.ensureAnnotationToolTargetInScope(ctx, backend.DB, annotation, params.scopeArgs.filters())
	if err != nil {
		return "", err
	}
	return FormatAnnotationResult("get_annotation", annotation, metadata), nil
}

func (s *Server) toolDeleteAnnotation(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		AnnotationID string `json:"annotation_id"`
		scopeArgs
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", userToolError("invalid arguments")
	}
	params.AnnotationID = strings.TrimSpace(params.AnnotationID)
	if params.AnnotationID == "" {
		return "", userToolError("annotation_id is required")
	}
	backend, err := s.annotationBackend(ctx)
	if err != nil {
		return "", err
	}
	current, err := store.GetTraceAnnotation(ctx, backend.DB, params.AnnotationID, false)
	if err != nil {
		return "", annotationToolError("failed to delete annotation", err)
	}
	metadata, err := s.ensureAnnotationToolTargetInScope(ctx, backend.DB, current, params.scopeArgs.filters())
	if err != nil {
		return "", err
	}
	annotation, err := store.DeleteTraceAnnotation(ctx, backend.DB, current.AnnotationID)
	if err != nil {
		return "", annotationToolError("failed to delete annotation", err)
	}
	return FormatAnnotationResult("delete_annotation", annotation, metadata), nil
}

func (s *Server) annotationBackend(ctx context.Context) (Backend, error) {
	backend, err := s.toolBackend(ctx)
	if err != nil {
		return Backend{}, err
	}
	if backend.DB == nil {
		return Backend{}, internalToolError("database unavailable", fmt.Errorf("database backend is not configured"))
	}
	return backend, nil
}

func (s *Server) resolveAnnotationWriteTarget(ctx context.Context, db *sql.DB, targetType, sessionID, eventID, messageID string, ref *openRef, requestedScope ScopeFilters) (annotationToolTarget, ScopeMetadata, error) {
	input, err := normalizeAnnotationTargetInput(targetType, sessionID, eventID, messageID, ref)
	if err != nil {
		return annotationToolTarget{}, ScopeMetadata{}, err
	}
	if input.TargetType == "" {
		switch {
		case input.MessageUID != "":
			input.TargetType = models.AnnotationTargetMessage
		case input.EventUID != "":
			input.TargetType = models.AnnotationTargetEvent
		case input.SessionID != "":
			input.TargetType = models.AnnotationTargetSession
		default:
			return annotationToolTarget{}, ScopeMetadata{}, userToolError("session_id, message_id, event_id, or open_ref is required")
		}
	}
	requestedScope = intersectScopes(requestedScope, input.RefScope)
	scope, metadata := s.effectiveScope(ctx, requestedScope)
	target, err := s.resolveAnnotationInputInScope(ctx, db, input, scope, metadata, true)
	return target, metadata, err
}

func (s *Server) resolveAnnotationListFilter(ctx context.Context, db *sql.DB, targetType, sessionID, eventID, messageID string, ref *openRef, requestedScope ScopeFilters) (store.AnnotationFilter, ScopeMetadata, error) {
	input, err := normalizeAnnotationTargetInput(targetType, sessionID, eventID, messageID, ref)
	if err != nil {
		return store.AnnotationFilter{}, ScopeMetadata{}, err
	}
	requestedScope = intersectScopes(requestedScope, input.RefScope)
	scope, metadata := s.effectiveScope(ctx, requestedScope)
	filter := store.AnnotationFilter{}
	switch input.TargetType {
	case "":
		switch {
		case input.MessageUID != "":
			input.TargetType = models.AnnotationTargetMessage
		case input.EventUID != "":
			input.TargetType = models.AnnotationTargetEvent
		case input.SessionID != "":
			sessionID, err := s.resolveSessionAnnotationTargetInScope(ctx, db, input.SessionID, scope, metadata)
			if err != nil {
				return store.AnnotationFilter{}, metadata, err
			}
			filter.SessionID = sessionID
			return filter, metadata, nil
		default:
			return store.AnnotationFilter{}, metadata, userToolError("session_id, message_id, event_id, or open_ref is required")
		}
	case models.AnnotationTargetSession:
		if input.EventUID != "" || input.MessageUID != "" {
			return store.AnnotationFilter{}, metadata, userToolError("session annotations must not include message_id or event_id")
		}
	case models.AnnotationTargetMessage, models.AnnotationTargetEvent:
		if input.MessageUID == "" && input.EventUID == "" && input.SessionID != "" {
			sessionID, err := s.resolveSessionAnnotationTargetInScope(ctx, db, input.SessionID, scope, metadata)
			if err != nil {
				return store.AnnotationFilter{}, metadata, err
			}
			filter.TargetType = input.TargetType
			filter.SessionID = sessionID
			return filter, metadata, nil
		}
	}
	target, err := s.resolveAnnotationInputInScope(ctx, db, input, scope, metadata, false)
	if err != nil {
		return store.AnnotationFilter{}, metadata, err
	}
	filter.TargetType = target.TargetType
	filter.SessionID = target.SessionID
	filter.EventUID = target.EventUID
	return filter, metadata, nil
}

func (s *Server) resolveAnnotationInputInScope(ctx context.Context, db *sql.DB, input annotationTargetInput, scope ScopeFilters, metadata ScopeMetadata, write bool) (annotationToolTarget, error) {
	switch input.TargetType {
	case models.AnnotationTargetSession:
		if input.EventUID != "" || input.MessageUID != "" {
			return annotationToolTarget{}, userToolError("session annotations must not include message_id or event_id")
		}
		sessionID, err := s.resolveSessionAnnotationTargetInScope(ctx, db, input.SessionID, scope, metadata)
		if err != nil {
			return annotationToolTarget{}, err
		}
		return annotationToolTarget{TargetType: models.AnnotationTargetSession, SessionID: sessionID}, nil
	case models.AnnotationTargetMessage:
		eventUID := firstNonEmptyString(input.MessageUID, input.EventUID)
		if input.MessageUID != "" && input.EventUID != "" && input.MessageUID != input.EventUID {
			return annotationToolTarget{}, userToolError("message_id and event_id refer to different events")
		}
		if eventUID == "" {
			if write {
				return annotationToolTarget{}, userToolError("message_id or message open_ref is required")
			}
			return annotationToolTarget{}, userToolError("message_id, event_id, session_id, or open_ref is required")
		}
		sessionID, err := s.resolveEventAnnotationTargetInScope(ctx, db, input.SessionID, eventUID, models.AnnotationTargetMessage, scope, metadata)
		if err != nil {
			return annotationToolTarget{}, err
		}
		return annotationToolTarget{TargetType: models.AnnotationTargetMessage, SessionID: sessionID, EventUID: eventUID}, nil
	case models.AnnotationTargetEvent:
		if input.MessageUID != "" {
			return annotationToolTarget{}, userToolError("message_id requires target_type message")
		}
		if input.EventUID == "" {
			if write {
				return annotationToolTarget{}, userToolError("event_id or event open_ref is required")
			}
			return annotationToolTarget{}, userToolError("event_id, session_id, or open_ref is required")
		}
		sessionID, err := s.resolveEventAnnotationTargetInScope(ctx, db, input.SessionID, input.EventUID, models.AnnotationTargetEvent, scope, metadata)
		if err != nil {
			return annotationToolTarget{}, err
		}
		return annotationToolTarget{TargetType: models.AnnotationTargetEvent, SessionID: sessionID, EventUID: input.EventUID}, nil
	default:
		return annotationToolTarget{}, userToolError("target_type must be session, message, or event")
	}
}

func normalizeAnnotationTargetInput(targetType, sessionID, eventID, messageID string, ref *openRef) (annotationTargetInput, error) {
	input := annotationTargetInput{
		TargetType: strings.TrimSpace(strings.ToLower(targetType)),
		SessionID:  stripBeaconPrefix(strings.TrimSpace(sessionID), "session:"),
		EventUID:   stripBeaconPrefix(strings.TrimSpace(eventID), "event:"),
		MessageUID: normalizeMessageID(messageID),
	}
	switch input.TargetType {
	case "", models.AnnotationTargetSession, models.AnnotationTargetMessage, models.AnnotationTargetEvent:
	default:
		return annotationTargetInput{}, userToolError("target_type must be session, message, or event")
	}
	if ref == nil {
		return input, nil
	}
	if ref.Scope != nil {
		input.RefScope = normalizeScopeFilters(*ref.Scope)
	}
	refType := strings.TrimSpace(strings.ToLower(ref.Type))
	refSessionID := stripBeaconPrefix(strings.TrimSpace(ref.SessionID), "session:")
	if refSessionID != "" {
		if input.SessionID != "" && input.SessionID != refSessionID {
			return annotationTargetInput{}, userToolError("session_id conflicts with open_ref")
		}
		input.SessionID = refSessionID
	}
	if refType == models.AnnotationTargetMessage {
		if input.TargetType != "" && input.TargetType != models.AnnotationTargetMessage {
			return annotationTargetInput{}, userToolError("message open_ref requires target_type message")
		}
		input.TargetType = models.AnnotationTargetMessage
	}
	refMessageUID := normalizeMessageID(ref.MessageID)
	refEventUID := stripBeaconPrefix(strings.TrimSpace(ref.EventID), "event:")
	if refMessageUID != "" {
		if input.TargetType == models.AnnotationTargetSession {
			return annotationTargetInput{}, userToolError("session target cannot use a message open_ref")
		}
		if input.TargetType != "" && input.TargetType != models.AnnotationTargetMessage {
			return annotationTargetInput{}, userToolError("message open_ref requires target_type message")
		}
		if refEventUID != "" && refEventUID != refMessageUID {
			return annotationTargetInput{}, userToolError("event_id conflicts with open_ref")
		}
		if input.EventUID != "" && input.EventUID != refMessageUID {
			return annotationTargetInput{}, userToolError("event_id conflicts with open_ref")
		}
		if input.MessageUID != "" && input.MessageUID != refMessageUID {
			return annotationTargetInput{}, userToolError("message_id conflicts with open_ref")
		}
		input.TargetType = models.AnnotationTargetMessage
		input.MessageUID = refMessageUID
		return input, nil
	}
	if refEventUID == "" {
		return input, nil
	}
	if input.TargetType == models.AnnotationTargetSession {
		return annotationTargetInput{}, userToolError("session target cannot use an event open_ref")
	}
	if input.EventUID != "" && input.EventUID != refEventUID {
		return annotationTargetInput{}, userToolError("event_id conflicts with open_ref")
	}
	if input.MessageUID != "" && input.MessageUID != refEventUID {
		return annotationTargetInput{}, userToolError("message_id conflicts with open_ref")
	}
	if input.TargetType == models.AnnotationTargetMessage || input.MessageUID != "" {
		input.MessageUID = refEventUID
		return input, nil
	}
	input.EventUID = refEventUID
	return input, nil
}

func (s *Server) listAnnotationsVisiblePage(ctx context.Context, db *sql.DB, filter store.AnnotationFilter, metadata ScopeMetadata) ([]models.TraceAnnotation, bool, error) {
	limit := normalizeAnnotationListLimit(filter.Limit)
	offset := 0
	if filter.Offset > 0 {
		offset = filter.Offset
	}
	visible := make([]models.TraceAnnotation, 0, limit)
	rawOffset := 0
	visibleSkipped := 0
	resultComplete := true
	for len(visible) <= limit {
		pageFilter := filter
		pageFilter.Limit = maxAnnotationListLimit
		pageFilter.Offset = rawOffset
		annotations, err := store.ListTraceAnnotations(ctx, db, pageFilter)
		if err != nil {
			return nil, false, err
		}
		if len(annotations) == 0 {
			break
		}
		for _, annotation := range annotations {
			if _, err := s.ensureAnnotationToolTargetInScope(ctx, db, annotation, metadata.Filters); err != nil {
				if isAnnotationListScopeMiss(err) {
					continue
				}
				return nil, false, err
			}
			if visibleSkipped < offset {
				visibleSkipped++
				continue
			}
			visible = append(visible, annotation)
			if len(visible) > limit {
				resultComplete = false
				return visible[:limit], resultComplete, nil
			}
		}
		rawOffset += len(annotations)
		if len(annotations) < pageFilter.Limit {
			break
		}
	}
	return visible, resultComplete, nil
}

func (s *Server) ensureAnnotationToolTargetInScope(ctx context.Context, db *sql.DB, annotation models.TraceAnnotation, requestedScope ScopeFilters) (ScopeMetadata, error) {
	input := annotationTargetInput{
		TargetType: strings.TrimSpace(strings.ToLower(annotation.TargetType)),
		SessionID:  annotation.SessionID,
		EventUID:   annotation.EventUID,
	}
	if input.TargetType == models.AnnotationTargetMessage {
		input.MessageUID = annotation.EventUID
		input.EventUID = ""
	}
	scope, metadata := s.effectiveScope(ctx, requestedScope)
	_, err := s.resolveAnnotationInputInScope(ctx, db, input, scope, metadata, false)
	return metadata, err
}

func (s *Server) resolveSessionAnnotationTargetInScope(ctx context.Context, db *sql.DB, sessionID string, scope ScopeFilters, metadata ScopeMetadata) (string, error) {
	sessionID = stripBeaconPrefix(strings.TrimSpace(sessionID), "session:")
	if sessionID == "" {
		return "", userToolError("session_id is required")
	}
	sessionSource, sourceArgs := mcpSessionProjectionSource(scope)
	scopeClause, scopeArgs := scope.sqlAndClause("")
	args := append([]any{}, sourceArgs...)
	args = append(args, sessionID)
	args = append(args, scopeArgs...)
	var resolved string
	err := db.QueryRowContext(ctx, `SELECT session_id FROM `+sessionSource+` WHERE session_id = ?`+scopeClause+` LIMIT 1`, args...).Scan(&resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return "", annotationTargetNotFoundError(scope, metadata)
	}
	if err != nil {
		return "", annotationToolError("failed to resolve annotation target", err)
	}
	return resolved, nil
}

func (s *Server) resolveEventAnnotationTargetInScope(ctx context.Context, db *sql.DB, sessionID, eventUID, targetType string, scope ScopeFilters, metadata ScopeMetadata) (string, error) {
	eventUID = stripBeaconPrefix(strings.TrimSpace(eventUID), "event:")
	if eventUID == "" {
		if targetType == models.AnnotationTargetMessage {
			return "", userToolError("message annotations require event_uid")
		}
		return "", userToolError("event annotations require event_uid")
	}
	scopeClause, scopeArgs := scope.eventAndSessionProjectSQLAndClause("e", "e.cwd", "s")
	kindClause := ""
	if strings.TrimSpace(strings.ToLower(targetType)) == models.AnnotationTargetMessage {
		kindClause = " AND e.event_kind = 'message'"
	}
	args := []any{eventUID}
	args = append(args, scopeArgs...)
	var resolved string
	err := db.QueryRowContext(ctx,
		`WITH latest_event AS (
			SELECT event_uid,
			       argMax(session_id, captured_at) AS session_id,
			       argMax(source_name, captured_at) AS source_name,
			       argMax(runtime, captured_at) AS runtime,
			       argMax(event_kind, captured_at) AS event_kind,
			       argMax(cwd, captured_at) AS cwd
			FROM activity_events
			WHERE event_uid = ?
			GROUP BY event_uid
		 )
		 SELECT e.session_id
		 FROM latest_event AS e
		 LEFT JOIN `+mcpSessionProjectFallbackSubquery("ae.session_id IN (SELECT session_id FROM latest_event)")+` AS s ON s.session_id = e.session_id
		 WHERE e.session_id != ''`+kindClause+scopeClause+`
		 LIMIT 1`, args...).Scan(&resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return "", annotationTargetNotFoundError(scope, metadata)
	}
	if err != nil {
		return "", annotationToolError("failed to resolve annotation target", err)
	}
	if sessionID = stripBeaconPrefix(strings.TrimSpace(sessionID), "session:"); sessionID != "" && sessionID != resolved {
		return "", annotationTargetNotFoundError(scope, metadata)
	}
	return resolved, nil
}

func annotationTargetNotFoundError(scope ScopeFilters, metadata ScopeMetadata) error {
	if metadata.AuthScopeApplied || hasScopeFilters(scope) {
		return userToolError("forbidden")
	}
	return userToolError("annotation target not found")
}

func isAnnotationListScopeMiss(err error) bool {
	var toolErr toolCallError
	if !errors.As(err, &toolErr) || toolErr.err != nil {
		return false
	}
	return toolErr.public == "forbidden" || toolErr.public == "annotation target not found"
}

func annotationToolError(publicMessage string, err error) error {
	var toolErr toolCallError
	var validation *models.AnnotationValidationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &toolErr):
		return err
	case errors.As(err, &validation):
		return userToolError("%s", validation.Message)
	case errors.Is(err, store.ErrAnnotationTargetNotFound):
		return userToolError("annotation target not found")
	case errors.Is(err, store.ErrAnnotationNotFound):
		return userToolError("annotation not found")
	default:
		return internalToolError(publicMessage, err)
	}
}

func normalizeAnnotationListLimit(limit int) int {
	if limit <= 0 {
		return defaultAnnotationListLimit
	}
	if limit > maxAnnotationListLimit {
		return maxAnnotationListLimit
	}
	return limit
}

func normalizeMessageID(id string) string {
	id = strings.TrimSpace(id)
	id = stripBeaconPrefix(id, "message:")
	return stripBeaconPrefix(id, "event:")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
