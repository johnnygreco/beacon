package web

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
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
	fake.sessions["session-1"] = true
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
	fake.sessions["session-1"] = true
	fake.events["event-1"] = "session-1"
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

func TestAnnotationAPIValidationAndMissingTargets(t *testing.T) {
	fake := newAnnotationAPIFake()
	fake.sessions["session-1"] = true
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

type annotationAPIFake struct {
	sessions    map[string]bool
	events      map[string]string
	annotations map[string]models.TraceAnnotation
}

func newAnnotationAPIFake() *annotationAPIFake {
	return &annotationAPIFake{
		sessions:    map[string]bool{},
		events:      map[string]string{},
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
	deletedAt := values.timeAt(20)
	annotation := models.TraceAnnotation{
		AnnotationID:  values.stringAt(0),
		TargetType:    values.stringAt(1),
		SessionID:     values.stringAt(2),
		EventUID:      values.stringAt(3),
		AuthorType:    values.stringAt(4),
		AuthorID:      values.stringAt(5),
		AuthorName:    values.stringAt(6),
		Source:        values.stringAt(7),
		Category:      values.stringAt(8),
		Outcome:       values.stringAt(9),
		QualityScore:  values.intAt(10),
		Confidence:    values.intAt(11),
		NeedsFollowup: values.intAt(12) != 0,
		Labels:        values.labelsAt(13),
		Note:          values.stringAt(14),
		MetadataJSON:  values.stringAt(15),
		Status:        values.stringAt(16),
		SchemaVersion: values.intAt(17),
		CreatedAt:     values.timeAt(18),
		UpdatedAt:     values.timeAt(19),
	}
	if !deletedAt.IsZero() && !deletedAt.Equal(time.Unix(0, 0).UTC()) {
		annotation.DeletedAt = &deletedAt
	}
	c.fake.annotations[annotation.AnnotationID] = annotation
	return driver.RowsAffected(1), nil
}

func (c annotationAPIConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	values := namedValues(args)
	switch {
	case strings.Contains(query, "trace_annotations FINAL"):
		return c.annotationRows(query, values), nil
	case strings.Contains(query, "session_projection"):
		sessionID := values.stringAt(0)
		if c.fake.sessions[sessionID] {
			return singleColumnRows("session_id", sessionID), nil
		}
		return emptyAnnotationRows([]string{"session_id"}), nil
	case strings.Contains(query, "activity_events"):
		eventUID := values.stringAt(0)
		sessionID := c.fake.events[eventUID]
		if sessionID != "" {
			return singleColumnRows("session_id", sessionID), nil
		}
		return emptyAnnotationRows([]string{"session_id"}), nil
	default:
		return nil, errors.New("unexpected query")
	}
}

func (c annotationAPIConn) annotationRows(query string, values annotationNamedValues) driver.Rows {
	idx := 0
	var annotationID, targetType, sessionID, eventUID, excludedStatus string
	if strings.Contains(query, "annotation_id = ?") {
		annotationID = values.stringAt(idx)
		idx++
	}
	if strings.Contains(query, "target_type = ?") {
		targetType = values.stringAt(idx)
		idx++
	}
	if strings.Contains(query, "session_id = ?") {
		sessionID = values.stringAt(idx)
		idx++
	}
	if strings.Contains(query, "event_uid = ?") {
		eventUID = values.stringAt(idx)
		idx++
	}
	if strings.Contains(query, "status != ?") {
		excludedStatus = values.stringAt(idx)
		idx++
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
		if eventUID != "" && annotation.EventUID != eventUID {
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
	rows := make([][]driver.Value, 0, len(annotations))
	for _, annotation := range annotations {
		deletedAt := time.Unix(0, 0).UTC()
		if annotation.DeletedAt != nil {
			deletedAt = *annotation.DeletedAt
		}
		rows = append(rows, []driver.Value{
			annotation.AnnotationID,
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
		})
	}
	return &annotationRows{
		columns: []string{"annotation_id", "target_type", "session_id", "event_uid", "author_type", "author_id", "author_name", "source", "category", "outcome", "quality_score", "confidence", "needs_followup", "labels", "note", "metadata_json", "status", "schema_version", "created_at", "updated_at", "deleted_at"},
		rows:    rows,
	}
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
