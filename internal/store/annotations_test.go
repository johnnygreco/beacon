package store

import (
	"reflect"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/models"
)

func TestTraceAnnotationSelectSQLTargetsFinalTable(t *testing.T) {
	query := traceAnnotationSelectSQL("WHERE session_id = ?")
	for _, want := range []string{
		"SELECT annotation_id, revision, target_type, session_id, event_uid",
		"arrayStringConcat(labels, ','",
		"FROM trace_annotations FINAL",
		"WHERE session_id = ?",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

func TestSplitAnnotationLabelsNormalizesValues(t *testing.T) {
	got := splitAnnotationLabels("zeta,alpha,zeta,quality:good")
	want := []string{"alpha", "quality:good", "zeta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %#v, want %#v", got, want)
	}
}

func TestAnnotationFilterWhereSupportsDatasetFilters(t *testing.T) {
	followup := true
	where, args := annotationFilterWhere(AnnotationFilter{
		TargetType:     models.AnnotationTargetMessage,
		SessionIDs:     []string{"session-2", "session-1", "session-2"},
		EventUID:       "event-1",
		AuthorType:     models.AnnotationAuthorAgent,
		Source:         models.AnnotationSourceMCP,
		Category:       "quality",
		Outcome:        "useful",
		Label:          "dataset:eval",
		NeedsFollowup:  &followup,
		IncludeDeleted: false,
	})
	joined := strings.Join(where, " AND ")
	for _, want := range []string{
		"target_type = ?",
		"session_id IN (?,?)",
		"event_uid = ?",
		"author_type = ?",
		"source = ?",
		"category = ?",
		"outcome = ?",
		"has(labels, ?)",
		"needs_followup = ?",
		"status != ?",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("filter where missing %q:\n%s", want, joined)
		}
	}
	wantArgs := []any{
		models.AnnotationTargetMessage,
		"session-2",
		"session-1",
		"event-1",
		models.AnnotationAuthorAgent,
		models.AnnotationSourceMCP,
		"quality",
		"useful",
		"dataset:eval",
		uint8(1),
		models.AnnotationStatusDeleted,
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("filter args = %#v, want %#v", args, wantArgs)
	}
}

func TestNewAnnotationIDUsesStablePrefix(t *testing.T) {
	id, err := newAnnotationID()
	if err != nil {
		t.Fatalf("newAnnotationID: %v", err)
	}
	if !strings.HasPrefix(id, "ann_") || len(id) != len("ann_")+32 {
		t.Fatalf("annotation id = %q, want ann_ plus 32 hex chars", id)
	}
}
