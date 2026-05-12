package web

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/sse"
)

func testUpdater() (*Updater, *sse.Broker) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	broker := sse.NewBroker(16, logger)
	return NewUpdater(broker, logger), broker
}

func readSSE(t *testing.T, sub *sse.Subscriber) sse.SSEMessage {
	t.Helper()
	select {
	case msg := <-sub.Chan():
		return msg
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for SSE message")
		return sse.SSEMessage{}
	}
}

func TestUpdaterNotifyChangesBroadcastsDashboardAndTranscriptInvalidations(t *testing.T) {
	updater, broker := testUpdater()
	dashboard := broker.Subscribe("dashboard")
	session := broker.Subscribe("session:abc")
	otherSession := broker.Subscribe("session:other")
	defer broker.Unsubscribe(dashboard)
	defer broker.Unsubscribe(session)
	defer broker.Unsubscribe(otherSession)

	updater.NotifyChanges([]string{"abc", "abc", ""})

	if msg := readSSE(t, dashboard); msg.Event != "active-sessions-update" {
		t.Fatalf("dashboard event = %q, want active-sessions-update", msg.Event)
	}
	if msg := readSSE(t, session); msg.Event != "conversation-update" {
		t.Fatalf("session event = %q, want conversation-update", msg.Event)
	}

	select {
	case msg := <-otherSession.Chan():
		t.Fatalf("unexpected other-session event: %s", msg.Event)
	case <-time.After(50 * time.Millisecond):
	}
}
