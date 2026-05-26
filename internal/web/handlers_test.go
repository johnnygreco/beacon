package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSearchRedirectsToDashboardSearch(t *testing.T) {
	h := NewHandlers(nil, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), "")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/search", nil)

	h.Search(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if got := w.Header().Get("Location"); got != "/#dashboard-search" {
		t.Fatalf("Location = %q, want /#dashboard-search", got)
	}
}
