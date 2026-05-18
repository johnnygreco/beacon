package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
)

type fakeSearchBackend struct {
	searchCalls   int
	browseCalls   int
	lastSearch    search.SearchQuery
	lastBrowse    search.SearchQuery
	searchResults []search.SearchResult
	browseResults []search.SearchResult
	err           error
}

func (f *fakeSearchBackend) Search(_ context.Context, q search.SearchQuery) ([]search.SearchResult, error) {
	f.searchCalls++
	f.lastSearch = q
	return f.searchResults, f.err
}

func (f *fakeSearchBackend) Browse(_ context.Context, q search.SearchQuery) ([]search.SearchResult, error) {
	f.browseCalls++
	f.lastBrowse = q
	return f.browseResults, f.err
}

func testPageHandlers(searcher searchBackend) *Handlers {
	return &Handlers{
		searcher: searcher,
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func TestSearchResults_BlankQueryAndFiltersShowsIdleState(t *testing.T) {
	fake := &fakeSearchBackend{}
	h := testPageHandlers(fake)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/search/results?q=%20%20", nil)
	h.SearchResults(w, r)

	if fake.searchCalls != 0 || fake.browseCalls != 0 {
		t.Fatalf("blank request should not query backend, search=%d browse=%d", fake.searchCalls, fake.browseCalls)
	}
	if !strings.Contains(w.Body.String(), `data-search-state="idle"`) {
		t.Fatalf("idle response missing state: %s", w.Body.String())
	}
}

func TestSearchResults_TextQueryUsesSearchAndRendersEventLinks(t *testing.T) {
	ts := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	fake := &fakeSearchBackend{
		searchResults: []search.SearchResult{
			{
				EventUID:    "event-1",
				SessionID:   "session-1",
				EventKind:   "tool_call",
				TextPreview: `Read: {"file_path":"internal/views/pages/search.templ"}`,
				ToolName:    "Read",
				Model:       "claude-sonnet-4",
				Provider:    "anthropic",
				Score:       2.75,
				Timestamp:   ts,
			},
		},
	}
	h := testPageHandlers(fake)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/search/results?q=%20dashboard%20payload%20&event_kind=tool_call&session_id=session&sort=newest&limit=60&range=1h", nil)
	h.SearchResults(w, r)

	if fake.searchCalls != 1 || fake.browseCalls != 0 {
		t.Fatalf("calls search=%d browse=%d, want search only", fake.searchCalls, fake.browseCalls)
	}
	q := fake.lastSearch
	if q.Query != "dashboard payload" || q.Limit != 61 || q.SessionID != "session" || q.SortBy != "newest" {
		t.Fatalf("unexpected search query: %#v", q)
	}
	if len(q.EventKinds) != 1 || q.EventKinds[0] != "tool_call" {
		t.Fatalf("EventKinds = %#v, want tool_call", q.EventKinds)
	}
	if q.FromTime.IsZero() {
		t.Fatal("range filter should set FromTime")
	}

	body := w.Body.String()
	for _, expected := range []string{
		`href="/sessions/session-1#event-1"`,
		`internal/views/pages/search.templ`,
		`Tool call`,
		`Claude Code`,
		`sonnet-4`,
		`1 shown`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %q: %s", expected, body)
		}
	}
}

func TestSearchResults_FilterOnlyUsesBrowseAndDetectsMoreResults(t *testing.T) {
	fake := &fakeSearchBackend{
		browseResults: []search.SearchResult{
			{EventUID: "event-a", SessionID: "session-a", EventKind: "error", TextPreview: "first error"},
			{EventUID: "event-b", SessionID: "session-b", EventKind: "error", TextPreview: "second error"},
		},
	}
	h := testPageHandlers(fake)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/search/results?event_kind=error&limit=1", nil)
	h.SearchResults(w, r)

	if fake.searchCalls != 0 || fake.browseCalls != 1 {
		t.Fatalf("calls search=%d browse=%d, want browse only", fake.searchCalls, fake.browseCalls)
	}
	if fake.lastBrowse.Limit != 2 {
		t.Fatalf("browse limit = %d, want requested + 1", fake.lastBrowse.Limit)
	}
	body := w.Body.String()
	if strings.Contains(body, "second error") {
		t.Fatalf("response should trim extra result used for hasMore: %s", body)
	}
	if !strings.Contains(body, "Show more") || !strings.Contains(body, `data-event-kind="error"`) {
		t.Fatalf("response missing pagination/error result: %s", body)
	}
}

func TestSearchResults_BackendErrorAndUnavailableStates(t *testing.T) {
	fake := &fakeSearchBackend{err: errors.New("boom")}
	h := testPageHandlers(fake)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/search/results?q=dashboard", nil)
	h.SearchResults(w, r)
	if !strings.Contains(w.Body.String(), `data-search-state="error"`) {
		t.Fatalf("error response missing state: %s", w.Body.String())
	}

	h = testPageHandlers(nil)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/search/results?q=dashboard", nil)
	h.SearchResults(w, r)
	if !strings.Contains(w.Body.String(), `data-search-state="unavailable"`) {
		t.Fatalf("unavailable response missing state: %s", w.Body.String())
	}
}

func TestNewHandlers_NilSearcherRendersUnavailable(t *testing.T) {
	h := NewHandlers(nil, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if h.searcher != nil {
		t.Fatalf("NewHandlers stored a typed nil searcher: %#v", h.searcher)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/search/results?q=dashboard", nil)
	h.SearchResults(w, r)

	if !strings.Contains(w.Body.String(), `data-search-state="unavailable"`) {
		t.Fatalf("unavailable response missing state: %s", w.Body.String())
	}
}
