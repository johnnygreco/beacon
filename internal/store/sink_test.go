package store

import (
	"fmt"
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

func TestSessionEndProjectionPredicateRequiresNormalizedEventKind(t *testing.T) {
	if sessionEndProjectionPredicate != "event_kind = 'session_end'" {
		t.Fatalf("predicate = %q, want normalized session_end only", sessionEndProjectionPredicate)
	}
	for _, legacyFragment := range []string{"event_msg", "payload_type", "last-prompt"} {
		if strings.Contains(sessionEndProjectionPredicate, legacyFragment) {
			t.Fatalf("predicate must not preserve legacy %q handling: %q", legacyFragment, sessionEndProjectionPredicate)
		}
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

func TestBuildSearchRowsIndexesFrequenciesAndMetadata(t *testing.T) {
	timestamp := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	event := models.Event{
		EventUID:    "evt-frequency",
		SessionID:   "session-frequency",
		EventKind:   "message",
		ActorRole:   "assistant",
		Timestamp:   timestamp,
		TextContent: "Alpha alpha beta",
		ToolName:    "Bash",
		Model:       "gpt-5.4",
		Provider:    "openai",
	}

	docs, postings := buildSearchRows([]models.Event{event}, nil)
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if docs[0].EventUID != event.EventUID || docs[0].Model != event.Model || docs[0].Provider != event.Provider {
		t.Fatalf("doc metadata = %#v, want event metadata", docs[0])
	}

	frequencies := make(map[string]int)
	totalFrequency := 0
	for _, posting := range postings {
		if posting.EventUID != event.EventUID || posting.DocumentLength != docs[0].DocumentLength {
			t.Fatalf("posting metadata = %#v, want event uid/document length", posting)
		}
		frequencies[posting.Token] = posting.TermFrequency
		totalFrequency += posting.TermFrequency
	}
	if docs[0].DocumentLength != totalFrequency {
		t.Fatalf("document length = %d, want sum of frequencies %d", docs[0].DocumentLength, totalFrequency)
	}
	for token, want := range map[string]int{"alpha": 2, "beta": 1, "message": 1, "assistant": 1} {
		if got := frequencies[token]; got != want {
			t.Fatalf("frequency[%q] = %d, want %d; postings=%v", token, got, want, frequencies)
		}
	}
}

func TestBuildSearchRowsSkipsEmptyDocuments(t *testing.T) {
	docs, postings := buildSearchRows([]models.Event{{EventUID: "empty-event"}}, nil)
	if len(docs) != 0 || len(postings) != 0 {
		t.Fatalf("empty event produced docs/postings = %d/%d", len(docs), len(postings))
	}
}

func TestRecordUIDChangesWithSourceGenerationAndPayload(t *testing.T) {
	base := recordUID("session.jsonl", 10, 20, 1, `{"message":"one"}`)
	tests := []struct {
		name string
		uid  string
	}{
		{name: "same inputs", uid: recordUID("session.jsonl", 10, 20, 1, `{"message":"one"}`)},
		{name: "different generation", uid: recordUID("session.jsonl", 10, 20, 2, `{"message":"one"}`)},
		{name: "different payload", uid: recordUID("session.jsonl", 10, 20, 1, `{"message":"two"}`)},
	}

	if len(base) != 32 {
		t.Fatalf("uid length = %d, want 32", len(base))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.name {
			case "same inputs":
				if tt.uid != base {
					t.Fatalf("uid = %q, want stable %q", tt.uid, base)
				}
			default:
				if tt.uid == base {
					t.Fatalf("%s uid should differ from base %q", tt.name, base)
				}
			}
		})
	}
}

func TestStorePureHelpers(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 123, time.FixedZone("offset", -5*60*60))

	if got := runtimeForSource("claude"); got != "claude-code" {
		t.Fatalf("runtimeForSource(claude) = %q", got)
	}
	if got := runtimeForSource("custom"); got != "custom" {
		t.Fatalf("runtimeForSource(custom) = %q", got)
	}
	if got := firstNonEmpty("", "fallback"); got != "fallback" {
		t.Fatalf("firstNonEmpty blank = %q", got)
	}
	if got := fmt.Sprint(sessionIDs([]models.Event{{SessionID: "s1"}, {}, {SessionID: "s2"}})); got != "[s1 s2]" {
		t.Fatalf("sessionIDs = %s", got)
	}
	if got := fmt.Sprint(uniqStrings([]string{"s1", "", "s2", "s1"})); got != "[s1 s2]" {
		t.Fatalf("uniqStrings = %s", got)
	}
	if got := placeholders(3); got != "?,?,?" {
		t.Fatalf("placeholders = %q", got)
	}
	if got := nonZeroTime(time.Time{}, now); !got.Equal(now.UTC()) {
		t.Fatalf("nonZeroTime zero = %s, want %s", got, now.UTC())
	}
	if got := nonNegativeInt(-7); got != 0 {
		t.Fatalf("nonNegativeInt = %d, want 0", got)
	}
	if got := nonNegativeInt64(-9); got != 0 {
		t.Fatalf("nonNegativeInt64 = %d, want 0", got)
	}
	if got := truncateString("abcdef", 3); got != "abc" {
		t.Fatalf("truncateString = %q, want abc", got)
	}
}
