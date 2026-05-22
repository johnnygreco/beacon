package web

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/views"
)

type fakeAPISearcher struct {
	query         search.SearchQuery
	searchCalls   int
	browseCalls   int
	results       []search.SearchResult
	browseResults []search.SearchResult
	err           error
}

func (f *fakeAPISearcher) Search(_ context.Context, query search.SearchQuery) ([]search.SearchResult, error) {
	f.searchCalls++
	f.query = query
	return f.results, f.err
}

func (f *fakeAPISearcher) Browse(_ context.Context, query search.SearchQuery) ([]search.SearchResult, error) {
	f.browseCalls++
	f.query = query
	if f.browseResults != nil {
		return f.browseResults, f.err
	}
	return f.results, f.err
}

func testAPIHandlers() *APIHandlers {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return &APIHandlers{logger: logger}
}

func TestDashboardSearch_TextQueryUsesSearchAndFormatsResults(t *testing.T) {
	fake := &fakeAPISearcher{
		results: []search.SearchResult{
			{
				EventUID:    "event-1",
				SessionID:   "session-1",
				EventKind:   "tool_call",
				TextPreview: `Read: {"file_path":"internal/web/api.go"}`,
				ToolName:    "Read",
				Provider:    "anthropic",
				Model:       "claude-sonnet-4-6",
				Score:       2.5,
				Timestamp:   time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC),
			},
			{EventUID: "event-2", SessionID: "session-2", EventKind: "message"},
		},
	}
	handlers := &APIHandlers{searcher: fake, logger: testLogger()}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/dashboard/search?q=dashboard&event_kind=tool_call&session_id=session&sort=newest&limit=1&range=7d", nil)
	handlers.GetDashboardSearch(w, r)

	if fake.searchCalls != 1 || fake.browseCalls != 0 {
		t.Fatalf("calls search=%d browse=%d, want search only", fake.searchCalls, fake.browseCalls)
	}
	if fake.query.Query != "dashboard" || fake.query.Limit != 2 || fake.query.SessionID != "session" || fake.query.SortBy != "newest" {
		t.Fatalf("unexpected query: %#v", fake.query)
	}
	if len(fake.query.EventKinds) != 1 || fake.query.EventKinds[0] != "tool_call" {
		t.Fatalf("event kinds = %#v, want tool_call", fake.query.EventKinds)
	}
	if fake.query.FromTime.IsZero() {
		t.Fatal("range should set FromTime")
	}

	var got APIDashboardSearchResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.State != "ready" || !got.HasMore || len(got.Items) != 1 {
		t.Fatalf("response state/has_more/items = %q/%v/%d, want ready/true/1", got.State, got.HasMore, len(got.Items))
	}
	if got.Items[0].Snippet != "internal/web/api.go" {
		t.Fatalf("snippet = %q, want formatted tool path", got.Items[0].Snippet)
	}
}

func TestDashboardSearch_FilterOnlyUsesBrowseAndExpandsErrors(t *testing.T) {
	fake := &fakeAPISearcher{
		browseResults: []search.SearchResult{{EventUID: "event-1", SessionID: "session-1", EventKind: "tool_error"}},
	}
	handlers := &APIHandlers{searcher: fake, logger: testLogger()}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/dashboard/search?event_kind=error", nil)
	handlers.GetDashboardSearch(w, r)

	if fake.searchCalls != 0 || fake.browseCalls != 1 {
		t.Fatalf("calls search=%d browse=%d, want browse only", fake.searchCalls, fake.browseCalls)
	}
	expected := []string{"error", "tool_error"}
	if fmt.Sprint(fake.query.EventKinds) != fmt.Sprint(expected) {
		t.Fatalf("event kinds = %#v, want %#v", fake.query.EventKinds, expected)
	}
	var got APIDashboardSearchResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.State != "ready" || len(got.Items) != 1 {
		t.Fatalf("response state/items = %q/%d, want ready/1", got.State, len(got.Items))
	}
}

func TestDashboardSearchSessionResultFormatsMetadataMatch(t *testing.T) {
	session := views.SessionSummary{
		ID:          "session-meta-001",
		Provider:    "openai",
		ActiveModel: "gpt-5.4-codex",
		WorkingDir:  "/Users/example/projects/beacon",
		EndedAt:     time.Date(2026, 5, 22, 14, 0, 0, 0, time.UTC),
	}

	got := dashboardSearchSessionResult(session)
	if got.ResultType != "session" || got.EventKind != "session" || got.EventUID != "" {
		t.Fatalf("result type/kind/event = %q/%q/%q, want session/session/empty", got.ResultType, got.EventKind, got.EventUID)
	}
	if got.SessionID != session.ID || got.Provider != session.Provider || got.Model != session.ActiveModel {
		t.Fatalf("metadata = %#v, want session metadata", got)
	}
	for _, want := range []string{"beacon", "gpt-5.4-codex", "openai"} {
		if !strings.Contains(got.Snippet, want) {
			t.Fatalf("snippet = %q, want to contain %q", got.Snippet, want)
		}
	}
}

func TestDashboardSearchMetadataSort(t *testing.T) {
	tests := []struct {
		sortBy  string
		wantKey string
		wantAsc bool
	}{
		{sortBy: "oldest", wantKey: "ended", wantAsc: true},
		{sortBy: "newest", wantKey: "ended", wantAsc: false},
		{sortBy: "relevance", wantKey: "ended", wantAsc: false},
		{sortBy: "", wantKey: "ended", wantAsc: false},
	}

	for _, tt := range tests {
		t.Run(tt.sortBy, func(t *testing.T) {
			key, asc := dashboardSearchMetadataSort(tt.sortBy)
			if key != tt.wantKey || asc != tt.wantAsc {
				t.Fatalf("dashboardSearchMetadataSort(%q) = %q/%v, want %q/%v", tt.sortBy, key, asc, tt.wantKey, tt.wantAsc)
			}
		})
	}
}

func TestDashboardSortSearchItemsOrdersMixedResultsByTime(t *testing.T) {
	older := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	items := []APIDashboardSearchResult{
		{ResultType: "event", SessionID: "event-session", Timestamp: older},
		{ResultType: "session", SessionID: "metadata-session", Timestamp: newer},
	}

	dashboardSortSearchItems(items, "newest")
	if items[0].SessionID != "metadata-session" {
		t.Fatalf("newest sort first = %q, want metadata-session", items[0].SessionID)
	}

	dashboardSortSearchItems(items, "oldest")
	if items[0].SessionID != "event-session" {
		t.Fatalf("oldest sort first = %q, want event-session", items[0].SessionID)
	}

	dashboardSortSearchItems(items, "relevance")
	if items[0].SessionID != "event-session" {
		t.Fatalf("relevance sort should preserve existing order, first = %q", items[0].SessionID)
	}
}

func TestDashboardSearch_IdleAndUnavailableStates(t *testing.T) {
	fake := &fakeAPISearcher{}
	handlers := &APIHandlers{searcher: fake, logger: testLogger()}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/dashboard/search", nil)
	handlers.GetDashboardSearch(w, r)
	if fake.searchCalls != 0 || fake.browseCalls != 0 {
		t.Fatalf("idle request should not query backend, search=%d browse=%d", fake.searchCalls, fake.browseCalls)
	}
	var got APIDashboardSearchResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode idle response: %v", err)
	}
	if got.State != "idle" {
		t.Fatalf("idle state = %q, want idle", got.State)
	}

	handlers = &APIHandlers{logger: testLogger()}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/dashboard/search?q=dashboard", nil)
	handlers.GetDashboardSearch(w, r)
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode unavailable response: %v", err)
	}
	if got.State != "unavailable" || w.Code != http.StatusOK {
		t.Fatalf("unavailable response state/status = %q/%d, want unavailable/200", got.State, w.Code)
	}
}

func TestAPIIntParamParsingIsBounded(t *testing.T) {
	spec := apiIntParam{Name: "limit", Default: 30, Min: 1, Max: 200}
	tests := []struct {
		name    string
		values  url.Values
		want    int
		wantErr string
	}{
		{name: "missing", values: url.Values{}, want: 30},
		{name: "blank", values: url.Values{"limit": {""}}, want: 30},
		{name: "valid", values: url.Values{"limit": {"75"}}, want: 75},
		{name: "below min", values: url.Values{"limit": {"0"}}, want: 30},
		{name: "above max", values: url.Values{"limit": {"500"}}, want: 200},
		{name: "malformed", values: url.Values{"limit": {"many"}}, wantErr: "invalid limit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAPIIntParam(tt.values, spec)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAPIIntParam error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseAPIIntParam = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAPIRequestParsingNormalizesFilters(t *testing.T) {
	sessions, err := parseDashboardSessionsAPIRequest(url.Values{
		"state":     {" active "},
		"range":     {" 7d "},
		"q":         {" dashboard "},
		"sort":      {" oldest "},
		"direction": {"ASC"},
		"offset":    {"-5"},
		"limit":     {"500"},
	})
	if err != nil {
		t.Fatalf("parseDashboardSessionsAPIRequest error: %v", err)
	}
	if sessions.State != "active" || sessions.Range != "7d" || sessions.Query != "dashboard" || sessions.SortKey != "oldest" || !sessions.SortAsc {
		t.Fatalf("normalized dashboard sessions request = %#v", sessions)
	}
	if sessions.Offset != 0 || sessions.Limit != maxDashboardSessionsAPILimit {
		t.Fatalf("dashboard sessions offset/limit = %d/%d, want 0/%d", sessions.Offset, sessions.Limit, maxDashboardSessionsAPILimit)
	}

	activity := parseActivityAPIRequest(url.Values{
		"range":      {""},
		"event_kind": {" tool_call,error ", "message"},
	})
	if activity.Since != nil {
		t.Fatalf("explicit blank activity range should mean all time, got %v", activity.Since)
	}
	expectedKinds := []string{"tool_call", "error", "message"}
	if fmt.Sprint(activity.EventKinds) != fmt.Sprint(expectedKinds) {
		t.Fatalf("activity event kinds = %#v, want %#v", activity.EventKinds, expectedKinds)
	}

	charts := parseDashboardChartsAPIRequest(url.Values{})
	if charts.Range != "24h" {
		t.Fatalf("default chart range = %q, want 24h", charts.Range)
	}
	charts = parseDashboardChartsAPIRequest(url.Values{"range": {""}})
	if charts.Range != "" {
		t.Fatalf("explicit blank chart range = %q, want all time", charts.Range)
	}
}

func TestDashboardSearchInvalidLimitReturnsBadRequest(t *testing.T) {
	fake := &fakeAPISearcher{}
	handlers := &APIHandlers{searcher: fake, logger: testLogger()}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/dashboard/search?q=dashboard&limit=many", nil)
	handlers.GetDashboardSearch(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if fake.searchCalls != 0 || fake.browseCalls != 0 {
		t.Fatalf("invalid request should not call backend, search=%d browse=%d", fake.searchCalls, fake.browseCalls)
	}
	assertAPIError(t, w.Body.String(), "invalid limit")
}

func TestSearchEventsBackendErrorIsSanitized(t *testing.T) {
	fake := &fakeAPISearcher{err: errors.New("raw backend failure: clickhouse credentials secret")}
	handlers := &APIHandlers{searcher: fake, logger: testLogger()}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/search?q=%20dashboard%20&limit=999", nil)
	handlers.SearchEvents(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if fake.query.Query != "dashboard" || fake.query.Limit != maxSearchEventsAPILimit {
		t.Fatalf("query = %#v, want trimmed query and capped limit", fake.query)
	}
	body := w.Body.String()
	assertAPIError(t, body, "search failed")
	if strings.Contains(body, "clickhouse credentials") {
		t.Fatalf("response leaked backend error: %s", body)
	}
}

func TestGetSessionsBackendErrorIsSanitized(t *testing.T) {
	db := newFailingAPIDB(t)
	handlers := &APIHandlers{db: db, logger: testLogger()}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	handlers.GetSessions(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	body := w.Body.String()
	assertAPIError(t, body, "failed to query sessions")
	if strings.Contains(body, "raw backend") || strings.Contains(body, "session_projection") {
		t.Fatalf("response leaked backend error: %s", body)
	}
}

func TestPointLookupBackendErrorsAreSanitized(t *testing.T) {
	db := newFailingAPIDB(t)
	handlers := &APIHandlers{db: db, logger: testLogger()}
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		target      string
		routeParams []string
		want        string
	}{
		{
			name:        "session detail",
			handler:     handlers.GetSessionDetail,
			target:      "/api/sessions/session-1",
			routeParams: []string{"id", "session-1"},
			want:        "failed to query session detail",
		},
		{
			name:        "event",
			handler:     handlers.GetEvent,
			target:      "/api/events/event-1",
			routeParams: []string{"event_id", "event-1"},
			want:        "failed to query event",
		},
		{
			name:        "tool payload",
			handler:     handlers.GetToolPayload,
			target:      "/api/tool-payloads/event-1",
			routeParams: []string{"event_id", "event-1"},
			want:        "failed to query tool payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handler(w, newAPIRequest(t, tt.target, tt.routeParams...))
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
			}
			body := w.Body.String()
			assertAPIError(t, body, tt.want)
			if strings.Contains(body, "raw backend") || strings.Contains(body, "session_projection") {
				t.Fatalf("response leaked backend error: %s", body)
			}
		})
	}
}

func TestJsonResponse(t *testing.T) {
	a := testAPIHandlers()
	w := httptest.NewRecorder()
	a.jsonResponse(w, map[string]string{"key": "value"})

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestJsonError(t *testing.T) {
	a := testAPIHandlers()
	w := httptest.NewRecorder()
	a.jsonError(w, "not found", http.StatusNotFound)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}
}

func TestCompletedSessionEventSearchSessionIDs_UsesSearchForMultiTokenQuery(t *testing.T) {
	fake := &fakeAPISearcher{
		results: []search.SearchResult{
			{SessionID: "session-older-001"},
			{SessionID: "session-older-001"},
			{SessionID: "session-completed-002"},
		},
	}
	handlers := &APIHandlers{searcher: fake}

	ids, err := handlers.completedSessionEventSearchSessionIDs(t.Context(), "dashboard payload")
	if err != nil {
		t.Fatalf("completedSessionEventSearchSessionIDs error: %v", err)
	}
	if fake.searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1", fake.searchCalls)
	}
	if fake.query.Query != "dashboard payload" {
		t.Fatalf("search query = %q, want dashboard payload", fake.query.Query)
	}
	if fake.query.Limit != completedSessionEventSearchLimit {
		t.Fatalf("search limit = %d, want %d", fake.query.Limit, completedSessionEventSearchLimit)
	}
	expected := []string{"session-older-001", "session-completed-002"}
	if fmt.Sprint(ids) != fmt.Sprint(expected) {
		t.Fatalf("ids = %#v, want %#v", ids, expected)
	}
}

func TestCompletedSessionEventSearchSessionIDs_SkipsBlankQueryAndMissingSearcher(t *testing.T) {
	fake := &fakeAPISearcher{}
	handlers := &APIHandlers{searcher: fake}
	ids, err := handlers.completedSessionEventSearchSessionIDs(t.Context(), "   ")
	if err != nil {
		t.Fatalf("blank query error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("blank query ids = %#v, want none", ids)
	}
	if fake.searchCalls != 0 {
		t.Fatalf("blank query should not call searcher, got %d calls", fake.searchCalls)
	}

	handlers = &APIHandlers{}
	ids, err = handlers.completedSessionEventSearchSessionIDs(t.Context(), "dashboard payload")
	if err != nil {
		t.Fatalf("missing searcher error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("missing searcher ids = %#v, want none", ids)
	}
}

func TestNewAPIHandlersWithNilSearcherSkipsEventSearch(t *testing.T) {
	handlers := NewAPIHandlers(nil, nil, testLogger())
	ids, err := handlers.completedSessionEventSearchSessionIDs(t.Context(), "dashboard payload")
	if err != nil {
		t.Fatalf("nil constructor searcher error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("nil constructor searcher ids = %#v, want none", ids)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func assertAPIError(t *testing.T, body, want string) {
	t.Helper()
	var got apiErrorResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode API error: %v\n%s", err, body)
	}
	if got.Error != want {
		t.Fatalf("error = %q, want %q", got.Error, want)
	}
}

func newAPIRequest(t *testing.T, target string, routeParams ...string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if len(routeParams) == 0 {
		return req
	}
	if len(routeParams)%2 != 0 {
		t.Fatalf("route params must be key/value pairs")
	}
	rctx := chi.NewRouteContext()
	for i := 0; i < len(routeParams); i += 2 {
		rctx.URLParams.Add(routeParams[i], routeParams[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

var registerFailingAPIDriver sync.Once

const failingAPIBackendError = "raw backend failure: session_projection secret"

type failingAPIDriver struct{}

func (failingAPIDriver) Open(string) (driver.Conn, error) {
	return failingAPIConn{}, nil
}

type failingAPIConn struct{}

func (failingAPIConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New(failingAPIBackendError)
}

func (failingAPIConn) Close() error {
	return nil
}

func (failingAPIConn) Begin() (driver.Tx, error) {
	return nil, errors.New(failingAPIBackendError)
}

func newFailingAPIDB(t *testing.T) *sql.DB {
	t.Helper()
	registerFailingAPIDriver.Do(func() {
		sql.Register("beacon_api_failing", failingAPIDriver{})
	})
	db, err := sql.Open("beacon_api_failing", "")
	if err != nil {
		t.Fatalf("open failing db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
