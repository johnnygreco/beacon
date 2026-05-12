package capture

import "testing"

func TestChangedSessionIDsDeduplicatesInFlushOrder(t *testing.T) {
	got := changedSessionIDs([]NormalizedEvent{
		{SessionID: "session-a"},
		{SessionID: ""},
		{SessionID: "session-b"},
		{SessionID: "session-a"},
	})
	want := []string{"session-a", "session-b"}
	if len(got) != len(want) {
		t.Fatalf("changedSessionIDs length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("changedSessionIDs[%d] = %q, want %q in %#v", i, got[i], want[i], got)
		}
	}
}

func TestEventUIDUsesOrdinalForMultipleEventsFromOneJSONLLine(t *testing.T) {
	first := eventUID("session.jsonl", 12, 345, 0, `{"type":"assistant"}`, 0)
	replayedFirst := eventUID("session.jsonl", 12, 345, 0, `{"type":"assistant"}`, 0)
	second := eventUID("session.jsonl", 12, 345, 0, `{"type":"assistant"}`, 1)

	if first != replayedFirst {
		t.Fatalf("eventUID ordinal 0 is not deterministic: %q vs %q", first, replayedFirst)
	}
	if first == second {
		t.Fatalf("eventUID did not distinguish secondary event ordinal: %q", first)
	}
}
