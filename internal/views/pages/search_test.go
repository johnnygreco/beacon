package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSearchPageRendersThemedSearchShell(t *testing.T) {
	var buf bytes.Buffer
	if err := Search(nil).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render search: %v", err)
	}
	html := buf.String()

	for _, expected := range []string{
		`document.body.setAttribute('data-page', 'search')`,
		`id="search-wrap"`,
		`href="/"`,
		`hx-get="/search/results"`,
		`hx-include="#search-filters"`,
		`aria-label="Event kind"`,
		`id="filter-limit"`,
		`value="30"`,
		`onclick="resetSearchFilters()"`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("search page missing %q", expected)
		}
	}
}
