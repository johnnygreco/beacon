package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
)

const maxAnnotationRequestBytes = 128 * 1024

type annotationWriteRequest struct {
	TargetType    string   `json:"target_type"`
	SessionID     string   `json:"session_id"`
	EventUID      string   `json:"event_uid"`
	AuthorType    string   `json:"author_type"`
	AuthorID      string   `json:"author_id"`
	AuthorName    string   `json:"author_name"`
	Source        string   `json:"source"`
	Category      string   `json:"category"`
	Outcome       string   `json:"outcome"`
	QualityScore  int      `json:"quality_score"`
	Confidence    int      `json:"confidence"`
	NeedsFollowup bool     `json:"needs_followup"`
	Labels        []string `json:"labels"`
	Note          string   `json:"note"`
	MetadataJSON  string   `json:"metadata_json"`
}

type annotationUpdateRequest struct {
	AuthorType    *string   `json:"author_type"`
	AuthorID      *string   `json:"author_id"`
	AuthorName    *string   `json:"author_name"`
	Source        *string   `json:"source"`
	Category      *string   `json:"category"`
	Outcome       *string   `json:"outcome"`
	QualityScore  *int      `json:"quality_score"`
	Confidence    *int      `json:"confidence"`
	NeedsFollowup *bool     `json:"needs_followup"`
	Labels        *[]string `json:"labels"`
	Note          *string   `json:"note"`
	MetadataJSON  *string   `json:"metadata_json"`
}

func (a *APIHandlers) ListAnnotations(w http.ResponseWriter, r *http.Request) {
	req, err := parseAnnotationsAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}
	req.Scope, _ = scopeForRequest(r.Context(), req.Scope)
	a.listAnnotations(w, r, req)
}

func (a *APIHandlers) GetSessionAnnotations(w http.ResponseWriter, r *http.Request) {
	req, err := parseAnnotationsAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}
	req.Scope, _ = scopeForRequest(r.Context(), req.Scope)
	req.SessionID = chi.URLParam(r, "id")
	a.listAnnotations(w, r, req)
}

func (a *APIHandlers) GetEventAnnotations(w http.ResponseWriter, r *http.Request) {
	req, err := parseAnnotationsAPIRequest(r.URL.Query())
	if err != nil {
		a.badRequest(w, err)
		return
	}
	req.Scope, _ = scopeForRequest(r.Context(), req.Scope)
	req.TargetType = models.AnnotationTargetEvent
	req.EventUID = chi.URLParam(r, "event_id")
	a.listAnnotations(w, r, req)
}

func (a *APIHandlers) GetAnnotation(w http.ResponseWriter, r *http.Request) {
	annotationID := chi.URLParam(r, "annotation_id")
	includeDeleted := strings.EqualFold(r.URL.Query().Get("include_deleted"), "true") || r.URL.Query().Get("include_deleted") == "1"
	annotation, err := store.GetTraceAnnotation(r.Context(), a.db, annotationID, includeDeleted)
	if err != nil {
		a.annotationError(w, "failed to query annotation", err)
		return
	}
	scope, _ := scopeForRequest(r.Context(), parseAPIScopeFilters(r.URL.Query()))
	if err := a.ensureAnnotationTargetInScope(r, annotation, scope); err != nil {
		a.annotationError(w, "failed to query annotation", err)
		return
	}
	a.jsonResponse(w, apiTraceAnnotationFromModel(annotation))
}

func (a *APIHandlers) CreateAnnotation(w http.ResponseWriter, r *http.Request) {
	var req annotationWriteRequest
	if err := decodeAnnotationJSON(w, r, &req); err != nil {
		a.badRequest(w, err)
		return
	}
	scope, _ := scopeForRequest(r.Context(), parseAPIScopeFilters(r.URL.Query()))
	input := req.traceAnnotation()
	resolvedSessionID, err := a.resolveAnnotationTargetInScope(r, input.TargetType, input.SessionID, input.EventUID, scope)
	if err != nil {
		a.annotationError(w, "failed to create annotation", err)
		return
	}
	input.SessionID = resolvedSessionID
	input = models.NormalizeTraceAnnotation(input)
	if err := models.ValidateTraceAnnotation(input); err != nil {
		a.annotationError(w, "failed to create annotation", err)
		return
	}
	annotation, err := store.CreateTraceAnnotation(r.Context(), a.db, input)
	if err != nil {
		a.annotationError(w, "failed to create annotation", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(apiTraceAnnotationFromModel(annotation)); err != nil {
		a.log().Debug("json response write failed", "error", err)
	}
}

func (a *APIHandlers) UpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	var req annotationUpdateRequest
	if err := decodeAnnotationJSON(w, r, &req); err != nil {
		a.badRequest(w, err)
		return
	}
	current, err := store.GetTraceAnnotation(r.Context(), a.db, chi.URLParam(r, "annotation_id"), false)
	if err != nil {
		a.annotationError(w, "failed to update annotation", err)
		return
	}
	scope, _ := scopeForRequest(r.Context(), parseAPIScopeFilters(r.URL.Query()))
	if err := a.ensureAnnotationTargetInScope(r, current, scope); err != nil {
		a.annotationError(w, "failed to update annotation", err)
		return
	}
	annotation, err := store.UpdateTraceAnnotation(r.Context(), a.db, current.AnnotationID, req.storeUpdate())
	if err != nil {
		a.annotationError(w, "failed to update annotation", err)
		return
	}
	a.jsonResponse(w, apiTraceAnnotationFromModel(annotation))
}

func (a *APIHandlers) DeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	current, err := store.GetTraceAnnotation(r.Context(), a.db, chi.URLParam(r, "annotation_id"), false)
	if err != nil {
		a.annotationError(w, "failed to delete annotation", err)
		return
	}
	scope, _ := scopeForRequest(r.Context(), parseAPIScopeFilters(r.URL.Query()))
	if err := a.ensureAnnotationTargetInScope(r, current, scope); err != nil {
		a.annotationError(w, "failed to delete annotation", err)
		return
	}
	annotation, err := store.DeleteTraceAnnotation(r.Context(), a.db, current.AnnotationID)
	if err != nil {
		a.annotationError(w, "failed to delete annotation", err)
		return
	}
	a.jsonResponse(w, apiTraceAnnotationFromModel(annotation))
}

func (a *APIHandlers) listAnnotations(w http.ResponseWriter, r *http.Request, req annotationsAPIRequest) {
	filter := store.AnnotationFilter{
		AnnotationID:   req.AnnotationID,
		TargetType:     strings.TrimSpace(strings.ToLower(req.TargetType)),
		SessionID:      req.SessionID,
		EventUID:       req.EventUID,
		IncludeDeleted: req.IncludeDeleted,
		Limit:          req.Limit,
		Offset:         req.Offset,
	}
	if filter.TargetType != "" && filter.TargetType != models.AnnotationTargetSession && filter.TargetType != models.AnnotationTargetEvent {
		a.badRequest(w, fmt.Errorf("target_type must be session or event"))
		return
	}
	if filter.AnnotationID == "" && filter.SessionID == "" && filter.EventUID == "" {
		a.badRequest(w, fmt.Errorf("session_id, event_uid, or annotation_id is required"))
		return
	}
	if filter.EventUID != "" {
		resolvedSessionID, err := a.resolveAnnotationTargetInScope(r, models.AnnotationTargetEvent, filter.SessionID, filter.EventUID, req.Scope)
		if err != nil {
			a.annotationError(w, "failed to query annotations", err)
			return
		}
		filter.SessionID = resolvedSessionID
	}
	if filter.SessionID != "" && filter.EventUID == "" {
		if _, err := a.resolveAnnotationTargetInScope(r, models.AnnotationTargetSession, filter.SessionID, "", req.Scope); err != nil {
			a.annotationError(w, "failed to query annotations", err)
			return
		}
	}

	annotations, err := store.ListTraceAnnotations(r.Context(), a.db, filter)
	if err != nil {
		a.annotationError(w, "failed to query annotations", err)
		return
	}
	items := make([]APITraceAnnotation, 0, len(annotations))
	for _, annotation := range annotations {
		if err := a.ensureAnnotationTargetInScope(r, annotation, req.Scope); err != nil {
			if errors.Is(err, store.ErrAnnotationTargetNotFound) {
				continue
			}
			a.annotationError(w, "failed to query annotations", err)
			return
		}
		items = append(items, apiTraceAnnotationFromModel(annotation))
	}
	a.jsonResponse(w, APITraceAnnotationListResponse{Items: items})
}

func (r annotationWriteRequest) traceAnnotation() models.TraceAnnotation {
	return models.TraceAnnotation{
		TargetType:    r.TargetType,
		SessionID:     r.SessionID,
		EventUID:      r.EventUID,
		AuthorType:    r.AuthorType,
		AuthorID:      r.AuthorID,
		AuthorName:    r.AuthorName,
		Source:        firstNonEmpty(strings.TrimSpace(r.Source), models.AnnotationSourceAPI),
		Category:      r.Category,
		Outcome:       r.Outcome,
		QualityScore:  r.QualityScore,
		Confidence:    r.Confidence,
		NeedsFollowup: r.NeedsFollowup,
		Labels:        r.Labels,
		Note:          r.Note,
		MetadataJSON:  r.MetadataJSON,
	}
}

func (r annotationUpdateRequest) storeUpdate() store.AnnotationUpdate {
	return store.AnnotationUpdate{
		AuthorType:    r.AuthorType,
		AuthorID:      r.AuthorID,
		AuthorName:    r.AuthorName,
		Source:        r.Source,
		Category:      r.Category,
		Outcome:       r.Outcome,
		QualityScore:  r.QualityScore,
		Confidence:    r.Confidence,
		NeedsFollowup: r.NeedsFollowup,
		Labels:        r.Labels,
		Note:          r.Note,
		MetadataJSON:  r.MetadataJSON,
	}
}

func decodeAnnotationJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAnnotationRequestBytes)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body")
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("invalid JSON body")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid JSON body")
	}
	return nil
}

func (a *APIHandlers) resolveAnnotationTargetInScope(r *http.Request, targetType, sessionID, eventUID string, scope APIScopeFilters) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("database is not configured")
	}
	targetType = strings.TrimSpace(strings.ToLower(targetType))
	if targetType == "" {
		if strings.TrimSpace(eventUID) != "" {
			targetType = models.AnnotationTargetEvent
		} else {
			targetType = models.AnnotationTargetSession
		}
	}
	switch targetType {
	case models.AnnotationTargetSession:
		return a.resolveSessionAnnotationTargetInScope(r, sessionID, scope)
	case models.AnnotationTargetEvent:
		return a.resolveEventAnnotationTargetInScope(r, sessionID, eventUID, scope)
	default:
		return "", models.NewAnnotationValidationError("target_type must be session or event")
	}
}

func (a *APIHandlers) resolveSessionAnnotationTargetInScope(r *http.Request, sessionID string, scope APIScopeFilters) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", models.NewAnnotationValidationError("session_id is required")
	}
	sessionScope := scope.withoutProjectKeys()
	sessionWhere := "session_id = ?"
	args := []any{sessionID}
	if clause, scopeArgs := sessionScope.sqlAndClause(""); clause != "" {
		sessionWhere += clause
		args = append(args, scopeArgs...)
	}
	sessionSource, sourceArgs := sessionProjectionSubqueryForScopeWithPrefilter(sessionWhere, "ae.session_id = ?", []any{sessionID}, scope)
	args = append(sourceArgs, args...)
	var resolved string
	err := a.db.QueryRowContext(r.Context(), `SELECT session_id FROM `+sessionSource+` LIMIT 1`, args...).Scan(&resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrAnnotationTargetNotFound
	}
	return resolved, err
}

func (a *APIHandlers) resolveEventAnnotationTargetInScope(r *http.Request, sessionID, eventUID string, scope APIScopeFilters) (string, error) {
	eventUID = strings.TrimSpace(eventUID)
	if eventUID == "" {
		return "", models.NewAnnotationValidationError("event annotations require event_uid")
	}
	scopeClause, scopeArgs := scope.eventAndSessionProjectSQLAndClause("e", "e.cwd", "s")
	args := []any{eventUID}
	args = append(args, scopeArgs...)
	var resolved string
	err := a.db.QueryRowContext(r.Context(),
		`WITH latest_event AS (
			SELECT event_uid,
			       argMax(session_id, captured_at) AS session_id,
			       argMax(source_name, captured_at) AS source_name,
			       argMax(runtime, captured_at) AS runtime,
			       argMax(cwd, captured_at) AS cwd
			FROM activity_events
			WHERE event_uid = ?
			GROUP BY event_uid
		 )
		 SELECT e.session_id
		 FROM latest_event AS e
		 LEFT JOIN `+sessionProjectFallbackSubquery("ae.session_id IN (SELECT session_id FROM latest_event)")+` AS s ON s.session_id = e.session_id
		 WHERE e.session_id != ''`+scopeClause+`
		 LIMIT 1`, args...).Scan(&resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrAnnotationTargetNotFound
	}
	if err != nil {
		return "", err
	}
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" && sessionID != resolved {
		return "", store.ErrAnnotationTargetNotFound
	}
	return resolved, nil
}

func (a *APIHandlers) ensureAnnotationTargetInScope(r *http.Request, annotation models.TraceAnnotation, scope APIScopeFilters) error {
	_, err := a.resolveAnnotationTargetInScope(r, annotation.TargetType, annotation.SessionID, annotation.EventUID, scope)
	return err
}

func (a *APIHandlers) annotationError(w http.ResponseWriter, publicMessage string, err error) {
	var validation *models.AnnotationValidationError
	switch {
	case err == nil:
		return
	case errors.As(err, &validation):
		a.jsonError(w, validation.Message, http.StatusBadRequest)
	case errors.Is(err, store.ErrAnnotationTargetNotFound):
		a.jsonError(w, "annotation target not found", http.StatusNotFound)
	case errors.Is(err, store.ErrAnnotationNotFound):
		a.jsonError(w, "annotation not found", http.StatusNotFound)
	default:
		a.internalError(w, publicMessage, err)
	}
}

func apiTraceAnnotationFromModel(a models.TraceAnnotation) APITraceAnnotation {
	labels := append([]string(nil), a.Labels...)
	if labels == nil {
		labels = []string{}
	}
	return APITraceAnnotation{
		AnnotationID:  a.AnnotationID,
		TargetType:    a.TargetType,
		SessionID:     a.SessionID,
		EventUID:      a.EventUID,
		AuthorType:    a.AuthorType,
		AuthorID:      a.AuthorID,
		AuthorName:    a.AuthorName,
		Source:        a.Source,
		Category:      a.Category,
		Outcome:       a.Outcome,
		QualityScore:  a.QualityScore,
		Confidence:    a.Confidence,
		NeedsFollowup: a.NeedsFollowup,
		Labels:        labels,
		Note:          a.Note,
		MetadataJSON:  a.MetadataJSON,
		Status:        a.Status,
		SchemaVersion: a.SchemaVersion,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
		DeletedAt:     a.DeletedAt,
	}
}
