package search

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestNormalizeSearchQueryDefaults(t *testing.T) {
	s := NewSearcher(nil, nil, 42, 0)

	q := s.normalize(SearchQuery{MinScore: -1})

	if q.Limit != 42 {
		t.Fatalf("Limit = %d, want searcher max", q.Limit)
	}
	if q.MinScore != 0 {
		t.Fatalf("MinScore = %.2f, want 0", q.MinScore)
	}

	s = NewSearcher(nil, nil, 0, 0)
	q = s.normalize(SearchQuery{})
	if q.Limit != 25 {
		t.Fatalf("Limit = %d, want fallback", q.Limit)
	}
}

func TestSearchFilterBuildersCoverAllControls(t *testing.T) {
	from := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)

	sql, args := buildPostingFilters(SearchQuery{
		SessionID:      "session-abc",
		EventKinds:     []string{"message", "tool_call"},
		FromTime:       from,
		ToTime:         to,
		ExcludeMCPSelf: true,
	})

	for _, expected := range []string{
		"AND startsWith(p.session_id, ?)",
		"AND p.event_kind IN (?,?)",
		"AND p.timestamp >= ?",
		"AND p.timestamp <= ?",
		"AND p.tool_name NOT IN ('search_sessions', 'open', 'list_sessions')",
		"positionCaseInsensitive(p.text_preview, 'beacon') = 0",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("filter SQL missing %q: %s", expected, sql)
		}
	}
	if len(args) != 5 {
		t.Fatalf("args = %#v, want 5 args", args)
	}
}

func TestDocumentFilterBuilderOmitsAliases(t *testing.T) {
	sql, args := buildDocumentFilters(SearchQuery{
		SessionID:  "session-abc",
		EventKinds: []string{"error"},
	})

	if strings.Contains(sql, "p.") {
		t.Fatalf("document filters should not include posting alias: %s", sql)
	}
	if !strings.Contains(sql, "startsWith(session_id, ?)") || !strings.Contains(sql, "event_kind IN (?)") {
		t.Fatalf("document filters missing clauses: %s", sql)
	}
	if len(args) != 2 {
		t.Fatalf("args = %#v, want 2 args", args)
	}
}

func TestSearchSortOrders(t *testing.T) {
	cases := map[string]string{
		"":          "score DESC, timestamp DESC",
		"relevance": "score DESC, timestamp DESC",
		"newest":    "timestamp DESC",
		"oldest":    "timestamp ASC",
	}
	for input, expected := range cases {
		if got := searchSortOrder(input); got != expected {
			t.Fatalf("searchSortOrder(%q) = %q, want %q", input, got, expected)
		}
	}

	if got := browseSortOrder("oldest"); got != "ASC" {
		t.Fatalf("browseSortOrder(oldest) = %q, want ASC", got)
	}
	if got := browseSortOrder("relevance"); got != "DESC" {
		t.Fatalf("browseSortOrder(relevance) = %q, want DESC", got)
	}

	hostile := "timestamp DESC; DROP TABLE search_documents"
	if got := searchSortOrder(hostile); got != "score DESC, timestamp DESC" {
		t.Fatalf("hostile search sort = %q, want relevance fallback", got)
	}
	if got := browseSortOrder(hostile); got != "DESC" {
		t.Fatalf("hostile browse sort = %q, want newest fallback", got)
	}
}

func TestUniqPreservesFirstOccurrence(t *testing.T) {
	got := uniq([]string{"dashboard", "search", "dashboard", "tools", "search"})
	want := []string{"dashboard", "search", "tools"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("uniq = %#v, want %#v", got, want)
	}
}

func TestSearchBuildsPostingsQueryWithDeterministicRankingAndFilters(t *testing.T) {
	from := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	rowTime := from.Add(time.Minute)
	db, stub := newSearchStubDB(t, []stubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertSQLContains(t, query, "FROM search_documents FINAL", "count() AS documents")
			assertNamedValues(t, args, nil)
			return newDriverRows([]string{"documents", "avg_doc_len"}, []driver.Value{int64(3), float64(10)}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertSQLContains(t, query,
				"FROM search_postings FINAL",
				"WHERE token IN (?,?)",
				"AND startsWith(p.session_id, ?)",
				"AND p.event_kind IN (?,?)",
				"AND p.timestamp >= ?",
				"AND p.timestamp <= ?",
				"AND p.tool_name NOT IN ('search_sessions', 'open', 'list_sessions')",
				"positionCaseInsensitive(p.text_preview, 'beacon') = 0",
				"HAVING score >= ?",
				"ORDER BY score DESC, timestamp DESC",
				"LIMIT ?",
			)
			assertNamedValues(t, args, []any{
				float64(3),
				float64(10),
				"alpha",
				"beta",
				"session-",
				"message",
				"tool_call",
				from,
				to,
				0.25,
				2,
			})
			return searchResultDriverRows([]SearchResult{
				{EventUID: "evt-high", SessionID: "session-a", EventKind: "message", TextPreview: "high", Score: 4.5, Timestamp: rowTime, Model: "gpt-5.4", Provider: "openai"},
				{EventUID: "evt-low", SessionID: "session-b", EventKind: "tool_call", TextPreview: "low", Score: 2.25, Timestamp: rowTime.Add(-time.Minute), ToolName: "Bash", Model: "gpt-5.4", Provider: "openai"},
			}), nil
		},
	}, nil)
	defer db.Close()
	defer stub.assertDone(t)

	s := NewSearcher(db, discardLogger, 25, 0)
	s.logSem = nil
	results, err := s.Search(context.Background(), SearchQuery{
		Query:          "Alpha alpha Beta",
		Limit:          2,
		MinScore:       0.25,
		SessionID:      "session-",
		EventKinds:     []string{"message", "tool_call"},
		FromTime:       from,
		ToTime:         to,
		ExcludeMCPSelf: true,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(results) != 2 || results[0].EventUID != "evt-high" || results[1].EventUID != "evt-low" {
		t.Fatalf("results = %#v, want deterministic ordered rows", results)
	}
	if results[0].Score != 4.5 || results[1].Score != 2.25 {
		t.Fatalf("scores = %.2f/%.2f, want 4.5/2.25", results[0].Score, results[1].Score)
	}
}

func TestSearchEmptyQueryBrowsesDocuments(t *testing.T) {
	rowTime := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	db, stub := newSearchStubDB(t, []stubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertSQLContains(t, query,
				"FROM search_documents FINAL",
				"AND startsWith(session_id, ?)",
				"ORDER BY timestamp ASC",
				"LIMIT ?",
			)
			if strings.Contains(query, "search_postings") {
				t.Fatalf("empty search query should browse documents, got postings query: %s", query)
			}
			assertNamedValues(t, args, []any{"session-", 7})
			return searchResultDriverRows([]SearchResult{
				{EventUID: "evt-browse", SessionID: "session-a", EventKind: "message", TextPreview: "browse", Timestamp: rowTime, Model: "gpt-5.4", Provider: "openai"},
			}), nil
		},
	}, nil)
	defer db.Close()
	defer stub.assertDone(t)

	s := NewSearcher(db, discardLogger, 25, 0)
	results, err := s.Search(context.Background(), SearchQuery{
		Query:     "a an the",
		Limit:     7,
		SessionID: "session-",
		SortBy:    "oldest",
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(results) != 1 || results[0].EventUID != "evt-browse" || results[0].Score != 0 {
		t.Fatalf("results = %#v, want browse result with zero score", results)
	}
}

func TestSearchCachesStatsAcrossSearches(t *testing.T) {
	rowTime := time.Date(2026, 5, 22, 13, 0, 0, 0, time.UTC)
	postingsQuery := func(id string) stubQuery {
		return func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertSQLContains(t, query, "FROM search_postings FINAL", "ORDER BY score DESC, timestamp DESC")
			assertNamedValues(t, args, []any{float64(2), float64(8), "cache", 0.0, 3})
			return searchResultDriverRows([]SearchResult{
				{EventUID: id, SessionID: "session-cache", EventKind: "message", Score: 1.5, Timestamp: rowTime},
			}), nil
		}
	}
	db, stub := newSearchStubDB(t, []stubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertSQLContains(t, query, "FROM search_documents FINAL", "avg_doc_len")
			assertNamedValues(t, args, nil)
			return newDriverRows([]string{"documents", "avg_doc_len"}, []driver.Value{int64(2), float64(8)}), nil
		},
		postingsQuery("evt-cache-1"),
		postingsQuery("evt-cache-2"),
	}, nil)
	defer db.Close()
	defer stub.assertDone(t)

	s := NewSearcher(db, discardLogger, 25, 0)
	s.logSem = nil
	for _, wantID := range []string{"evt-cache-1", "evt-cache-2"} {
		results, err := s.Search(context.Background(), SearchQuery{Query: "cache", Limit: 3})
		if err != nil {
			t.Fatalf("Search error = %v", err)
		}
		if len(results) != 1 || results[0].EventUID != wantID {
			t.Fatalf("results = %#v, want %s", results, wantID)
		}
	}
}

func TestSearchIgnoresQueryLogInsertFailure(t *testing.T) {
	logged := make(chan error, 1)
	logErr := errors.New("query log insert failed")
	rowTime := time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC)
	db, stub := newSearchStubDB(t, []stubQuery{
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertSQLContains(t, query, "FROM search_documents FINAL")
			return newDriverRows([]string{"documents", "avg_doc_len"}, []driver.Value{int64(1), float64(5)}), nil
		},
		func(query string, args []driver.NamedValue) (driver.Rows, error) {
			assertSQLContains(t, query, "FROM search_postings FINAL")
			assertNamedValues(t, args, []any{float64(1), float64(5), "alpha", 0.0, 5})
			return searchResultDriverRows([]SearchResult{
				{EventUID: "evt-alpha", SessionID: "session-alpha", EventKind: "message", Score: 1.25, Timestamp: rowTime},
			}), nil
		},
	}, []stubExec{
		func(query string, args []driver.NamedValue) (driver.Result, error) {
			var checkErr error
			if !strings.Contains(query, "INSERT INTO search_query_log") {
				checkErr = fmt.Errorf("query log SQL = %s", query)
			} else if len(args) != 4 {
				checkErr = fmt.Errorf("query log args = %#v, want 4", args)
			} else if args[0].Value != "alpha" {
				checkErr = fmt.Errorf("logged query = %#v, want alpha", args[0].Value)
			} else if terms, ok := args[1].Value.([]string); !ok || !reflect.DeepEqual(terms, []string{"alpha"}) {
				checkErr = fmt.Errorf("logged terms = %#v, want [alpha]", args[1].Value)
			} else if args[2].Value != uint32(1) {
				checkErr = fmt.Errorf("logged result count = %#v, want 1", args[2].Value)
			} else if _, ok := args[3].Value.(uint64); !ok {
				checkErr = fmt.Errorf("logged duration = %#v, want uint64", args[3].Value)
			}
			logged <- checkErr
			return driver.RowsAffected(0), logErr
		},
	})
	defer db.Close()
	defer stub.assertDone(t)

	s := NewSearcher(db, discardLogger, 25, 0)
	results, err := s.Search(context.Background(), SearchQuery{Query: "alpha", Limit: 5})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(results) != 1 || results[0].EventUID != "evt-alpha" {
		t.Fatalf("results = %#v, want search result despite query log failure", results)
	}
	select {
	case err := <-logged:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async query log attempt")
	}
}

func TestSearchCountsDroppedQueryLogsWhenLoggerQueueIsFull(t *testing.T) {
	db, stub := newSearchStubDB(t, nil, nil)
	defer db.Close()
	defer stub.assertDone(t)

	s := NewSearcher(db, discardLogger, 25, 0)
	for i := 0; i < cap(s.logSem); i++ {
		s.logSem <- struct{}{}
	}

	s.logQuery("alpha", []string{"alpha"}, 1, time.Millisecond)

	if got := s.DroppedQueryLogCount(); got != 1 {
		t.Fatalf("DroppedQueryLogCount = %d, want 1", got)
	}
	for i := 0; i < cap(s.logSem); i++ {
		<-s.logSem
	}
}

func TestScanResultsReturnsScanErrors(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	scanErr := errors.New("bad row")
	rows := &fakeResultRows{
		results: []SearchResult{
			{EventUID: "evt-good-1", SessionID: "session-1", EventKind: "message", Timestamp: now},
			{EventUID: "evt-bad", SessionID: "session-1", EventKind: "message", Timestamp: now},
			{EventUID: "evt-good-2", SessionID: "session-2", EventKind: "tool_call", Timestamp: now.Add(time.Minute)},
		},
		scanErrors: map[int]error{1: scanErr},
	}

	results, err := scanResults(rows)
	if !errors.Is(err, scanErr) {
		t.Fatalf("scanResults error = %v, want %v", err, scanErr)
	}
	if len(results) != 1 || results[0].EventUID != "evt-good-1" {
		t.Fatalf("results = %#v, want rows scanned before error", results)
	}
	if !strings.Contains(err.Error(), "scan search result") {
		t.Fatalf("scanResults error = %v, want context", err)
	}
}

func TestScanResultsReturnsRowsErr(t *testing.T) {
	iterErr := errors.New("iterator failed")
	rows := &fakeResultRows{
		results: []SearchResult{{EventUID: "evt-good", Timestamp: time.Now().UTC()}},
		err:     iterErr,
	}

	results, err := scanResults(rows)
	if !errors.Is(err, iterErr) {
		t.Fatalf("scanResults error = %v, want %v", err, iterErr)
	}
	if len(results) != 1 || results[0].EventUID != "evt-good" {
		t.Fatalf("results = %#v, want scanned row before iterator error", results)
	}
}

type fakeResultRows struct {
	results    []SearchResult
	scanErrors map[int]error
	err        error
	idx        int
}

func (f *fakeResultRows) Next() bool {
	if f.idx >= len(f.results) {
		return false
	}
	f.idx++
	return true
}

func (f *fakeResultRows) Scan(dest ...any) error {
	rowIndex := f.idx - 1
	if err := f.scanErrors[rowIndex]; err != nil {
		return err
	}
	row := f.results[rowIndex]
	values := []any{
		row.EventUID,
		row.SessionID,
		row.EventKind,
		row.TextPreview,
		row.Score,
		row.Timestamp,
		row.ToolName,
		row.Model,
		row.Provider,
	}
	for i, value := range values {
		switch ptr := dest[i].(type) {
		case *string:
			*ptr = value.(string)
		case *float64:
			*ptr = value.(float64)
		case *time.Time:
			*ptr = value.(time.Time)
		default:
			return errors.New("unexpected scan destination")
		}
	}
	return nil
}

func (f *fakeResultRows) Err() error {
	return f.err
}

type stubQuery func(query string, args []driver.NamedValue) (driver.Rows, error)
type stubExec func(query string, args []driver.NamedValue) (driver.Result, error)

type searchStubDB struct {
	mu      sync.Mutex
	queries []stubQuery
	execs   []stubExec
}

func newSearchStubDB(t *testing.T, queries []stubQuery, execs []stubExec) (*sql.DB, *searchStubDB) {
	t.Helper()
	stub := &searchStubDB{queries: queries, execs: execs}
	db := sql.OpenDB(searchStubConnector{stub: stub})
	return db, stub
}

func (s *searchStubDB) assertDone(t *testing.T) {
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

type searchStubConnector struct {
	stub *searchStubDB
}

func (c searchStubConnector) Connect(context.Context) (driver.Conn, error) {
	return searchStubConn(c), nil
}

func (c searchStubConnector) Driver() driver.Driver {
	return searchStubDriver{}
}

type searchStubDriver struct{}

func (searchStubDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use searchStubConnector")
}

type searchStubConn struct {
	stub *searchStubDB
}

func (c searchStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}

func (c searchStubConn) Close() error {
	return nil
}

func (c searchStubConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions unsupported")
}

func (c searchStubConn) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (c searchStubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.stub.mu.Lock()
	defer c.stub.mu.Unlock()
	if len(c.stub.queries) == 0 {
		return nil, fmt.Errorf("unexpected query: %s args=%#v", query, namedValues(args))
	}
	next := c.stub.queries[0]
	c.stub.queries = c.stub.queries[1:]
	return next(query, args)
}

func (c searchStubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.stub.mu.Lock()
	defer c.stub.mu.Unlock()
	if len(c.stub.execs) == 0 {
		return nil, fmt.Errorf("unexpected exec: %s args=%#v", query, namedValues(args))
	}
	next := c.stub.execs[0]
	c.stub.execs = c.stub.execs[1:]
	return next(query, args)
}

type driverRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func newDriverRows(columns []string, rows ...[]driver.Value) *driverRows {
	return &driverRows{columns: columns, rows: rows}
}

func searchResultDriverRows(results []SearchResult) *driverRows {
	rows := make([][]driver.Value, 0, len(results))
	for _, result := range results {
		rows = append(rows, []driver.Value{
			result.EventUID,
			result.SessionID,
			result.EventKind,
			result.TextPreview,
			result.Score,
			result.Timestamp,
			result.ToolName,
			result.Model,
			result.Provider,
		})
	}
	return newDriverRows(
		[]string{"event_uid", "session_id", "event_kind", "text_preview", "score", "timestamp", "tool_name", "model", "provider"},
		rows...,
	)
}

func (r *driverRows) Columns() []string {
	return r.columns
}

func (r *driverRows) Close() error {
	return nil
}

func (r *driverRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

func assertSQLContains(t *testing.T, query string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query missing %q:\n%s", fragment, query)
		}
	}
}

func assertNamedValues(t *testing.T, args []driver.NamedValue, want []any) {
	t.Helper()
	got := namedValues(args)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func namedValues(args []driver.NamedValue) []any {
	values := make([]any, 0, len(args))
	for _, arg := range args {
		values = append(values, arg.Value)
	}
	return values
}
