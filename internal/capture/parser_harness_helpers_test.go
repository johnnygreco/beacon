package capture

import (
	"encoding/json"
	"testing"
)

func TestJSONExtractionHelpers(t *testing.T) {
	raw := map[string]any{
		"string":  "value",
		"int":     7,
		"int64":   int64(8),
		"float":   9.8,
		"number":  json.Number("10"),
		"object":  map[string]any{"nested": true},
		"array":   []any{"a", "b"},
		"invalid": "not a number",
	}

	if got := stringField(raw, "string"); got != "value" {
		t.Fatalf("stringField = %q, want value", got)
	}
	if got := stringField(nil, "string"); got != "" {
		t.Fatalf("stringField nil map = %q, want empty", got)
	}
	if got := int64Field(raw, "int"); got != 7 {
		t.Fatalf("int64Field int = %d, want 7", got)
	}
	if got := int64Field(raw, "int64"); got != 8 {
		t.Fatalf("int64Field int64 = %d, want 8", got)
	}
	if got := int64Field(raw, "float"); got != 9 {
		t.Fatalf("int64Field float = %d, want 9", got)
	}
	if got := int64Field(raw, "number"); got != 10 {
		t.Fatalf("int64Field json.Number = %d, want 10", got)
	}
	if got := int64Field(raw, "invalid"); got != 0 {
		t.Fatalf("int64Field invalid = %d, want 0", got)
	}
	if got := floatFromAny(json.Number("3.5")); got != 3.5 {
		t.Fatalf("floatFromAny json.Number = %f, want 3.5", got)
	}
	if got := objectFromAny(raw["object"]); got == nil || got["nested"] != true {
		t.Fatalf("objectFromAny = %#v, want nested object", got)
	}
	if got := arrayFromAny(raw["array"]); len(got) != 2 {
		t.Fatalf("arrayFromAny len = %d, want 2", len(got))
	}
}

func TestSQLiteHarnessHelpers(t *testing.T) {
	dbPath := createSQLiteFixtureFromSQL(t, `CREATE TABLE sessions (id TEXT PRIMARY KEY, message TEXT);`)
	db, err := openSQLiteReadOnly(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if !sqliteHasTable(db, "sessions") {
		t.Fatal("expected sessions table")
	}
	if sqliteHasTable(db, "missing") {
		t.Fatal("expected missing table to be absent")
	}
	if !sqliteHasColumn(db, "sessions", "message") {
		t.Fatal("expected sessions.message column")
	}
	if sqliteHasColumn(db, "sessions", "missing") {
		t.Fatal("expected missing column to be absent")
	}

	if got, want := sqliteStableRaw("source", "table", "id-1", "kind"), `{"id":"id-1","kind":"kind","source":"source","table":"table"}`; got != want {
		t.Fatalf("sqliteStableRaw = %q, want %q", got, want)
	}
	if got, want := scopedMessageUUID("base", "", "part"), "base:part"; got != want {
		t.Fatalf("scopedMessageUUID = %q, want %q", got, want)
	}
	if got := scopedMessageUUID(""); got != "" {
		t.Fatalf("scopedMessageUUID empty base = %q, want empty", got)
	}
	if got := stableLineNo("same", "key"); got == 0 || got != stableLineNo("same", "key") {
		t.Fatalf("stableLineNo not stable/non-zero: %d", got)
	}
	if got := stableOffset("same", "key"); got == 0 || got != stableOffset("same", "key") {
		t.Fatalf("stableOffset not stable/non-zero: %d", got)
	}
	if got := timeFromUnixSeconds(1.5); got.Unix() != 1 || got.Nanosecond() != 500000000 {
		t.Fatalf("timeFromUnixSeconds = %s, want 1.5s unix", got)
	}
	if got := timeFromUnixMillis(1500); got.Unix() != 1 || got.Nanosecond() != 500000000 {
		t.Fatalf("timeFromUnixMillis = %s, want 1.5s unix", got)
	}
	if !timeFromUnixSeconds(0).IsZero() || !timeFromUnixMillis(0).IsZero() {
		t.Fatal("expected non-positive unix times to return zero time")
	}
}

func TestDecodeHarnessJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want any
	}{
		{name: "empty", raw: "", want: ""},
		{name: "plain", raw: "plain text", want: "plain text"},
		{name: "object", raw: `{"text":"hello"}`, want: map[string]any{"text": "hello"}},
		{name: "array", raw: `[{"text":"one"},{"text":"two"}]`, want: []any{map[string]any{"text": "one"}, map[string]any{"text": "two"}}},
		{name: "hermes prefix", raw: hermesJSONContentPrefix + `{"text":"hello"}`, want: map[string]any{"text": "hello"}},
		{name: "invalid json", raw: `{"text":`, want: `{"text":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeHarnessJSON(tt.raw)
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tt.want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("decodeHarnessJSON = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestTextFromHarnessContent(t *testing.T) {
	content := []any{
		map[string]any{"text": "one"},
		map[string]any{"thinking": "two"},
		map[string]any{"content": []any{
			map[string]any{"output": "three"},
			"",
		}},
	}

	if got, want := textFromHarnessContent(content), "one\ntwo\nthree"; got != want {
		t.Fatalf("textFromHarnessContent nested = %q, want %q", got, want)
	}
	if got, want := textFromHarnessContent(hermesJSONContentPrefix+`{"content":{"text":"decoded"}}`), "decoded"; got != want {
		t.Fatalf("textFromHarnessContent decoded = %q, want %q", got, want)
	}
	if got := textFromHarnessContent(map[string]any{"irrelevant": true}); got != "" {
		t.Fatalf("textFromHarnessContent unknown object = %q, want empty", got)
	}
}

func TestJSONPayload(t *testing.T) {
	if got := jsonPayload(nil); got != "" {
		t.Fatalf("jsonPayload nil = %q, want empty", got)
	}
	if got := jsonPayload(`{"already":"json"}`); got != `{"already":"json"}` {
		t.Fatalf("jsonPayload json string = %q", got)
	}
	if got := jsonPayload("plain"); got != `"plain"` {
		t.Fatalf("jsonPayload plain string = %q, want quoted string", got)
	}
	if got := jsonPayload(map[string]any{"b": 2, "a": 1}); got != `{"a":1,"b":2}` {
		t.Fatalf("jsonPayload map = %q, want sorted JSON object", got)
	}
}

func TestParseJSONMap(t *testing.T) {
	if got := parseJSONMap(""); got != nil {
		t.Fatalf("parseJSONMap empty = %#v, want nil", got)
	}
	if got := parseJSONMap("{bad json"); got != nil {
		t.Fatalf("parseJSONMap invalid = %#v, want nil", got)
	}
	got := parseJSONMap(`{"modelID":"gpt-5","providerID":"openai"}`)
	if got == nil || stringFromAny(got["modelID"]) != "gpt-5" || stringFromAny(got["providerID"]) != "openai" {
		t.Fatalf("parseJSONMap valid = %#v", got)
	}
}
