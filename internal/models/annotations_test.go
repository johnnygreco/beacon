package models

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTraceAnnotationDefaultsAndLabels(t *testing.T) {
	got := NormalizeTraceAnnotation(TraceAnnotation{
		SessionID:  "session-1",
		Labels:     []string{" Regression ", "quality:good", "regression", "debug.fix"},
		AuthorName: strings.Repeat("a", MaxAnnotationFieldBytes+20),
	})

	if got.TargetType != AnnotationTargetSession || got.AuthorType != AnnotationAuthorHuman || got.Source != AnnotationSourceAPI || got.Status != AnnotationStatusActive {
		t.Fatalf("defaults = target %q author %q source %q status %q", got.TargetType, got.AuthorType, got.Source, got.Status)
	}
	if got.SchemaVersion != AnnotationSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got.SchemaVersion, AnnotationSchemaVersion)
	}
	if got.Revision != 1 {
		t.Fatalf("revision = %d, want 1", got.Revision)
	}
	wantLabels := []string{"debug.fix", "quality:good", "regression"}
	if strings.Join(got.Labels, ",") != strings.Join(wantLabels, ",") {
		t.Fatalf("labels = %#v, want %#v", got.Labels, wantLabels)
	}
	if len(got.AuthorName) != MaxAnnotationFieldBytes+20 {
		t.Fatalf("author name length = %d, want untruncated input", len(got.AuthorName))
	}
}

func TestValidateTraceAnnotation(t *testing.T) {
	valid := TraceAnnotation{
		TargetType:   AnnotationTargetEvent,
		SessionID:    "session-1",
		EventUID:     "event-1",
		AuthorType:   AnnotationAuthorAgent,
		Source:       AnnotationSourceMCP,
		Labels:       []string{"quality:good"},
		MetadataJSON: `{"rubric":"qa"}`,
	}
	if err := ValidateTraceAnnotation(valid); err != nil {
		t.Fatalf("valid annotation rejected: %v", err)
	}
	valid.TargetType = AnnotationTargetMessage
	if err := ValidateTraceAnnotation(valid); err != nil {
		t.Fatalf("valid message annotation rejected: %v", err)
	}

	tests := []struct {
		name string
		ann  TraceAnnotation
		want string
	}{
		{
			name: "session target with event",
			ann:  TraceAnnotation{TargetType: AnnotationTargetSession, SessionID: "session-1", EventUID: "event-1", Note: "bad"},
			want: "session annotations must not include event_uid",
		},
		{
			name: "event without event uid",
			ann:  TraceAnnotation{TargetType: AnnotationTargetEvent, SessionID: "session-1", Note: "bad"},
			want: "event annotations require event_uid",
		},
		{
			name: "message without event uid",
			ann:  TraceAnnotation{TargetType: AnnotationTargetMessage, SessionID: "session-1", Note: "bad"},
			want: "message annotations require event_uid",
		},
		{
			name: "empty content",
			ann:  TraceAnnotation{TargetType: AnnotationTargetSession, SessionID: "session-1"},
			want: "annotation requires",
		},
		{
			name: "bad metadata",
			ann:  TraceAnnotation{TargetType: AnnotationTargetSession, SessionID: "session-1", MetadataJSON: `[]`},
			want: "metadata_json must be a JSON object",
		},
		{
			name: "bad label",
			ann:  TraceAnnotation{TargetType: AnnotationTargetSession, SessionID: "session-1", Labels: []string{"bad,label"}},
			want: "labels must use lowercase",
		},
		{
			name: "bad score",
			ann:  TraceAnnotation{TargetType: AnnotationTargetSession, SessionID: "session-1", Note: "score", QualityScore: 7},
			want: "quality_score must be between 0 and 5",
		},
		{
			name: "bad confidence",
			ann:  TraceAnnotation{TargetType: AnnotationTargetSession, SessionID: "session-1", Note: "confidence", Confidence: 101},
			want: "confidence must be between 0 and 100",
		},
		{
			name: "oversize author",
			ann:  TraceAnnotation{TargetType: AnnotationTargetSession, SessionID: "session-1", Note: "author", AuthorName: strings.Repeat("a", MaxAnnotationFieldBytes+1)},
			want: "author_name exceeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTraceAnnotation(NormalizeTraceAnnotation(tt.ann))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			var validation *AnnotationValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error type = %T, want AnnotationValidationError", err)
			}
		})
	}
}
