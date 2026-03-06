package sse

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteSSEMessage_SingleLine(t *testing.T) {
	var buf bytes.Buffer
	w := httptest.NewRecorder()

	msg := SSEMessage{Event: "test", Data: []byte("hello")}
	writeSSEMessage(w, msg)

	buf.Write(w.Body.Bytes())
	expected := "event: test\ndata: hello\n\n"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestWriteSSEMessage_MultiLine(t *testing.T) {
	w := httptest.NewRecorder()

	msg := SSEMessage{Event: "update", Data: []byte("line1\nline2\nline3")}
	writeSSEMessage(w, msg)

	expected := "event: update\ndata: line1\ndata: line2\ndata: line3\n\n"
	if w.Body.String() != expected {
		t.Errorf("expected %q, got %q", expected, w.Body.String())
	}
}

func TestWriteSSEMessage_NoEvent(t *testing.T) {
	w := httptest.NewRecorder()

	msg := SSEMessage{Data: []byte("data only")}
	writeSSEMessage(w, msg)

	expected := "data: data only\n\n"
	if w.Body.String() != expected {
		t.Errorf("expected %q, got %q", expected, w.Body.String())
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
func (w *nonFlushableWriter) Write(b []byte) (int, error)  { return w.rec.Write(b) }
func (w *nonFlushableWriter) WriteHeader(code int)         { w.rec.WriteHeader(code) }
