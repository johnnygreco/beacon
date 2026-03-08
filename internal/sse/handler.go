package sse

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// drainAndFlush writes msg and any queued messages, then flushes once.
// Returns false if the channel was closed.
func drainAndFlush(w http.ResponseWriter, flusher http.Flusher, ch <-chan SSEMessage, msg SSEMessage) bool {
	w.Write(msg.Formatted)
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return false
			}
			w.Write(msg.Formatted)
		default:
			flusher.Flush()
			return true
		}
	}
}

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
	w.Write(FormatSSE("connected", []byte("{}")))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-sub.Chan():
			if !ok {
				return
			}
			if !drainAndFlush(w, flusher, sub.Chan(), msg) {
				return
			}
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

	w.Write(FormatSSE("connected", []byte("{}")))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-sub.Chan():
			if !ok {
				return
			}
			if !drainAndFlush(w, flusher, sub.Chan(), msg) {
				return
			}
		}
	}
}
