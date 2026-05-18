package search

import (
	"strings"
	"testing"
	"time"
)

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
}

func TestUniqPreservesFirstOccurrence(t *testing.T) {
	got := uniq([]string{"dashboard", "search", "dashboard", "tools", "search"})
	want := []string{"dashboard", "search", "tools"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("uniq = %#v, want %#v", got, want)
	}
}
