package capture

import (
	"testing"
)

func TestDeduplicateTokens_Nil(t *testing.T) {
	result := DeduplicateTokens(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestDeduplicateTokens_SingleEvent(t *testing.T) {
	events := []NormalizedEvent{
		{MessageUUID: "uuid-1", InputTokens: 100, OutputTokens: 200, CacheReadTokens: 50, CacheCreateTokens: 30},
	}
	result := DeduplicateTokens(events)
	if len(result) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result))
	}
	if result[0].InputTokens != 100 || result[0].OutputTokens != 200 {
		t.Errorf("single event tokens should be unchanged: input=%d output=%d", result[0].InputTokens, result[0].OutputTokens)
	}
}

func TestDeduplicateTokens_ThinkingPlusText(t *testing.T) {
	// Simulates the core bug: two JSONL lines from the same API call
	// (thinking + text) with identical usage values.
	events := []NormalizedEvent{
		{
			MessageUUID:       "uuid-1",
			EventKind:         "reasoning",
			InputTokens:       3,
			OutputTokens:      9,
			CacheReadTokens:   5708,
			CacheCreateTokens: 3882,
		},
		{
			MessageUUID:       "uuid-1",
			EventKind:         "message",
			InputTokens:       3,
			OutputTokens:      9,
			CacheReadTokens:   5708,
			CacheCreateTokens: 3882,
		},
	}

	result := DeduplicateTokens(events)
	if len(result) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result))
	}

	// First event (thinking) should have tokens zeroed
	if result[0].InputTokens != 0 || result[0].OutputTokens != 0 ||
		result[0].CacheReadTokens != 0 || result[0].CacheCreateTokens != 0 {
		t.Errorf("first event tokens should be zeroed: input=%d output=%d cache_read=%d cache_create=%d",
			result[0].InputTokens, result[0].OutputTokens, result[0].CacheReadTokens, result[0].CacheCreateTokens)
	}

	// Last event (text) should keep its tokens
	if result[1].InputTokens != 3 || result[1].OutputTokens != 9 ||
		result[1].CacheReadTokens != 5708 || result[1].CacheCreateTokens != 3882 {
		t.Errorf("last event tokens should be preserved: input=%d output=%d cache_read=%d cache_create=%d",
			result[1].InputTokens, result[1].OutputTokens, result[1].CacheReadTokens, result[1].CacheCreateTokens)
	}
}

func TestDeduplicateTokens_DifferentUUIDs(t *testing.T) {
	// Events from different API calls should not affect each other.
	events := []NormalizedEvent{
		{MessageUUID: "uuid-1", InputTokens: 100, OutputTokens: 200},
		{MessageUUID: "uuid-2", InputTokens: 300, OutputTokens: 400},
	}

	result := DeduplicateTokens(events)
	if result[0].InputTokens != 100 || result[0].OutputTokens != 200 {
		t.Errorf("uuid-1 tokens changed: input=%d output=%d", result[0].InputTokens, result[0].OutputTokens)
	}
	if result[1].InputTokens != 300 || result[1].OutputTokens != 400 {
		t.Errorf("uuid-2 tokens changed: input=%d output=%d", result[1].InputTokens, result[1].OutputTokens)
	}
}

func TestDeduplicateTokens_EmptyUUID(t *testing.T) {
	// Events without a MessageUUID should be left unchanged.
	events := []NormalizedEvent{
		{MessageUUID: "", InputTokens: 100, OutputTokens: 200},
		{MessageUUID: "", InputTokens: 300, OutputTokens: 400},
	}

	result := DeduplicateTokens(events)
	if result[0].InputTokens != 100 || result[0].OutputTokens != 200 {
		t.Errorf("empty-uuid event 0 changed: input=%d output=%d", result[0].InputTokens, result[0].OutputTokens)
	}
	if result[1].InputTokens != 300 || result[1].OutputTokens != 400 {
		t.Errorf("empty-uuid event 1 changed: input=%d output=%d", result[1].InputTokens, result[1].OutputTokens)
	}
}

// PropagateModel tests

func TestPropagateModel_ForwardFill(t *testing.T) {
	events := []NormalizedEvent{
		{SessionID: "s1", Model: "gpt-5.4", EventKind: "turn_context"},
		{SessionID: "s1", Model: "", EventKind: "message"},
		{SessionID: "s1", Model: "", EventKind: "tool_call"},
	}
	PropagateModel(events)
	for i, evt := range events {
		if evt.Model != "gpt-5.4" {
			t.Errorf("event[%d] expected model=gpt-5.4, got %q", i, evt.Model)
		}
	}
}

func TestPropagateModel_NoOverwrite(t *testing.T) {
	events := []NormalizedEvent{
		{SessionID: "s1", Model: "gpt-5.4", EventKind: "turn_context"},
		{SessionID: "s1", Model: "o3-pro", EventKind: "message"},
		{SessionID: "s1", Model: "", EventKind: "tool_call"},
	}
	PropagateModel(events)
	if events[1].Model != "o3-pro" {
		t.Errorf("event[1] should keep explicit model, got %q", events[1].Model)
	}
	// After seeing o3-pro, forward-fill should use o3-pro
	if events[2].Model != "o3-pro" {
		t.Errorf("event[2] expected model=o3-pro after switch, got %q", events[2].Model)
	}
}

func TestPropagateModel_MultipleSessions(t *testing.T) {
	events := []NormalizedEvent{
		{SessionID: "s1", Model: "gpt-5.4"},
		{SessionID: "s2", Model: "o3-pro"},
		{SessionID: "s1", Model: ""},
		{SessionID: "s2", Model: ""},
	}
	PropagateModel(events)
	if events[2].Model != "gpt-5.4" {
		t.Errorf("s1 event expected gpt-5.4, got %q", events[2].Model)
	}
	if events[3].Model != "o3-pro" {
		t.Errorf("s2 event expected o3-pro, got %q", events[3].Model)
	}
}

func TestPropagateModel_NoModelAtAll(t *testing.T) {
	events := []NormalizedEvent{
		{SessionID: "s1", Model: ""},
		{SessionID: "s1", Model: ""},
	}
	PropagateModel(events)
	if events[0].Model != "" || events[1].Model != "" {
		t.Errorf("events without any model should stay empty")
	}
}

func TestPropagateModel_Nil(t *testing.T) {
	// Should not panic on nil input
	PropagateModel(nil)
}

func TestDeduplicateTokens_ThreeBlocksSameUUID(t *testing.T) {
	// Three content blocks from the same API call (e.g., thinking + text + tool_use).
	events := []NormalizedEvent{
		{MessageUUID: "uuid-1", EventKind: "reasoning", InputTokens: 10, OutputTokens: 8, CacheReadTokens: 1000},
		{MessageUUID: "uuid-1", EventKind: "message", InputTokens: 10, OutputTokens: 8, CacheReadTokens: 1000},
		{MessageUUID: "uuid-1", EventKind: "tool_call", InputTokens: 10, OutputTokens: 500, CacheReadTokens: 1000},
	}

	result := DeduplicateTokens(events)

	// Only the last event should have tokens
	for i := 0; i < 2; i++ {
		if result[i].InputTokens != 0 || result[i].OutputTokens != 0 || result[i].CacheReadTokens != 0 {
			t.Errorf("event[%d] should have zeroed tokens: input=%d output=%d cache_read=%d",
				i, result[i].InputTokens, result[i].OutputTokens, result[i].CacheReadTokens)
		}
	}
	if result[2].InputTokens != 10 || result[2].OutputTokens != 500 || result[2].CacheReadTokens != 1000 {
		t.Errorf("last event tokens changed: input=%d output=%d cache_read=%d",
			result[2].InputTokens, result[2].OutputTokens, result[2].CacheReadTokens)
	}
}

func TestDeduplicateTokens_InterleavedUUIDs(t *testing.T) {
	// Two API calls interleaved (shouldn't normally happen, but test robustness).
	events := []NormalizedEvent{
		{MessageUUID: "uuid-1", EventKind: "reasoning", InputTokens: 10, OutputTokens: 5},
		{MessageUUID: "uuid-2", EventKind: "message", InputTokens: 20, OutputTokens: 30},
		{MessageUUID: "uuid-1", EventKind: "message", InputTokens: 10, OutputTokens: 50},
		{MessageUUID: "uuid-2", EventKind: "tool_call", InputTokens: 20, OutputTokens: 300},
	}

	result := DeduplicateTokens(events)

	// uuid-1: event[0] zeroed, event[2] kept
	if result[0].InputTokens != 0 || result[0].OutputTokens != 0 {
		t.Errorf("uuid-1 event[0] should be zeroed: input=%d output=%d", result[0].InputTokens, result[0].OutputTokens)
	}
	if result[2].InputTokens != 10 || result[2].OutputTokens != 50 {
		t.Errorf("uuid-1 event[2] should be kept: input=%d output=%d", result[2].InputTokens, result[2].OutputTokens)
	}

	// uuid-2: event[1] zeroed, event[3] kept
	if result[1].InputTokens != 0 || result[1].OutputTokens != 0 {
		t.Errorf("uuid-2 event[1] should be zeroed: input=%d output=%d", result[1].InputTokens, result[1].OutputTokens)
	}
	if result[3].InputTokens != 20 || result[3].OutputTokens != 300 {
		t.Errorf("uuid-2 event[3] should be kept: input=%d output=%d", result[3].InputTokens, result[3].OutputTokens)
	}
}

func TestDeduplicateTokens_MixedUUIDAndEmpty(t *testing.T) {
	// Mix of events with and without MessageUUID.
	events := []NormalizedEvent{
		{MessageUUID: "uuid-1", EventKind: "reasoning", InputTokens: 10, OutputTokens: 5},
		{MessageUUID: "", EventKind: "event_msg", InputTokens: 0, OutputTokens: 0},
		{MessageUUID: "uuid-1", EventKind: "message", InputTokens: 10, OutputTokens: 50},
	}

	result := DeduplicateTokens(events)

	// uuid-1 reasoning: zeroed
	if result[0].InputTokens != 0 {
		t.Errorf("uuid-1 reasoning should be zeroed: input=%d", result[0].InputTokens)
	}
	// empty uuid: unchanged
	if result[1].InputTokens != 0 || result[1].OutputTokens != 0 {
		t.Errorf("empty-uuid event changed")
	}
	// uuid-1 message: kept
	if result[2].InputTokens != 10 || result[2].OutputTokens != 50 {
		t.Errorf("uuid-1 message should be kept: input=%d output=%d", result[2].InputTokens, result[2].OutputTokens)
	}
}

func TestDeduplicateTokens_CodexRepeatedTotals(t *testing.T) {
	events := []NormalizedEvent{
		{
			SessionID:          "codex-sess-1",
			SourceName:         "codex",
			PayloadType:        "token_count",
			TokenUsageTotalKey: "9516:8064:157:9673",
			InputTokens:        1452,
			OutputTokens:       157,
			CacheReadTokens:    8064,
		},
		{
			SessionID:          "codex-sess-1",
			SourceName:         "codex",
			PayloadType:        "token_count",
			TokenUsageTotalKey: "9516:8064:157:9673",
			InputTokens:        1452,
			OutputTokens:       157,
			CacheReadTokens:    8064,
		},
		{
			SessionID:          "codex-sess-1",
			SourceName:         "codex",
			PayloadType:        "token_count",
			TokenUsageTotalKey: "10516:8832:207:10723",
			InputTokens:        232,
			OutputTokens:       50,
			CacheReadTokens:    768,
		},
	}

	result := DeduplicateTokens(events)

	if result[0].InputTokens != 1452 || result[0].OutputTokens != 157 || result[0].CacheReadTokens != 8064 {
		t.Errorf("first codex token_count should be preserved: input=%d output=%d cache_read=%d",
			result[0].InputTokens, result[0].OutputTokens, result[0].CacheReadTokens)
	}
	if result[1].InputTokens != 0 || result[1].OutputTokens != 0 || result[1].CacheReadTokens != 0 {
		t.Errorf("duplicate codex token_count should be zeroed: input=%d output=%d cache_read=%d",
			result[1].InputTokens, result[1].OutputTokens, result[1].CacheReadTokens)
	}
	if result[2].InputTokens != 232 || result[2].OutputTokens != 50 || result[2].CacheReadTokens != 768 {
		t.Errorf("next codex token_count should be preserved: input=%d output=%d cache_read=%d",
			result[2].InputTokens, result[2].OutputTokens, result[2].CacheReadTokens)
	}
}
