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

type fakeLegacySearcher struct {
	query   string
	limit   int
	results []search.SearchResult
	err     error
}

func (f *fakeLegacySearcher) LegacySearch(_ context.Context, query string, limit int) ([]search.SearchResult, error) {
	f.query = query
	f.limit = limit
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

func TestCompletedSessionEventSearchSessionIDs_UsesLegacySearchForMultiTokenQuery(t *testing.T) {
	fake := &fakeLegacySearcher{
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
	if fake.query != "dashboard payload" {
		t.Fatalf("legacy search query = %q, want dashboard payload", fake.query)
	}
	if fake.limit != completedSessionEventSearchLimit {
		t.Fatalf("legacy search limit = %d, want %d", fake.limit, completedSessionEventSearchLimit)
	}
	expected := []string{"session-older-001", "session-completed-002"}
	if fmt.Sprint(ids) != fmt.Sprint(expected) {
		t.Fatalf("ids = %#v, want %#v", ids, expected)
	}
}

func TestCompletedSessionEventSearchSessionIDs_SkipsBlankQueryAndMissingSearcher(t *testing.T) {
	fake := &fakeLegacySearcher{}
	handlers := &APIHandlers{searcher: fake}
	ids, err := handlers.completedSessionEventSearchSessionIDs(t.Context(), "   ")
	if err != nil {
		t.Fatalf("blank query error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("blank query ids = %#v, want none", ids)
	}
	if fake.query != "" {
		t.Fatalf("blank query should not call searcher, got query %q", fake.query)
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
