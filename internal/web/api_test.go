package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
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
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
