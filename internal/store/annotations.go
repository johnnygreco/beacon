package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

var (
	ErrAnnotationNotFound       = errors.New("annotation not found")
	ErrAnnotationTargetNotFound = errors.New("annotation target not found")
)

type AnnotationFilter struct {
	AnnotationID   string
	TargetType     string
	SessionID      string
	SessionIDs     []string
	EventUID       string
	AuthorType     string
	Source         string
	Category       string
	Outcome        string
	Label          string
	NeedsFollowup  *bool
	IncludeDeleted bool
	Limit          int
	Offset         int
}

type TraceAnnotationSessionSummary struct {
	SessionID              string
	AnnotationCount        int
	SessionAnnotationCount int
	MessageAnnotationCount int
	EventAnnotationCount   int
	NeedsFollowupCount     int
	FirstAnnotationAt      time.Time
	LastAnnotationAt       time.Time
}

type AnnotationUpdate struct {
	AuthorType    *string
	AuthorID      *string
	AuthorName    *string
	Source        *string
	Category      *string
	Outcome       *string
	QualityScore  *int
	Confidence    *int
	NeedsFollowup *bool
	Labels        *[]string
	Note          *string
	MetadataJSON  *string
}

const (
	defaultAnnotationLimit = 200
	maxAnnotationLimit     = 500
	annotationLabelJoiner  = ","
)

func CreateTraceAnnotation(ctx context.Context, db *sql.DB, input models.TraceAnnotation) (models.TraceAnnotation, error) {
	if db == nil {
		return models.TraceAnnotation{}, fmt.Errorf("database is not configured")
	}
	a := models.NormalizeTraceAnnotation(input)
	if a.AnnotationID == "" {
		id, err := newAnnotationID()
		if err != nil {
			return models.TraceAnnotation{}, err
		}
		a.AnnotationID = id
	}
	sessionID, err := ResolveTraceAnnotationTarget(ctx, db, a.TargetType, a.SessionID, a.EventUID)
	if err != nil {
		return models.TraceAnnotation{}, err
	}
	a.SessionID = sessionID
	if err := models.ValidateTraceAnnotation(a); err != nil {
		return models.TraceAnnotation{}, err
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = a.CreatedAt
	}
	if a.Status == models.AnnotationStatusDeleted && a.DeletedAt == nil {
		deletedAt := a.UpdatedAt
		a.DeletedAt = &deletedAt
	}
	if a.Revision == 0 {
		a.Revision = 1
	}
	if err := insertTraceAnnotation(ctx, db, a); err != nil {
		return models.TraceAnnotation{}, err
	}
	return a, nil
}

func GetTraceAnnotation(ctx context.Context, db *sql.DB, annotationID string, includeDeleted bool) (models.TraceAnnotation, error) {
	annotations, err := ListTraceAnnotations(ctx, db, AnnotationFilter{
		AnnotationID:   strings.TrimSpace(annotationID),
		IncludeDeleted: includeDeleted,
		Limit:          1,
	})
	if err != nil {
		return models.TraceAnnotation{}, err
	}
	if len(annotations) == 0 {
		return models.TraceAnnotation{}, ErrAnnotationNotFound
	}
	return annotations[0], nil
}

func ListTraceAnnotations(ctx context.Context, db *sql.DB, filter AnnotationFilter) ([]models.TraceAnnotation, error) {
	if db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	filter.AnnotationID = strings.TrimSpace(filter.AnnotationID)
	filter.TargetType = strings.TrimSpace(strings.ToLower(filter.TargetType))
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.EventUID = strings.TrimSpace(filter.EventUID)
	filter.Limit = normalizeAnnotationLimit(filter.Limit)
	filter.Offset = normalizeAnnotationOffset(filter.Offset)
	where, args := annotationFilterWhere(filter)
	args = append(args, filter.Limit, filter.Offset)

	rows, err := db.QueryContext(ctx, traceAnnotationSelectSQL("WHERE "+strings.Join(where, " AND ")+" ORDER BY created_at ASC, annotation_id ASC LIMIT ? OFFSET ?"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	annotations := make([]models.TraceAnnotation, 0)
	for rows.Next() {
		a, err := scanTraceAnnotation(rows)
		if err != nil {
			return nil, err
		}
		annotations = append(annotations, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return annotations, nil
}

func ListTraceAnnotationSessionSummaries(ctx context.Context, db *sql.DB, filter AnnotationFilter) ([]TraceAnnotationSessionSummary, error) {
	if db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	filter.Limit = normalizeAnnotationLimit(filter.Limit)
	filter.Offset = normalizeAnnotationOffset(filter.Offset)
	where, args := annotationFilterWhere(filter)
	args = append(args, filter.Limit, filter.Offset)
	rows, err := db.QueryContext(ctx, `SELECT session_id,
		       count() AS annotation_count,
		       countIf(target_type = ?) AS session_annotation_count,
		       countIf(target_type = ?) AS message_annotation_count,
		       countIf(target_type = ?) AS event_annotation_count,
		       countIf(needs_followup != 0) AS needs_followup_count,
		       min(created_at) AS first_annotation_at,
		       max(updated_at) AS last_annotation_at
		FROM trace_annotations FINAL
		WHERE `+strings.Join(where, " AND ")+`
		GROUP BY session_id
		ORDER BY last_annotation_at DESC, session_id ASC
		LIMIT ? OFFSET ?`,
		append([]any{
			models.AnnotationTargetSession,
			models.AnnotationTargetMessage,
			models.AnnotationTargetEvent,
		}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]TraceAnnotationSessionSummary, 0)
	for rows.Next() {
		var summary TraceAnnotationSessionSummary
		var annotationCount, sessionCount, messageCount, eventCount, followupCount uint64
		if err := rows.Scan(
			&summary.SessionID,
			&annotationCount,
			&sessionCount,
			&messageCount,
			&eventCount,
			&followupCount,
			&summary.FirstAnnotationAt,
			&summary.LastAnnotationAt,
		); err != nil {
			return nil, err
		}
		summary.AnnotationCount = int(annotationCount)
		summary.SessionAnnotationCount = int(sessionCount)
		summary.MessageAnnotationCount = int(messageCount)
		summary.EventAnnotationCount = int(eventCount)
		summary.NeedsFollowupCount = int(followupCount)
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summaries, nil
}

func UpdateTraceAnnotation(ctx context.Context, db *sql.DB, annotationID string, update AnnotationUpdate) (models.TraceAnnotation, error) {
	current, err := GetTraceAnnotation(ctx, db, annotationID, false)
	if err != nil {
		return models.TraceAnnotation{}, err
	}
	if update.AuthorType != nil {
		current.AuthorType = *update.AuthorType
	}
	if update.AuthorID != nil {
		current.AuthorID = *update.AuthorID
	}
	if update.AuthorName != nil {
		current.AuthorName = *update.AuthorName
	}
	if update.Source != nil {
		current.Source = *update.Source
	}
	if update.Category != nil {
		current.Category = *update.Category
	}
	if update.Outcome != nil {
		current.Outcome = *update.Outcome
	}
	if update.QualityScore != nil {
		current.QualityScore = *update.QualityScore
	}
	if update.Confidence != nil {
		current.Confidence = *update.Confidence
	}
	if update.NeedsFollowup != nil {
		current.NeedsFollowup = *update.NeedsFollowup
	}
	if update.Labels != nil {
		current.Labels = *update.Labels
	}
	if update.Note != nil {
		current.Note = *update.Note
	}
	if update.MetadataJSON != nil {
		current.MetadataJSON = *update.MetadataJSON
	}
	current = models.NormalizeTraceAnnotation(current)
	current.Revision++
	current.UpdatedAt = time.Now().UTC()
	if err := models.ValidateTraceAnnotation(current); err != nil {
		return models.TraceAnnotation{}, err
	}
	if err := insertTraceAnnotation(ctx, db, current); err != nil {
		return models.TraceAnnotation{}, err
	}
	return current, nil
}

func DeleteTraceAnnotation(ctx context.Context, db *sql.DB, annotationID string) (models.TraceAnnotation, error) {
	current, err := GetTraceAnnotation(ctx, db, annotationID, false)
	if err != nil {
		return models.TraceAnnotation{}, err
	}
	now := time.Now().UTC()
	current.Status = models.AnnotationStatusDeleted
	current.Revision++
	current.UpdatedAt = now
	current.DeletedAt = &now
	if err := insertTraceAnnotation(ctx, db, current); err != nil {
		return models.TraceAnnotation{}, err
	}
	return current, nil
}

func ResolveTraceAnnotationTarget(ctx context.Context, db *sql.DB, targetType, sessionID, eventUID string) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database is not configured")
	}
	targetType = strings.TrimSpace(strings.ToLower(targetType))
	sessionID = strings.TrimSpace(sessionID)
	eventUID = strings.TrimSpace(eventUID)
	switch targetType {
	case models.AnnotationTargetSession:
		if sessionID == "" {
			return "", models.NewAnnotationValidationError("session_id is required")
		}
		var resolved string
		err := db.QueryRowContext(ctx, `SELECT session_id FROM session_projection FINAL WHERE session_id = ? LIMIT 1`, sessionID).Scan(&resolved)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrAnnotationTargetNotFound
		}
		return resolved, err
	case models.AnnotationTargetMessage:
		if eventUID == "" {
			return "", models.NewAnnotationValidationError("message annotations require event_uid")
		}
		var resolved string
		err := db.QueryRowContext(ctx, `SELECT session_id FROM (
				SELECT argMax(session_id, captured_at) AS session_id,
				       argMax(event_kind, captured_at) AS event_kind
				FROM activity_events
				WHERE event_uid = ?
				GROUP BY event_uid
			) WHERE session_id != '' AND event_kind = 'message' LIMIT 1`, eventUID).Scan(&resolved)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrAnnotationTargetNotFound
		}
		if err != nil {
			return "", err
		}
		if sessionID != "" && sessionID != resolved {
			return "", ErrAnnotationTargetNotFound
		}
		return resolved, nil
	case models.AnnotationTargetEvent:
		if eventUID == "" {
			return "", models.NewAnnotationValidationError("event annotations require event_uid")
		}
		var resolved string
		err := db.QueryRowContext(ctx, `SELECT session_id FROM (
				SELECT argMax(session_id, captured_at) AS session_id
				FROM activity_events
				WHERE event_uid = ?
				GROUP BY event_uid
			) WHERE session_id != '' LIMIT 1`, eventUID).Scan(&resolved)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrAnnotationTargetNotFound
		}
		if err != nil {
			return "", err
		}
		if sessionID != "" && sessionID != resolved {
			return "", ErrAnnotationTargetNotFound
		}
		return resolved, nil
	default:
		return "", models.NewAnnotationValidationError("target_type must be session, message, or event")
	}
}

func insertTraceAnnotation(ctx context.Context, db *sql.DB, a models.TraceAnnotation) error {
	deletedAt := time.Unix(0, 0).UTC()
	if a.DeletedAt != nil {
		deletedAt = a.DeletedAt.UTC()
	}
	_, err := db.ExecContext(ctx, `INSERT INTO trace_annotations (
		annotation_id, revision, target_type, session_id, event_uid,
		author_type, author_id, author_name, source,
		category, outcome, quality_score, confidence, needs_followup,
		labels, note, metadata_json, status, schema_version,
		created_at, updated_at, deleted_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.AnnotationID,
		a.Revision,
		a.TargetType,
		a.SessionID,
		a.EventUID,
		a.AuthorType,
		a.AuthorID,
		a.AuthorName,
		a.Source,
		a.Category,
		a.Outcome,
		int16(a.QualityScore),
		uint8(a.Confidence),
		boolToUInt8(a.NeedsFollowup),
		a.Labels,
		a.Note,
		a.MetadataJSON,
		a.Status,
		uint16(a.SchemaVersion),
		a.CreatedAt.UTC(),
		a.UpdatedAt.UTC(),
		deletedAt,
	)
	return err
}

func traceAnnotationSelectSQL(suffix string) string {
	return `SELECT annotation_id, revision, target_type, session_id, event_uid,
	       author_type, author_id, author_name, source,
	       category, outcome, quality_score, confidence, needs_followup,
	       arrayStringConcat(labels, '` + annotationLabelJoiner + `') AS labels,
	       note, metadata_json, status, schema_version,
	       created_at, updated_at, deleted_at
	FROM trace_annotations FINAL ` + suffix
}

type traceAnnotationScanner interface {
	Scan(dest ...any) error
}

func scanTraceAnnotation(scanner traceAnnotationScanner) (models.TraceAnnotation, error) {
	var a models.TraceAnnotation
	var revision int64
	var qualityScore int16
	var confidence uint8
	var needsFollowup uint8
	var labels string
	var schemaVersion uint16
	var deletedAt time.Time
	if err := scanner.Scan(
		&a.AnnotationID,
		&revision,
		&a.TargetType,
		&a.SessionID,
		&a.EventUID,
		&a.AuthorType,
		&a.AuthorID,
		&a.AuthorName,
		&a.Source,
		&a.Category,
		&a.Outcome,
		&qualityScore,
		&confidence,
		&needsFollowup,
		&labels,
		&a.Note,
		&a.MetadataJSON,
		&a.Status,
		&schemaVersion,
		&a.CreatedAt,
		&a.UpdatedAt,
		&deletedAt,
	); err != nil {
		return models.TraceAnnotation{}, err
	}
	if revision > 0 {
		a.Revision = uint64(revision)
	}
	a.QualityScore = int(qualityScore)
	a.Confidence = int(confidence)
	a.NeedsFollowup = needsFollowup != 0
	a.Labels = splitAnnotationLabels(labels)
	a.SchemaVersion = int(schemaVersion)
	if !deletedAt.IsZero() && !deletedAt.Equal(time.Unix(0, 0).UTC()) {
		a.DeletedAt = &deletedAt
	}
	return a, nil
}

func newAnnotationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate annotation id: %w", err)
	}
	return "ann_" + hex.EncodeToString(b[:]), nil
}

func splitAnnotationLabels(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return models.NormalizeAnnotationLabels(strings.Split(value, annotationLabelJoiner))
}

func annotationFilterWhere(filter AnnotationFilter) ([]string, []any) {
	filter.AnnotationID = strings.TrimSpace(filter.AnnotationID)
	filter.TargetType = strings.TrimSpace(strings.ToLower(filter.TargetType))
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.EventUID = strings.TrimSpace(filter.EventUID)
	filter.AuthorType = strings.TrimSpace(strings.ToLower(filter.AuthorType))
	filter.Source = strings.TrimSpace(strings.ToLower(filter.Source))
	filter.Category = strings.TrimSpace(strings.ToLower(filter.Category))
	filter.Outcome = strings.TrimSpace(strings.ToLower(filter.Outcome))
	filter.Label = strings.TrimSpace(strings.ToLower(filter.Label))
	filter.SessionIDs = compactAnnotationStrings(filter.SessionIDs)

	where := []string{"1 = 1"}
	args := []any{}
	if filter.AnnotationID != "" {
		where = append(where, "annotation_id = ?")
		args = append(args, filter.AnnotationID)
	}
	if filter.TargetType != "" {
		where = append(where, "target_type = ?")
		args = append(args, filter.TargetType)
	}
	if filter.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if len(filter.SessionIDs) > 0 {
		where = append(where, "session_id IN ("+strings.TrimRight(strings.Repeat("?,", len(filter.SessionIDs)), ",")+")")
		for _, sessionID := range filter.SessionIDs {
			args = append(args, sessionID)
		}
	}
	if filter.EventUID != "" {
		where = append(where, "event_uid = ?")
		args = append(args, filter.EventUID)
	}
	if filter.AuthorType != "" {
		where = append(where, "author_type = ?")
		args = append(args, filter.AuthorType)
	}
	if filter.Source != "" {
		where = append(where, "source = ?")
		args = append(args, filter.Source)
	}
	if filter.Category != "" {
		where = append(where, "category = ?")
		args = append(args, filter.Category)
	}
	if filter.Outcome != "" {
		where = append(where, "outcome = ?")
		args = append(args, filter.Outcome)
	}
	if filter.Label != "" {
		where = append(where, "has(labels, ?)")
		args = append(args, filter.Label)
	}
	if filter.NeedsFollowup != nil {
		where = append(where, "needs_followup = ?")
		args = append(args, boolToUInt8(*filter.NeedsFollowup))
	}
	if !filter.IncludeDeleted {
		where = append(where, "status != ?")
		args = append(args, models.AnnotationStatusDeleted)
	}
	return where, args
}

func normalizeAnnotationLimit(limit int) int {
	if limit <= 0 {
		return defaultAnnotationLimit
	}
	if limit > maxAnnotationLimit {
		return maxAnnotationLimit
	}
	return limit
}

func normalizeAnnotationOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func compactAnnotationStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func boolToUInt8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}
