package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/store"
	"github.com/johnnygreco/beacon/internal/views"
)

func setupLiveWebStore(t *testing.T) *store.Store {
	t.Helper()
	addr := os.Getenv("BEACON_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("set BEACON_TEST_CLICKHOUSE to run ClickHouse web integration tests")
	}

	opts := store.DefaultOptions()
	opts.Addrs = []string{addr}
	opts.Database = "beacon_test_web"
	ch, err := store.Open(t.Context(), opts)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	if err := store.Reset(t.Context(), ch.DB, ch.Database()); err != nil {
		ch.Close()
		t.Fatalf("reset: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

func TestAPIEventsUsePreviewsAndPayloadEndpointLoadsFullJSON(t *testing.T) {
	ch := setupLiveWebStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	api := NewAPIHandlers(ch.DB, nil, logger)

	now := time.Now().UTC()
	sessionID := "api-lazy-session"
	eventID := "api-lazy-tool"
	fullMarker := "FULL_PAYLOAD_ONLY_MARKER"
	inputPreview := `{"command":"echo preview-only"}`
	outputPreview := `{"stdout":"preview-only"}`

	event := models.Event{
		EventUID:     eventID,
		SessionID:    sessionID,
		NodeID:       "api-node",
		CollectorID:  "api-collector",
		SourceID:     "api-source",
		SourceName:   "test-source",
		Runtime:      "test-runtime",
		Provider:     "test-provider",
		Format:       "jsonl",
		EventKind:    "tool_call",
		PayloadType:  "tool_use",
		ActorRole:    "assistant",
		Timestamp:    now,
		TextContent:  "tool invocation preview text",
		TextPreview:  "tool invocation preview text",
		ToolName:     "Bash",
		ToolUseID:    "toolu-api-lazy",
		Model:        "gpt-4",
		InputTokens:  4,
		OutputTokens: 5,
		CWD:          "/Users/example/projects/beacon",
		EventVersion: 1,
		PayloadJSON:  `{"event":"preview"}`,
		SourceFile:   "api-live.jsonl",
		SourceLineNo: 1,
		SourceOffset: 0,
		CreatedAt:    now,
	}
	payload := models.ToolPayload{
		EventUID:      eventID,
		ToolName:      "Bash",
		ToolPhase:     "call",
		InputJSON:     `{"secret":"` + fullMarker + strings.Repeat("x", 8192) + `"}`,
		OutputJSON:    `{"secret":"` + fullMarker + strings.Repeat("y", 8192) + `"}`,
		InputPreview:  inputPreview,
		OutputPreview: outputPreview,
	}
	batch := store.RowBatch{
		RawRecords:     []models.RawRecord{store.NewRawRecord(event)},
		ActivityEvents: []models.Event{event},
		ToolPayloads:   []models.ToolPayload{payload},
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("replay flush: %v", err)
	}

	eventsBody := recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?limit=10", "id", sessionID)
	if strings.Contains(eventsBody, fullMarker) {
		t.Fatalf("session events response included full payload marker")
	}
	var events []APISessionEvent
	if err := json.Unmarshal([]byte(eventsBody), &events); err != nil {
		t.Fatalf("decode session events: %v\n%s", err, eventsBody)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 session event, got %d", len(events))
	}
	if events[0].EventUID != eventID || events[0].InputPreview != inputPreview || events[0].OutputPreview != outputPreview {
		t.Fatalf("unexpected event preview payload: %#v", events[0])
	}
	scopedEventsBody := recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?limit=10&collector_id=api-collector&source_id=api-source&project_key=beacon", "id", sessionID)
	events = nil
	if err := json.Unmarshal([]byte(scopedEventsBody), &events); err != nil {
		t.Fatalf("decode scoped session events: %v\n%s", err, scopedEventsBody)
	}
	if len(events) != 1 || events[0].EventUID != eventID {
		t.Fatalf("scoped session events = %#v, want only %s", events, eventID)
	}
	outScopedEventsBody := recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?limit=10&project_key=other", "id", sessionID)
	events = nil
	if err := json.Unmarshal([]byte(outScopedEventsBody), &events); err != nil {
		t.Fatalf("decode out-of-scope session events: %v\n%s", err, outScopedEventsBody)
	}
	if len(events) != 0 {
		t.Fatalf("out-of-scope session events leaked: %#v", events)
	}

	eventBody := recordAPIResponse(t, api.GetEvent, "/api/events/"+eventID, "event_id", eventID)
	if strings.Contains(eventBody, fullMarker) {
		t.Fatalf("single event response included full payload marker")
	}
	var single APISessionEvent
	if err := json.Unmarshal([]byte(eventBody), &single); err != nil {
		t.Fatalf("decode event: %v\n%s", err, eventBody)
	}
	if single.EventUID != eventID || single.InputPreview != inputPreview || single.OutputPreview != outputPreview {
		t.Fatalf("unexpected single event preview payload: %#v", single)
	}
	scopedEventBody := recordAPIResponse(t, api.GetEvent, "/api/events/"+eventID+"?collector_id=api-collector&source_id=api-source&project_key=beacon", "event_id", eventID)
	if err := json.Unmarshal([]byte(scopedEventBody), &single); err != nil {
		t.Fatalf("decode scoped event: %v\n%s", err, scopedEventBody)
	}
	if single.EventUID != eventID {
		t.Fatalf("scoped event = %#v, want %s", single, eventID)
	}
	recordAPIStatus(t, api.GetEvent, "/api/events/"+eventID+"?project_key=other", http.StatusNotFound, "event_id", eventID)

	payloadBody := recordAPIResponse(t, api.GetToolPayload, "/api/tool-payloads/"+eventID, "event_id", eventID)
	if !strings.Contains(payloadBody, fullMarker) {
		t.Fatalf("tool payload response did not include full payload marker")
	}
	var full APIToolPayload
	if err := json.Unmarshal([]byte(payloadBody), &full); err != nil {
		t.Fatalf("decode tool payload: %v\n%s", err, payloadBody)
	}
	if full.EventUID != eventID || !strings.Contains(full.InputJSON, fullMarker) || !strings.Contains(full.OutputJSON, fullMarker) {
		t.Fatalf("unexpected full tool payload: %#v", full)
	}
	scopedPayloadBody := recordAPIResponse(t, api.GetToolPayload, "/api/tool-payloads/"+eventID+"?collector_id=api-collector&source_id=api-source&project_key=beacon", "event_id", eventID)
	if err := json.Unmarshal([]byte(scopedPayloadBody), &full); err != nil {
		t.Fatalf("decode scoped tool payload: %v\n%s", err, scopedPayloadBody)
	}
	if full.EventUID != eventID || !strings.Contains(full.InputJSON, fullMarker) {
		t.Fatalf("scoped tool payload = %#v, want full payload for %s", full, eventID)
	}
	recordAPIStatus(t, api.GetToolPayload, "/api/tool-payloads/"+eventID+"?project_key=other", http.StatusNotFound, "event_id", eventID)
}

func TestAPISessionEventsTailReturnsLatestBoundedSliceChronologically(t *testing.T) {
	ch := setupLiveWebStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	api := NewAPIHandlers(ch.DB, nil, logger)

	now := time.Now().UTC().Truncate(time.Second)
	sessionID := "api-tail-session"
	events := make([]models.Event, 0, 205)
	for i := 0; i < 205; i++ {
		uid := fmt.Sprintf("tail-event-%03d", i)
		event := liveEvent(uid, sessionID, "message", "assistant", now.Add(time.Duration(i)*time.Second), "openai", "gpt-5", "", 1, 1, 0)
		event.SourceLineNo = i + 1
		event.SourceOffset = int64(i)
		events = append(events, event)
	}
	batch := store.RowBatch{ActivityEvents: events}
	for _, event := range events {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}

	body := recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?limit=200&tail=1", "id", sessionID)
	var got []APISessionEvent
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode session events: %v\n%s", err, body)
	}
	if len(got) != 200 {
		t.Fatalf("tail event count = %d, want 200", len(got))
	}
	if got[0].EventUID != "tail-event-005" || got[len(got)-1].EventUID != "tail-event-204" {
		t.Fatalf("tail events returned wrong chronological slice: first=%s last=%s", got[0].EventUID, got[len(got)-1].EventUID)
	}
	for _, event := range got {
		if event.EventUID == "tail-event-000" {
			t.Fatalf("tail events included oldest event outside latest slice")
		}
	}

	body = recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?limit=3", "id", sessionID)
	got = nil
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode default session events: %v\n%s", err, body)
	}
	if len(got) != 3 || got[0].EventUID != "tail-event-000" || got[2].EventUID != "tail-event-002" {
		t.Fatalf("default events should remain oldest-first paginated, got %#v", got)
	}
}

func TestSessionEventsAndTranscriptUseEventProjectBeforeSessionFallback(t *testing.T) {
	ch := setupLiveWebStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	api := NewAPIHandlers(ch.DB, nil, logger)

	now := time.Now().UTC().Truncate(time.Second)
	sessionID := "mixed-project-session"
	beaconEvent := liveEvent("mixed-project-beacon", sessionID, "message", "assistant", now, "openai", "gpt-5", "", 1, 1, 0)
	beaconEvent.SourceID = "remote-source"
	beaconEvent.CWD = "/Users/example/projects/beacon"
	beaconEvent.TextContent = "beacon project visible"
	beaconEvent.TextPreview = "beacon project visible"
	otherEvent := liveEvent("mixed-project-other", sessionID, "message", "assistant", now.Add(time.Second), "openai", "gpt-5", "", 1, 1, 0)
	otherEvent.SourceID = "local-source"
	otherEvent.CWD = "/Users/example/projects/other"
	otherEvent.TextContent = "other project hidden"
	otherEvent.TextPreview = "other project hidden"
	blankProjectEvent := liveEvent("mixed-project-blank", sessionID, "message", "assistant", now.Add(2*time.Second), "openai", "gpt-5", "", 1, 1, 0)
	blankProjectEvent.SourceID = "remote-source"
	blankProjectEvent.TextContent = "blank project hidden"
	blankProjectEvent.TextPreview = "blank project hidden"

	batch := store.RowBatch{ActivityEvents: []models.Event{beaconEvent, otherEvent, blankProjectEvent}}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush mixed project events: %v", err)
	}

	body := recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?project_key=beacon", "id", sessionID)
	var events []APISessionEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("decode beacon-scoped session events: %v\n%s", err, body)
	}
	if len(events) != 1 || events[0].EventUID != beaconEvent.EventUID {
		t.Fatalf("beacon-scoped session events = %#v, want only %s", events, beaconEvent.EventUID)
	}
	body = recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?project_key=beacon&source_id=remote-source", "id", sessionID)
	events = nil
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("decode remote beacon-scoped session events: %v\n%s", err, body)
	}
	if len(events) != 1 || events[0].EventUID != beaconEvent.EventUID {
		t.Fatalf("remote beacon-scoped session events = %#v, want %s despite global source fallback", events, beaconEvent.EventUID)
	}

	body = recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+sessionID+"/events?project_key=other", "id", sessionID)
	events = nil
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("decode other-scoped session events: %v\n%s", err, body)
	}
	if len(events) != 1 || events[0].EventUID != otherEvent.EventUID {
		t.Fatalf("other-scoped session events = %#v, want only %s", events, otherEvent.EventUID)
	}
	recordAPIStatus(t, api.GetEvent, "/api/events/"+blankProjectEvent.EventUID+"?project_key=beacon", http.StatusNotFound, "event_id", blankProjectEvent.EventUID)
	recordAPIStatus(t, api.GetEvent, "/api/events/"+blankProjectEvent.EventUID+"?project_key=other", http.StatusNotFound, "event_id", blankProjectEvent.EventUID)

	_, turns := QuerySessionConversationScoped(context.Background(), ch.DB, sessionID, APIScopeFilters{ProjectKeys: []string{"beacon"}})
	seen := map[string]bool{}
	for _, turn := range turns {
		for _, event := range turn.Events {
			seen[event.EventUID] = true
			if strings.Contains(event.TextPreview, "hidden") || strings.Contains(event.TextContent, "hidden") {
				t.Fatalf("beacon-scoped transcript leaked out-of-project event: %#v", event)
			}
		}
	}
	if !seen[beaconEvent.EventUID] || seen[otherEvent.EventUID] || seen[blankProjectEvent.EventUID] {
		t.Fatalf("beacon-scoped transcript event set = %#v", seen)
	}
	_, turns = QuerySessionConversationScoped(context.Background(), ch.DB, sessionID, APIScopeFilters{ProjectKeys: []string{"beacon"}, SourceIDs: []string{"remote-source"}})
	seen = map[string]bool{}
	for _, turn := range turns {
		for _, event := range turn.Events {
			seen[event.EventUID] = true
		}
	}
	if !seen[beaconEvent.EventUID] || seen[otherEvent.EventUID] || seen[blankProjectEvent.EventUID] {
		t.Fatalf("remote beacon-scoped transcript event set = %#v", seen)
	}

	singleSessionID := "single-project-session"
	singleBeaconEvent := liveEvent("single-project-beacon", singleSessionID, "message", "assistant", now.Add(3*time.Second), "openai", "gpt-5", "", 1, 1, 0)
	singleBeaconEvent.CWD = "/Users/example/projects/beacon"
	singleBlankEvent := liveEvent("single-project-blank", singleSessionID, "message", "assistant", now.Add(4*time.Second), "openai", "gpt-5", "", 1, 1, 0)
	singleBlankEvent.TextContent = "blank project inherits single project"
	singleBlankEvent.TextPreview = "blank project inherits single project"
	batch = store.RowBatch{ActivityEvents: []models.Event{singleBeaconEvent, singleBlankEvent}}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush single project events: %v", err)
	}
	body = recordAPIResponse(t, api.GetSessionEvents, "/api/sessions/"+singleSessionID+"/events?project_key=beacon", "id", singleSessionID)
	events = nil
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		t.Fatalf("decode single-project session events: %v\n%s", err, body)
	}
	if len(events) != 2 {
		t.Fatalf("single-project scoped events = %#v, want both project and blank-cwd events", events)
	}
	recordAPIResponse(t, api.GetEvent, "/api/events/"+singleBlankEvent.EventUID+"?project_key=beacon", "event_id", singleBlankEvent.EventUID)
	recordAPIStatus(t, api.GetEvent, "/api/events/"+singleBlankEvent.EventUID+"?project_key=other", http.StatusNotFound, "event_id", singleBlankEvent.EventUID)

	_, turns = QuerySessionConversationScoped(context.Background(), ch.DB, singleSessionID, APIScopeFilters{ProjectKeys: []string{"beacon"}})
	seen = map[string]bool{}
	for _, turn := range turns {
		for _, event := range turn.Events {
			seen[event.EventUID] = true
		}
	}
	if !seen[singleBeaconEvent.EventUID] || !seen[singleBlankEvent.EventUID] {
		t.Fatalf("single-project transcript event set = %#v, want project and blank-cwd events", seen)
	}
}

func TestProjectScopedSessionSummariesUseMatchingEventRows(t *testing.T) {
	ch := setupLiveWebStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	api := NewAPIHandlers(ch.DB, nil, logger)

	now := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	sessionID := "mixed-project-summary"
	otherEvent := liveEvent("summary-other-message", sessionID, "message", "assistant", now, "openai", "", "", 11, 13, 0)
	otherEvent.CWD = "/Users/example/projects/other"
	otherEvent.TextContent = "other-scoped needle"
	otherEvent.TextPreview = "other-scoped needle"
	beaconEvent := liveEvent("summary-beacon-message", sessionID, "message", "assistant", now.Add(time.Minute), "openai", "gpt-beacon", "", 2, 3, 0)
	beaconEvent.CWD = "/Users/example/projects/beacon"
	endEvent := liveEvent("summary-session-end", sessionID, "session_end", "system", now.Add(2*time.Minute), "openai", "", "", 0, 0, 0)
	endEvent.CWD = "/Users/example/projects/beacon"

	batch := store.RowBatch{ActivityEvents: []models.Event{otherEvent, beaconEvent, endEvent}}
	for _, event := range batch.ActivityEvents {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush mixed project summary events: %v", err)
	}

	otherScope := APIScopeFilters{ProjectKeys: []string{"other"}}
	detail, err := QuerySessionDetailScoped(context.Background(), ch.DB, sessionID, otherScope)
	if err != nil {
		t.Fatalf("other-scoped detail should find session through event project: %v", err)
	}
	if detail.Session.TotalTokens != 24 || detail.Session.ProjectKey != "other" || detail.Session.WorkingDir != "/Users/example/projects/other" {
		t.Fatalf("other-scoped detail summary = %#v, want 24 tokens in other project", detail.Session)
	}
	if !detail.Session.EndedAt.Equal(otherEvent.Timestamp) || detail.Session.HasSessionEnd || detail.Session.CompletionState != "active" {
		t.Fatalf("other-scoped lifecycle = ended %s hasEnd %v state %q, want latest other event only", detail.Session.EndedAt, detail.Session.HasSessionEnd, detail.Session.CompletionState)
	}
	recordAPIResponse(t, api.GetSessionDetail, "/api/sessions/"+sessionID+"?project_key=other", "id", sessionID)

	beaconDetail, err := QuerySessionDetailScoped(context.Background(), ch.DB, sessionID, APIScopeFilters{ProjectKeys: []string{"beacon"}})
	if err != nil {
		t.Fatalf("beacon-scoped detail: %v", err)
	}
	if beaconDetail.Session.TotalTokens != 5 || beaconDetail.Session.ProjectKey != "beacon" {
		t.Fatalf("beacon-scoped detail summary = %#v, want only beacon tokens", beaconDetail.Session)
	}
	if !beaconDetail.Session.EndedAt.Equal(endEvent.Timestamp) || !beaconDetail.Session.HasSessionEnd || beaconDetail.Session.CompletionState != "completed" {
		t.Fatalf("beacon-scoped lifecycle = ended %s hasEnd %v state %q, want scoped session_end", beaconDetail.Session.EndedAt, beaconDetail.Session.HasSessionEnd, beaconDetail.Session.CompletionState)
	}

	eventSessionIDs, err := queryCompletedSessionContentMatchIDs(context.Background(), ch.DB, nil, "other-scoped needle", "", 10, otherScope)
	if err != nil {
		t.Fatalf("other-scoped content search ids: %v", err)
	}
	if len(eventSessionIDs) != 1 || eventSessionIDs[0] != sessionID {
		t.Fatalf("other-scoped content search ids = %#v, want %s", eventSessionIDs, sessionID)
	}
	leakedIDs, err := queryCompletedSessionContentMatchIDs(context.Background(), ch.DB, nil, "gpt-beacon", "", 10, otherScope)
	if err != nil {
		t.Fatalf("other-scoped metadata leak search ids: %v", err)
	}
	if len(leakedIDs) != 0 {
		t.Fatalf("other-scoped content search matched out-of-scope session metadata: %#v", leakedIDs)
	}
	completed, _ := queryCompletedSessionsFiltered(context.Background(), ch.DB, nil, 0, 10, "other-scoped needle", eventSessionIDs, "ended", false, "", otherScope)
	if len(completed) != 1 || completed[0].ID != sessionID || completed[0].TotalTokens != 24 {
		t.Fatalf("other-scoped completed search = %#v, want scoped summary for %s", completed, sessionID)
	}
}

func TestDashboardJSONAndAnalyticsAPIsUseProjectionRowsAfterReplay(t *testing.T) {
	ch := setupLiveWebStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	api := NewAPIHandlers(ch.DB, nil, logger)

	now := time.Now().UTC().Truncate(time.Second)
	activeID := "dashboard-live-active"
	completedID := "dashboard-live-completed"
	reopenedID := "dashboard-live-reopened"
	events := []models.Event{
		liveEvent("dash-active-user", activeID, "message", "user", now, "openai", "", "", 0, 0, 0),
		liveEvent("dash-active-assistant", activeID, "message", "assistant", now.Add(time.Second), "openai", "gpt-4.1", "", 10, 20, 0),
		liveEvent("dash-active-tool", activeID, "tool_call", "assistant", now.Add(2*time.Second), "openai", "", "mcp__filesystem__read_file", 5, 1, 100),
		liveEvent("dash-completed-user", completedID, "message", "user", now.Add(-10*time.Minute), "openai", "", "", 0, 0, 0),
		liveEvent("dash-completed-assistant", completedID, "message", "assistant", now.Add(-10*time.Minute+time.Second), "openai", "gpt-4.1", "", 7, 8, 0),
		liveEvent("dash-completed-end", completedID, "session_end", "system", now.Add(-9*time.Minute), "openai", "", "", 0, 0, 0),
		liveEvent("dash-reopened-end", reopenedID, "session_end", "system", now.Add(-2*time.Minute), "openai", "", "", 0, 0, 0),
		liveEvent("dash-reopened-user", reopenedID, "message", "user", now.Add(-10*time.Second), "openai", "", "", 0, 0, 0),
	}
	events[5].PayloadType = "last-prompt"
	for i := range events {
		events[i].SourceLineNo = i + 1
		events[i].SourceOffset = int64(i * 10)
	}

	batch := store.RowBatch{ActivityEvents: events}
	for _, event := range events {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("replay flush: %v", err)
	}

	activeBody := recordAPIResponse(t, api.GetDashboardSessions, "/api/dashboard/sessions?state=active")
	var active APIDashboardSessionsResponse
	if err := json.Unmarshal([]byte(activeBody), &active); err != nil {
		t.Fatalf("decode active sessions: %v\n%s", err, activeBody)
	}
	if !containsSession(active.Items, activeID) || !containsSession(active.Items, reopenedID) || containsSession(active.Items, completedID) {
		t.Fatalf("active sessions = %#v", active.Items)
	}

	completedBody := recordAPIResponse(t, api.GetDashboardSessions, "/api/dashboard/sessions?state=completed&range=24h&limit=10")
	var completed APIDashboardSessionsResponse
	if err := json.Unmarshal([]byte(completedBody), &completed); err != nil {
		t.Fatalf("decode completed sessions: %v\n%s", err, completedBody)
	}
	if !containsSession(completed.Items, completedID) || containsSession(completed.Items, activeID) || containsSession(completed.Items, reopenedID) {
		t.Fatalf("completed sessions = %#v", completed.Items)
	}

	activityBody := recordAPIResponse(t, api.GetActivity, "/api/dashboard/activity?range=24h&event_kind=tool_call")
	var activity []APIActivityItem
	if err := json.Unmarshal([]byte(activityBody), &activity); err != nil {
		t.Fatalf("decode activity: %v\n%s", err, activityBody)
	}
	if len(activity) != 1 || activity[0].ID != "dash-active-tool" || activity[0].SessionID != activeID {
		t.Fatalf("activity = %#v", activity)
	}

	metricsBody := recordAPIResponse(t, api.GetMetrics, "/api/metrics")
	var metrics []APIMetricData
	if err := json.Unmarshal([]byte(metricsBody), &metrics); err != nil {
		t.Fatalf("decode metrics: %v\n%s", err, metricsBody)
	}
	assertMetric(t, metrics, "Total Sessions", 3)
	assertMetric(t, metrics, "Active Sessions", 2)
	assertMetric(t, metrics, "Input Tokens", 22)
	assertMetric(t, metrics, "Output Tokens", 29)
	assertMetric(t, metrics, "Tool Calls", 1)
	assertMetric(t, metrics, "MCP Calls", 1)

	chartsBody := recordAPIResponse(t, api.GetDashboardCharts, "/api/dashboard/charts")
	var charts APIDashboardCharts
	if err := json.Unmarshal([]byte(chartsBody), &charts); err != nil {
		t.Fatalf("decode charts: %v\n%s", err, chartsBody)
	}
	if charts.Range != "" {
		t.Fatalf("dashboard chart default range = %q", charts.Range)
	}
	if len(charts.TokenCumulative.Datasets) == 0 {
		t.Fatalf("dashboard token chart missing datasets: %#v", charts.TokenCumulative)
	}
	if charts.TokenCumulative.Summary.TotalTokens != 51 {
		t.Fatalf("dashboard token summary = %#v", charts.TokenCumulative.Summary)
	}
	if got := modelSeriesSum(charts.TokenCumulative.Datasets, "gpt-4.1"); got != 51 {
		t.Fatalf("gpt-4.1 token volume = %v in %#v", got, charts.TokenCumulative.Datasets)
	}
	if got := metricSeriesTotal(charts.ModelActivity.Metrics["tool_calls"].Datasets); got != 1 {
		t.Fatalf("dashboard tool call metric total = %v in %#v", got, charts.ModelActivity.Metrics["tool_calls"].Datasets)
	}

	tokensBody := recordAPIResponse(t, api.GetTokensPerMinute, "/api/tokens-per-minute")
	var perMinute []APITokensPerMinute
	if err := json.Unmarshal([]byte(tokensBody), &perMinute); err != nil {
		t.Fatalf("decode tokens per minute: %v\n%s", err, tokensBody)
	}
	var tokenSum int64
	var callSum int
	for _, point := range perMinute {
		tokenSum += point.TotalTokens
		callSum += point.CallCount
	}
	if tokenSum != 51 || callSum != 3 {
		t.Fatalf("tokens per minute sum = tokens %d calls %d", tokenSum, callSum)
	}

	toolBody := recordAPIResponse(t, api.GetToolStats, "/api/tool-stats")
	var tools []APIToolStats
	if err := json.Unmarshal([]byte(toolBody), &tools); err != nil {
		t.Fatalf("decode tool stats: %v\n%s", err, toolBody)
	}
	if len(tools) != 1 || tools[0].ToolName != "mcp__filesystem__read_file" || tools[0].Calls != 1 || tools[0].Total != 1 || tools[0].AvgDurationMs != 100 || !tools[0].IsMCP {
		t.Fatalf("tool stats = %#v", tools)
	}

	modelBody := recordAPIResponse(t, api.GetTokensByModel, "/api/tokens-by-model")
	var models []APITokensByModel
	if err := json.Unmarshal([]byte(modelBody), &models); err != nil {
		t.Fatalf("decode tokens by model: %v\n%s", err, modelBody)
	}
	if len(models) != 1 || models[0].Model != "gpt-4.1" || models[0].TotalTokens != 45 || models[0].CallCount != 2 {
		t.Fatalf("tokens by model = %#v", models)
	}
}

func TestQuerySessionDetailKeepsUnattributedModelTokensSeparate(t *testing.T) {
	ch := setupLiveWebStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	sessionID := "session-detail-model-fallback"
	events := []models.Event{
		liveEvent("detail-model-a", sessionID, "message", "assistant", now, "openai", "gpt-4.1", "", 10, 20, 0),
		liveEvent("detail-model-b", sessionID, "message", "assistant", now.Add(time.Second), "openai", "gpt-5", "", 7, 8, 0),
		liveEvent("detail-unattributed", sessionID, "tool_call", "assistant", now.Add(2*time.Second), "openai", "", "shell", 3, 4, 0),
		liveEvent("detail-end", sessionID, "session_end", "system", now.Add(3*time.Second), "openai", "", "", 0, 0, 0),
	}
	events[len(events)-1].PayloadType = "last-prompt"
	for i := range events {
		events[i].SourceLineNo = i + 1
		events[i].SourceOffset = int64(i * 10)
	}

	batch := store.RowBatch{ActivityEvents: events}
	for _, event := range events {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}

	detail, err := QuerySessionDetail(context.Background(), ch.DB, sessionID)
	if err != nil {
		t.Fatalf("query session detail: %v", err)
	}
	if detail.Session.ActiveModel != "gpt-5" {
		t.Fatalf("last model = %q, want gpt-5", detail.Session.ActiveModel)
	}
	if got := modelTokenTotal(detail.TokensByModel, "gpt-4.1"); got != 30 {
		t.Fatalf("gpt-4.1 total = %d in %#v", got, detail.TokensByModel)
	}
	if got := modelTokenTotal(detail.TokensByModel, "gpt-5"); got != 15 {
		t.Fatalf("gpt-5 total = %d in %#v", got, detail.TokensByModel)
	}
	if got := modelTokenTotal(detail.TokensByModel, "unknown"); got != 7 {
		t.Fatalf("unknown total = %d in %#v", got, detail.TokensByModel)
	}
}

func TestDashboardChartsAttributeBlankModelsFromSessionTimeline(t *testing.T) {
	ch := setupLiveWebStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	sessionID := "dashboard-single-model-fallback"
	mixedSessionID := "dashboard-mixed-model-no-fallback"
	emptySessionID := "dashboard-empty-no-model"
	events := []models.Event{
		liveEvent("single-context", sessionID, "turn_context", "system", now, "openai", "gpt-5.5", "", 0, 0, 0),
		liveEvent("single-token-count", sessionID, "event_msg", "assistant", now.Add(time.Second), "openai", "", "", 12, 5, 0),
		liveEvent("mixed-first-model", mixedSessionID, "message", "assistant", now.Add(2*time.Second), "openai", "gpt-4.1", "", 2, 3, 0),
		liveEvent("mixed-second-model", mixedSessionID, "message", "assistant", now.Add(3*time.Second), "openai", "gpt-5", "", 4, 6, 0),
		liveEvent("mixed-blank-model", mixedSessionID, "event_msg", "assistant", now.Add(4*time.Second), "openai", "", "", 3, 4, 0),
		liveEvent("empty-user", emptySessionID, "message", "user", now.Add(5*time.Second), "openai", "", "", 0, 0, 0),
		liveEvent("empty-tool", emptySessionID, "tool_call", "assistant", now.Add(6*time.Second), "openai", "", "shell", 0, 0, 100),
		liveEvent("empty-end", emptySessionID, "session_end", "system", now.Add(7*time.Second), "openai", "", "", 0, 0, 0),
	}
	events[1].PayloadType = "token_count"
	events[4].PayloadType = "token_count"
	events[len(events)-1].PayloadType = "last-prompt"
	for i := range events {
		events[i].SourceLineNo = i + 1
		events[i].SourceOffset = int64(i * 10)
	}

	batch := store.RowBatch{ActivityEvents: events}
	for _, event := range events {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}

	tokens, activity := QueryDashboardModelAnalytics(context.Background(), ch.DB, nil, "")
	if got := modelSeriesSum(tokens.Datasets, "gpt-5.5"); got != 17 {
		t.Fatalf("gpt-5.5 token volume = %v in %#v", got, tokens.Datasets)
	}
	if got := modelSeriesSum(tokens.Datasets, "gpt-4.1"); got != 5 {
		t.Fatalf("gpt-4.1 token volume = %v in %#v", got, tokens.Datasets)
	}
	if got := modelSeriesSum(tokens.Datasets, "gpt-5"); got != 17 {
		t.Fatalf("gpt-5 token volume = %v in %#v", got, tokens.Datasets)
	}
	if got := modelSeriesSum(tokens.Datasets, "unknown"); got != 0 {
		t.Fatalf("unknown token volume = %v in %#v", got, tokens.Datasets)
	}
	if hasModelSeries(tokens.Datasets, "unknown") {
		t.Fatalf("dashboard token chart contains unknown model from empty session: %#v", tokens.Datasets)
	}
	for metricName, metric := range activity.Metrics {
		if hasModelSeries(metric.Datasets, "unknown") {
			t.Fatalf("dashboard %s chart contains unknown model from empty session: %#v", metricName, metric.Datasets)
		}
	}
	if got := metricSeriesTotal(activity.Metrics["input_tokens"].Datasets); got != 21 {
		t.Fatalf("input token model activity = %v, want 21 in %#v", got, activity.Metrics["input_tokens"].Datasets)
	}
	if got := metricSeriesTotal(activity.Metrics["output_tokens"].Datasets); got != 18 {
		t.Fatalf("output token model activity = %v, want 18 in %#v", got, activity.Metrics["output_tokens"].Datasets)
	}
	if got := metricSeriesTotal(activity.Metrics["tool_calls"].Datasets); got != 0 {
		t.Fatalf("unattributed empty-session tool calls leaked into model activity: %v in %#v", got, activity.Metrics["tool_calls"].Datasets)
	}
}

func TestRecentActivityProjectScopeFiltersBeforeCandidateLimit(t *testing.T) {
	ch := setupLiveWebStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	inScopeID := "activity-project-in-scope"
	outScopeID := "activity-project-out-scope"
	events := []models.Event{
		liveEvent("activity-in-meta", inScopeID, "session_meta", "system", now.Add(-time.Hour), "openai", "", "", 0, 0, 0),
		liveEvent("activity-in-message", inScopeID, "message", "assistant", now.Add(-30*time.Minute), "openai", "gpt-5", "", 1, 1, 0),
		liveEvent("activity-out-meta", outScopeID, "session_meta", "system", now.Add(-time.Hour), "openai", "", "", 0, 0, 0),
	}
	events[0].CWD = "/Users/example/projects/beacon"
	events[1].TextPreview = "scoped project activity"
	events[2].CWD = "/Users/example/projects/other"
	for i := 0; i < recentActivityCandidates+5; i++ {
		uid := fmt.Sprintf("activity-out-message-%04d", i)
		event := liveEvent(uid, outScopeID, "message", "assistant", now.Add(time.Duration(i)*time.Second), "openai", "gpt-5", "", 1, 1, 0)
		event.TextPreview = "newer out of scope activity"
		events = append(events, event)
	}
	for i := range events {
		events[i].SourceLineNo = i + 1
		events[i].SourceOffset = int64(i * 10)
	}

	batch := store.RowBatch{ActivityEvents: events}
	for _, event := range events {
		batch.RawRecords = append(batch.RawRecords, store.NewRawRecord(event))
	}
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("flush: %v", err)
	}

	items := QueryRecentActivityFilteredByKindScoped(context.Background(), ch.DB, nil, []string{"message"}, APIScopeFilters{ProjectKeys: []string{"beacon"}})
	if len(items) == 0 {
		t.Fatalf("expected scoped project activity despite newer out-of-scope candidates")
	}
	if items[0].ID != "activity-in-message" || items[0].SessionID != inScopeID {
		t.Fatalf("activity = %#v, want in-scope message first", items)
	}
	for _, item := range items {
		if item.SessionID == outScopeID {
			t.Fatalf("out-of-scope activity leaked into project-scoped result: %#v", items)
		}
	}
}

func TestRecentActivityProjectScopeUsesLatestReplayedEvent(t *testing.T) {
	ch := setupLiveWebStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	sessionID := "activity-project-replay"
	event := liveEvent("activity-project-replayed", sessionID, "message", "assistant", now, "openai", "gpt-5", "", 1, 1, 0)
	event.CWD = "/Users/example/projects/beacon"
	event.TextPreview = "replayed project activity"
	if err := ch.Flush(context.Background(), store.RowBatch{
		ActivityEvents: []models.Event{event},
		RawRecords:     []models.RawRecord{store.NewRawRecord(event)},
	}); err != nil {
		t.Fatalf("initial flush: %v", err)
	}

	time.Sleep(5 * time.Millisecond)
	replayed := event
	replayed.CWD = "/Users/example/projects/other"
	replayed.SourceOffset = 10
	if err := ch.Flush(context.Background(), store.RowBatch{
		ActivityEvents: []models.Event{replayed},
		RawRecords:     []models.RawRecord{store.NewRawRecord(replayed)},
	}); err != nil {
		t.Fatalf("replay flush: %v", err)
	}

	items := QueryRecentActivityFilteredByKindScoped(context.Background(), ch.DB, nil, []string{"message"}, APIScopeFilters{ProjectKeys: []string{"beacon"}})
	for _, item := range items {
		if item.ID == event.EventUID {
			t.Fatalf("stale beacon-scoped activity leaked after replay: %#v", items)
		}
	}
	items = QueryRecentActivityFilteredByKindScoped(context.Background(), ch.DB, nil, []string{"message"}, APIScopeFilters{ProjectKeys: []string{"other"}})
	if len(items) == 0 || items[0].ID != event.EventUID {
		t.Fatalf("other-scoped replayed activity = %#v, want %s first", items, event.EventUID)
	}
}

func liveEvent(uid, sessionID, kind, role string, ts time.Time, provider, model, tool string, input, output, duration int64) models.Event {
	return models.Event{
		EventUID:     uid,
		SessionID:    sessionID,
		SourceName:   "test-source",
		Runtime:      "test-runtime",
		Provider:     provider,
		Format:       "jsonl",
		EventKind:    kind,
		ActorRole:    role,
		Timestamp:    ts,
		TextContent:  uid + " text",
		TextPreview:  uid + " text",
		ToolName:     tool,
		Model:        model,
		InputTokens:  input,
		OutputTokens: output,
		DurationMs:   duration,
		EventVersion: 1,
		PayloadJSON:  `{"event":"` + uid + `"}`,
		SourceFile:   "dashboard-live.jsonl",
		SourceLineNo: 1,
		SourceOffset: 1,
		CreatedAt:    ts,
	}
}

func modelTokenTotal(items []views.ModelTokens, model string) int64 {
	for _, item := range items {
		if item.Model == model {
			return item.Total
		}
	}
	return 0
}

func containsSession(items []APISessionSummary, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func assertMetric(t *testing.T, metrics []APIMetricData, label string, want float64) {
	t.Helper()
	for _, metric := range metrics {
		if metric.Label == label {
			if metric.Value != want {
				t.Fatalf("%s = %v, want %v", label, metric.Value, want)
			}
			return
		}
	}
	t.Fatalf("missing metric %q in %#v", label, metrics)
}

func sumFloat64(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total
}

func modelSeriesSum(datasets []views.ModelSeriesDataset, model string) float64 {
	for _, dataset := range datasets {
		if dataset.Model == model {
			return sumFloat64(dataset.Values)
		}
	}
	return 0
}

func hasModelSeries(datasets []views.ModelSeriesDataset, model string) bool {
	for _, dataset := range datasets {
		if dataset.Model == model {
			return true
		}
	}
	return false
}

func metricSeriesTotal(datasets []views.ModelSeriesDataset) float64 {
	var total float64
	for _, dataset := range datasets {
		total += sumFloat64(dataset.Values)
	}
	return total
}

func recordAPIResponse(t *testing.T, handler http.HandlerFunc, target string, routeParams ...string) string {
	t.Helper()
	rec := recordAPI(t, handler, target, routeParams...)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func recordAPIStatus(t *testing.T, handler http.HandlerFunc, target string, want int, routeParams ...string) string {
	t.Helper()
	rec := recordAPI(t, handler, target, routeParams...)
	if rec.Code != want {
		t.Fatalf("%s returned %d, want %d: %s", target, rec.Code, want, rec.Body.String())
	}
	return rec.Body.String()
}

func recordAPI(t *testing.T, handler http.HandlerFunc, target string, routeParams ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if len(routeParams)%2 != 0 {
		t.Fatalf("route params must be key/value pairs")
	}
	if len(routeParams) > 0 {
		rctx := chi.NewRouteContext()
		for i := 0; i < len(routeParams); i += 2 {
			rctx.URLParams.Add(routeParams[i], routeParams[i+1])
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}
