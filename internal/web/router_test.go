package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestStaticFilesDisableBrowserCaching(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{
			"js/dashboard/render.js": &fstest.MapFile{Data: []byte("window.renderDashboard = true;")},
		},
		nil,
		nil,
		nil,
	)
	req := httptest.NewRequest(http.MethodGet, "/static/js/dashboard/render.js?v=test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q", got)
	}
	if got := rec.Header().Get("Expires"); got != "0" {
		t.Fatalf("Expires = %q", got)
	}
}
