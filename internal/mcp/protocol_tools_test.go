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
	if fake.query.Query != "needle" || fake.query.Limit != 2 || fake.query.SessionID != "session-search" || !fake.query.ExcludeMCPSelf {
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
			assertMCPNamedValues(t, args, []any{"evt-target", "evt-target", 1, 2})
			return mcpRows(
				[]string{"event_uid", "event_kind", "actor_role", "text_preview", "tool_name", "model", "tokens", "timestamp"},
				[]driver.Value{"evt-before", "message", "user", "before", "", "gpt-5.4", int64(3), targetTime.Add(-time.Minute)},
				[]driver.Value{"evt-target", "tool_call", "assistant", "target", "Bash", "gpt-5.4", int64(5), targetTime},
				[]driver.Value{"evt-after", "tool_result", "tool", "after", "Bash", "gpt-5.4", int64(7), targetTime.Add(time.Minute)},
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
	if !strings.Contains(text, `"schema":"beacon.mcp.open.v1"`) || !strings.Contains(text, `"event_id":"event:evt-target"`) || !strings.Contains(text, `"target":true`) {
		t.Fatalf("open output = %s", text)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertMCPQueryContains(t, query, "WITH target AS", "ROW_NUMBER() OVER", "WHERE n.rn BETWEEN")
			assertMCPNamedValues(t, args, []any{"evt-target", "evt-target", 4, 4})
			return mcpRows(
				[]string{"event_uid", "event_kind", "actor_role", "text_preview", "tool_name", "model", "tokens", "timestamp"},
				[]driver.Value{"evt-target", "tool_call", "assistant", "target", "Bash", "gpt-5.4", int64(5), targetTime},
			), nil
		},
	})
	defer db.Close()
	defer stub.assertDone(t)
	srv.db = db
	srv.SetDefaultContextWindow(4)
	if _, err := srv.toolOpen(context.Background(), json.RawMessage(`{"event_id":"event:evt-target"}`)); err != nil {
		t.Fatalf("toolOpen with configured default context: %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return mcpRows([]string{"event_uid", "event_kind", "actor_role", "text_preview", "tool_name", "model", "tokens", "timestamp"}), nil
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
	if err == nil || !strings.Contains(err.Error(), "event_id is required") {
		t.Fatalf("toolOpen required error = %v", err)
	}
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"event_uid":"evt-target"}`))
	if err == nil || !strings.Contains(err.Error(), "event_id is required") {
		t.Fatalf("toolOpen event_uid argument error = %v", err)
	}
	_, err = srv.toolOpen(context.Background(), json.RawMessage(`{"id":"event:evt-target"}`))
	if err == nil || !strings.Contains(err.Error(), "event_id is required") {
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
				[]string{"event_uid", "event_kind", "actor_role", "text_preview", "tool_name", "model", "tokens", "timestamp"},
				[]driver.Value{"evt-target", "message", "assistant", "bad timestamp", "", "gpt-5.4", int64(1), "not-a-time"},
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
			assertMCPQueryContains(t, query, "FROM (SELECT", "WHERE sp.started_at >= ?", "ORDER BY started_at DESC LIMIT ?")
			assertMCPNamedValues(t, args, []any{since, 5})
			return mcpRows(
				[]string{"session_id", "source_name", "started_at", "ended_at", "event_count", "turn_count", "total_tokens", "tool_call_count", "mcp_call_count", "error_count", "last_model"},
				[]driver.Value{"session-1", "codex", started, ended, int64(4), int64(2), int64(30), int64(1), int64(1), int64(0), "gpt-5.4"},
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
	if !strings.Contains(text, `"schema":"beacon.mcp.list_sessions.v1"`) || !strings.Contains(text, `"session_id":"session:session-1"`) || !strings.Contains(text, `"total_tokens":30`) {
		t.Fatalf("list output = %s", text)
	}

	_, err = srv.toolListSessions(context.Background(), json.RawMessage(`{"since":"not-a-time"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid since timestamp") {
		t.Fatalf("invalid since error = %v", err)
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
		t.Fatalf("list query error = %v", err)
	}

	db, stub = newMCPStubDB(t, []mcpStubQuery{
		func(string, []driver.NamedValue) (driver.Rows, error) {
			return mcpRows(
				[]string{"session_id", "source_name", "started_at", "ended_at", "event_count", "turn_count", "total_tokens", "tool_call_count", "mcp_call_count", "error_count", "last_model"},
				[]driver.Value{"session-1", "codex", "not-a-time", ended, int64(4), int64(2), int64(30), int64(1), int64(1), int64(0), "gpt-5.4"},
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

func TestToolDefinitionsMatchImplementedArguments(t *testing.T) {
	defs := toolDefinitionsByName(t)

	searchSchema := inputSchema(t, defs["search_sessions"])
	assertSchemaProperties(t, searchSchema, "query", "limit", "session_id", "event_kinds")
	assertRequired(t, searchSchema, "query")

	openSchema := inputSchema(t, defs["open"])
	assertSchemaProperties(t, openSchema, "event_id", "before", "after")
	assertRequired(t, openSchema, "event_id")

	listSchema := inputSchema(t, defs["list_sessions"])
	assertSchemaProperties(t, listSchema, "limit", "since")
	if _, ok := listSchema["required"]; ok {
		t.Fatalf("list_sessions should not require optional args: %#v", listSchema["required"])
	}
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
	for _, name := range []string{"search_sessions", "open", "list_sessions"} {
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
