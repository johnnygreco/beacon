package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/johnnygreco/beacon/internal/search"
)

type fakeAPISearcher struct {
	query       search.SearchQuery
	searchCalls int
	results     []search.SearchResult
	err         error
}

func (f *fakeAPISearcher) Search(_ context.Context, query search.SearchQuery) ([]search.SearchResult, error) {
	f.searchCalls++
	f.query = query
	return f.results, f.err
}

func testAPIHandlers() *APIHandlers {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &APIHandlers{logger: logger}
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
