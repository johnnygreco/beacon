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

	if got := batch.ActivityEvents[0].SourceEventIndex; got == 0 {
		t.Fatal("source event index is empty, want deterministic synthetic index")
	}
	if len(batch.EventLinks) != 1 {
		t.Fatalf("event links = %d, want one unresolved parent link", len(batch.EventLinks))
	}
	link := batch.EventLinks[0]
	if link.RawLinkedEventID != "missing-parent" || link.ResolutionStatus != "unresolved" ||
		link.LinkScope != "same_session" || link.LinkedEventUID != "" {
		t.Fatalf("link identity = %#v, want unresolved same-session raw parent", link)
	}
}

func TestBuildInsertRowBatchSubeventIdentityIsFlushIndependent(t *testing.T) {
	base := NormalizedEvent{
		SessionID:    "raw-session",
		RawSessionID: "raw-session",
		RawEventID:   "row-1",
		SourceName:   "hermes",
		SourceFile:   "state.db",
		SourceLineNo: 42,
		SourceOffset: 100,
		RawPayload:   `{"row":1}`,
	}
	message := base
	message.EventKind = "message"
	message.TextContent = "assistant text"
	tool := base
	tool.EventKind = "tool_call"
	tool.ToolPhase = "call"
	tool.ToolName = "bash"
	tool.ToolUseID = "tool-1"
	tool.TextContent = "bash"
	identity := FleetIdentity{
		CollectorID: "collector-a",
		Sources:     map[string]FleetSourceIdentity{"hermes": {SourceID: "source-hermes-a"}},
	}

	together := buildInsertRowBatch([]NormalizedEvent{message, tool}, 0, 0, identity)
	messageOnly := buildInsertRowBatch([]NormalizedEvent{message}, 0, 0, identity)
	toolOnly := buildInsertRowBatch([]NormalizedEvent{tool}, 0, 0, identity)

	if together.ActivityEvents[0].EventUID != messageOnly.ActivityEvents[0].EventUID {
		t.Fatalf("message UID changed across flush shape: %q vs %q", together.ActivityEvents[0].EventUID, messageOnly.ActivityEvents[0].EventUID)
	}
	if together.ActivityEvents[1].EventUID != toolOnly.ActivityEvents[0].EventUID {
		t.Fatalf("tool UID changed across flush shape: %q vs %q", together.ActivityEvents[1].EventUID, toolOnly.ActivityEvents[0].EventUID)
	}
	if together.ActivityEvents[0].EventUID == together.ActivityEvents[1].EventUID {
		t.Fatalf("distinct subevents collapsed to one UID: %q", together.ActivityEvents[0].EventUID)
	}
	if together.ActivityEvents[0].SourceEventIndex == together.ActivityEvents[1].SourceEventIndex {
		t.Fatalf("distinct subevents share source_event_index: %#v", together.ActivityEvents)
	}
}

func TestBuildInsertRowBatchParentLinksResolveOutOfOrder(t *testing.T) {
	child := NormalizedEvent{
		SessionID:        "raw-session",
		RawSessionID:     "raw-session",
		RawEventID:       "child-raw",
		RawLinkedEventID: "parent-raw",
		SourceName:       "claude",
		EventKind:        "message",
		SourceLineNo:     8,
		RawPayload:       `{"uuid":"child-raw","parentUuid":"parent-raw"}`,
	}
	parent := NormalizedEvent{
		SessionID:    "raw-session",
		RawSessionID: "raw-session",
		RawEventID:   "parent-raw",
		SourceName:   "claude",
		EventKind:    "message",
		SourceLineNo: 7,
		RawPayload:   `{"uuid":"parent-raw"}`,
	}
	batch := buildInsertRowBatch([]NormalizedEvent{child, parent}, 0, 0, FleetIdentity{
		CollectorID: "collector-a",
		Sources:     map[string]FleetSourceIdentity{"claude": {SourceID: "source-claude-a"}},
	})

	if len(batch.EventLinks) != 1 {
		t.Fatalf("event links = %d, want one parent link", len(batch.EventLinks))
	}
	link := batch.EventLinks[0]
	if link.ResolutionStatus != "resolved" || link.LinkedEventUID != batch.ActivityEvents[1].EventUID {
		t.Fatalf("link = %#v, want resolved link to later parent %q", link, batch.ActivityEvents[1].EventUID)
	}
}

func TestBuildInsertRowBatchParentLinksDefaultToCurrentSession(t *testing.T) {
	parent := NormalizedEvent{
		SessionID:          "child-session",
		RawSessionID:       "child-session",
		RawParentSessionID: "parent-session",
		RawEventID:         "parent-event",
		SourceName:         "pi",
		EventKind:          "message",
		SourceLineNo:       9,
		RawPayload:         `{"id":"parent-event"}`,
	}
	child := NormalizedEvent{
		SessionID:          "child-session",
		RawSessionID:       "child-session",
		RawParentSessionID: "parent-session",
		RawEventID:         "child-event",
		RawLinkedEventID:   "parent-event",
		SourceName:         "pi",
		EventKind:          "message",
		SourceLineNo:       10,
		RawPayload:         `{"id":"child-event","parentId":"parent-event"}`,
	}
	batch := buildInsertRowBatch([]NormalizedEvent{parent, child}, 0, 0, FleetIdentity{
		CollectorID: "collector-a",
		Sources:     map[string]FleetSourceIdentity{"pi": {SourceID: "source-pi-a"}},
	})

	if len(batch.EventLinks) != 1 {
		t.Fatalf("event links = %d, want one parent link", len(batch.EventLinks))
	}
	link := batch.EventLinks[0]
	if link.ResolutionStatus != "resolved" || link.LinkScope != "same_session" ||
		link.RawLinkedSessionID != "child-session" || link.LinkedEventUID != batch.ActivityEvents[0].EventUID {
		t.Fatalf("link = %#v, want resolved same-session child link to %q", link, batch.ActivityEvents[0].EventUID)
	}
}

func TestBuildInsertRowBatchResolvesKnownRawParent(t *testing.T) {
	identity := FleetIdentity{
		CollectorID: "collector-a",
		Sources:     map[string]FleetSourceIdentity{"claude": {SourceID: "source-claude-a"}},
	}
	parent := NormalizedEvent{
		SessionID:    "raw-session",
		RawSessionID: "raw-session",
		RawEventID:   "parent-raw",
		SourceName:   "claude",
		EventKind:    "message",
		SourceLineNo: 1,
		RawPayload:   `{"uuid":"parent-raw"}`,
	}
	parentBatch, known := buildInsertRowBatchWithKnown([]NormalizedEvent{parent}, 0, 0, identity, nil)
	child := NormalizedEvent{
		SessionID:        "raw-session",
		RawSessionID:     "raw-session",
		RawEventID:       "child-raw",
		RawLinkedEventID: "parent-raw",
		SourceName:       "claude",
		EventKind:        "message",
		SourceLineNo:     2,
		RawPayload:       `{"uuid":"child-raw","parentUuid":"parent-raw"}`,
	}
	childBatch, _ := buildInsertRowBatchWithKnown([]NormalizedEvent{child}, 0, 0, identity, known)

	if len(childBatch.EventLinks) != 1 {
		t.Fatalf("event links = %d, want one parent link", len(childBatch.EventLinks))
	}
	link := childBatch.EventLinks[0]
	if link.ResolutionStatus != "resolved" || link.LinkedEventUID != parentBatch.ActivityEvents[0].EventUID {
		t.Fatalf("link = %#v, want resolved link to prior batch parent %q", link, parentBatch.ActivityEvents[0].EventUID)
	}
}

func TestBatcherRawEventCacheIsBounded(t *testing.T) {
	b := &Batcher{rawEventCache: map[string]string{}}
	b.rememberRawEvents(map[string]string{
		"first":  "event-1",
		"second": "event-2",
		"third":  "event-3",
	})
	if len(b.rawEventCache) != 3 || len(b.rawCacheOrder) != 3 {
		t.Fatalf("raw event cache = %#v order=%#v, want three entries", b.rawEventCache, b.rawCacheOrder)
	}

	b.trimRawEventCache(2)

	if len(b.rawEventCache) != 2 || len(b.rawCacheOrder) != 2 {
		t.Fatalf("raw event cache = %#v order=%#v, want two entries", b.rawEventCache, b.rawCacheOrder)
	}
	if _, ok := b.rawEventCache[b.rawCacheOrder[0]]; !ok {
		t.Fatalf("cache order references missing key: %#v in %#v", b.rawCacheOrder, b.rawEventCache)
	}
	if _, ok := b.rawEventCache[b.rawCacheOrder[1]]; !ok {
		t.Fatalf("cache order references missing key: %#v in %#v", b.rawCacheOrder, b.rawEventCache)
	}
}

func TestBuildInsertRowBatchSourceEventIndexIgnoresMutableClassification(t *testing.T) {
	base := NormalizedEvent{
		SessionID:    "raw-session",
		RawSessionID: "raw-session",
		RawEventID:   "row-1",
		MessageUUID:  "row-1:tool_result:tool-1",
		ToolUseID:    "tool-1",
		ToolPhase:    "result",
		SourceName:   "opencode",
		SourceFile:   "opencode.db",
		SourceLineNo: 42,
		SourceOffset: 100,
		RawPayload:   `{"kind":"tool_result","id":"row-1"}`,
	}
	result := base
	result.EventKind = "tool_result"
	result.ToolOutput = "ok"
	toolError := base
	toolError.EventKind = "tool_error"
	toolError.ErrorCode = "tool_execution_failed"
	toolError.ToolOutput = "failed"
	identity := FleetIdentity{
		CollectorID: "collector-a",
		Sources:     map[string]FleetSourceIdentity{"opencode": {SourceID: "source-opencode-a"}},
	}

	resultBatch := buildInsertRowBatch([]NormalizedEvent{result}, 0, 0, identity)
	errorBatch := buildInsertRowBatch([]NormalizedEvent{toolError}, 0, 0, identity)

	if resultBatch.ActivityEvents[0].SourceEventIndex != errorBatch.ActivityEvents[0].SourceEventIndex {
		t.Fatalf("source indexes changed across mutable classification: %d vs %d",
			resultBatch.ActivityEvents[0].SourceEventIndex,
			errorBatch.ActivityEvents[0].SourceEventIndex)
	}
	if resultBatch.ActivityEvents[0].EventUID != errorBatch.ActivityEvents[0].EventUID {
		t.Fatalf("event UID changed across mutable classification: %q vs %q",
			resultBatch.ActivityEvents[0].EventUID,
			errorBatch.ActivityEvents[0].EventUID)
	}
}
