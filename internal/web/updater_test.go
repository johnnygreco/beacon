package web

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"sort"
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
	return readSSEWithin(t, sub, 100*time.Millisecond)
}

func readSSEWithin(t *testing.T, sub *sse.Subscriber, timeout time.Duration) sse.SSEMessage {
	t.Helper()
	select {
	case msg := <-sub.Chan():
		return msg
	case <-time.After(timeout):
		t.Fatal("timed out waiting for SSE message")
		return sse.SSEMessage{}
	}
}

func expectNoSSE(t *testing.T, sub *sse.Subscriber, wait time.Duration) {
	t.Helper()
	select {
	case msg := <-sub.Chan():
		t.Fatalf("unexpected SSE event: %s", msg.Event)
	case <-time.After(wait):
	}
}

func readEvents(t *testing.T, sub *sse.Subscriber, count int) []string {
	t.Helper()
	events := make([]string, 0, count)
	for range count {
		events = append(events, readSSEWithin(t, sub, 500*time.Millisecond).Event)
	}
	return events
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

	wantDashboardEvents := []string{
		"active-sessions-update",
		"completed-sessions-update",
		"activity-update",
		"dashboard-charts-update",
	}
	if got := readEvents(t, dashboard, len(wantDashboardEvents)); !reflect.DeepEqual(got, wantDashboardEvents) {
		t.Fatalf("dashboard events = %#v, want %#v", got, wantDashboardEvents)
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

func TestUpdaterRunSendsActiveImmediatelyAndDebouncesDashboardPanels(t *testing.T) {
	updater, broker := testUpdater()
	dashboard := broker.Subscribe("dashboard")
	session := broker.Subscribe("session:abc")
	defer broker.Unsubscribe(dashboard)
	defer broker.Unsubscribe(session)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go updater.Run(ctx)

	updater.MarkDirty([]string{"abc"})

	if msg := readSSEWithin(t, dashboard, 100*time.Millisecond); msg.Event != "active-sessions-update" {
		t.Fatalf("first dashboard event = %q, want active-sessions-update", msg.Event)
	}
	expectNoSSE(t, dashboard, 75*time.Millisecond)

	wantDashboardEvents := []string{
		"completed-sessions-update",
		"activity-update",
		"dashboard-charts-update",
	}
	if got := readEvents(t, dashboard, len(wantDashboardEvents)); !reflect.DeepEqual(got, wantDashboardEvents) {
		t.Fatalf("debounced dashboard events = %#v, want %#v", got, wantDashboardEvents)
	}
	if msg := readSSEWithin(t, session, 100*time.Millisecond); msg.Event != "conversation-update" {
		t.Fatalf("session event = %q, want conversation-update", msg.Event)
	}
}

func TestUpdaterRunKeepsActivePromptWhileCoalescingHeavierBurst(t *testing.T) {
	updater, broker := testUpdater()
	dashboard := broker.Subscribe("dashboard")
	sessionA := broker.Subscribe("session:a")
	sessionB := broker.Subscribe("session:b")
	defer broker.Unsubscribe(dashboard)
	defer broker.Unsubscribe(sessionA)
	defer broker.Unsubscribe(sessionB)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go updater.Run(ctx)

	updater.MarkDirty([]string{"a"})
	if msg := readSSEWithin(t, dashboard, 100*time.Millisecond); msg.Event != "active-sessions-update" {
		t.Fatalf("first dashboard event = %q, want active-sessions-update", msg.Event)
	}

	updater.MarkDirty([]string{"b"})
	if msg := readSSEWithin(t, dashboard, 100*time.Millisecond); msg.Event != "active-sessions-update" {
		t.Fatalf("second dashboard event = %q, want active-sessions-update", msg.Event)
	}

	expectNoSSE(t, dashboard, 75*time.Millisecond)
	wantDashboardEvents := []string{
		"completed-sessions-update",
		"activity-update",
		"dashboard-charts-update",
	}
	if got := readEvents(t, dashboard, len(wantDashboardEvents)); !reflect.DeepEqual(got, wantDashboardEvents) {
		t.Fatalf("debounced dashboard events = %#v, want %#v", got, wantDashboardEvents)
	}
	expectNoSSE(t, dashboard, 150*time.Millisecond)

	if msg := readSSEWithin(t, sessionA, 100*time.Millisecond); msg.Event != "conversation-update" {
		t.Fatalf("session a event = %q, want conversation-update", msg.Event)
	}
	if msg := readSSEWithin(t, sessionB, 100*time.Millisecond); msg.Event != "conversation-update" {
		t.Fatalf("session b event = %q, want conversation-update", msg.Event)
	}
}

func TestUpdaterCoalescesBurstDirtySignalsWithoutDroppingSessionIDs(t *testing.T) {
	updater, _ := testUpdater()
	wantSessions := make([]string, 0, 10)

	for i := range 10 {
		sessionID := "session-" + string(rune('a'+i))
		wantSessions = append(wantSessions, sessionID)
		updater.MarkDirty([]string{sessionID})
	}

	if got := updater.CoalescedSignalCount(); got != 9 {
		t.Fatalf("CoalescedSignalCount = %d, want 9", got)
	}

	gotSessions := updater.drainPendingSessions()
	sort.Strings(gotSessions)
	sort.Strings(wantSessions)
	if !reflect.DeepEqual(gotSessions, wantSessions) {
		t.Fatalf("pending sessions = %#v, want %#v", gotSessions, wantSessions)
	}
}
