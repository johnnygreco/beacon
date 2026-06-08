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

func TestBuildInsertRowBatchGlobalIdentityUsesCollectorAndSource(t *testing.T) {
	evt := NormalizedEvent{
		SessionID:    "raw-session",
		RawSessionID: "raw-session",
		RawEventID:   "raw-event",
		SourceName:   "codex",
		EventKind:    "message",
		RawPayload:   `{"id":"raw-event"}`,
	}
	left := buildInsertRowBatch([]NormalizedEvent{evt}, 0, 0, FleetIdentity{
		NodeID:            "node-a",
		CollectorID:       "collector-a",
		ControlPlaneEpoch: "1",
		Sources:           map[string]FleetSourceIdentity{"codex": {SourceID: "source-codex-a"}},
	})
	right := buildInsertRowBatch([]NormalizedEvent{evt}, 0, 0, FleetIdentity{
		NodeID:            "node-b",
		CollectorID:       "collector-b",
		ControlPlaneEpoch: "1",
		Sources:           map[string]FleetSourceIdentity{"codex": {SourceID: "source-codex-b"}},
	})

	leftEvent := left.ActivityEvents[0]
	rightEvent := right.ActivityEvents[0]
	if leftEvent.EventUID == rightEvent.EventUID {
		t.Fatalf("event UID did not vary by collector/source: %q", leftEvent.EventUID)
	}
	if leftEvent.SessionID == rightEvent.SessionID {
		t.Fatalf("session ID did not vary by collector/source: %q", leftEvent.SessionID)
	}
	if leftEvent.RawSessionID != "raw-session" || leftEvent.RawEventID != "raw-event" {
		t.Fatalf("raw IDs not preserved: %#v", leftEvent)
	}
	if leftEvent.CollectorID != "collector-a" || leftEvent.SourceID != "source-codex-a" || leftEvent.NodeID != "node-a" {
		t.Fatalf("fleet identity not copied into event: %#v", leftEvent)
	}
	if left.RawRecords[0].EventUID != leftEvent.EventUID || left.RawRecords[0].PayloadDigest == "" {
		t.Fatalf("raw record identity not populated: %#v", left.RawRecords[0])
	}
}

func TestBuildInsertRowBatchSyntheticSourceEventIndexAndUnresolvedLink(t *testing.T) {
	parent := NormalizedEvent{
		SessionID:    "raw-session",
		SourceName:   "claude",
		MessageUUID:  "parent-uuid",
		SourceLineNo: 7,
		RawPayload:   `{"uuid":"parent-uuid"}`,
		EventKind:    "message",
	}
	child := NormalizedEvent{
		SessionID:    "raw-session",
		SourceName:   "claude",
		MessageUUID:  "child-uuid",
		ParentUUID:   "missing-parent",
		SourceLineNo: 8,
		RawPayload:   `{"uuid":"child-uuid","parentUuid":"missing-parent"}`,
		EventKind:    "message",
	}
	batch := buildInsertRowBatch([]NormalizedEvent{parent, child}, 0, 0, FleetIdentity{
		CollectorID: "collector-a",
		Sources:     map[string]FleetSourceIdentity{"claude": {SourceID: "source-claude-a"}},
	})

	if got := batch.ActivityEvents[0].SourceEventIndex; got != 700000 {
		t.Fatalf("source event index = %d, want line-derived synthetic index", got)
	}
	if len(batch.EventLinks) != 1 {
		t.Fatalf("event links = %d, want one unresolved parent link", len(batch.EventLinks))
	}
	link := batch.EventLinks[0]
	if link.RawLinkedEventID != "missing-parent" || link.ResolutionStatus != "unresolved" || link.LinkScope != "same_session" {
		t.Fatalf("link identity = %#v, want unresolved same-session raw parent", link)
	}
}
