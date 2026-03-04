package sse

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DashboardHandler streams SSE events for the dashboard.
func (b *Broker) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sub := b.Subscribe("dashboard", "*")
	defer b.Unsubscribe(sub)

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-sub.Chan():
			if !ok {
				return
			}
			writeSSEMessage(w, msg)
			flusher.Flush()
		}
	}
}

// SessionHandler streams SSE events for a specific session.
func (b *Broker) SessionHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	topic := "session:" + sessionID
	sub := b.Subscribe(topic, "dashboard")
	defer b.Unsubscribe(sub)

	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-sub.Chan():
			if !ok {
				return
			}
			writeSSEMessage(w, msg)
			flusher.Flush()
		}
	}
}

// writeSSEMessage writes a properly formatted SSE message.
// Multi-line data is handled by prefixing each line with "data: ".
func writeSSEMessage(w http.ResponseWriter, msg SSEMessage) {
	if msg.Event != "" {
		fmt.Fprintf(w, "event: %s\n", msg.Event)
	}
	lines := bytes.Split(msg.Data, []byte("\n"))
	for _, line := range lines {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
}
