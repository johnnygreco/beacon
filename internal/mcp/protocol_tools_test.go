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

func TestToolSearchAppliesAuthScopeAndEmitsProvenance(t *testing.T) {
	fake := &fakeMCPSearcher{
		results: []search.SearchResult{{
			EventUID:    "evt-search",
			SessionID:   "session-search",
			NodeID:      "node-a",
			CollectorID: "collector-a",
			SourceID:    "source-a",
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
	ctx := ContextWithAuthScope(context.Background(), ScopeFilters{NodeIDs: []string{"node-a"}, SourceIDs: []string{"source-a"}})

	text, err := srv.toolSearch(ctx, json.RawMessage(`{"query":"needle","limit":2,"node_id":"node-a","source_id":"source-a"}`))
	if err != nil {
		t.Fatalf("toolSearch: %v", err)
	}
	if !reflect.DeepEqual(fake.query.NodeIDs, []string{"node-a"}) || !reflect.DeepEqual(fake.query.SourceIDs, []string{"source-a"}) {
		t.Fatalf("search scope = nodes %#v sources %#v", fake.query.NodeIDs, fake.query.SourceIDs)
	}
	for _, want := range []string{
		`"auth_scope_applied":true`,
		`"node_ids":["node-a"]`,
		`"source_ids":["source-a"]`,
		`"node_id":"node-a"`,
		`"collector_id":"collector-a"`,
		`"runtime":"runtime"`,
		`"project_key":"beacon"`,
		`"open_ref":{"type":"event"`,
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
	ctx := ContextWithAuthScope(context.Background(), ScopeFilters{NodeIDs: []string{"node-a"}})

	if _, err := srv.toolSearch(ctx, json.RawMessage(`{"query":"needle","node_id":"node-b"}`)); err != nil {
		t.Fatalf("toolSearch: %v", err)
	}
	if !reflect.DeepEqual(fake.query.NodeIDs, []string{scopeImpossibleValue}) {
		t.Fatalf("broadened auth scope query nodes = %#v", fake.query.NodeIDs)
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
				[]driver.Value{"evt-target", "session-1", "node-a", "collector-a", "source-a", "source", "runtime", "project", "/work/project", "message", "assistant", "bad timestamp", "", "gpt-5.4", int64(1), "not-a-time", uint8(1)},
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
			assertMCPQueryContains(t, query, "ae.event_uid = ?", "ae.source_id IN (?)", "WHERE n.rn BETWEEN")
			assertMCPNamedValues(t, args, []any{"evt-secret", "source-a", "source-a", 3, 3})
			return mcpRows(mcpOpenColumns()), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	ctx := ContextWithAuthScope(context.Background(), ScopeFilters{SourceIDs: []string{"source-a"}})
	_, err := srv.toolOpen(ctx, json.RawMessage(`{"event_id":"event:evt-secret"}`))
	if err == nil || err.Error() != "forbidden" {
		t.Fatalf("scoped open error = %v, want forbidden", err)
	}
	if strings.Contains(err.Error(), "evt-secret") {
		t.Fatalf("forbidden open leaked event id: %v", err)
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
		`"node_id":"node-a"`,
		`"collector_id":"collector-a"`,
		`"source_id":"source-a"`,
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

func TestToolListAgentsReturnsFleetProvenanceAndOpenRefs(t *testing.T) {
	started := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	ended := started.Add(10 * time.Minute)
	db, stub := newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "SELECT count()", "GROUP BY node_id, collector_id, source_id")
			assertMCPNamedValues(t, args, []any{"node-a"})
			return mcpRows([]string{"count"}, []driver.Value{int64(1)}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "latest_session_id", "ORDER BY last_ended_at DESC")
			assertMCPNamedValues(t, args, []any{"node-a", 10})
			return mcpRows(
				[]string{"node_id", "collector_id", "source_id", "source_name", "runtime", "project_key", "project_path", "session_count", "event_count", "total_tokens", "last_started_at", "last_ended_at", "latest_session_id"},
				[]driver.Value{"node-a", "collector-a", "source-a", "source", "runtime", "beacon", "/work/beacon", int64(2), int64(12), int64(500), started, ended, "session-1"},
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	srv := testServer()
	srv.db = db
	text, err := srv.toolListAgents(context.Background(), json.RawMessage(`{"limit":10,"node_id":"node-a"}`))
	if err != nil {
		t.Fatalf("toolListAgents: %v", err)
	}
	for _, want := range []string{
		`"schema":"beacon.mcp.list_agents.v1"`,
		`"node_id":"node-a"`,
		`"collector_id":"collector-a"`,
		`"source_id":"source-a"`,
		`"runtime":"runtime"`,
		`"project_key":"beacon"`,
		`"latest_session_id":"session:session-1"`,
		`"open_ref":{"type":"session_latest"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("agent output missing %q:\n%s", want, text)
		}
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
			name: "list_agents",
			args: `{"name":"list_agents","arguments":{"limit":5}}`,
		},
		{
			name: "list_sessions",
			args: `{"name":"list_sessions","arguments":{"limit":5,"since":null}}`,
		},
		{
			name: "usage_summary",
			args: `{"name":"usage_summary","arguments":{"since":"now-24h","until":"now","window_mode":null,"token_mode":null,"source_name":null,"model":null,"provider":null,"working_dir":null,"group_by":null,"limit":null}}`,
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
	assertSchemaProperties(t, openSchema, "event_id", "session_id", "anchor", "open_ref", "before", "after")
	assertRequired(t, openSchema, "event_id", "session_id", "anchor", "open_ref", "before", "after")
	assertPropertyNullableType(t, openSchema, "event_id", "string")
	assertPropertyNullableType(t, openSchema, "session_id", "string")
	assertPropertyNullableType(t, openSchema, "anchor", "string")
	assertPropertyNullableType(t, openSchema, "before", "integer")
	assertPropertyNullableType(t, openSchema, "after", "integer")

	agentsSchema := inputSchema(t, defs["list_agents"])
	assertSchemaProperties(t, agentsSchema, append([]string{"limit"}, scopeRequiredProperties()...)...)
	assertRequired(t, agentsSchema, append([]string{"limit"}, scopeRequiredProperties()...)...)
	assertPropertyNullableType(t, agentsSchema, "limit", "integer")

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
	for _, name := range []string{"search_sessions", "open", "list_agents", "list_sessions", "usage_summary"} {
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
	if !reflect.DeepEqual(required, names) {
		t.Fatalf("required = %#v, want %#v", required, names)
	}
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

type mcpStubDB struct {
	mu      sync.Mutex
	queries []mcpStubQuery
}

func newMCPStubDB(t *testing.T, queries []mcpStubQuery) (*sql.DB, *mcpStubDB) {
	t.Helper()
	stub := &mcpStubDB{queries: queries}
	return sql.OpenDB(mcpStubConnector{stub: stub}), stub
}

func (s *mcpStubDB) assertDone(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) != 0 {
		t.Fatalf("unconsumed query expectations = %d", len(s.queries))
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

type mcpDriverRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func mcpRows(columns []string, rows ...[]driver.Value) *mcpDriverRows {
	return &mcpDriverRows{columns: columns, rows: rows}
}

func mcpOpenColumns() []string {
	return []string{"event_uid", "event_session_id", "node_id", "collector_id", "source_id", "source_name", "runtime", "project_key", "project_path", "event_kind", "actor_role", "text_preview", "tool_name", "model", "tokens", "timestamp", "target"}
}

func mcpOpenRow(eventUID, sessionID, eventKind, actorRole, preview, toolName, model string, tokens int64, timestamp time.Time, target uint8) []driver.Value {
	return []driver.Value{
		eventUID,
		sessionID,
		"node-a",
		"collector-a",
		"source-a",
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
	return []string{"session_id", "node_id", "collector_id", "source_id", "source_name", "runtime", "project_key", "project_path", "provider", "started_at", "ended_at", "event_count", "turn_count", "total_tokens", "tool_call_count", "mcp_call_count", "error_count", "last_model", "working_dir"}
}

func mcpSessionRow(sessionID string, started, ended any) []driver.Value {
	return []driver.Value{sessionID, "node-a", "collector-a", "source-a", "codex", "codex", "beacon", "/work/beacon", "openai", started, ended, int64(4), int64(2), int64(30), int64(1), int64(1), int64(0), "gpt-5.4", "/work/beacon"}
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
