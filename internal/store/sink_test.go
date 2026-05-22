package store

import (
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func TestBuildSearchRowsUsesToolInputPreviewForSnippet(t *testing.T) {
	event := models.Event{
		EventUID:    "evt-tool-call",
		SessionID:   "session-1",
		EventKind:   "tool_call",
		Timestamp:   time.Now().UTC(),
		TextPreview: "Bash",
		ToolName:    "Bash",
	}
	payload := models.ToolPayload{
		EventUID:     event.EventUID,
		InputPreview: `{"command":"rg unique-needle internal"}`,
	}

	docs, postings := buildSearchRows([]models.Event{event}, []models.ToolPayload{payload})
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if !strings.Contains(docs[0].SearchableText, "unique-needle") {
		t.Fatalf("searchable text missing payload preview: %q", docs[0].SearchableText)
	}
	if !strings.Contains(docs[0].TextPreview, "unique-needle") {
		t.Fatalf("text preview = %q, want payload command context", docs[0].TextPreview)
	}
	for _, posting := range postings {
		if posting.TextPreview != docs[0].TextPreview {
			t.Fatalf("posting preview = %q, want %q", posting.TextPreview, docs[0].TextPreview)
		}
	}
}

func TestSessionEndProjectionPredicateRecognizesHarnessEndEvents(t *testing.T) {
	if !strings.Contains(sessionEndProjectionPredicate, "event_kind = 'session_end'") {
		t.Fatalf("predicate must recognize session_end events: %q", sessionEndProjectionPredicate)
	}
	if strings.Contains(sessionEndProjectionPredicate, "event_kind = 'session_end' AND payload_type") {
		t.Fatalf("session_end events must not be restricted to one payload type: %q", sessionEndProjectionPredicate)
	}
	if !strings.Contains(sessionEndProjectionPredicate, "payload_type = 'last-prompt'") {
		t.Fatalf("predicate must preserve legacy last-prompt event_msg handling: %q", sessionEndProjectionPredicate)
	}
}

func TestSessionProjectionSQLIgnoresZeroTimestampForTiming(t *testing.T) {
	query := sessionProjectionInsertSQL("?,?")
	if !strings.Contains(query, "minIf(timestamp, "+validEventTimestampPredicate+")") {
		t.Fatalf("session started_at must ignore zero timestamp events: %s", query)
	}
	if !strings.Contains(query, "maxIf(timestamp, "+validEventTimestampPredicate+")") {
		t.Fatalf("session ended_at must ignore zero timestamp events: %s", query)
	}
	if !strings.Contains(query, "countIf("+validEventTimestampPredicate+") > 0") {
		t.Fatalf("session projection must guard sessions with no valid timestamps: %s", query)
	}
	if !strings.Contains(query, "max(if("+sessionEndProjectionPredicate+", 1, 0)) AS has_session_end") {
		t.Fatalf("session end detection should remain independent from timestamp validity: %s", query)
	}
}

func TestBuildSearchRowsUsesToolOutputPreviewForSnippet(t *testing.T) {
	event := models.Event{
		EventUID:  "evt-tool-error",
		SessionID: "session-1",
		EventKind: "tool_error",
		Timestamp: time.Now().UTC(),
		ToolName:  "Bash",
	}
	payload := models.ToolPayload{
		InputPreview:  `{"command":"cat /secure/path"}`,
		EventUID:      event.EventUID,
		OutputPreview: "permission denied opening /secure/path",
	}

	docs, _ := buildSearchRows([]models.Event{event}, []models.ToolPayload{payload})
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if !strings.Contains(docs[0].TextPreview, "permission denied") {
		t.Fatalf("text preview = %q, want output error context", docs[0].TextPreview)
	}
}

func TestBuildSearchRowsKeepsNonToolSnippet(t *testing.T) {
	event := models.Event{
		EventUID:    "evt-message",
		SessionID:   "session-1",
		EventKind:   "message",
		Timestamp:   time.Now().UTC(),
		TextContent: "message full text",
		TextPreview: "message preview",
	}

	docs, _ := buildSearchRows([]models.Event{event}, nil)
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if docs[0].TextPreview != "message preview" {
		t.Fatalf("text preview = %q, want non-tool preview unchanged", docs[0].TextPreview)
	}
}
