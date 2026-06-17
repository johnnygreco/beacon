package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/store"
)

func TestRunStreamsJSONRPCRequestsAndResponses(t *testing.T) {
	srv := testServer()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"initialized"}`,
		`{"jsonrpc":"2.0","method":"ping"}`,
		`{"jsonrpc":"2.0","method":"unknown/notification"}`,
		`{not-json`,
		`{"jsonrpc":"2.0","id":2,"method":"unknown/method"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
	}, "\n") + "\n"
	var out bytes.Buffer

	if err := srv.run(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	responses := decodeJSONRPCResponses(t, out.String())
	if len(responses) != 4 {
		t.Fatalf("responses = %d, want 4: %s", len(responses), out.String())
	}
	assertResponseID(t, responses[0], `1`)
	if responses[0].Error != nil {
		t.Fatalf("initialize error = %+v", responses[0].Error)
	}
	if responses[1].Error == nil || responses[1].Error.Code != -32700 {
		t.Fatalf("parse error response = %+v", responses[1])
	}
	if string(responses[1].ID) != "null" {
		t.Fatalf("parse error id = %s, want null", responses[1].ID)
	}
	assertResponseID(t, responses[2], `2`)
	if responses[2].Error == nil || responses[2].Error.Code != -32601 {
		t.Fatalf("unknown method response = %+v", responses[2])
	}
	assertResponseID(t, responses[3], `3`)
	if responses[3].Error != nil || !strings.Contains(fmt.Sprint(responses[3].Result), "search_sessions") {
		t.Fatalf("tools/list response = %+v", responses[3])
	}
}

func TestToolsCallSearchSessionsSuccessAndError(t *testing.T) {
	fake := &fakeMCPSearcher{
		results: []search.SearchResult{{
			EventUID:    "evt-search",
			SessionID:   "session-search",
			EventKind:   "message",
			TextPreview: "needle result",
			Score:       2.5,
			Timestamp:   time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
			Model:       "gpt-5.4",
			Provider:    "openai",
		}},
	}
	srv := testServer()
	srv.searcher = fake
	resp := srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`10`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search_sessions","arguments":{"query":"needle","limit":2,"session_id":"session:session-search","event_kinds":["message"]}}`),
	})

	if resp == nil || resp.Error != nil {
		t.Fatalf("search_sessions response = %+v", resp)
	}
	if fake.query.Query != "needle" || fake.query.Limit != 2 || fake.query.SessionID != "session-search" || !fake.query.ExcludeMCPSelf || !fake.query.SkipQueryLog {
		t.Fatalf("search query = %#v", fake.query)
	}
	if !reflect.DeepEqual(fake.query.EventKinds, []string{"message"}) {
		t.Fatalf("event kinds = %#v", fake.query.EventKinds)
	}
	text, isError := toolText(t, resp)
	if isError || !strings.Contains(text, `"schema":"beacon.mcp.search_sessions.v1"`) || !strings.Contains(text, `"event_id":"event:evt-search"`) {
		t.Fatalf("tool text/isError = %q/%v", text, isError)
	}

	resp = srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`101`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search_sessions","arguments":{"query":"needle","limit":null,"session_id":null,"event_kinds":null}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("search_sessions null-default response = %+v", resp)
	}
	if fake.query.Query != "needle" || fake.query.Limit != 25 || fake.query.SessionID != "" || fake.query.EventKinds != nil || !fake.query.SkipQueryLog {
		t.Fatalf("null-default search query = %#v", fake.query)
	}

	resp = srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`102`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search_sessions","arguments":{"query":"needle","limit":999}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("search_sessions capped-limit response = %+v", resp)
	}
	if fake.query.Limit != maxSearchSessionsLimit {
		t.Fatalf("capped search limit = %d, want %d", fake.query.Limit, maxSearchSessionsLimit)
	}

	resp = srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`11`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search_sessions","arguments":{"limit":2}}`),
	})
	text, isError = toolText(t, resp)
	if !isError || !strings.Contains(text, "query is required") {
		t.Fatalf("missing query response text/isError = %q/%v", text, isError)
	}

	fake.err = errors.New("search backend failed")
	resp = srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`12`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search_sessions","arguments":{"query":"needle"}}`),
	})
	text, isError = toolText(t, resp)
	if !isError || !strings.Contains(text, "search failed") {
		t.Fatalf("search backend error response text/isError = %q/%v", text, isError)
	}
	if strings.Contains(text, "search backend failed") {
		t.Fatalf("search backend error leaked internal detail: %q", text)
	}
}

func TestToolSearchAppliesAuthScope(t *testing.T) {
	fake := &fakeMCPSearcher{
		results: []search.SearchResult{{
			EventUID:    "evt-search",
			SessionID:   "session-search",
			SourceName:  "source",
			Runtime:     "runtime",
			ProjectKey:  "beacon",
			ProjectPath: "/work/beacon",
			EventKind:   "message",
			TextPreview: "needle result",
			Score:       2.5,
			Timestamp:   time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
		}},
	}
	srv := testServer()
	srv.searcher = fake
	ctx := ContextWithAuthScope(context.Background(), ScopeFilters{SourceNames: []string{"source"}})

	text, err := srv.toolSearch(ctx, json.RawMessage(`{"query":"needle","limit":2,"source_name":"source"}`))
	if err != nil {
		t.Fatalf("toolSearch: %v", err)
	}
	if !reflect.DeepEqual(fake.query.SourceNames, []string{"source"}) {
		t.Fatalf("search scope = sources %#v", fake.query.SourceNames)
	}
	for _, want := range []string{
		`"auth_scope_applied":true`,
		`"source_names":["source"]`,
		`"runtime":"runtime"`,
		`"project_key":"beacon"`,
		`"open_ref":{"type":"event"`,
		`"scope":{"source_names":["source"]}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("search output missing %q:\n%s", want, text)
		}
	}
}

func TestToolSearchAuthScopeCannotBeBroadened(t *testing.T) {
	fake := &fakeMCPSearcher{}
	srv := testServer()
	srv.searcher = fake
	ctx := ContextWithAuthScope(context.Background(), ScopeFilters{SourceNames: []string{"source-a"}})

	if _, err := srv.toolSearch(ctx, json.RawMessage(`{"query":"needle","source_name":"source-b"}`)); err != nil {
		t.Fatalf("toolSearch: %v", err)
	}
	if !reflect.DeepEqual(fake.query.SourceNames, []string{scopeImpossibleValue}) {
		t.Fatalf("broadened auth scope query sources = %#v", fake.query.SourceNames)
	}
}

func TestMCPScopeEventProjectKeyDerivesFromCWD(t *testing.T) {
	clause, args := ScopeFilters{ProjectKeys: []string{"beacon"}}.eventSQLAndClause("ae", "")
	if strings.Contains(clause, "ae.project_key") {
		t.Fatalf("event project scope should not reference raw event project_key: %s", clause)
	}
	for _, want := range []string{"ae.cwd", "replaceRegexpOne", "IN (?)"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("event project scope missing %q: %s", want, clause)
		}
	}
	if fmt.Sprint(args) != "[beacon]" {
		t.Fatalf("scope args = %#v, want [beacon]", args)
	}
}

func TestMCPScopeEventAndSessionProjectKeyUsesSingleProjectFallback(t *testing.T) {
	clause, args := ScopeFilters{ProjectKeys: []string{"beacon"}}.eventAndSessionProjectSQLAndClause("e", "e.cwd", "s")
	for _, want := range []string{"COALESCE(NULLIF(if(e.cwd", "COALESCE(s.project_count, 0) <= 1", "NULLIF(s.project_key, '')", "IN (?)"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("event/session project scope missing %q: %s", want, clause)
		}
	}
	if strings.Index(clause, "if(e.cwd") > strings.Index(clause, "s.project_key") {
		t.Fatalf("event cwd project must be preferred before session fallback: %s", clause)
	}
	if fmt.Sprint(args) != "[beacon]" {
		t.Fatalf("scope args = %#v, want [beacon]", args)
	}
}

func TestMCPScopeSourceNameFiltersRows(t *testing.T) {
	clause, args := ScopeFilters{SourceNames: []string{"codex"}}.sqlAndClause("s")
	for _, want := range []string{"s.source_name", "IN (?)"} {
		if !strings.Contains(clause, want) {
			t.Fatalf("source scope missing %q: %s", want, clause)
		}
	}
	if fmt.Sprint(args) != "[codex]" {
		t.Fatalf("scope args = %#v, want [codex]", args)
	}
}

func TestToolsCallUnknownToolAndInvalidParams(t *testing.T) {
	srv := testServer()
	resp := srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`13`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"missing","arguments":{}}`),
	})
	text, isError := toolText(t, resp)
	if !isError || !strings.Contains(text, "unknown tool: missing") {
		t.Fatalf("unknown tool response text/isError = %q/%v", text, isError)
	}

	resp = srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`14`),
		Method:  "tools/call",
		Params:  json.RawMessage(`not-json`),
	})
	if resp == nil || resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("invalid params response = %+v", resp)
	}
}

func TestToolOpenSuccessAndErrors(t *testing.T) {
	targetTime := time.Date(2026, 5, 22, 13, 0, 0, 0, time.UTC)
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH target AS", "ROW_NUMBER() OVER", "WHERE n.rn BETWEEN")
			assertMCPNamedValues(t, args, []any{"evt-target", 1, 2})
			return mcpRows(
				mcpOpenColumns(),
				mcpOpenRow("evt-before", "session-1", "message", "user", "before", "", "gpt-5.4", int64(3), targetTime.Add(-time.Minute), 0),
				mcpOpenRow("evt-target", "session-1", "tool_call", "assistant", "target", "Bash", "gpt-5.4", int64(5), targetTime, 1),
				mcpOpenRow("evt-after", "session-1", "tool_result", "tool", "after", "Bash", "gpt-5.4", int64(7), targetTime.Add(time.Minute), 0),
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:evt-target","before":1,"after":2}`))
	if err != nil {
		t.Fatalf("toolOpen: %v", err)
	}
	if !strings.Contains(text, `"schema":"beacon.mcp.open.v1"`) || !strings.Contains(text, `"event_id":"event:evt-target"`) || !strings.Contains(text, `"session_id":"session:session-1"`) || !strings.Contains(text, `"target":true`) {
		t.Fatalf("open output = %s", text)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH target AS", "ROW_NUMBER() OVER", "WHERE n.rn BETWEEN")
			assertMCPNamedValues(t, args, []any{"evt-target", 4, 4})
			return mcpRows(
				mcpOpenColumns(),
				mcpOpenRow("evt-target", "session-1", "tool_call", "assistant", "target", "Bash", "gpt-5.4", int64(5), targetTime, 1),
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	srv.SetDefaultContextWindow(4)
	if _, err := srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:evt-target","before":null,"after":null}`)); err != nil {
		t.Fatalf("toolOpen with configured default context: %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return mcpRows(mcpOpenColumns()), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:missing"}`))
	if err == nil || !strings.Contains(err.Error(), "event not found: missing") {
		t.Fatalf("toolOpen missing error = %v", err)
	}

	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"before":1}`))
	if err == nil || !strings.Contains(err.Error(), "event_id or open_ref is required") {
		t.Fatalf("toolOpen required error = %v", err)
	}
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"event_uid":"evt-target"}`))
	if err == nil || !strings.Contains(err.Error(), "event_id or open_ref is required") {
		t.Fatalf("toolOpen event_uid argument error = %v", err)
	}
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"id":"event:evt-target"}`))
	if err == nil || !strings.Contains(err.Error(), "event_id or open_ref is required") {
		t.Fatalf("toolOpen id argument error = %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("open query failed")
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:evt-target"}`))
	if err == nil || !strings.Contains(err.Error(), "open query failed") {
		t.Fatalf("toolOpen query error = %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return mcpRows(
				mcpOpenColumns(),
				[]driver.Value{"evt-target", "session-1", "source", "runtime", "project", "/work/project", "message", "assistant", "bad timestamp", "", "gpt-5.4", int64(1), "not-a-time", uint8(1)},
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:evt-target"}`))
	if err == nil || !strings.Contains(err.Error(), "scan context event") {
		t.Fatalf("toolOpen scan error = %v", err)
	}
}

func TestToolOpenRejectsOversizedContextWindow(t *testing.T) {
	srv := testServer()
	_, err := srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:evt-target","before":26,"after":1}`))
	if err == nil || !strings.Contains(err.Error(), "before must be <= 25") {
		t.Fatalf("oversized before error = %v", err)
	}
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:evt-target","before":1,"after":26}`))
	if err == nil || !strings.Contains(err.Error(), "after must be <= 25") {
		t.Fatalf("oversized after error = %v", err)
	}
}

func TestDefaultOpenContextWindowIsClamped(t *testing.T) {
	srv := testServer()
	srv.SetDefaultContextWindow(1000)
	if got := srv.defaultContextWindow(); got != maxOpenContextWindow {
		t.Fatalf("default context window = %d, want %d", got, maxOpenContextWindow)
	}
}

func TestToolOpenAcceptsSessionLatestOpenRef(t *testing.T) {
	targetTime := time.Date(2026, 5, 22, 13, 0, 0, 0, time.UTC)
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "ae.session_id = ?", "ORDER BY timestamp DESC, event_uid DESC", "WHERE n.rn BETWEEN")
			assertMCPNamedValues(t, args, []any{"session-1", 1, 1})
			return mcpRows(
				mcpOpenColumns(),
				mcpOpenRow("evt-latest", "session-1", "message", "assistant", "latest", "", "gpt-5.4", int64(5), targetTime, 1),
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolOpen(context.Background(), json.RawMessage(`{"open_ref":{"type":"session_latest","session_id":"session:session-1","anchor":"latest"},"before":1,"after":1}`))
	if err != nil {
		t.Fatalf("toolOpen open_ref: %v", err)
	}
	if !strings.Contains(text, `"event_id":"event:evt-latest"`) || !strings.Contains(text, `"session_id":"session:session-1"`) {
		t.Fatalf("open_ref output = %s", text)
	}
}

func TestToolOpenScopedMissReturnsForbiddenWithoutExistenceLeak(t *testing.T) {
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "ae.event_uid = ?", "ae.source_name IN (?)", "WHERE n.rn BETWEEN")
			assertMCPNamedValues(t, args, []any{"evt-secret", "source-a", "source-a", 3, 3})
			return mcpRows(mcpOpenColumns()), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	ctx := ContextWithAuthScope(context.Background(), ScopeFilters{SourceNames: []string{"source-a"}})
	_, err := srv.toolOpen(ctx, json.RawMessage(`{"event_id":"event:evt-secret"}`))
	if err == nil || err.Error() != "forbidden" {
		t.Fatalf("scoped open error = %v, want forbidden", err)
	}
	if strings.Contains(err.Error(), "evt-secret") {
		t.Fatalf("forbidden open leaked event id: %v", err)
	}
}

func TestToolsCallOpenRefScopeIsAppliedAndForbiddenIsExact(t *testing.T) {
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "ae.event_uid = ?", "ae.source_name IN (?)", "WHERE n.rn BETWEEN")
			assertMCPNamedValues(t, args, []any{"evt-secret", "source-a", "source-a", 3, 3})
			return mcpRows(mcpOpenColumns()), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	resp := srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`151`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"open","arguments":{"open_ref":{"type":"event","event_id":"event:evt-secret","session_id":"session:session-1","scope":{"source_names":["source-a"]}}}}`),
	})
	text, isError := toolText(t, resp)
	if !isError || text != "forbidden" {
		t.Fatalf("scoped open_ref text/isError = %q/%v, want exact forbidden", text, isError)
	}
}

func TestToolsCallOpenBackendErrorIsSanitized(t *testing.T) {
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("open query failed: secret clickhouse dsn")
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	resp := srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`15`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"open","arguments":{"event_id":"event:evt-target"}}`),
	})
	text, isError := toolText(t, resp)
	if !isError || !strings.Contains(text, "failed to open event context") {
		t.Fatalf("open backend error response text/isError = %q/%v", text, isError)
	}
	if strings.Contains(text, "secret clickhouse") || strings.Contains(text, "open query failed") {
		t.Fatalf("open backend error leaked internal detail: %q", text)
	}
}

func TestToolListSessionsSuccessAndErrors(t *testing.T) {
	started := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	ended := started.Add(10 * time.Minute)
	since := started.Add(-time.Hour)
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT count()", "FROM (SELECT", "started_at >= ?")
			assertMCPNamedValues(t, args, []any{since})
			return mcpRows([]string{"count"}, []driver.Value{int64(2)}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM (SELECT", "started_at >= ?", "ORDER BY started_at DESC, session_id DESC LIMIT ? OFFSET ?")
			assertMCPNamedValues(t, args, []any{since, 5, 0})
			return mcpRows(
				mcpSessionColumns(),
				mcpSessionRow("session-1", started, ended),
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolListSessions(context.Background(), json.RawMessage(fmt.Sprintf(`{"since":%q,"limit":5}`, since.Format(time.RFC3339))))
	if err != nil {
		t.Fatalf("toolListSessions: %v", err)
	}
	for _, want := range []string{
		`"schema":"beacon.mcp.list_sessions.v1"`,
		`"session_id":"session:session-1"`,
		`"runtime":"codex"`,
		`"project_key":"beacon"`,
		`"open_ref":{"type":"session_latest"`,
		`"provider":"openai"`,
		`"working_dir":"/work/beacon"`,
		`"total_tokens":30`,
		`"result_count":1`,
		`"total_matching_count":2`,
		`"result_complete":false`,
		`"next_cursor":"offset:1"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("list output missing %q:\n%s", want, text)
		}
	}

	_, err = srv.toolListSessions(context.Background(), json.RawMessage(`{"cursor":"bad"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid cursor") {
		t.Fatalf("invalid cursor error = %v", err)
	}
	_, err = srv.toolListSessions(context.Background(), json.RawMessage(fmt.Sprintf(`{"cursor":"offset:%d"}`, maxListSessionsCursorOffset+1)))
	if err == nil || !strings.Contains(err.Error(), "cursor offset must be <=") {
		t.Fatalf("too-deep cursor error = %v", err)
	}

	where, values, offset, err := listSessionsFilterSQL(listSessionsParams{
		Until:             ended.Format(time.RFC3339),
		SourceName:        " codex ",
		Model:             "gpt-5.4",
		Provider:          "openai",
		WorkingDir:        "/work/beacon",
		ActiveDuringSince: started.Format(time.RFC3339),
		ActiveDuringUntil: ended.Format(time.RFC3339),
		Cursor:            "offset:25",
	})
	if err != nil {
		t.Fatalf("listSessionsFilterSQL: %v", err)
	}
	for _, want := range []string{"started_at <= ?", "source_name = ?", "last_model = ?", "provider = ?", "working_dir = ?", "ended_at >= ?"} {
		if !strings.Contains(where, want) {
			t.Fatalf("where missing %q: %s", want, where)
		}
	}
	if offset != 25 || len(values) != 7 {
		t.Fatalf("offset/values = %d/%#v", offset, values)
	}

	_, err = srv.toolListSessions(context.Background(), json.RawMessage(`{"since":"not-a-time"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid since timestamp") {
		t.Fatalf("invalid since error = %v", err)
	}
	_, err = srv.toolListSessions(context.Background(), json.RawMessage(`{"active_during_until":"not-a-time"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid active_during_until timestamp") {
		t.Fatalf("invalid active_during_until error = %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("list query failed")
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	_, err = srv.toolListSessions(context.Background(), json.RawMessage(`{"limit":1}`))
	if err == nil || !strings.Contains(err.Error(), "list query failed") {
		t.Fatalf("list count error = %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return mcpRows([]string{"count"}, []driver.Value{int64(1)}), nil
		},
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("list query failed")
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	_, err = srv.toolListSessions(context.Background(), json.RawMessage(`{"limit":1}`))
	if err == nil || !strings.Contains(err.Error(), "list query failed") {
		t.Fatalf("list query error = %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return mcpRows([]string{"count"}, []driver.Value{int64(1)}), nil
		},
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return mcpRows(
				mcpSessionColumns(),
				mcpSessionRow("session-1", "not-a-time", ended),
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	_, err = srv.toolListSessions(context.Background(), json.RawMessage(`{"limit":1}`))
	if err == nil || !strings.Contains(err.Error(), "scan session") {
		t.Fatalf("list scan error = %v", err)
	}
}

func TestToolListSessionsProjectScopeUsesScopedActivitySource(t *testing.T) {
	started := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	ended := started.Add(10 * time.Minute)
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT count()", "FROM activity_events AS ae", "LEFT JOIN", "s.project_key", "project_key IN (?)", "last_model = ?")
			assertMCPNamedValues(t, args, []any{"beacon", "gpt-5.4", "beacon"})
			return mcpRows([]string{"count"}, []driver.Value{int64(1)}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM activity_events AS ae", "LEFT JOIN", "s.project_key", "ORDER BY started_at DESC, session_id DESC LIMIT ? OFFSET ?")
			assertMCPNamedValues(t, args, []any{"beacon", "gpt-5.4", "beacon", 1, 0})
			return mcpRows(
				mcpSessionColumns(),
				mcpSessionRow("session-1", started, ended),
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	if _, err := srv.toolListSessions(context.Background(), json.RawMessage(`{"project_key":"beacon","model":"gpt-5.4","limit":1}`)); err != nil {
		t.Fatalf("toolListSessions project scope: %v", err)
	}
}

func TestToolListSessionsFilterSQLRejectsInvalidTimestamp(t *testing.T) {
	_, _, _, err := listSessionsFilterSQL(listSessionsParams{Until: "bad"})
	if err == nil || !strings.Contains(err.Error(), "invalid until timestamp") {
		t.Fatalf("invalid until error = %v", err)
	}
}

func TestToolListSessionsLimitIsCapped(t *testing.T) {
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(_ string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPNamedValues(t, args, []any{})
			return mcpRows([]string{"count"}, []driver.Value{int64(0)}), nil
		},
		func(_ string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPNamedValues(t, args, []any{maxListSessionsLimit, 0})
			return mcpRows(mcpSessionColumns()), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolListSessions(context.Background(), json.RawMessage(`{"limit":999}`))
	if err != nil {
		t.Fatalf("toolListSessions: %v", err)
	}
	if !strings.Contains(text, `"limit":100`) || !strings.Contains(text, `"result_complete":true`) {
		t.Fatalf("list output = %s", text)
	}
}

func TestToolsCallListSessionsBackendErrorIsSanitized(t *testing.T) {
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("list query failed: secret clickhouse dsn")
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	resp := srv.dispatch(context.Background(), &jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`16`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"list_sessions","arguments":{"limit":5}}`),
	})
	text, isError := toolText(t, resp)
	if !isError || !strings.Contains(text, "failed to list sessions") {
		t.Fatalf("list backend error response text/isError = %q/%v", text, isError)
	}
	if strings.Contains(text, "secret clickhouse") || strings.Contains(text, "list query failed") {
		t.Fatalf("list backend error leaked internal detail: %q", text)
	}
}

func TestToolUsageSummarySuccessAndErrors(t *testing.T) {
	since := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_events AS", "GROUP BY event_uid", "e.timestamp >= ?", "e.timestamp < ?", "e.source_name IN (?)", "AS selected_total_tokens")
			assertMCPNamedValues(t, args, []any{since, until, "codex"})
			return mcpRows(
				[]string{"session_count", "event_count", "input_tokens", "output_tokens", "total_tokens", "cache_read_tokens", "cache_create_tokens", "selected_total_tokens"},
				[]driver.Value{int64(2), int64(5), int64(100), int64(50), int64(150), int64(20), int64(5), int64(175)},
			), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT count()", "GROUP BY e.session_id")
			assertMCPNamedValues(t, args, []any{since, until, "codex"})
			return mcpRows([]string{"count"}, []driver.Value{int64(2)}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "e.session_id AS session_id", "GROUP BY e.session_id", "ORDER BY selected_total_tokens DESC", "LIMIT ?")
			assertMCPNamedValues(t, args, []any{since, until, "codex", 1})
			return mcpRows(
				[]string{"session_id", "session_count", "event_count", "input_tokens", "output_tokens", "total_tokens", "cache_read_tokens", "cache_create_tokens", "selected_total_tokens"},
				[]driver.Value{"session-a", int64(1), int64(3), int64(70), int64(30), int64(100), int64(10), int64(5), int64(115)},
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolUsageSummary(context.Background(), json.RawMessage(fmt.Sprintf(`{
		"since":%q,
		"until":%q,
		"window_mode":"event_timestamp",
		"token_mode":"include_cache",
		"source_name":"codex",
		"model":null,
		"provider":null,
		"working_dir":null,
		"group_by":["session_id"],
		"limit":1
	}`, since.Format(time.RFC3339), until.Format(time.RFC3339))))
	if err != nil {
		t.Fatalf("toolUsageSummary: %v", err)
	}
	for _, want := range []string{
		`"schema":"beacon.mcp.usage_summary.v1"`,
		`"token_mode":"include_cache"`,
		`"total_definition":"input_tokens + output_tokens"`,
		`"selected_total_definition":"input_tokens + output_tokens + cache_read_tokens + cache_create_tokens"`,
		`"selected_total_tokens":175`,
		`"total_matching_count":2`,
		`"result_complete":false`,
		`"session_id":"session-a"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("usage output missing %q:\n%s", want, text)
		}
	}

	_, err = srv.toolUsageSummary(context.Background(), json.RawMessage(`{"since":"now-24h","until":"now","window_mode":"bad"}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported window_mode") {
		t.Fatalf("invalid window_mode error = %v", err)
	}
	_, err = srv.toolUsageSummary(context.Background(), json.RawMessage(`{"since":"now-24h","until":"now","group_by":["source_name; DROP TABLE activity_events"]}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported group_by field") {
		t.Fatalf("invalid group_by error = %v", err)
	}
	_, err = srv.toolUsageSummary(context.Background(), json.RawMessage(`{"since":"not-time"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid since timestamp") {
		t.Fatalf("invalid since error = %v", err)
	}
}

func TestToolCreateAnnotationSupportsMessageTarget(t *testing.T) {
	db, stub := newMCPStubDBWithExecs(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "argMax(event_kind, captured_at) AS event_kind", "e.event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM activity_events", "event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
	}, []mcpStubExec{
		func(query string, args []driver.NamedValue) (driver.Result, error) {
			assertMCPQueryContains(t, query, "INSERT INTO trace_annotations")
			values := mcpNamedValues(args)
			if len(values) != 22 {
				t.Fatalf("insert values = %d, want 22: %#v", len(values), values)
			}
			id, _ := values[0].(string)
			if !strings.HasPrefix(id, "ann_") || values[2] != models.AnnotationTargetMessage || values[3] != "session-1" || values[4] != "msg-1" || values[5] != models.AnnotationAuthorAgent || values[8] != models.AnnotationSourceMCP {
				t.Fatalf("insert annotation target/agent/source = %#v", values[:9])
			}
			if values[9] != "quality" || values[15] != "message note" || values[16] != `{"rubric":"qa"}` {
				t.Fatalf("insert annotation content = %#v", values[9:17])
			}
			return mcpExecResult(1), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolCreateAnnotation(context.Background(), json.RawMessage(`{
		"target_type":"message",
		"message_id":"event:msg-1",
		"author_id":"agent-1",
		"author_name":"Reviewer Agent",
		"category":"quality",
		"labels":["dataset:eval"],
		"note":"message note",
		"metadata_json":"{\"rubric\":\"qa\"}"
	}`))
	if err != nil {
		t.Fatalf("toolCreateAnnotation: %v", err)
	}
	for _, want := range []string{
		`"schema":"beacon.mcp.create_annotation.v1"`,
		`"target_type":"message"`,
		`"session_id":"session:session-1"`,
		`"event_id":"event:msg-1"`,
		`"message_id":"message:msg-1"`,
		`"author_type":"agent"`,
		`"source":"mcp"`,
		`"note":"message note"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("create annotation output missing %q:\n%s", want, text)
		}
	}
}

func TestToolListAnnotationsSessionTargetListsAllSessionAnnotations(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	sessionAnnotation := models.TraceAnnotation{
		AnnotationID:  "ann-session",
		Revision:      1,
		TargetType:    models.AnnotationTargetSession,
		SessionID:     "session-1",
		AuthorType:    models.AnnotationAuthorAgent,
		Source:        models.AnnotationSourceMCP,
		Note:          "session note",
		Status:        models.AnnotationStatusActive,
		SchemaVersion: models.AnnotationSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	messageAnnotation := sessionAnnotation
	messageAnnotation.AnnotationID = "ann-message"
	messageAnnotation.TargetType = models.AnnotationTargetMessage
	messageAnnotation.EventUID = "msg-1"
	messageAnnotation.Note = "message note"
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT session_id FROM", "session_id = ?")
			assertMCPNamedValues(t, args, []any{"session-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "session_id = ?", "status != ?")
			if strings.Contains(query, "target_type = ?") {
				t.Fatalf("session list without target_type must not filter target_type:\n%s", query)
			}
			assertMCPNamedValues(t, args, []any{"session-1", models.AnnotationStatusDeleted, maxAnnotationListLimit, 0})
			return mcpRows(
				mcpAnnotationColumns(),
				mcpAnnotationRow(sessionAnnotation),
				mcpAnnotationRow(messageAnnotation),
			), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT session_id FROM", "session_id = ?")
			assertMCPNamedValues(t, args, []any{"session-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolListAnnotations(context.Background(), json.RawMessage(`{"session_id":"session:session-1"}`))
	if err != nil {
		t.Fatalf("toolListAnnotations: %v", err)
	}
	for _, want := range []string{
		`"schema":"beacon.mcp.list_annotations.v1"`,
		`"annotation_id":"ann-session"`,
		`"annotation_id":"ann-message"`,
		`"message_id":"message:msg-1"`,
		`"open_ref":{"type":"message"`,
		`"result_count":2`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("list annotation output missing %q:\n%s", want, text)
		}
	}
}

func TestToolListAnnotationsFiltersOutOfScopeTargets(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	sessionAnnotation := models.TraceAnnotation{
		AnnotationID:  "ann-session",
		Revision:      1,
		TargetType:    models.AnnotationTargetSession,
		SessionID:     "session-1",
		AuthorType:    models.AnnotationAuthorAgent,
		Source:        models.AnnotationSourceMCP,
		Note:          "session note",
		Status:        models.AnnotationStatusActive,
		SchemaVersion: models.AnnotationSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	messageAnnotation := sessionAnnotation
	messageAnnotation.AnnotationID = "ann-message"
	messageAnnotation.TargetType = models.AnnotationTargetMessage
	messageAnnotation.EventUID = "msg-1"
	messageAnnotation.Note = "visible message note"
	secretAnnotation := sessionAnnotation
	secretAnnotation.AnnotationID = "ann-secret"
	secretAnnotation.TargetType = models.AnnotationTargetMessage
	secretAnnotation.EventUID = "msg-secret"
	secretAnnotation.Note = "secret note"
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT session_id FROM", "session_id = ?", "source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"session-1", "source-a"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "session_id = ?", "status != ?")
			assertMCPNamedValues(t, args, []any{"session-1", models.AnnotationStatusDeleted, maxAnnotationListLimit, 0})
			return mcpRows(
				mcpAnnotationColumns(),
				mcpAnnotationRow(sessionAnnotation),
				mcpAnnotationRow(messageAnnotation),
				mcpAnnotationRow(secretAnnotation),
			), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT session_id FROM", "session_id = ?", "source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"session-1", "source-a"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'", "e.source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"msg-1", "source-a"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'", "e.source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"msg-secret", "source-a"})
			return mcpRows([]string{"session_id"}), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolListAnnotations(context.Background(), json.RawMessage(`{"session_id":"session:session-1","source_name":"source-a"}`))
	if err != nil {
		t.Fatalf("toolListAnnotations: %v", err)
	}
	for _, want := range []string{
		`"annotation_id":"ann-session"`,
		`"annotation_id":"ann-message"`,
		`"result_count":2`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("scoped list annotation output missing %q:\n%s", want, text)
		}
	}
	for _, notWant := range []string{`"annotation_id":"ann-secret"`, "secret note"} {
		if strings.Contains(text, notWant) {
			t.Fatalf("scoped list annotation output leaked %q:\n%s", notWant, text)
		}
	}
}

func TestToolListAnnotationsPaginatesAfterScopeFiltering(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	base := models.TraceAnnotation{
		Revision:      1,
		TargetType:    models.AnnotationTargetMessage,
		SessionID:     "session-1",
		AuthorType:    models.AnnotationAuthorAgent,
		Source:        models.AnnotationSourceMCP,
		Status:        models.AnnotationStatusActive,
		SchemaVersion: models.AnnotationSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	hidden := base
	hidden.AnnotationID = "ann-hidden"
	hidden.EventUID = "msg-hidden"
	hidden.Note = "hidden note"
	visible := base
	visible.AnnotationID = "ann-visible"
	visible.EventUID = "msg-visible"
	visible.Note = "visible note"
	nextVisible := base
	nextVisible.AnnotationID = "ann-next"
	nextVisible.EventUID = "msg-next"
	nextVisible.Note = "next visible note"

	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT session_id FROM", "session_id = ?", "source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"session-1", "source-a"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "session_id = ?", "status != ?")
			assertMCPNamedValues(t, args, []any{"session-1", models.AnnotationStatusDeleted, maxAnnotationListLimit, 0})
			return mcpRows(
				mcpAnnotationColumns(),
				mcpAnnotationRow(hidden),
				mcpAnnotationRow(visible),
				mcpAnnotationRow(nextVisible),
			), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'", "e.source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"msg-hidden", "source-a"})
			return mcpRows([]string{"session_id"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'", "e.source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"msg-visible", "source-a"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'", "e.source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"msg-next", "source-a"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolListAnnotations(context.Background(), json.RawMessage(`{"session_id":"session:session-1","source_name":"source-a","limit":1}`))
	if err != nil {
		t.Fatalf("toolListAnnotations: %v", err)
	}
	for _, want := range []string{
		`"annotation_id":"ann-visible"`,
		`"result_count":1`,
		`"result_complete":false`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("scoped paginated output missing %q:\n%s", want, text)
		}
	}
	for _, notWant := range []string{`"annotation_id":"ann-hidden"`, `"annotation_id":"ann-next"`, "hidden note", "next visible note"} {
		if strings.Contains(text, notWant) {
			t.Fatalf("scoped paginated output leaked %q:\n%s", notWant, text)
		}
	}
}

func TestToolListAnnotationsMessageOpenRefRoundTripsFromCreate(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	annotation := models.TraceAnnotation{
		AnnotationID:  "ann-message",
		Revision:      1,
		TargetType:    models.AnnotationTargetMessage,
		SessionID:     "session-1",
		EventUID:      "msg-1",
		AuthorType:    models.AnnotationAuthorAgent,
		AuthorID:      "agent-1",
		AuthorName:    "Reviewer Agent",
		Source:        models.AnnotationSourceMCP,
		Note:          "message note",
		Status:        models.AnnotationStatusActive,
		SchemaVersion: models.AnnotationSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	db, stub := newMCPStubDBWithExecs(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM activity_events", "event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "target_type = ?", "session_id = ?", "event_uid = ?", "status != ?")
			assertMCPNamedValues(t, args, []any{models.AnnotationTargetMessage, "session-1", "msg-1", models.AnnotationStatusDeleted, maxAnnotationListLimit, 0})
			return mcpRows(mcpAnnotationColumns(), mcpAnnotationRow(annotation)), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
	}, []mcpStubExec{
		func(query string, args []driver.NamedValue) (driver.Result, error) {
			assertMCPQueryContains(t, query, "INSERT INTO trace_annotations")
			return mcpExecResult(1), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	createdText, err := srv.toolCreateAnnotation(context.Background(), json.RawMessage(`{
		"target_type":"message",
		"message_id":"message:msg-1",
		"author_id":"agent-1",
		"author_name":"Reviewer Agent",
		"note":"message note"
	}`))
	if err != nil {
		t.Fatalf("toolCreateAnnotation: %v", err)
	}
	var created struct {
		Annotation struct {
			OpenRef openRef `json:"open_ref"`
		} `json:"annotation"`
	}
	if err := json.Unmarshal([]byte(createdText), &created); err != nil {
		t.Fatalf("unmarshal create output: %v\n%s", err, createdText)
	}
	if created.Annotation.OpenRef.Type != "message" || created.Annotation.OpenRef.MessageID != "message:msg-1" {
		t.Fatalf("create message open_ref = %#v", created.Annotation.OpenRef)
	}
	refJSON, err := json.Marshal(created.Annotation.OpenRef)
	if err != nil {
		t.Fatalf("marshal open_ref: %v", err)
	}
	text, err := srv.toolListAnnotations(context.Background(), json.RawMessage(`{"open_ref":`+string(refJSON)+`}`))
	if err != nil {
		t.Fatalf("toolListAnnotations open_ref: %v", err)
	}
	for _, want := range []string{
		`"annotation_id":"ann-message"`,
		`"target_type":"message"`,
		`"message_id":"message:msg-1"`,
		`"open_ref":{"type":"message"`,
		`"result_count":1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("message open_ref list output missing %q:\n%s", want, text)
		}
	}
}

func TestToolUpdateAndDeleteAnnotationVerifyMessageScope(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	current := models.TraceAnnotation{
		AnnotationID:  "ann-message",
		Revision:      1,
		TargetType:    models.AnnotationTargetMessage,
		SessionID:     "session-1",
		EventUID:      "msg-1",
		AuthorType:    models.AnnotationAuthorHuman,
		Source:        models.AnnotationSourceUI,
		Note:          "old note",
		Status:        models.AnnotationStatusActive,
		SchemaVersion: models.AnnotationSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	db, stub := newMCPStubDBWithExecs(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "annotation_id = ?")
			assertMCPNamedValues(t, args, []any{"ann-message", models.AnnotationStatusDeleted, 1, 0})
			return mcpRows(mcpAnnotationColumns(), mcpAnnotationRow(current)), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "annotation_id = ?")
			assertMCPNamedValues(t, args, []any{"ann-message", models.AnnotationStatusDeleted, 1, 0})
			return mcpRows(mcpAnnotationColumns(), mcpAnnotationRow(current)), nil
		},
	}, []mcpStubExec{
		func(query string, args []driver.NamedValue) (driver.Result, error) {
			assertMCPQueryContains(t, query, "INSERT INTO trace_annotations")
			values := mcpNamedValues(args)
			if values[1] != uint64(2) || values[5] != models.AnnotationAuthorHuman || values[8] != models.AnnotationSourceUI || values[15] != "new note" {
				t.Fatalf("updated insert values = %#v", values)
			}
			return mcpExecResult(1), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolUpdateAnnotation(context.Background(), json.RawMessage(`{"annotation_id":"ann-message","note":"new note"}`))
	if err != nil {
		t.Fatalf("toolUpdateAnnotation: %v", err)
	}
	if !strings.Contains(text, `"schema":"beacon.mcp.update_annotation.v1"`) || !strings.Contains(text, `"revision":2`) || !strings.Contains(text, `"source":"ui"`) {
		t.Fatalf("update output = %s", text)
	}

	deletedAt := now.Add(time.Minute)
	deleted := current
	deleted.Revision = 2
	deleted.Status = models.AnnotationStatusDeleted
	deleted.DeletedAt = &deletedAt
	db, stub = newMCPStubDBWithExecs(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "annotation_id = ?")
			assertMCPNamedValues(t, args, []any{"ann-message", models.AnnotationStatusDeleted, 1, 0})
			return mcpRows(mcpAnnotationColumns(), mcpAnnotationRow(current)), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'")
			assertMCPNamedValues(t, args, []any{"msg-1"})
			return mcpRows([]string{"session_id"}, []driver.Value{"session-1"}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "annotation_id = ?")
			assertMCPNamedValues(t, args, []any{"ann-message", models.AnnotationStatusDeleted, 1, 0})
			return mcpRows(mcpAnnotationColumns(), mcpAnnotationRow(current)), nil
		},
	}, []mcpStubExec{
		func(query string, args []driver.NamedValue) (driver.Result, error) {
			assertMCPQueryContains(t, query, "INSERT INTO trace_annotations")
			values := mcpNamedValues(args)
			if values[1] != uint64(2) || values[17] != models.AnnotationStatusDeleted {
				t.Fatalf("deleted insert values = %#v", values)
			}
			return mcpExecResult(1), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	text, err = srv.toolDeleteAnnotation(context.Background(), json.RawMessage(`{"annotation_id":"ann-message"}`))
	if err != nil {
		t.Fatalf("toolDeleteAnnotation: %v", err)
	}
	if !strings.Contains(text, `"schema":"beacon.mcp.delete_annotation.v1"`) || !strings.Contains(text, `"status":"deleted"`) {
		t.Fatalf("delete output = %s", text)
	}
}

func TestToolGetAnnotationScopedMissReturnsForbidden(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	annotation := models.TraceAnnotation{
		AnnotationID:  "ann-secret",
		Revision:      1,
		TargetType:    models.AnnotationTargetMessage,
		SessionID:     "session-secret",
		EventUID:      "msg-secret",
		AuthorType:    models.AnnotationAuthorAgent,
		Source:        models.AnnotationSourceMCP,
		Note:          "secret note",
		Status:        models.AnnotationStatusActive,
		SchemaVersion: models.AnnotationSchemaVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "FROM trace_annotations FINAL", "annotation_id = ?")
			assertMCPNamedValues(t, args, []any{"ann-secret", models.AnnotationStatusDeleted, 1, 0})
			return mcpRows(mcpAnnotationColumns(), mcpAnnotationRow(annotation)), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH latest_event AS", "e.event_kind = 'message'", "e.source_name IN (?)")
			assertMCPNamedValues(t, args, []any{"msg-secret", "source-a"})
			return mcpRows([]string{"session_id"}), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	_, err := srv.toolGetAnnotation(context.Background(), json.RawMessage(`{"annotation_id":"ann-secret","source_name":"source-a"}`))
	if err == nil || err.Error() != "forbidden" {
		t.Fatalf("scoped get annotation error = %v, want forbidden", err)
	}
}

func TestDataBackedToolsReturnDatabaseUnavailableToolErrors(t *testing.T) {
	srv := NewServerWithBackend(failingBackendProvider{
		err: databaseUnavailableError(store.Options{
			Addrs:    []string{"127.0.0.1:9000"},
			Database: "beacon",
		}, errors.New("connect clickhouse: secret dsn")),
	}, nil)

	tests := []struct {
		name string
		args string
	}{
		{
			name: "search_sessions",
			args: `{"name":"search_sessions","arguments":{"query":"needle","limit":1,"session_id":null,"event_kinds":null}}`,
		},
		{
			name: "open",
			args: `{"name":"open","arguments":{"event_id":"event:evt-target","before":null,"after":null}}`,
		},
		{
			name: "list_sessions",
			args: `{"name":"list_sessions","arguments":{"limit":5,"since":null}}`,
		},
		{
			name: "usage_summary",
			args: `{"name":"usage_summary","arguments":{"since":"now-24h","until":"now","window_mode":null,"token_mode":null,"source_name":null,"model":null,"provider":null,"working_dir":null,"group_by":null,"limit":null}}`,
		},
		{
			name: "create_annotation",
			args: `{"name":"create_annotation","arguments":{"target_type":"session","session_id":"session:session-1","message_id":null,"event_id":null,"open_ref":null,"author_id":null,"author_name":null,"category":null,"outcome":null,"quality_score":null,"confidence":null,"needs_followup":null,"labels":null,"note":"note","metadata_json":null}}`,
		},
		{
			name: "update_annotation",
			args: `{"name":"update_annotation","arguments":{"annotation_id":"ann_1","category":null,"outcome":null,"quality_score":null,"confidence":null,"needs_followup":null,"labels":null,"note":"note","metadata_json":null}}`,
		},
		{
			name: "list_annotations",
			args: `{"name":"list_annotations","arguments":{"target_type":null,"session_id":"session:session-1","message_id":null,"event_id":null,"open_ref":null,"include_deleted":null,"limit":null,"offset":null}}`,
		},
		{
			name: "get_annotation",
			args: `{"name":"get_annotation","arguments":{"annotation_id":"ann_1","include_deleted":null}}`,
		},
		{
			name: "delete_annotation",
			args: `{"name":"delete_annotation","arguments":{"annotation_id":"ann_1"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := srv.dispatch(context.Background(), &jsonRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`17`),
				Method:  "tools/call",
				Params:  json.RawMessage(tt.args),
			})
			text, isError := toolText(t, resp)
			if !isError || !strings.Contains(text, "Beacon database is not available at 127.0.0.1:9000") || !strings.Contains(text, "beacon up") {
				t.Fatalf("%s unavailable text/isError = %q/%v", tt.name, text, isError)
			}
			if strings.Contains(text, "secret dsn") || strings.Contains(text, "connect clickhouse") {
				t.Fatalf("%s leaked internal backend error: %q", tt.name, text)
			}
		})
	}
}

func TestClickHouseBackendRetriesAfterFailureAndCachesSuccess(t *testing.T) {
	db, stub := newMCPStubDB(t, nil)
	defer stub.assertDone(t)

	var calls int
	backend := &ClickHouseBackend{
		opts: store.Options{
			Addrs:    []string{"127.0.0.1:9000"},
			Database: "beacon",
		},
		open: func(context.Context, store.Options) (*store.Store, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("dial tcp secret")
			}
			return &store.Store{DB: db}, nil
		},
	}
	t.Cleanup(func() { _ = backend.Close() })

	if _, err := backend.Backend(context.Background()); err == nil || !strings.Contains(publicToolErrorMessage(err), "Beacon database is not available at 127.0.0.1:9000") {
		t.Fatalf("first backend error = %v", err)
	}
	got, err := backend.Backend(context.Background())
	if err != nil {
		t.Fatalf("second backend open: %v", err)
	}
	if got.DB != db || got.Searcher == nil {
		t.Fatalf("backend = %#v, want cached db and searcher", got)
	}
	got, err = backend.Backend(context.Background())
	if err != nil {
		t.Fatalf("cached backend: %v", err)
	}
	if got.DB != db || calls != 2 {
		t.Fatalf("cached backend db/calls = %p/%d, want %p/2", got.DB, calls, db)
	}
}

func TestToolDefinitionsMatchImplementedArguments(t *testing.T) {
	defs := toolDefinitionsByName(t)

	searchSchema := inputSchema(t, defs["search_sessions"])
	assertSchemaProperties(t, searchSchema, append([]string{"query", "limit", "session_id", "event_kinds"}, scopeRequiredProperties()...)...)
	assertRequired(t, searchSchema, append([]string{"query", "limit", "session_id", "event_kinds"}, scopeRequiredProperties()...)...)
	assertPropertyType(t, searchSchema, "query", "string")
	assertPropertyNullableType(t, searchSchema, "limit", "integer")
	assertPropertyNullableType(t, searchSchema, "session_id", "string")
	assertPropertyNullableType(t, searchSchema, "event_kinds", "array")
	for _, name := range scopeRequiredProperties() {
		property := schemaProperty(t, searchSchema, name)
		if strings.HasSuffix(name, "s") {
			assertPropertyNullableType(t, searchSchema, name, "array")
		} else if property["type"] == nil {
			t.Fatalf("scope property %s has no type", name)
		}
	}

	openSchema := inputSchema(t, defs["open"])
	assertSchemaProperties(t, openSchema, append([]string{"event_id", "session_id", "anchor", "open_ref", "before", "after"}, scopeRequiredProperties()...)...)
	assertRequired(t, openSchema, append([]string{"event_id", "session_id", "anchor", "open_ref", "before", "after"}, scopeRequiredProperties()...)...)
	assertPropertyNullableType(t, openSchema, "event_id", "string")
	assertPropertyNullableType(t, openSchema, "session_id", "string")
	assertPropertyNullableType(t, openSchema, "anchor", "string")
	assertPropertyNullableType(t, openSchema, "before", "integer")
	assertPropertyNullableType(t, openSchema, "after", "integer")

	listSchema := inputSchema(t, defs["list_sessions"])
	assertSchemaProperties(t, listSchema, append([]string{"limit", "since", "until", "model", "provider", "working_dir", "active_during_since", "active_during_until", "cursor"}, scopeRequiredProperties()...)...)
	assertRequired(t, listSchema, append([]string{"limit", "since", "until", "model", "provider", "working_dir", "active_during_since", "active_during_until", "cursor"}, scopeRequiredProperties()...)...)
	assertPropertyNullableType(t, listSchema, "limit", "integer")
	for _, name := range []string{"since", "until", "source_name", "model", "provider", "working_dir", "active_during_since", "active_during_until", "cursor"} {
		assertPropertyNullableType(t, listSchema, name, "string")
	}

	usageSchema := inputSchema(t, defs["usage_summary"])
	assertSchemaProperties(t, usageSchema, append([]string{"since", "until", "window_mode", "token_mode", "model", "provider", "working_dir", "group_by", "limit"}, scopeRequiredProperties()...)...)
	assertRequired(t, usageSchema, append([]string{"since", "until", "window_mode", "token_mode", "model", "provider", "working_dir", "group_by", "limit"}, scopeRequiredProperties()...)...)
	for _, name := range []string{"since", "until", "window_mode", "token_mode", "source_name", "model", "provider", "working_dir"} {
		assertPropertyNullableType(t, usageSchema, name, "string")
	}
	assertPropertyNullableType(t, usageSchema, "group_by", "array")
	assertPropertyNullableType(t, usageSchema, "limit", "integer")

	createSchema := inputSchema(t, defs["create_annotation"])
	createRequired := append(append(annotationTargetRequiredProperties(), annotationCreateContentRequiredProperties()...), scopeRequiredProperties()...)
	assertSchemaProperties(t, createSchema, createRequired...)
	assertRequired(t, createSchema, createRequired...)
	for _, name := range []string{"target_type", "session_id", "message_id", "event_id", "author_id", "author_name", "category", "outcome", "note", "metadata_json"} {
		assertPropertyNullableType(t, createSchema, name, "string")
	}
	assertPropertyNullableType(t, createSchema, "quality_score", "integer")
	assertPropertyNullableType(t, createSchema, "confidence", "integer")
	assertPropertyNullableType(t, createSchema, "needs_followup", "boolean")
	assertPropertyNullableType(t, createSchema, "labels", "array")
	assertToolAnnotations(t, defs["create_annotation"], false, false)

	updateSchema := inputSchema(t, defs["update_annotation"])
	updateRequired := append(append([]string{"annotation_id"}, annotationUpdateContentRequiredProperties()...), scopeRequiredProperties()...)
	assertSchemaProperties(t, updateSchema, updateRequired...)
	assertRequired(t, updateSchema, updateRequired...)
	assertPropertyType(t, updateSchema, "annotation_id", "string")
	assertPropertyNullableType(t, updateSchema, "note", "string")
	if _, ok := updateSchema["properties"].(map[string]any)["author_id"]; ok {
		t.Fatalf("update_annotation schema must not expose author_id")
	}
	if _, ok := updateSchema["properties"].(map[string]any)["author_name"]; ok {
		t.Fatalf("update_annotation schema must not expose author_name")
	}
	assertToolAnnotations(t, defs["update_annotation"], false, false)

	listAnnotationsSchema := inputSchema(t, defs["list_annotations"])
	listRequired := append(append(annotationTargetRequiredProperties(), "include_deleted", "limit", "offset"), scopeRequiredProperties()...)
	assertSchemaProperties(t, listAnnotationsSchema, listRequired...)
	assertRequired(t, listAnnotationsSchema, listRequired...)
	assertPropertyNullableType(t, listAnnotationsSchema, "include_deleted", "boolean")
	assertPropertyNullableType(t, listAnnotationsSchema, "limit", "integer")
	assertPropertyNullableType(t, listAnnotationsSchema, "offset", "integer")
	assertToolAnnotations(t, defs["list_annotations"], true, false)

	getSchema := inputSchema(t, defs["get_annotation"])
	assertSchemaProperties(t, getSchema, append([]string{"annotation_id", "include_deleted"}, scopeRequiredProperties()...)...)
	assertRequired(t, getSchema, append([]string{"annotation_id", "include_deleted"}, scopeRequiredProperties()...)...)
	assertPropertyType(t, getSchema, "annotation_id", "string")
	assertPropertyNullableType(t, getSchema, "include_deleted", "boolean")
	assertToolAnnotations(t, defs["get_annotation"], true, false)

	deleteSchema := inputSchema(t, defs["delete_annotation"])
	assertSchemaProperties(t, deleteSchema, append([]string{"annotation_id"}, scopeRequiredProperties()...)...)
	assertRequired(t, deleteSchema, append([]string{"annotation_id"}, scopeRequiredProperties()...)...)
	assertPropertyType(t, deleteSchema, "annotation_id", "string")
	assertToolAnnotations(t, defs["delete_annotation"], false, true)
}

type failingBackendProvider struct {
	err error
}

func (p failingBackendProvider) Backend(context.Context) (Backend, error) {
	return Backend{}, p.err
}

func TestToolDefinitionsAreOpenAIFunctionCompatible(t *testing.T) {
	for _, def := range toolDefinitions() {
		name, _ := def["name"].(string)
		schema := inputSchema(t, def)
		if schema["type"] != "object" {
			t.Fatalf("%s inputSchema type = %#v, want object", name, schema["type"])
		}
		for _, keyword := range []string{"oneOf", "anyOf", "allOf", "enum", "not"} {
			if _, ok := schema[keyword]; ok {
				t.Fatalf("%s inputSchema uses top-level %s: %#v", name, keyword, schema)
			}
		}
		if schema["additionalProperties"] != false {
			t.Fatalf("%s inputSchema additionalProperties = %#v, want false", name, schema["additionalProperties"])
		}
		assertRequiredCoversAllProperties(t, name, schema)
	}
}

type fakeMCPSearcher struct {
	query   search.SearchQuery
	results []search.SearchResult
	err     error
}

func (f *fakeMCPSearcher) Search(_ context.Context, q search.SearchQuery) ([]search.SearchResult, error) {
	f.query = q
	return f.results, f.err
}

func decodeJSONRPCResponses(t *testing.T, output string) []jsonRPCResponse {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	responses := make([]jsonRPCResponse, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func assertResponseID(t *testing.T, resp jsonRPCResponse, want string) {
	t.Helper()
	if string(resp.ID) != want {
		t.Fatalf("response id = %s, want %s: %+v", resp.ID, want, resp)
	}
}

func toolText(t *testing.T, resp *jsonRPCResponse) (string, bool) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
		return "", false
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result = %T, want map[string]any", resp.Result)
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v, want one text item", result["content"])
	}
	text, ok := content[0]["text"].(string)
	if !ok {
		t.Fatalf("content text = %#v", content[0]["text"])
	}
	isError, _ := result["isError"].(bool)
	return text, isError
}

func toolDefinitionsByName(t *testing.T) map[string]map[string]any {
	t.Helper()
	defs := make(map[string]map[string]any)
	for _, def := range toolDefinitions() {
		name, _ := def["name"].(string)
		defs[name] = def
	}
	for _, name := range expectedMCPToolNames() {
		if defs[name] == nil {
			t.Fatalf("missing tool definition %q", name)
		}
	}
	return defs
}

func inputSchema(t *testing.T, def map[string]any) map[string]any {
	t.Helper()
	schema, ok := def["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("inputSchema = %T", def["inputSchema"])
	}
	return schema
}

func assertSchemaProperties(t *testing.T, schema map[string]any, names ...string) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T", schema["properties"])
	}
	for _, name := range names {
		if _, ok := properties[name]; !ok {
			t.Fatalf("schema missing property %q: %#v", name, properties)
		}
	}
}

func assertRequired(t *testing.T, schema map[string]any, names ...string) {
	t.Helper()
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required = %#v, want []string", schema["required"])
	}
	if !sameStringSet(required, names) {
		t.Fatalf("required = %#v, want set %#v", required, names)
	}
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[string]int, len(want))
	for _, value := range want {
		counts[value]++
	}
	for _, value := range got {
		if counts[value] == 0 {
			return false
		}
		counts[value]--
	}
	return true
}

func assertPropertyType(t *testing.T, schema map[string]any, name, wantType string) {
	t.Helper()
	property := schemaProperty(t, schema, name)
	if property["type"] != wantType {
		t.Fatalf("%s type = %#v, want %q", name, property["type"], wantType)
	}
}

func assertPropertyNullableType(t *testing.T, schema map[string]any, name, wantType string) {
	t.Helper()
	property := schemaProperty(t, schema, name)
	want := []string{wantType, "null"}
	if !reflect.DeepEqual(property["type"], want) {
		t.Fatalf("%s type = %#v, want %#v", name, property["type"], want)
	}
}

func schemaProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T", schema["properties"])
	}
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("property %q = %T", name, properties[name])
	}
	return property
}

func assertToolAnnotations(t *testing.T, def map[string]any, readOnly, destructive bool) {
	t.Helper()
	annotations, ok := def["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("annotations = %T", def["annotations"])
	}
	if annotations["readOnlyHint"] != readOnly || annotations["destructiveHint"] != destructive {
		t.Fatalf("annotations = %#v, want readOnly=%v destructive=%v", annotations, readOnly, destructive)
	}
	if annotations["idempotentHint"] != readOnly || annotations["openWorldHint"] != false {
		t.Fatalf("annotations idempotent/openWorld = %#v", annotations)
	}
}

func assertRequiredCoversAllProperties(t *testing.T, toolName string, schema map[string]any) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s properties = %T", toolName, schema["properties"])
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("%s required = %#v, want []string", toolName, schema["required"])
	}
	requiredSet := make(map[string]bool, len(required))
	for _, name := range required {
		requiredSet[name] = true
	}
	for name := range properties {
		if !requiredSet[name] {
			t.Fatalf("%s property %q is not required: %#v", toolName, name, required)
		}
	}
	if len(requiredSet) != len(properties) {
		t.Fatalf("%s required = %#v, properties = %#v", toolName, required, properties)
	}
}

type mcpStubQuery func(query string, args []driver.NamedValue) (driver.Rows, error)
type mcpStubExec func(query string, args []driver.NamedValue) (driver.Result, error)

type mcpStubDB struct {
	mu      sync.Mutex
	queries []mcpStubQuery
	execs   []mcpStubExec
}

func newMCPStubDB(t *testing.T, queries []mcpStubQuery) (*sql.DB, *mcpStubDB) {
	t.Helper()
	return newMCPStubDBWithExecs(t, queries, nil)
}

func newMCPStubDBWithExecs(t *testing.T, queries []mcpStubQuery, execs []mcpStubExec) (*sql.DB, *mcpStubDB) {
	t.Helper()
	stub := &mcpStubDB{queries: queries, execs: execs}
	return sql.OpenDB(mcpStubConnector{stub: stub}), stub
}

func (s *mcpStubDB) assertDone(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) != 0 {
		t.Fatalf("unconsumed query expectations = %d", len(s.queries))
	}
	if len(s.execs) != 0 {
		t.Fatalf("unconsumed exec expectations = %d", len(s.execs))
	}
}

type mcpStubConnector struct {
	stub *mcpStubDB
}

func (c mcpStubConnector) Connect(context.Context) (driver.Conn, error) {
	return mcpStubConn(c), nil
}

func (c mcpStubConnector) Driver() driver.Driver {
	return mcpStubDriver{}
}

type mcpStubDriver struct{}

func (mcpStubDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use mcpStubConnector")
}

type mcpStubConn struct {
	stub *mcpStubDB
}

func (c mcpStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}

func (c mcpStubConn) Close() error {
	return nil
}

func (c mcpStubConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions unsupported")
}

func (c mcpStubConn) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (c mcpStubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.stub.mu.Lock()
	defer c.stub.mu.Unlock()
	if len(c.stub.queries) == 0 {
		return nil, fmt.Errorf("unexpected query: %s args=%#v", query, mcpNamedValues(args))
	}
	next := c.stub.queries[0]
	c.stub.queries = c.stub.queries[1:]
	return next(query, args)
}

func (c mcpStubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.stub.mu.Lock()
	defer c.stub.mu.Unlock()
	if len(c.stub.execs) == 0 {
		return nil, fmt.Errorf("unexpected exec: %s args=%#v", query, mcpNamedValues(args))
	}
	next := c.stub.execs[0]
	c.stub.execs = c.stub.execs[1:]
	return next(query, args)
}

type mcpExecResult int64

func (r mcpExecResult) LastInsertId() (int64, error) {
	return 0, errors.New("last insert id unsupported")
}

func (r mcpExecResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

type mcpDriverRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func mcpRows(columns []string, rows ...[]driver.Value) *mcpDriverRows {
	return &mcpDriverRows{columns: columns, rows: rows}
}

func mcpOpenColumns() []string {
	return []string{"event_uid", "event_session_id", "source_name", "runtime", "project_key", "project_path", "event_kind", "actor_role", "text_preview", "tool_name", "model", "tokens", "timestamp", "target"}
}

func mcpOpenRow(eventUID, sessionID, eventKind, actorRole, preview, toolName, model string, tokens int64, timestamp time.Time, target uint8) []driver.Value {
	return []driver.Value{
		eventUID,
		sessionID,
		"source",
		"runtime",
		"project",
		"/work/project",
		eventKind,
		actorRole,
		preview,
		toolName,
		model,
		tokens,
		timestamp,
		target,
	}
}

func mcpSessionColumns() []string {
	return []string{"session_id", "source_name", "runtime", "project_key", "project_path", "provider", "started_at", "ended_at", "event_count", "turn_count", "total_tokens", "tool_call_count", "mcp_call_count", "error_count", "last_model", "working_dir"}
}

func mcpSessionRow(sessionID string, started, ended any) []driver.Value {
	return []driver.Value{sessionID, "codex", "codex", "beacon", "/work/beacon", "openai", started, ended, int64(4), int64(2), int64(30), int64(1), int64(1), int64(0), "gpt-5.4", "/work/beacon"}
}

func mcpAnnotationColumns() []string {
	return []string{"annotation_id", "revision", "target_type", "session_id", "event_uid", "author_type", "author_id", "author_name", "source", "category", "outcome", "quality_score", "confidence", "needs_followup", "labels", "note", "metadata_json", "status", "schema_version", "created_at", "updated_at", "deleted_at"}
}

func mcpAnnotationRow(a models.TraceAnnotation) []driver.Value {
	deletedAt := time.Unix(0, 0).UTC()
	if a.DeletedAt != nil {
		deletedAt = *a.DeletedAt
	}
	return []driver.Value{
		a.AnnotationID,
		int64(a.Revision),
		a.TargetType,
		a.SessionID,
		a.EventUID,
		a.AuthorType,
		a.AuthorID,
		a.AuthorName,
		a.Source,
		a.Category,
		a.Outcome,
		int64(a.QualityScore),
		int64(a.Confidence),
		boolAsMCPUInt8(a.NeedsFollowup),
		strings.Join(a.Labels, ","),
		a.Note,
		a.MetadataJSON,
		a.Status,
		int64(a.SchemaVersion),
		a.CreatedAt,
		a.UpdatedAt,
		deletedAt,
	}
}

func boolAsMCPUInt8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func (r *mcpDriverRows) Columns() []string {
	return r.columns
}

func (r *mcpDriverRows) Close() error {
	return nil
}

func (r *mcpDriverRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

func assertMCPQueryContains(t *testing.T, query string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query missing %q:\n%s", fragment, query)
		}
	}
}

func assertMCPNamedValues(t *testing.T, args []driver.NamedValue, want []any) {
	t.Helper()
	got := mcpNamedValues(args)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func mcpNamedValues(args []driver.NamedValue) []any {
	values := make([]any, 0, len(args))
	for _, arg := range args {
		values = append(values, arg.Value)
	}
	return values
}
