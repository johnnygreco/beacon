package models

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	AnnotationSchemaVersion = 1

	AnnotationTargetSession = "session"
	AnnotationTargetMessage = "message"
	AnnotationTargetEvent   = "event"

	AnnotationAuthorHuman = "human"
	AnnotationAuthorAgent = "agent"

	AnnotationSourceAPI = "api"
	AnnotationSourceUI  = "ui"
	AnnotationSourceMCP = "mcp"

	AnnotationStatusActive  = "active"
	AnnotationStatusDeleted = "deleted"

	MaxAnnotationNoteBytes     = 20 * 1024
	MaxAnnotationMetadataBytes = 64 * 1024
	MaxAnnotationLabels        = 32
	MaxAnnotationLabelBytes    = 64
	MaxAnnotationFieldBytes    = 256
)

var annotationLabelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,63}$`)

type AnnotationValidationError struct {
	Message string
}

func (e *AnnotationValidationError) Error() string {
	return e.Message
}

func NewAnnotationValidationError(format string, args ...any) error {
	return &AnnotationValidationError{Message: fmt.Sprintf(format, args...)}
}

type TraceAnnotation struct {
	AnnotationID  string     `json:"annotation_id"`
	Revision      uint64     `json:"revision"`
	TargetType    string     `json:"target_type"`
	SessionID     string     `json:"session_id"`
	EventUID      string     `json:"event_uid,omitempty"`
	AuthorType    string     `json:"author_type"`
	AuthorID      string     `json:"author_id,omitempty"`
	AuthorName    string     `json:"author_name,omitempty"`
	Source        string     `json:"source"`
	Category      string     `json:"category,omitempty"`
	Outcome       string     `json:"outcome,omitempty"`
	QualityScore  int        `json:"quality_score,omitempty"`
	Confidence    int        `json:"confidence,omitempty"`
	NeedsFollowup bool       `json:"needs_followup"`
	Labels        []string   `json:"labels"`
	Note          string     `json:"note"`
	MetadataJSON  string     `json:"metadata_json,omitempty"`
	Status        string     `json:"status"`
	SchemaVersion int        `json:"schema_version"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

func NormalizeTraceAnnotation(a TraceAnnotation) TraceAnnotation {
	a.AnnotationID = strings.TrimSpace(a.AnnotationID)
	a.TargetType = normalizeAnnotationEnum(a.TargetType)
	a.SessionID = strings.TrimSpace(a.SessionID)
	a.EventUID = strings.TrimSpace(a.EventUID)
	a.AuthorType = normalizeAnnotationEnum(a.AuthorType)
	a.AuthorID = strings.TrimSpace(a.AuthorID)
	a.AuthorName = strings.TrimSpace(a.AuthorName)
	a.Source = normalizeAnnotationEnum(a.Source)
	a.Category = normalizeAnnotationEnum(a.Category)
	a.Outcome = normalizeAnnotationEnum(a.Outcome)
	a.Labels = NormalizeAnnotationLabels(a.Labels)
	a.MetadataJSON = strings.TrimSpace(a.MetadataJSON)
	a.Status = normalizeAnnotationEnum(a.Status)

	if a.TargetType == "" {
		if a.EventUID != "" {
			a.TargetType = AnnotationTargetEvent
		} else {
			a.TargetType = AnnotationTargetSession
		}
	}
	if a.AuthorType == "" {
		a.AuthorType = AnnotationAuthorHuman
	}
	if a.Source == "" {
		a.Source = AnnotationSourceAPI
	}
	if a.Status == "" {
		a.Status = AnnotationStatusActive
	}
	if a.SchemaVersion == 0 {
		a.SchemaVersion = AnnotationSchemaVersion
	}
	if a.Revision == 0 {
		a.Revision = 1
	}
	return a
}

func NormalizeAnnotationLabels(labels []string) []string {
	seen := make(map[string]struct{}, len(labels))
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = normalizeAnnotationEnum(label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

func ValidateTraceAnnotation(a TraceAnnotation) error {
	a = NormalizeTraceAnnotation(a)
	switch a.TargetType {
	case AnnotationTargetSession:
		if a.EventUID != "" {
			return NewAnnotationValidationError("session annotations must not include event_uid")
		}
	case AnnotationTargetMessage:
		if a.EventUID == "" {
			return NewAnnotationValidationError("message annotations require event_uid")
		}
	case AnnotationTargetEvent:
		if a.EventUID == "" {
			return NewAnnotationValidationError("event annotations require event_uid")
		}
	default:
		return NewAnnotationValidationError("target_type must be session, message, or event")
	}
	if a.SessionID == "" {
		return NewAnnotationValidationError("session_id is required")
	}
	switch a.AuthorType {
	case AnnotationAuthorHuman, AnnotationAuthorAgent:
	default:
		return NewAnnotationValidationError("author_type must be human or agent")
	}
	switch a.Source {
	case AnnotationSourceAPI, AnnotationSourceUI, AnnotationSourceMCP:
	default:
		return NewAnnotationValidationError("source must be api, ui, or mcp")
	}
	switch a.Status {
	case AnnotationStatusActive, AnnotationStatusDeleted:
	default:
		return NewAnnotationValidationError("status must be active or deleted")
	}
	if a.Note == "" && len(a.Labels) == 0 && a.Category == "" && a.Outcome == "" && a.QualityScore == 0 && a.Confidence == 0 && !a.NeedsFollowup && a.MetadataJSON == "" {
		return NewAnnotationValidationError("annotation requires note, labels, category, outcome, score, confidence, follow-up, or metadata")
	}
	if len(a.Note) > MaxAnnotationNoteBytes {
		return NewAnnotationValidationError("note exceeds %d bytes", MaxAnnotationNoteBytes)
	}
	if len(a.MetadataJSON) > MaxAnnotationMetadataBytes {
		return NewAnnotationValidationError("metadata_json exceeds %d bytes", MaxAnnotationMetadataBytes)
	}
	if a.MetadataJSON != "" {
		var metadata map[string]any
		if err := json.Unmarshal([]byte(a.MetadataJSON), &metadata); err != nil {
			return NewAnnotationValidationError("metadata_json must be a JSON object")
		}
		if metadata == nil {
			return NewAnnotationValidationError("metadata_json must be a JSON object")
		}
	}
	if len(a.Labels) > MaxAnnotationLabels {
		return NewAnnotationValidationError("labels exceed %d items", MaxAnnotationLabels)
	}
	for _, label := range a.Labels {
		if len(label) > MaxAnnotationLabelBytes || !annotationLabelPattern.MatchString(label) {
			return NewAnnotationValidationError("labels must use lowercase letters, numbers, '.', '_', ':', or '-' and be at most %d bytes", MaxAnnotationLabelBytes)
		}
	}
	if a.Category != "" && len(a.Category) > MaxAnnotationFieldBytes {
		return NewAnnotationValidationError("category exceeds %d bytes", MaxAnnotationFieldBytes)
	}
	if a.Outcome != "" && len(a.Outcome) > MaxAnnotationFieldBytes {
		return NewAnnotationValidationError("outcome exceeds %d bytes", MaxAnnotationFieldBytes)
	}
	if a.AuthorID != "" && len(a.AuthorID) > MaxAnnotationFieldBytes {
		return NewAnnotationValidationError("author_id exceeds %d bytes", MaxAnnotationFieldBytes)
	}
	if a.AuthorName != "" && len(a.AuthorName) > MaxAnnotationFieldBytes {
		return NewAnnotationValidationError("author_name exceeds %d bytes", MaxAnnotationFieldBytes)
	}
	if a.QualityScore < 0 || a.QualityScore > 5 {
		return NewAnnotationValidationError("quality_score must be between 0 and 5")
	}
	if a.Confidence < 0 || a.Confidence > 100 {
		return NewAnnotationValidationError("confidence must be between 0 and 100")
	}
	return nil
}

func normalizeAnnotationEnum(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "-")
	return value
}
