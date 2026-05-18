package partials

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/johnnygreco/beacon/internal/views"
)

func renderToString(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestSearchResultsWrapperRendersNonEmptyResults(t *testing.T) {
	html := renderToString(t, SearchResults([]views.SearchResult{
		{
			EventUID:  "event-1",
			SessionID: "session-1",
			EventKind: "message",
			Snippet:   "dashboard payload",
		},
	}))

	if strings.Contains(html, `data-search-state="idle"`) {
		t.Fatalf("wrapper rendered idle state for nonempty results: %s", html)
	}
	for _, expected := range []string{
		`href="/sessions/session-1#event-1"`,
		`dashboard payload`,
		`Message`,
		`1 shown`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("wrapper response missing %q: %s", expected, html)
		}
	}
}
