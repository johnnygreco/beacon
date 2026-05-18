//go:build e2e

package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	beacon "github.com/johnnygreco/beacon"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/sse"
	"github.com/johnnygreco/beacon/internal/web"
)

const searchSessionID = "session-search-001"

type searchFixtureBackend struct {
	now time.Time
}

func (b searchFixtureBackend) Search(_ context.Context, q search.SearchQuery) ([]search.SearchResult, error) {
	return b.results(q), nil
}

func (b searchFixtureBackend) Browse(_ context.Context, q search.SearchQuery) ([]search.SearchResult, error) {
	return b.results(q), nil
}

func (b searchFixtureBackend) results(q search.SearchQuery) []search.SearchResult {
	source := b.baseResults()
	if strings.EqualFold(strings.TrimSpace(q.Query), "many") {
		source = b.manyResults()
	}

	filtered := make([]search.SearchResult, 0, len(source))
	for _, result := range source {
		if !matchesTextQuery(result, q.Query) {
			continue
		}
		if !matchesEventKinds(result, q.EventKinds) {
			continue
		}
		if q.SessionID != "" && !strings.HasPrefix(strings.ToLower(result.SessionID), strings.ToLower(q.SessionID)) {
			continue
		}
		if !q.FromTime.IsZero() && result.Timestamp.Before(q.FromTime) {
			continue
		}
		if !q.ToTime.IsZero() && result.Timestamp.After(q.ToTime) {
			continue
		}
		filtered = append(filtered, result)
	}

	switch q.SortBy {
	case "newest":
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Timestamp.After(filtered[j].Timestamp)
		})
	case "oldest":
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Timestamp.Before(filtered[j].Timestamp)
		})
	default:
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Score > filtered[j].Score
		})
	}

	if q.Limit > 0 && len(filtered) > q.Limit {
		return filtered[:q.Limit]
	}
	return filtered
}

func (b searchFixtureBackend) baseResults() []search.SearchResult {
	return []search.SearchResult{
		{
			EventUID:    "event-search-001",
			SessionID:   searchSessionID,
			EventKind:   "message",
			TextPreview: "Dashboard payload search surfaced the exact migration note inside the assistant response.",
			Provider:    "anthropic",
			Model:       "claude-sonnet-4",
			Score:       3.18,
			Timestamp:   b.now.Add(-48 * time.Hour),
		},
		{
			EventUID:    "event-search-002",
			SessionID:   searchSessionID,
			EventKind:   "tool_call",
			TextPreview: `Read: {"file_path":"internal/views/pages/search.templ"}`,
			Provider:    "openai",
			Model:       "gpt-5.4-codex",
			ToolName:    "Read",
			Score:       2.44,
			Timestamp:   b.now.Add(-47 * time.Hour),
		},
		{
			EventUID:    "event-search-003",
			SessionID:   "session-search-002",
			EventKind:   "error",
			TextPreview: "Recoverable search timeout while loading a large result set.",
			Provider:    "openai",
			Model:       "gpt-5.4-codex",
			Score:       1.72,
			Timestamp:   b.now.Add(-46 * time.Hour),
		},
	}
}

func (b searchFixtureBackend) manyResults() []search.SearchResult {
	base := b.baseResults()
	results := make([]search.SearchResult, 0, 35)
	for i := 0; i < 35; i++ {
		result := base[i%len(base)]
		result.EventUID = fmt.Sprintf("event-many-%03d", i+1)
		if i%2 == 0 {
			result.SessionID = searchSessionID
		} else {
			result.SessionID = "session-search-many"
		}
		result.TextPreview = fmt.Sprintf("Many-result fixture item %d for pagination and visual density checks.", i+1)
		result.Score = 3 - float64(i)*0.03
		result.Timestamp = b.now.Add(-48*time.Hour - time.Duration(i)*time.Minute)
		results = append(results, result)
	}
	return results
}

func matchesTextQuery(result search.SearchResult, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || query == "many" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		result.TextPreview,
		result.EventKind,
		result.ToolName,
		result.SessionID,
		result.Model,
		result.Provider,
	}, " "))
	for _, token := range strings.Fields(query) {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func matchesEventKinds(result search.SearchResult, eventKinds []string) bool {
	if len(eventKinds) == 0 {
		return true
	}
	for _, eventKind := range eventKinds {
		if result.EventKind == eventKind {
			return true
		}
	}
	return false
}

func main() {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	staticFS, err := fs.Sub(beacon.StaticFS, "static")
	if err != nil {
		log.Fatalf("prepare static fs: %v", err)
	}

	handlers := web.NewHandlersForE2E(nil, searchFixtureBackend{now: time.Now()}, logger)
	apiHandlers := web.NewAPIHandlers(nil, nil, logger)
	router := web.NewRouter(staticFS, sse.NewBroker(16, logger), handlers, apiHandlers)

	addr := os.Getenv("BEACON_E2E_ADDR")
	if addr == "" {
		addr = "127.0.0.1:4610"
	}
	log.Printf("e2e server listening on http://%s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
