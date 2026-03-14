package sse

import (
	"bytes"
	"log/slog"
	"sync"
)

// SSEMessage is a named SSE event with data payload.
type SSEMessage struct {
	Event     string // SSE event name (e.g., "metrics-update")
	Data      []byte // SSE data payload (HTML or JSON)
	Formatted []byte // Pre-formatted SSE wire bytes
}

// FormatSSE pre-formats an SSE event+data into wire bytes.
func FormatSSE(event string, data []byte) []byte {
	// Pre-size: "event: " + event + "\n" + per-line "data: " prefix + data + "\n\n"
	buf := bytes.NewBuffer(make([]byte, 0, len(event)+len(data)+64))
	if event != "" {
		buf.WriteString("event: ")
		buf.WriteString(event)
		buf.WriteByte('\n')
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		buf.WriteString("data: ")
		buf.Write(line)
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// Subscriber is a channel that receives SSE messages.
type Subscriber struct {
	ch     chan SSEMessage
	topics map[string]bool // subscribed topics (e.g., "dashboard", "session:abc")
}

// Broker manages SSE subscribers and broadcasts events.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[*Subscriber]struct{}
	logger      *slog.Logger
	bufferSize  int
}

// NewBroker creates a new SSE broker.
func NewBroker(bufferSize int, logger *slog.Logger) *Broker {
	return &Broker{
		subscribers: make(map[*Subscriber]struct{}),
		logger:      logger,
		bufferSize:  bufferSize,
	}
}

// Subscribe creates a new subscriber for the given topics.
func (b *Broker) Subscribe(topics ...string) *Subscriber {
	topicMap := make(map[string]bool)
	for _, t := range topics {
		topicMap[t] = true
	}

	sub := &Subscriber{
		ch:     make(chan SSEMessage, b.bufferSize),
		topics: topicMap,
	}

	b.mu.Lock()
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	return sub
}

// Unsubscribe removes a subscriber.
func (b *Broker) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	delete(b.subscribers, sub)
	b.mu.Unlock()
	close(sub.ch)
}

// Chan returns the subscriber's event channel.
func (s *Subscriber) Chan() <-chan SSEMessage {
	return s.ch
}

// Broadcast sends a named SSE message to all subscribers matching the topic.
// It pre-formats the wire bytes once before distributing to subscribers.
func (b *Broker) Broadcast(topic string, msg SSEMessage) {
	if msg.Formatted == nil {
		msg.Formatted = FormatSSE(msg.Event, msg.Data)
	}

	b.logger.Debug("SSE broadcast", "topic", topic, "event", msg.Event, "payload_bytes", len(msg.Formatted))

	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		if sub.topics[topic] || sub.topics["*"] {
			select {
			case sub.ch <- msg:
			default:
				// Drop event if subscriber buffer is full
				b.logger.Warn("dropping SSE event for slow subscriber")
			}
		}
	}
}

// Notify is a convenience method called after batcher flush to broadcast a dashboard refresh.
// In practice, the Updater replaces this with richer partial rendering.
func (b *Broker) Notify() {
	b.Broadcast("dashboard", SSEMessage{Event: "refresh", Data: []byte("{}")})
}

// SubscriberCount returns the current number of subscribers.
func (b *Broker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
