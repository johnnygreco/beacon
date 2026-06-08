package store

import (
	"reflect"
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

func TestBuildSearchRowsIndexesFullPayloadContent(t *testing.T) {
	event := models.Event{
		EventUID:    "evt-tool-call-full",
		SessionID:   "session-1",
		EventKind:   "tool_call",
		Timestamp:   time.Now().UTC(),
		TextPreview: "Read",
		ToolName:    "Read",
		PayloadJSON: `{"request":"eventjsonneedle"}`,
	}
	payload := models.ToolPayload{
		EventUID:   event.EventUID,
		InputJSON:  `{"file_path":"inputjsonneedle.go"}`,
		OutputJSON: `{"result":"outputjsonneedle"}`,
	}

	docs, postings := buildSearchRows([]models.Event{event}, []models.ToolPayload{payload})
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	for _, needle := range []string{"eventjsonneedle", "inputjsonneedle.go", "outputjsonneedle"} {
		if !strings.Contains(docs[0].SearchableText, needle) {
			t.Fatalf("searchable text missing %q: %q", needle, docs[0].SearchableText)
		}
	}
	tokens := make(map[string]int, len(postings))
	for _, posting := range postings {
		tokens[posting.Token] = posting.TermFrequency
	}
	for _, token := range []string{"eventjsonneedle", "inputjsonneedle.go", "outputjsonneedle"} {
		if tokens[token] != 1 {
			t.Fatalf("posting frequency[%q] = %d, want 1; postings=%#v", token, tokens[token], postings)
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

func TestProjectionInsertSQLUsesDedupedActivityEvents(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "session projection",
			query: sessionProjectionInsertSQL("?,?"),
			want: []string{
				"FROM (\n\t\t\tSELECT event_uid",
				"argMax(session_id, captured_at) AS projected_session_id",
				"WHERE session_id IN (?,?)",
				"GROUP BY event_uid",
				"GROUP BY projected_session_id",
			},
		},
		{
			name:  "analytics projection",
			query: analyticsProjectionInsertSQL("?,?,?"),
			want: []string{
				"FROM (\n\t\t\tSELECT event_uid",
				"argMax(session_id, captured_at) AS projected_session_id",
				"WHERE session_id IN (?,?,?)",
				"GROUP BY event_uid",
				"GROUP BY projected_session_id, minute, provider, model, tool_name, event_kind",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, fragment := range tt.want {
				if !strings.Contains(tt.query, fragment) {
					t.Fatalf("%s SQL missing %q:\n%s", tt.name, fragment, tt.query)
				}
			}
		})
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

func TestRecordUIDChangesWithFleetRawIdentityAndPayload(t *testing.T) {
	base := recordUID("collector-a", "source-a", "raw-session", "raw-event", "digest-one")
	tests := []struct {
		name string
		uid  string
	}{
		{name: "same inputs", uid: recordUID("collector-a", "source-a", "raw-session", "raw-event", "digest-one")},
		{name: "different collector", uid: recordUID("collector-b", "source-a", "raw-session", "raw-event", "digest-one")},
		{name: "different source", uid: recordUID("collector-a", "source-b", "raw-session", "raw-event", "digest-one")},
		{name: "different raw event", uid: recordUID("collector-a", "source-a", "raw-session", "raw-event-2", "digest-one")},
		{name: "different payload", uid: recordUID("collector-a", "source-a", "raw-session", "raw-event", "digest-two")},
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

	for _, tt := range []struct {
		source string
		want   string
	}{
		{source: "claude", want: "claude-code"},
		{source: "codex", want: "codex"},
		{source: "custom", want: "custom"},
	} {
		t.Run("runtimeForSource/"+tt.source, func(t *testing.T) {
			if got := runtimeForSource(tt.source); got != tt.want {
				t.Fatalf("runtimeForSource(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{name: "value present", value: "value", fallback: "fallback", want: "value"},
		{name: "fallback used", fallback: "fallback", want: "fallback"},
	} {
		t.Run("firstNonEmpty/"+tt.name, func(t *testing.T) {
			if got := firstNonEmpty(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("firstNonEmpty(%q, %q) = %q, want %q", tt.value, tt.fallback, got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name   string
		events []models.Event
		want   []string
	}{
		{name: "filters empty ids", events: []models.Event{{SessionID: "s1"}, {}, {SessionID: "s2"}}, want: []string{"s1", "s2"}},
		{name: "empty", events: nil, want: []string{}},
	} {
		t.Run("sessionIDs/"+tt.name, func(t *testing.T) {
			if got := sessionIDs(tt.events); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("sessionIDs = %#v, want %#v", got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name   string
		values []string
		want   []string
	}{
		{name: "dedupes and filters empty", values: []string{"s1", "", "s2", "s1"}, want: []string{"s1", "s2"}},
		{name: "keeps order", values: []string{"b", "a", "b"}, want: []string{"b", "a"}},
	} {
		t.Run("uniqStrings/"+tt.name, func(t *testing.T) {
			if got := uniqStrings(tt.values); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("uniqStrings = %#v, want %#v", got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name string
		n    int
		want string
	}{
		{name: "zero", n: 0, want: ""},
		{name: "negative", n: -1, want: ""},
		{name: "three", n: 3, want: "?,?,?"},
	} {
		t.Run("placeholders/"+tt.name, func(t *testing.T) {
			if got := placeholders(tt.n); got != tt.want {
				t.Fatalf("placeholders(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name     string
		t        time.Time
		fallback time.Time
		want     time.Time
	}{
		{name: "zero uses fallback", fallback: now, want: now.UTC()},
		{name: "non-zero converted to UTC", t: now, fallback: time.Time{}, want: now.UTC()},
	} {
		t.Run("nonZeroTime/"+tt.name, func(t *testing.T) {
			if got := nonZeroTime(tt.t, tt.fallback); !got.Equal(tt.want) {
				t.Fatalf("nonZeroTime = %s, want %s", got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name string
		in   int
		want int
	}{
		{name: "positive", in: 7, want: 7},
		{name: "negative", in: -7, want: 0},
	} {
		t.Run("nonNegativeInt/"+tt.name, func(t *testing.T) {
			if got := nonNegativeInt(tt.in); got != tt.want {
				t.Fatalf("nonNegativeInt(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name string
		in   int64
		want int64
	}{
		{name: "positive", in: 9, want: 9},
		{name: "negative", in: -9, want: 0},
	} {
		t.Run("nonNegativeInt64/"+tt.name, func(t *testing.T) {
			if got := nonNegativeInt64(tt.in); got != tt.want {
				t.Fatalf("nonNegativeInt64(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "truncates", in: "abcdef", max: 3, want: "abc"},
		{name: "keeps short", in: "abc", max: 3, want: "abc"},
	} {
		t.Run("truncateString/"+tt.name, func(t *testing.T) {
			if got := truncateString(tt.in, tt.max); got != tt.want {
				t.Fatalf("truncateString(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}
