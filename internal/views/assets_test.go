package views

import (
	"strings"
	"testing"
)

func TestAssetURLAppendsBuildVersion(t *testing.T) {
	got := AssetURL("/static/js/dashboard/render.js")
	if !strings.HasPrefix(got, "/static/js/dashboard/render.js?v=") {
		t.Fatalf("asset url = %q, want versioned static URL", got)
	}
}

func TestAssetURLUsesAmpersandForExistingQuery(t *testing.T) {
	got := AssetURL("/static/js/dashboard/render.js?debug=1")
	if !strings.Contains(got, "?debug=1&v=") {
		t.Fatalf("asset url = %q, want version appended with ampersand", got)
	}
}
