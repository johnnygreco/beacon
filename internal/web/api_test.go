package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

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
