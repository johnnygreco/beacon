package sse

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormatSSE_SingleLine(t *testing.T) {
	result := string(FormatSSE("test", []byte("hello")))
	expected := "event: test\ndata: hello\n\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFormatSSE_MultiLine(t *testing.T) {
	result := string(FormatSSE("update", []byte("line1\nline2\nline3")))
	expected := "event: update\ndata: line1\ndata: line2\ndata: line3\n\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestFormatSSE_NoEvent(t *testing.T) {
	result := string(FormatSSE("", []byte("data only")))
	expected := "data: data only\n\n"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestDashboardHandler_StreamingNotSupported(t *testing.T) {
	b := newTestBroker()

	// httptest.ResponseRecorder implements Flusher, so we need a custom writer
	// that does NOT implement Flusher
	handler := http.HandlerFunc(b.DashboardHandler)
	req := httptest.NewRequest("GET", "/sse/dashboard", nil)
	w := &nonFlushableWriter{httptest.NewRecorder()}

	handler.ServeHTTP(w, req)

	if w.rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.rec.Code)
	}
}

type nonFlushableWriter struct {
	rec *httptest.ResponseRecorder
}

func (w *nonFlushableWriter) Header() http.Header         { return w.rec.Header() }
func (w *nonFlushableWriter) Write(b []byte) (int, error) { return w.rec.Write(b) }
func (w *nonFlushableWriter) WriteHeader(code int)        { w.rec.WriteHeader(code) }
