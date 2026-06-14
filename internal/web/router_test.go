package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestStaticFilesDoNotServeDirectoriesOrHiddenFiles(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{
			".gitkeep":  &fstest.MapFile{Data: []byte{}},
			"js/app.js": &fstest.MapFile{Data: []byte("window.app = true;")},
		},
		nil,
		nil,
		nil,
	)

	for _, path := range []string{"/static/", "/static/js/", "/static/.gitkeep"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

func TestRouterSetsSecurityHeaders(t *testing.T) {
	router := NewRouter(
		fstest.MapFS{
			"js/prelude.js": &fstest.MapFile{Data: []byte("window.beacon = true;")},
		},
		nil,
		nil,
		nil,
	)
	req := httptest.NewRequest(http.MethodGet, "/static/js/prelude.js?v=test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP = %q, want self-only scripts", csp)
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP allows inline scripts: %q", csp)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "same-origin",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
}
