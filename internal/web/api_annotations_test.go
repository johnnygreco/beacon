package web

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/johnnygreco/beacon/internal/models"
)

func TestAnnotationAPICreateListUpdateDeleteSessionAnnotation(t *testing.T) {
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{}
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	create := `{
		"target_type":"session",
		"session_id":"session-1",
		"author_type":"human",
		"author_name":"Reviewer",
		"source":"ui",
		"category":"quality",
		"outcome":"useful",
		"quality_score":4,
		"confidence":80,
		"needs_followup":true,
		"labels":["regression","quality:good","regression"],
		"note":"Strong recovery from a tool error.",
		"metadata_json":"{\"rubric\":\"qa\"}"
	}`
	w := httptest.NewRecorder()
	handlers.CreateAnnotation(w, annotationAPIRequest(http.MethodPost, "/api/annotations", create))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created APITraceAnnotation
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.AnnotationID == "" || created.TargetType != "session" || created.SessionID != "session-1" || created.EventUID != "" {
		t.Fatalf("created annotation target = %#v", created)
	}
	if strings.Join(created.Labels, ",") != "quality:good,regression" {
		t.Fatalf("created labels = %#v", created.Labels)
	}
	if !created.NeedsFollowup || created.SchemaVersion != models.AnnotationSchemaVersion {
		t.Fatalf("created followup/schema = %v/%d", created.NeedsFollowup, created.SchemaVersion)
	}

	update := `{"note":"Updated note","labels":["dataset:train"],"quality_score":5,"needs_followup":false}`
	w = httptest.NewRecorder()
	handlers.UpdateAnnotation(w, annotationAPIRequest(http.MethodPatch, "/api/annotations/"+created.AnnotationID, update, "annotation_id", created.AnnotationID))
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", w.Code, w.Body.String())
	}
	var updated APITraceAnnotation
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Note != "Updated note" || updated.QualityScore != 5 || updated.NeedsFollowup || strings.Join(updated.Labels, ",") != "dataset:train" {
		t.Fatalf("updated annotation = %#v", updated)
	}

	w = httptest.NewRecorder()
	handlers.GetSessionAnnotations(w, annotationAPIRequest(http.MethodGet, "/api/sessions/session-1/annotations", "", "id", "session-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", w.Code, w.Body.String())
	}
	var list APITraceAnnotationListResponse
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].AnnotationID != created.AnnotationID || list.Items[0].Note != "Updated note" {
		t.Fatalf("list items = %#v", list.Items)
	}
}

func TestAnnotationAPIEventAnnotationResolvesSessionAndDeleteFilters(t *testing.T) {
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{}
	fake.events["event-1"] = annotationAPIEvent{sessionID: "session-1"}
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	w := httptest.NewRecorder()
	handlers.CreateAnnotation(w, annotationAPIRequest(http.MethodPost, "/api/annotations", `{
		"target_type":"event",
		"event_uid":"event-1",
		"author_type":"agent",
		"source":"api",
		"labels":["dataset:eval"],
		"note":"This event shows a concise correction."
	}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create event status = %d body=%s", w.Code, w.Body.String())
	}
	var created APITraceAnnotation
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode event create: %v", err)
	}
	if created.SessionID != "session-1" || created.EventUID != "event-1" || created.AuthorType != "agent" {
		t.Fatalf("event annotation = %#v", created)
	}

	w = httptest.NewRecorder()
	handlers.DeleteAnnotation(w, annotationAPIRequest(http.MethodDelete, "/api/annotations/"+created.AnnotationID, "", "annotation_id", created.AnnotationID))
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", w.Code, w.Body.String())
	}
	var deleted APITraceAnnotation
	if err := json.NewDecoder(w.Body).Decode(&deleted); err != nil {
		t.Fatalf("decode delete: %v", err)
	}
	if deleted.Status != models.AnnotationStatusDeleted || deleted.DeletedAt == nil {
		t.Fatalf("deleted annotation = %#v", deleted)
	}

	w = httptest.NewRecorder()
	handlers.GetSessionAnnotations(w, annotationAPIRequest(http.MethodGet, "/api/sessions/session-1/annotations", "", "id", "session-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("list active status = %d body=%s", w.Code, w.Body.String())
	}
	var activeList APITraceAnnotationListResponse
	if err := json.NewDecoder(w.Body).Decode(&activeList); err != nil {
		t.Fatalf("decode active list: %v", err)
	}
	if len(activeList.Items) != 0 {
		t.Fatalf("active list = %#v, want deleted annotation filtered", activeList.Items)
	}

	w = httptest.NewRecorder()
	handlers.GetSessionAnnotations(w, annotationAPIRequest(http.MethodGet, "/api/sessions/session-1/annotations?include_deleted=1", "", "id", "session-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("list deleted status = %d body=%s", w.Code, w.Body.String())
	}
	var deletedList APITraceAnnotationListResponse
	if err := json.NewDecoder(w.Body).Decode(&deletedList); err != nil {
		t.Fatalf("decode deleted list: %v", err)
	}
	if len(deletedList.Items) != 1 || deletedList.Items[0].Status != models.AnnotationStatusDeleted {
		t.Fatalf("deleted list = %#v", deletedList.Items)
	}
}

func TestAnnotationAPIMessageAnnotationTargetsMessageEvents(t *testing.T) {
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{}
	fake.events["message-1"] = annotationAPIEvent{sessionID: "session-1", eventKind: "message"}
	fake.events["tool-1"] = annotationAPIEvent{sessionID: "session-1", eventKind: "tool_call"}
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	w := httptest.NewRecorder()
	handlers.CreateAnnotation(w, annotationAPIRequest(http.MethodPost, "/api/annotations", `{
		"target_type":"message",
		"event_uid":"message-1",
		"author_type":"agent",
		"source":"mcp",
		"note":"Message-level finding."
	}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create message status = %d body=%s", w.Code, w.Body.String())
	}
	var created APITraceAnnotation
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode message create: %v", err)
	}
	if created.TargetType != models.AnnotationTargetMessage || created.SessionID != "session-1" || created.EventUID != "message-1" {
		t.Fatalf("message annotation = %#v", created)
	}

	w = httptest.NewRecorder()
	handlers.GetEventAnnotations(w, annotationAPIRequest(http.MethodGet, "/api/events/message-1/annotations", "", "event_id", "message-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("event annotations status = %d body=%s", w.Code, w.Body.String())
	}
	var list APITraceAnnotationListResponse
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode event annotations: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].AnnotationID != created.AnnotationID {
		t.Fatalf("event annotations = %#v", list.Items)
	}

	w = httptest.NewRecorder()
	handlers.CreateAnnotation(w, annotationAPIRequest(http.MethodPost, "/api/annotations", `{
		"target_type":"message",
		"event_uid":"tool-1",
		"note":"Should not attach to a tool event."
	}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-message target status = %d body=%s", w.Code, w.Body.String())
	}
	assertAPIError(t, w.Body.String(), "annotation target not found")
}

func TestAnnotationAPIValidationAndMissingTargets(t *testing.T) {
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{}
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	tests := []struct {
		name   string
		body   string
		status int
		err    string
	}{
		{
			name:   "invalid metadata",
			body:   `{"target_type":"session","session_id":"session-1","note":"bad metadata","metadata_json":"[]"}`,
			status: http.StatusBadRequest,
			err:    "metadata_json must be a JSON object",
		},
		{
			name:   "unknown target",
			body:   `{"target_type":"session","session_id":"missing","note":"missing target"}`,
			status: http.StatusNotFound,
			err:    "annotation target not found",
		},
		{
			name:   "unknown field",
			body:   `{"target_type":"session","session_id":"session-1","note":"ok","legacy_field":true}`,
			status: http.StatusBadRequest,
			err:    "invalid JSON body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handlers.CreateAnnotation(w, annotationAPIRequest(http.MethodPost, "/api/annotations", tt.body))
			if w.Code != tt.status {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
			assertAPIError(t, w.Body.String(), tt.err)
		})
	}
}

func TestAnnotationAPIListByAnnotationIDAppliesScope(t *testing.T) {
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{sourceName: "source-a"}
	fake.events["event-1"] = annotationAPIEvent{sessionID: "session-1", sourceName: "source-a"}
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	w := httptest.NewRecorder()
	handlers.CreateAnnotation(w, annotationAPIRequest(http.MethodPost, "/api/annotations", `{
		"target_type":"event",
		"event_uid":"event-1",
		"author_type":"agent",
		"source":"api",
		"note":"scoped event annotation"
	}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created APITraceAnnotation
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	req := annotationAPIRequest(http.MethodGet, "/api/annotations?annotation_id="+created.AnnotationID, "")
	req = req.WithContext(ContextWithAPIScope(req.Context(), APIScopeFilters{SourceNames: []string{"source-b"}}))
	w = httptest.NewRecorder()
	handlers.ListAnnotations(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", w.Code, w.Body.String())
	}
	var out APITraceAnnotationListResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("out-of-scope annotation_id list returned %#v", out.Items)
	}
}

func TestAnnotationAPIAnnotatedTracesListsSessionsAndTargets(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{sourceName: "source-a", runtime: "runtime-a", startedAt: now.Add(-time.Hour), endedAt: now}
	fake.events["message-1"] = annotationAPIEvent{sessionID: "session-1", eventKind: "message", sourceName: "source-a", runtime: "runtime-a", timestamp: now.Add(-time.Minute), textPreview: "user asks for help"}
	fake.annotations["ann-session"] = testTraceAnnotation("ann-session", models.AnnotationTargetSession, "session-1", "", now, "session note", []string{"dataset:eval"})
	fake.annotations["ann-message"] = testTraceAnnotation("ann-message", models.AnnotationTargetMessage, "session-1", "message-1", now.Add(time.Second), "message note", []string{"dataset:eval"})
	hidden := testTraceAnnotation("ann-hidden", models.AnnotationTargetMessage, "session-1", "hidden-message", now.Add(2*time.Second), "hidden note", []string{"dataset:eval"})
	fake.annotations[hidden.AnnotationID] = hidden
	fake.events["hidden-message"] = annotationAPIEvent{sessionID: "session-1", eventKind: "message", sourceName: "source-b", runtime: "runtime-a", timestamp: now}
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	req := annotationAPIRequest(http.MethodGet, "/api/annotations/traces?label=dataset:eval&source_name=source-a", "")
	w := httptest.NewRecorder()
	handlers.ListAnnotatedTraces(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("annotated traces status = %d body=%s", w.Code, w.Body.String())
	}
	var got APIAnnotatedTracesResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode annotated traces: %v", err)
	}
	if got.Schema != annotatedTraceIndexSchema || len(got.Items) != 1 {
		t.Fatalf("annotated traces response = %#v", got)
	}
	item := got.Items[0]
	if item.Session.ID != "session-1" || item.Counts.AnnotationCount != 2 || item.Counts.SessionAnnotationCount != 1 || item.Counts.MessageAnnotationCount != 1 {
		t.Fatalf("annotated trace item = %#v", item)
	}
	if len(item.Targets) != 2 {
		t.Fatalf("targets = %#v, want session and visible message targets", item.Targets)
	}
	for _, target := range item.Targets {
		if target.EventUID == "hidden-message" {
			t.Fatalf("out-of-scope target leaked: %#v", item.Targets)
		}
	}
}

func TestAnnotationAPIAnnotatedTracesOrdersByVisibleScopedAnnotations(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	fake := newAnnotationAPIFake()
	fake.sessions["session-a"] = annotationAPISession{sourceName: "source-a", runtime: "runtime-a", startedAt: now.Add(-2 * time.Hour), endedAt: now}
	fake.sessions["session-b"] = annotationAPISession{sourceName: "source-a", runtime: "runtime-a", startedAt: now.Add(-time.Hour), endedAt: now}
	fake.events["hidden-message"] = annotationAPIEvent{sessionID: "session-a", eventKind: "message", sourceName: "source-b", runtime: "runtime-a", timestamp: now}
	fake.annotations["ann-a-visible"] = testTraceAnnotation("ann-a-visible", models.AnnotationTargetSession, "session-a", "", now.Add(time.Second), "older visible note", []string{"dataset:eval"})
	fake.annotations["ann-b-visible"] = testTraceAnnotation("ann-b-visible", models.AnnotationTargetSession, "session-b", "", now.Add(5*time.Second), "newer visible note", []string{"dataset:eval"})
	fake.annotations["ann-a-hidden"] = testTraceAnnotation("ann-a-hidden", models.AnnotationTargetMessage, "session-a", "hidden-message", now.Add(10*time.Second), "newest hidden note", []string{"dataset:eval"})
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	w := httptest.NewRecorder()
	handlers.ListAnnotatedTraces(w, annotationAPIRequest(http.MethodGet, "/api/annotations/traces?source_name=source-a&label=dataset:eval&limit=1", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("annotated traces status = %d body=%s", w.Code, w.Body.String())
	}
	var firstPage APIAnnotatedTracesResponse
	if err := json.NewDecoder(w.Body).Decode(&firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.Items[0].Session.ID != "session-b" || !firstPage.HasMore {
		t.Fatalf("first page = %#v, want session-b first with more results", firstPage)
	}

	w = httptest.NewRecorder()
	handlers.ListAnnotatedTraces(w, annotationAPIRequest(http.MethodGet, "/api/annotations/traces?source_name=source-a&label=dataset:eval&limit=1&offset=1", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("annotated traces second page status = %d body=%s", w.Code, w.Body.String())
	}
	var secondPage APIAnnotatedTracesResponse
	if err := json.NewDecoder(w.Body).Decode(&secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].Session.ID != "session-a" || secondPage.HasMore {
		t.Fatalf("second page = %#v, want session-a without hidden target influence", secondPage)
	}
}

func TestAnnotationAPIExportAnnotatedTracesIncludesContextAndDeleted(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{sourceName: "source-a", runtime: "runtime-a", startedAt: now.Add(-time.Hour), endedAt: now}
	fake.events["message-1"] = annotationAPIEvent{sessionID: "session-1", eventKind: "message", sourceName: "source-a", runtime: "runtime-a", timestamp: now.Add(-2 * time.Minute), textPreview: "first message", actorRole: "user", model: "gpt-test", tokens: 12}
	fake.events["event-1"] = annotationAPIEvent{sessionID: "session-1", eventKind: "tool_call", sourceName: "source-a", runtime: "runtime-a", timestamp: now.Add(-time.Minute), textPreview: "tool call", toolName: "shell", tokens: 3}
	active := testTraceAnnotation("ann-session", models.AnnotationTargetSession, "session-1", "", now, "session note", []string{"dataset:eval"})
	deleted := testTraceAnnotation("ann-event", models.AnnotationTargetEvent, "session-1", "event-1", now.Add(time.Second), "deleted event note", []string{"dataset:eval"})
	deleted.Status = models.AnnotationStatusDeleted
	deletedAt := now.Add(2 * time.Second)
	deleted.DeletedAt = &deletedAt
	fake.annotations[active.AnnotationID] = active
	fake.annotations[deleted.AnnotationID] = deleted
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	w := httptest.NewRecorder()
	handlers.ExportAnnotatedTraces(w, annotationAPIRequest(http.MethodGet, "/api/annotations/export?session_id=session-1&include_deleted=1&event_limit=1", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", w.Code, w.Body.String())
	}
	var got APIAnnotatedTraceExportResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if got.Schema != annotatedTraceExportSchema || len(got.Traces) != 1 {
		t.Fatalf("export response = %#v", got)
	}
	trace := got.Traces[0]
	if trace.Session.ID != "session-1" || trace.Counts.AnnotationCount != 2 || trace.Counts.EventAnnotationCount != 1 {
		t.Fatalf("export trace counts = %#v", trace)
	}
	if len(trace.Annotations) != 2 || trace.Annotations[1].Status != models.AnnotationStatusDeleted {
		t.Fatalf("export annotations = %#v", trace.Annotations)
	}
	if len(trace.Events) != 1 || trace.Events[0].EventUID != "message-1" || !trace.EventTruncated || len(got.Warnings) != 1 {
		t.Fatalf("export events/truncation = events:%#v truncated:%v warnings:%#v", trace.Events, trace.EventTruncated, got.Warnings)
	}
}

func TestAnnotationAPIExportAnnotatedTracesPaginatesAnnotations(t *testing.T) {
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = annotationAPISession{sourceName: "source-a", runtime: "runtime-a", startedAt: now.Add(-time.Hour), endedAt: now}
	for i := 0; i < maxAnnotationsAPILimit+5; i++ {
		id := fmt.Sprintf("ann-%03d", i)
		fake.annotations[id] = testTraceAnnotation(id, models.AnnotationTargetSession, "session-1", "", now.Add(time.Duration(i)*time.Second), "session note", []string{"dataset:eval"})
	}
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, fake), logger: testLogger()}

	w := httptest.NewRecorder()
	handlers.ExportAnnotatedTraces(w, annotationAPIRequest(http.MethodGet, "/api/annotations/export?session_id=session-1&label=dataset:eval", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", w.Code, w.Body.String())
	}
	var got APIAnnotatedTraceExportResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if len(got.Traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(got.Traces))
	}
	trace := got.Traces[0]
	if trace.Counts.AnnotationCount != maxAnnotationsAPILimit+5 || len(trace.Annotations) != maxAnnotationsAPILimit+5 {
		t.Fatalf("paginated annotations = count:%d len:%d, want %d", trace.Counts.AnnotationCount, len(trace.Annotations), maxAnnotationsAPILimit+5)
	}
}

func TestAnnotationAPIAnnotatedTracesEmptyResults(t *testing.T) {
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, newAnnotationAPIFake()), logger: testLogger()}
	w := httptest.NewRecorder()
	handlers.ListAnnotatedTraces(w, annotationAPIRequest(http.MethodGet, "/api/annotations/traces", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("empty traces status = %d body=%s", w.Code, w.Body.String())
	}
	var got APIAnnotatedTracesResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode empty traces: %v", err)
	}
	if got.Schema != annotatedTraceIndexSchema || len(got.Items) != 0 {
		t.Fatalf("empty traces = %#v", got)
	}
}

func TestAnnotationAPIAnnotatedTracesCapsOffset(t *testing.T) {
	handlers := &APIHandlers{db: newAnnotationAPIDB(t, newAnnotationAPIFake()), logger: testLogger()}
	w := httptest.NewRecorder()
	handlers.ListAnnotatedTraces(w, annotationAPIRequest(http.MethodGet, "/api/annotations/traces?offset=999999999", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("large offset status = %d body=%s", w.Code, w.Body.String())
	}
	var got APIAnnotatedTracesResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode large offset: %v", err)
	}
	if got.Offset != maxAnnotatedTracesOffset || len(got.Items) != 0 || got.HasMore {
		t.Fatalf("large offset response = %#v", got)
	}
}

func annotationAPIRequest(method, target, body string, routeParams ...string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if len(routeParams) == 0 {
		return req
	}
	rctx := chi.NewRouteContext()
	for i := 0; i < len(routeParams); i += 2 {
		rctx.URLParams.Add(routeParams[i], routeParams[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func testTraceAnnotation(id, targetType, sessionID, eventUID string, at time.Time, note string, labels []string) models.TraceAnnotation {
	return models.NormalizeTraceAnnotation(models.TraceAnnotation{
		AnnotationID:  id,
		Revision:      1,
		TargetType:    targetType,
		SessionID:     sessionID,
		EventUID:      eventUID,
		AuthorType:    models.AnnotationAuthorAgent,
		Source:        models.AnnotationSourceMCP,
		Labels:        labels,
		Note:          note,
		Status:        models.AnnotationStatusActive,
		SchemaVersion: models.AnnotationSchemaVersion,
		CreatedAt:     at,
		UpdatedAt:     at,
	})
}

type annotationAPIFake struct {
	sessions    map[string]annotationAPISession
	events      map[string]annotationAPIEvent
	annotations map[string]models.TraceAnnotation
}

type annotationAPISession struct {
	sourceName string
	runtime    string
	provider   string
	startedAt  time.Time
	endedAt    time.Time
}

type annotationAPIEvent struct {
	sessionID     string
	eventKind     string
	payloadType   string
	actorRole     string
	sourceName    string
	runtime       string
	timestamp     time.Time
	textPreview   string
	toolName      string
	toolUseID     string
	model         string
	tokens        int64
	durationMs    int64
	inputPreview  string
	outputPreview string
}

func newAnnotationAPIFake() *annotationAPIFake {
	return &annotationAPIFake{
		sessions:    map[string]annotationAPISession{},
		events:      map[string]annotationAPIEvent{},
		annotations: map[string]models.TraceAnnotation{},
	}
}

var (
	registerAnnotationAPIDriver sync.Once
	annotationAPIDriverMu       sync.Mutex
	annotationAPICurrentFake    *annotationAPIFake
)

func newAnnotationAPIDB(t *testing.T, fake *annotationAPIFake) *sql.DB {
	t.Helper()
	registerAnnotationAPIDriver.Do(func() {
		sql.Register("beacon_api_annotations", annotationAPIDriver{})
	})
	annotationAPIDriverMu.Lock()
	annotationAPICurrentFake = fake
	annotationAPIDriverMu.Unlock()
	db, err := sql.Open("beacon_api_annotations", "")
	if err != nil {
		t.Fatalf("open annotation api db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type annotationAPIDriver struct{}

func (annotationAPIDriver) Open(string) (driver.Conn, error) {
	annotationAPIDriverMu.Lock()
	defer annotationAPIDriverMu.Unlock()
	return annotationAPIConn{fake: annotationAPICurrentFake}, nil
}

type annotationAPIConn struct {
	fake *annotationAPIFake
}

func (c annotationAPIConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("annotation api driver does not prepare statements")
}

func (c annotationAPIConn) Close() error {
	return nil
}

func (c annotationAPIConn) Begin() (driver.Tx, error) {
	return nil, errors.New("annotation api driver does not support transactions")
}

func (c annotationAPIConn) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (c annotationAPIConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "INSERT INTO trace_annotations") {
		return nil, errors.New("unexpected exec query")
	}
	values := namedValues(args)
	annotation := models.TraceAnnotation{
		AnnotationID:  values.stringAt(0),
		Revision:      uint64(values.intAt(1)),
		TargetType:    values.stringAt(2),
		SessionID:     values.stringAt(3),
		EventUID:      values.stringAt(4),
		AuthorType:    values.stringAt(5),
		AuthorID:      values.stringAt(6),
		AuthorName:    values.stringAt(7),
		Source:        values.stringAt(8),
		Category:      values.stringAt(9),
		Outcome:       values.stringAt(10),
		QualityScore:  values.intAt(11),
		Confidence:    values.intAt(12),
		NeedsFollowup: values.intAt(13) != 0,
		Labels:        values.labelsAt(14),
		Note:          values.stringAt(15),
		MetadataJSON:  values.stringAt(16),
		Status:        values.stringAt(17),
		SchemaVersion: values.intAt(18),
		CreatedAt:     values.timeAt(19),
		UpdatedAt:     values.timeAt(20),
	}
	deletedAt := values.timeAt(21)
	if !deletedAt.IsZero() && !deletedAt.Equal(time.Unix(0, 0).UTC()) {
		annotation.DeletedAt = &deletedAt
	}
	c.fake.annotations[annotation.AnnotationID] = annotation
	return driver.RowsAffected(1), nil
}

func (c annotationAPIConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	values := namedValues(args)
	switch {
	case strings.Contains(query, "trace_annotations FINAL") && strings.Contains(query, "GROUP BY session_id"):
		return c.annotationSessionSummaryRows(query, values), nil
	case strings.Contains(query, "trace_annotations FINAL"):
		return c.annotationRows(query, values), nil
	case strings.Contains(query, "session_events AS"):
		return c.sessionEventRows(query, values), nil
	case strings.Contains(query, "COALESCE(source_name") && strings.Contains(query, "session_projection"):
		return c.sessionSummaryRows(query, values), nil
	case strings.Contains(query, "session_projection"):
		sessionID := values.stringAt(0)
		session, ok := c.fake.sessions[sessionID]
		if ok && queryScopeMatches(query, values, 1, session.sourceName, session.runtime) {
			return singleColumnRows("session_id", sessionID), nil
		}
		return emptyAnnotationRows([]string{"session_id"}), nil
	case strings.Contains(query, "activity_events"):
		eventUID := values.stringAt(0)
		event := c.fake.events[eventUID]
		if event.sessionID != "" && queryEventKindMatches(query, event.eventKind) && queryScopeMatches(query, values, 1, event.sourceName, event.runtime) {
			return singleColumnRows("session_id", event.sessionID), nil
		}
		return emptyAnnotationRows([]string{"session_id"}), nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func (c annotationAPIConn) annotationSessionSummaryRows(query string, values annotationNamedValues) driver.Rows {
	annotations := c.filteredAnnotations(query, values, 3)
	type summary struct {
		sessionID     string
		count         uint64
		sessionCount  uint64
		messageCount  uint64
		eventCount    uint64
		followupCount uint64
		first         time.Time
		last          time.Time
	}
	bySession := map[string]*summary{}
	for _, annotation := range annotations {
		s := bySession[annotation.SessionID]
		if s == nil {
			s = &summary{sessionID: annotation.SessionID}
			bySession[annotation.SessionID] = s
		}
		s.count++
		switch annotation.TargetType {
		case models.AnnotationTargetSession:
			s.sessionCount++
		case models.AnnotationTargetMessage:
			s.messageCount++
		case models.AnnotationTargetEvent:
			s.eventCount++
		}
		if annotation.NeedsFollowup {
			s.followupCount++
		}
		if s.first.IsZero() || annotation.CreatedAt.Before(s.first) {
			s.first = annotation.CreatedAt
		}
		if s.last.IsZero() || annotation.UpdatedAt.After(s.last) {
			s.last = annotation.UpdatedAt
		}
	}
	summaries := make([]*summary, 0, len(bySession))
	for _, s := range bySession {
		summaries = append(summaries, s)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].last.Equal(summaries[j].last) {
			return summaries[i].sessionID < summaries[j].sessionID
		}
		return summaries[i].last.After(summaries[j].last)
	})
	rows := make([][]driver.Value, 0, len(summaries))
	for _, s := range summaries {
		rows = append(rows, []driver.Value{s.sessionID, s.count, s.sessionCount, s.messageCount, s.eventCount, s.followupCount, s.first, s.last})
	}
	rows = paginateDriverRows(rows, values)
	return &annotationRows{
		columns: []string{"session_id", "annotation_count", "session_annotation_count", "message_annotation_count", "event_annotation_count", "needs_followup_count", "first_annotation_at", "last_annotation_at"},
		rows:    rows,
	}
}

func (c annotationAPIConn) annotationRows(query string, values annotationNamedValues) driver.Rows {
	annotations := c.filteredAnnotations(query, values, 0)
	annotations = paginateTraceAnnotations(annotations, values)
	rows := make([][]driver.Value, 0, len(annotations))
	for _, annotation := range annotations {
		rows = append(rows, annotationRowValues(annotation))
	}
	return &annotationRows{
		columns: []string{"annotation_id", "revision", "target_type", "session_id", "event_uid", "author_type", "author_id", "author_name", "source", "category", "outcome", "quality_score", "confidence", "needs_followup", "labels", "note", "metadata_json", "status", "schema_version", "created_at", "updated_at", "deleted_at"},
		rows:    rows,
	}
}

func (c annotationAPIConn) filteredAnnotations(query string, values annotationNamedValues, idx int) []models.TraceAnnotation {
	var annotationID, targetType, sessionID, eventUID, excludedStatus string
	var sessionIDs []string
	var authorType, source, category, outcome, label string
	var needsFollowup *bool
	if hasAnnotationFilter(query, "annotation_id = ?") {
		annotationID = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "target_type = ?") {
		targetType = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "session_id = ?") {
		sessionID = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "session_id IN (") {
		count := placeholderCountInClause(query, "session_id IN (")
		for i := 0; i < count; i++ {
			sessionIDs = append(sessionIDs, values.stringAt(idx))
			idx++
		}
	}
	if hasAnnotationFilter(query, "event_uid = ?") {
		eventUID = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "author_type = ?") {
		authorType = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "source = ?") {
		source = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "category = ?") {
		category = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "outcome = ?") {
		outcome = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "has(labels, ?)") {
		label = values.stringAt(idx)
		idx++
	}
	if hasAnnotationFilter(query, "needs_followup = ?") {
		value := values.intAt(idx) != 0
		needsFollowup = &value
		idx++
	}
	if hasAnnotationFilter(query, "status != ?") {
		excludedStatus = values.stringAt(idx)
	}
	sessionIDSet := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		sessionIDSet[id] = struct{}{}
	}

	var annotations []models.TraceAnnotation
	for _, annotation := range c.fake.annotations {
		if annotationID != "" && annotation.AnnotationID != annotationID {
			continue
		}
		if targetType != "" && annotation.TargetType != targetType {
			continue
		}
		if sessionID != "" && annotation.SessionID != sessionID {
			continue
		}
		if len(sessionIDSet) > 0 {
			if _, ok := sessionIDSet[annotation.SessionID]; !ok {
				continue
			}
		}
		if eventUID != "" && annotation.EventUID != eventUID {
			continue
		}
		if authorType != "" && annotation.AuthorType != authorType {
			continue
		}
		if source != "" && annotation.Source != source {
			continue
		}
		if category != "" && annotation.Category != category {
			continue
		}
		if outcome != "" && annotation.Outcome != outcome {
			continue
		}
		if label != "" && !annotationHasLabel(annotation, label) {
			continue
		}
		if needsFollowup != nil && annotation.NeedsFollowup != *needsFollowup {
			continue
		}
		if excludedStatus != "" && annotation.Status == excludedStatus {
			continue
		}
		annotations = append(annotations, annotation)
	}
	sort.Slice(annotations, func(i, j int) bool {
		if annotations[i].CreatedAt.Equal(annotations[j].CreatedAt) {
			return annotations[i].AnnotationID < annotations[j].AnnotationID
		}
		return annotations[i].CreatedAt.Before(annotations[j].CreatedAt)
	})
	return annotations
}

func (c annotationAPIConn) sessionSummaryRows(query string, values annotationNamedValues) driver.Rows {
	sessionIDs := map[string]struct{}{}
	for _, value := range values {
		if id, ok := value.(string); ok {
			if _, exists := c.fake.sessions[id]; exists {
				sessionIDs[id] = struct{}{}
			}
		}
	}
	rows := make([][]driver.Value, 0, len(sessionIDs))
	for id := range sessionIDs {
		session := c.fake.sessions[id]
		if !queryScopeMatches(query, values, firstSessionSummaryScopeArg(query, values), session.sourceName, session.runtime) {
			continue
		}
		rows = append(rows, annotationAPISessionSummaryRow(id, session))
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0].(string) < rows[j][0].(string)
	})
	return &annotationRows{
		columns: []string{"session_id", "source_name", "runtime", "provider", "format", "project_key", "project_path", "started_at", "ended_at", "turn_count", "total_tokens", "total_input_tokens", "total_output_tokens", "total_cache_read_tokens", "total_cache_create_tokens", "tool_call_count", "mcp_call_count", "error_count", "last_model", "working_dir", "parent_session_id", "has_session_end", "completion_state", "total_cost_usd", "cost_event_count", "cost_provenance", "attention_score", "attention_reasons", "archive_reason", "archived_at", "reopened"},
		rows:    rows,
	}
}

func (c annotationAPIConn) sessionEventRows(_ string, values annotationNamedValues) driver.Rows {
	sessionID := values.stringAt(0)
	type keyedEvent struct {
		uid   string
		event annotationAPIEvent
	}
	var events []keyedEvent
	for eventUID, event := range c.fake.events {
		if event.sessionID == sessionID {
			events = append(events, keyedEvent{uid: eventUID, event: event})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].event.timestamp.Equal(events[j].event.timestamp) {
			return events[i].uid < events[j].uid
		}
		return events[i].event.timestamp.Before(events[j].event.timestamp)
	})
	limit := values.intAt(len(values) - 1)
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	rows := make([][]driver.Value, 0, len(events))
	for _, item := range events {
		event := item.event
		rows = append(rows, []driver.Value{
			item.uid,
			event.sessionID,
			firstNonEmpty(event.eventKind, "message"),
			event.payloadType,
			event.actorRole,
			event.timestamp,
			event.textPreview,
			event.toolName,
			event.toolUseID,
			event.model,
			event.tokens,
			event.durationMs,
			event.inputPreview,
			event.outputPreview,
		})
	}
	return &annotationRows{
		columns: []string{"event_uid", "session_id", "event_kind", "payload_type", "actor_role", "timestamp", "text_preview", "tool_name", "tool_use_id", "model", "tokens", "duration_ms", "input_preview", "output_preview"},
		rows:    rows,
	}
}

func annotationAPISessionSummaryRow(id string, session annotationAPISession) []driver.Value {
	startedAt := session.startedAt
	if startedAt.IsZero() {
		startedAt = time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	}
	endedAt := session.endedAt
	if endedAt.IsZero() {
		endedAt = startedAt.Add(time.Minute)
	}
	return []driver.Value{
		id,
		firstNonEmpty(session.sourceName, "source-a"),
		firstNonEmpty(session.runtime, "runtime-a"),
		firstNonEmpty(session.provider, "provider-a"),
		"jsonl",
		"beacon",
		"/work/beacon",
		startedAt,
		endedAt,
		int64(2),
		int64(30),
		int64(10),
		int64(20),
		int64(0),
		int64(0),
		int64(1),
		int64(0),
		int64(0),
		"gpt-test",
		"/work/beacon",
		"",
		int64(1),
		"completed",
		float64(0),
		int64(0),
		"none",
		int64(0),
		[]string{},
		"",
		time.Unix(0, 0).UTC(),
		int64(0),
	}
}

func annotationRowValues(annotation models.TraceAnnotation) []driver.Value {
	deletedAt := time.Unix(0, 0).UTC()
	if annotation.DeletedAt != nil {
		deletedAt = *annotation.DeletedAt
	}
	return []driver.Value{
		annotation.AnnotationID,
		int64(annotation.Revision),
		annotation.TargetType,
		annotation.SessionID,
		annotation.EventUID,
		annotation.AuthorType,
		annotation.AuthorID,
		annotation.AuthorName,
		annotation.Source,
		annotation.Category,
		annotation.Outcome,
		int64(annotation.QualityScore),
		int64(annotation.Confidence),
		boolAsInt64(annotation.NeedsFollowup),
		strings.Join(annotation.Labels, ","),
		annotation.Note,
		annotation.MetadataJSON,
		annotation.Status,
		int64(annotation.SchemaVersion),
		annotation.CreatedAt,
		annotation.UpdatedAt,
		deletedAt,
	}
}

func placeholderCountInClause(query, marker string) int {
	start := strings.Index(query, marker)
	if start < 0 {
		return 0
	}
	start += len(marker)
	end := strings.Index(query[start:], ")")
	if end < 0 {
		return 0
	}
	return strings.Count(query[start:start+end], "?")
}

func paginateTraceAnnotations(annotations []models.TraceAnnotation, values annotationNamedValues) []models.TraceAnnotation {
	limit, offset := queryLimitOffset(values)
	if offset >= len(annotations) {
		return nil
	}
	end := len(annotations)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return annotations[offset:end]
}

func paginateDriverRows(rows [][]driver.Value, values annotationNamedValues) [][]driver.Value {
	limit, offset := queryLimitOffset(values)
	if offset >= len(rows) {
		return nil
	}
	end := len(rows)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return rows[offset:end]
}

func queryLimitOffset(values annotationNamedValues) (int, int) {
	if len(values) < 2 {
		return 0, 0
	}
	return values.intAt(len(values) - 2), values.intAt(len(values) - 1)
}

func hasAnnotationFilter(query, clause string) bool {
	return strings.Contains(query, "AND "+clause) || strings.Contains(query, "WHERE "+clause)
}

func firstSessionSummaryScopeArg(query string, values annotationNamedValues) int {
	scopeArgs := 0
	if strings.Contains(query, "source_name IN") {
		scopeArgs++
	}
	if strings.Contains(query, "runtime IN") {
		scopeArgs++
	}
	if scopeArgs == 0 {
		return len(values)
	}
	return len(values) - scopeArgs
}

func annotationHasLabel(annotation models.TraceAnnotation, label string) bool {
	for _, candidate := range annotation.Labels {
		if candidate == label {
			return true
		}
	}
	return false
}

type annotationNamedValues []driver.Value

func namedValues(args []driver.NamedValue) annotationNamedValues {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	return values
}

func (v annotationNamedValues) stringAt(idx int) string {
	if idx < 0 || idx >= len(v) || v[idx] == nil {
		return ""
	}
	if s, ok := v[idx].(string); ok {
		return s
	}
	return ""
}

func (v annotationNamedValues) intAt(idx int) int {
	if idx < 0 || idx >= len(v) || v[idx] == nil {
		return 0
	}
	switch value := v[idx].(type) {
	case int:
		return value
	case int16:
		return int(value)
	case int64:
		return int(value)
	case uint8:
		return int(value)
	case uint16:
		return int(value)
	case uint64:
		return int(value)
	default:
		return 0
	}
}

func (v annotationNamedValues) timeAt(idx int) time.Time {
	if idx < 0 || idx >= len(v) || v[idx] == nil {
		return time.Time{}
	}
	if t, ok := v[idx].(time.Time); ok {
		return t
	}
	return time.Time{}
}

func (v annotationNamedValues) labelsAt(idx int) []string {
	if idx < 0 || idx >= len(v) || v[idx] == nil {
		return nil
	}
	switch labels := v[idx].(type) {
	case []string:
		return labels
	default:
		return nil
	}
}

func singleColumnRows(column string, value driver.Value) driver.Rows {
	return &annotationRows{columns: []string{column}, rows: [][]driver.Value{{value}}}
}

func emptyAnnotationRows(columns []string) driver.Rows {
	return &annotationRows{columns: columns}
}

type annotationRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func (r *annotationRows) Columns() []string {
	return r.columns
}

func (r *annotationRows) Close() error {
	return nil
}

func (r *annotationRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

func boolAsInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func queryScopeMatches(query string, values annotationNamedValues, firstScopeArg int, sourceName, runtime string) bool {
	if strings.Contains(query, "source_name IN") {
		if sourceName != values.stringAt(firstScopeArg) {
			return false
		}
	}
	if strings.Contains(query, "runtime IN") {
		runtimeArg := firstScopeArg
		if strings.Contains(query, "source_name IN") {
			runtimeArg++
		}
		return runtime == values.stringAt(runtimeArg)
	}
	return true
}

func queryEventKindMatches(query, eventKind string) bool {
	if strings.Contains(query, "event_kind = 'message'") {
		return eventKind == "message"
	}
	return true
}
