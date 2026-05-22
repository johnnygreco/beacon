package sse

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func newTestBroker() *Broker {
	return NewBroker(16, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func TestBrokerSubscribeUnsubscribe(t *testing.T) {
	b := newTestBroker()

	if b.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers, got %d", b.SubscriberCount())
	}

	sub := b.Subscribe("dashboard")
	if b.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", b.SubscriberCount())
	}

	b.Unsubscribe(sub)
	if b.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe, got %d", b.SubscriberCount())
	}
}

func TestBrokerBroadcastToMatchingTopic(t *testing.T) {
	b := newTestBroker()
	sub := b.Subscribe("dashboard")
	defer b.Unsubscribe(sub)

	msg := SSEMessage{Event: "test", Data: []byte("hello")}
	b.Broadcast("dashboard", msg)

	select {
	case received := <-sub.Chan():
		if received.Event != "test" {
			t.Errorf("expected event 'test', got '%s'", received.Event)
		}
		if string(received.Data) != "hello" {
			t.Errorf("expected data 'hello', got '%s'", string(received.Data))
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for message")
	}
}

func TestBrokerBroadcastNoMatchingTopic(t *testing.T) {
	b := newTestBroker()
	sub := b.Subscribe("dashboard")
	defer b.Unsubscribe(sub)

	b.Broadcast("other-topic", SSEMessage{Event: "test", Data: []byte("data")})

	select {
	case <-sub.Chan():
		t.Error("should not receive message for non-matching topic")
	case <-time.After(50 * time.Millisecond):
		// Expected: no message
	}
}

func TestBrokerWildcardSubscription(t *testing.T) {
	b := newTestBroker()
	sub := b.Subscribe("*")
	defer b.Unsubscribe(sub)

	b.Broadcast("any-topic", SSEMessage{Event: "test", Data: []byte("data")})

	select {
	case received := <-sub.Chan():
		if received.Event != "test" {
			t.Errorf("expected event 'test', got '%s'", received.Event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("wildcard subscriber should receive all messages")
	}
}

func TestBrokerMultipleTopics(t *testing.T) {
	b := newTestBroker()
	sub := b.Subscribe("dashboard", "session:abc")
	defer b.Unsubscribe(sub)

	b.Broadcast("session:abc", SSEMessage{Event: "update", Data: []byte("session data")})

	select {
	case received := <-sub.Chan():
		if received.Event != "update" {
			t.Errorf("expected 'update', got '%s'", received.Event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("should receive message for subscribed topic")
	}
}

func TestBrokerMultipleSubscribers(t *testing.T) {
	b := newTestBroker()
	sub1 := b.Subscribe("dashboard")
	sub2 := b.Subscribe("dashboard")
	defer b.Unsubscribe(sub1)
	defer b.Unsubscribe(sub2)

	b.Broadcast("dashboard", SSEMessage{Event: "test", Data: []byte("data")})

	// Both should receive the message
	for _, sub := range []*Subscriber{sub1, sub2} {
		select {
		case <-sub.Chan():
		case <-time.After(100 * time.Millisecond):
			t.Error("subscriber should receive message")
		}
	}
}

func TestBrokerDropsMessageForSlowSubscriber(t *testing.T) {
	b := NewBroker(1, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	sub := b.Subscribe("dashboard")
	defer b.Unsubscribe(sub)

	// Fill the buffer
	b.Broadcast("dashboard", SSEMessage{Event: "1", Data: []byte("first")})

	// This should be dropped (buffer full, non-blocking)
	b.Broadcast("dashboard", SSEMessage{Event: "2", Data: []byte("second")})

	// We should only get the first message
	select {
	case msg := <-sub.Chan():
		if msg.Event != "1" {
			t.Errorf("expected first message, got '%s'", msg.Event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("should receive at least the first message")
	}
	if got := b.DroppedCount(); got != 1 {
		t.Fatalf("DroppedCount = %d, want 1", got)
	}
}

func TestBrokerCountsBurstDropsForSlowSubscriber(t *testing.T) {
	b := NewBroker(2, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	sub := b.Subscribe("dashboard")
	defer b.Unsubscribe(sub)

	for i := range 10 {
		b.Broadcast("dashboard", SSEMessage{Event: "burst", Data: []byte{byte(i)}})
	}

	if got := b.DroppedCount(); got != 8 {
		t.Fatalf("DroppedCount = %d, want 8", got)
	}
}

func TestBrokerNotify(t *testing.T) {
	b := newTestBroker()
	sub := b.Subscribe("dashboard")
	defer b.Unsubscribe(sub)

	b.Notify()

	select {
	case msg := <-sub.Chan():
		if msg.Event != "refresh" {
			t.Errorf("expected 'refresh', got '%s'", msg.Event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("should receive notify message")
	}
}

func TestSubscriberChanClosedOnUnsubscribe(t *testing.T) {
	b := newTestBroker()
	sub := b.Subscribe("dashboard")
	b.Unsubscribe(sub)

	_, ok := <-sub.Chan()
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}
