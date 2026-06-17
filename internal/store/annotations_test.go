package store

import (
	"strings"
	"testing"
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

func TestNewAnnotationIDUsesStablePrefix(t *testing.T) {
	id, err := newAnnotationID()
	if err != nil {
		t.Fatalf("newAnnotationID: %v", err)
	}
	if !strings.HasPrefix(id, "ann_") || len(id) != len("ann_")+32 {
		t.Fatalf("annotation id = %q, want ann_ plus 32 hex chars", id)
	}
}
