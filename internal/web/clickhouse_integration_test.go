package web

import (
	"context"
	"encoding/json"
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
}

func TestDashboardJSONAndAnalyticsAPIsUseProjectionRowsAfterReplay(t *testing.T) {
	ch := setupLiveWebStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	api := NewAPIHandlers(ch.DB, nil, logger)

	now := time.Now().UTC().Truncate(time.Second)
	activeID := "dashboard-live-active"
	completedID := "dashboard-live-completed"
	events := []models.Event{
		liveEvent("dash-active-user", activeID, "message", "user", now, "openai", "", "", 0, 0, 0),
		liveEvent("dash-active-assistant", activeID, "message", "assistant", now.Add(time.Second), "openai", "gpt-4.1", "", 10, 20, 0),
		liveEvent("dash-active-tool", activeID, "tool_call", "assistant", now.Add(2*time.Second), "openai", "", "mcp__filesystem__read_file", 5, 1, 100),
		liveEvent("dash-completed-user", completedID, "message", "user", now.Add(-10*time.Minute), "openai", "", "", 0, 0, 0),
		liveEvent("dash-completed-assistant", completedID, "message", "assistant", now.Add(-10*time.Minute+time.Second), "openai", "gpt-4.1", "", 7, 8, 0),
		liveEvent("dash-completed-end", completedID, "session_end", "system", now.Add(-9*time.Minute), "openai", "", "", 0, 0, 0),
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
	if err := ch.Flush(context.Background(), batch); err != nil {
		t.Fatalf("replay flush: %v", err)
	}

	activeBody := recordAPIResponse(t, api.GetDashboardSessions, "/api/dashboard/sessions?state=active")
	var active APIDashboardSessionsResponse
	if err := json.Unmarshal([]byte(activeBody), &active); err != nil {
		t.Fatalf("decode active sessions: %v\n%s", err, activeBody)
	}
	if !containsSession(active.Items, activeID) || containsSession(active.Items, completedID) {
		t.Fatalf("active sessions = %#v", active.Items)
	}

	completedBody := recordAPIResponse(t, api.GetDashboardSessions, "/api/dashboard/sessions?state=completed&range=24h&limit=10")
	var completed APIDashboardSessionsResponse
	if err := json.Unmarshal([]byte(completedBody), &completed); err != nil {
		t.Fatalf("decode completed sessions: %v\n%s", err, completedBody)
	}
	if !containsSession(completed.Items, completedID) || containsSession(completed.Items, activeID) {
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
	assertMetric(t, metrics, "Total Sessions", 2)
	assertMetric(t, metrics, "Active Sessions", 1)
	assertMetric(t, metrics, "Input Tokens", 22)
	assertMetric(t, metrics, "Output Tokens", 29)
	assertMetric(t, metrics, "Tool Calls", 1)
	assertMetric(t, metrics, "MCP Calls", 1)

	chartsBody := recordAPIResponse(t, api.GetDashboardCharts, "/api/dashboard/charts")
	var charts APIDashboardCharts
	if err := json.Unmarshal([]byte(chartsBody), &charts); err != nil {
		t.Fatalf("decode charts: %v\n%s", err, chartsBody)
	}
	if charts.Range != "24h" {
		t.Fatalf("dashboard chart default range = %q", charts.Range)
	}
	if len(charts.TokenCumulative.Datasets) == 0 {
		t.Fatalf("dashboard cumulative token chart missing datasets: %#v", charts.TokenCumulative)
	}
	if charts.TokenCumulative.Summary.TotalTokens != 51 {
		t.Fatalf("dashboard cumulative token summary = %#v", charts.TokenCumulative.Summary)
	}
	if got := modelSeriesTotal(charts.TokenCumulative.Datasets, "gpt-4.1"); got != 51 {
		t.Fatalf("gpt-4.1 cumulative total = %v in %#v", got, charts.TokenCumulative.Datasets)
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
	events := []models.Event{
		liveEvent("single-context", sessionID, "turn_context", "system", now, "openai", "gpt-5.5", "", 0, 0, 0),
		liveEvent("single-token-count", sessionID, "event_msg", "assistant", now.Add(time.Second), "openai", "", "", 12, 5, 0),
		liveEvent("mixed-first-model", mixedSessionID, "message", "assistant", now.Add(2*time.Second), "openai", "gpt-4.1", "", 2, 3, 0),
		liveEvent("mixed-second-model", mixedSessionID, "message", "assistant", now.Add(3*time.Second), "openai", "gpt-5", "", 4, 6, 0),
		liveEvent("mixed-blank-model", mixedSessionID, "event_msg", "assistant", now.Add(4*time.Second), "openai", "", "", 3, 4, 0),
	}
	events[1].PayloadType = "token_count"
	events[4].PayloadType = "token_count"
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

	tokens, _ := QueryDashboardModelAnalytics(context.Background(), ch.DB, nil, "")
	if got := modelSeriesTotal(tokens.Datasets, "gpt-5.5"); got != 17 {
		t.Fatalf("gpt-5.5 cumulative total = %v in %#v", got, tokens.Datasets)
	}
	if got := modelSeriesTotal(tokens.Datasets, "gpt-4.1"); got != 5 {
		t.Fatalf("gpt-4.1 cumulative total = %v in %#v", got, tokens.Datasets)
	}
	if got := modelSeriesTotal(tokens.Datasets, "gpt-5"); got != 17 {
		t.Fatalf("gpt-5 cumulative total = %v in %#v", got, tokens.Datasets)
	}
	if got := modelSeriesTotal(tokens.Datasets, "unknown"); got != 0 {
		t.Fatalf("unknown cumulative total = %v in %#v", got, tokens.Datasets)
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

func modelSeriesTotal(datasets []views.ModelSeriesDataset, model string) float64 {
	for _, dataset := range datasets {
		if dataset.Model == model {
			if len(dataset.Values) == 0 {
				return 0
			}
			return dataset.Values[len(dataset.Values)-1]
		}
	}
	return 0
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
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
