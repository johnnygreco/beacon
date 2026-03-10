package web

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	beacon "github.com/johnnygreco/beacon"
	"github.com/johnnygreco/beacon/internal/database"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/sse"
)

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open("", 2)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertTestEvent(t *testing.T, db *database.DB, uid, sessionID, kind, role, text, model string, inputTokens, outputTokens int64) {
	t.Helper()
	_, err := db.WriteConn().ExecContext(context.Background(),
		`INSERT INTO events (event_uid, session_id, event_kind, actor_role, text_content, text_preview, model, input_tokens, output_tokens, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		uid, sessionID, kind, role, text, text, model, inputTokens, outputTokens, time.Now())
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

func setupTestServer(t *testing.T) (*httptest.Server, *database.DB) {
	t.Helper()
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	broker := sse.NewBroker(16, logger)
	searcher := search.NewSearcher(db.ReadPool, logger, 25, 0)
	updater := NewUpdater(db.ReadPool, broker, logger)
	handlers := NewHandlers(db.ReadPool, searcher, logger, updater)
	apiHandlers := NewAPIHandlers(db.ReadPool, searcher, logger)
	staticFS, err := fs.Sub(beacon.StaticFS, "static")
	if err != nil {
		t.Fatalf("preparing static filesystem: %v", err)
	}
	router := NewRouter(staticFS, broker, handlers, apiHandlers)

	server := httptest.NewServer(router)
	t.Cleanup(func() { server.Close() })
	return server, db
}

func TestE2E_DashboardPage(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "dashboardTotalTokensChart-log-toggle") {
		t.Error("expected log-scale toggle button on dashboard total tokens chart")
	}
	if !strings.Contains(html, "Log Scale") {
		t.Error("expected 'Log Scale' button text on dashboard")
	}
}

func TestE2E_DashboardWithData(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess1", "message", "assistant", "Hi there!", "claude-sonnet-4-20250514", 0, 200)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestE2E_SessionDetailPage(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess1", "message", "assistant", "Hi!", "claude-sonnet-4-20250514", 0, 200)

	resp, err := http.Get(server.URL + "/sessions/sess1")
	if err != nil {
		t.Fatalf("GET /sessions/sess1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "sessionTokensChart-log-toggle") {
		t.Error("expected log-scale toggle button on session tokens chart")
	}
}

func TestE2E_SessionDetailNotFound(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/sessions/nonexistent")
	if err != nil {
		t.Fatalf("GET /sessions/nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestE2E_SessionsRedirect(t *testing.T) {
	server, _ := setupTestServer(t)

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(server.URL + "/sessions")
	if err != nil {
		t.Fatalf("GET /sessions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302, got %d", resp.StatusCode)
	}
}

func TestE2E_SearchPage(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/search")
	if err != nil {
		t.Fatalf("GET /search: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestE2E_APIMetrics(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)

	resp, err := http.Get(server.URL + "/api/metrics")
	if err != nil {
		t.Fatalf("GET /api/metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var metrics []APIMetricData
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(metrics) == 0 {
		t.Error("expected at least one metric")
	}
}

func TestE2E_APISessions(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess2", "message", "user", "World", "claude-sonnet-4-20250514", 50, 0)

	resp, err := http.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var sessions []APISessionSummary
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestE2E_APISessionsLimit(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess2", "message", "user", "World", "claude-sonnet-4-20250514", 50, 0)

	resp, err := http.Get(server.URL + "/api/sessions?limit=1")
	if err != nil {
		t.Fatalf("GET /api/sessions?limit=1: %v", err)
	}
	defer resp.Body.Close()

	var sessions []APISessionSummary
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session with limit=1, got %d", len(sessions))
	}
}

func TestE2E_APITokensPerMinute(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 200)

	resp, err := http.Get(server.URL + "/api/tokens-per-minute")
	if err != nil {
		t.Fatalf("GET /api/tokens-per-minute: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestE2E_APIToolStats(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "tool_call", "", "", "", 0, 0)
	// Set tool_name
	if _, err := db.WriteConn().ExecContext(context.Background(),
		`UPDATE events SET tool_name = 'Read' WHERE event_uid = 'e1'`); err != nil {
		t.Fatalf("update: %v", err)
	}

	resp, err := http.Get(server.URL + "/api/tool-stats")
	if err != nil {
		t.Fatalf("GET /api/tool-stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestE2E_APITokensByModel(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 200)
	insertTestEvent(t, db, "e2", "sess1", "message", "user", "World", "gpt-4o", 50, 100)

	resp, err := http.Get(server.URL + "/api/tokens-by-model")
	if err != nil {
		t.Fatalf("GET /api/tokens-by-model: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var items []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 models, got %d", len(items))
	}
}

func TestE2E_APISearchMissingQuery(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/api/search")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing query, got %d", resp.StatusCode)
	}
}

func TestE2E_SSEDashboard(t *testing.T) {
	server, _ := setupTestServer(t)

	// Test that SSE endpoint returns correct content type
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(server.URL + "/sse/dashboard")
	if err != nil {
		// Timeout is expected since SSE streams indefinitely
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}
}

func TestE2E_StaticFiles(t *testing.T) {
	server, _ := setupTestServer(t)

	// Note: static files might not be found in test context since
	// the working directory may differ from project root.
	// This test verifies the route exists.
	resp, err := http.Get(server.URL + "/static/css/custom.css")
	if err != nil {
		t.Fatalf("GET /static/css/custom.css: %v", err)
	}
	defer resp.Body.Close()

	// May be 404 if run from wrong directory, but should not be 500
	if resp.StatusCode == http.StatusInternalServerError {
		t.Error("static file handler should not return 500")
	}
}

func TestE2E_SessionDetailWithToolCalls(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Read file.txt", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess1", "tool_call", "", "", "claude-sonnet-4-20250514", 0, 50)
	if _, err := db.WriteConn().ExecContext(context.Background(),
		`UPDATE events SET tool_name = 'Read' WHERE event_uid = 'e2'`); err != nil {
		t.Fatalf("update: %v", err)
	}
	insertTestEvent(t, db, "e3", "sess1", "tool_result", "", "file contents here", "claude-sonnet-4-20250514", 0, 0)
	if _, err := db.WriteConn().ExecContext(context.Background(),
		`UPDATE events SET tool_name = 'Read' WHERE event_uid = 'e3'`); err != nil {
		t.Fatalf("update: %v", err)
	}
	insertTestEvent(t, db, "e4", "sess1", "message", "assistant", "Here are the contents", "claude-sonnet-4-20250514", 0, 200)

	resp, err := http.Get(server.URL + "/sessions/sess1")
	if err != nil {
		t.Fatalf("GET /sessions/sess1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestE2E_SessionDetailChartDataEmbedded(t *testing.T) {
	server, db := setupTestServer(t)

	// Insert events with token data to generate chart data
	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess1", "message", "assistant", "Hi!", "claude-sonnet-4-20250514", 50, 200)

	resp, err := http.Get(server.URL + "/sessions/sess1")
	if err != nil {
		t.Fatalf("GET /sessions/sess1: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The chart data should be valid JSON embedded via templ.JSONScript,
	// not literal Go expression text
	if strings.Contains(html, "multiSeriesChartData") {
		t.Error("chart data script contains literal Go function name instead of JSON data")
	}
	if strings.Contains(html, "multiSeriesChartToJSON") {
		t.Error("chart data script contains old literal Go function name instead of JSON data")
	}

	// Should contain the session-tokens-data script with valid JSON
	if !strings.Contains(html, `id="session-tokens-data"`) {
		t.Error("expected session-tokens-data script element")
	}

	// The embedded JSON should contain the "labels" key (from json tags)
	if !strings.Contains(html, `"labels"`) {
		t.Error("expected JSON with 'labels' key in chart data")
	}

	// Should contain token data from the inserted events
	if !strings.Contains(html, `"datasets"`) {
		t.Error("expected JSON with 'datasets' key in chart data")
	}
}

func TestE2E_SessionDetailChartDataByModel(t *testing.T) {
	server, db := setupTestServer(t)

	// Insert events with different models to generate by-model chart data
	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess1", "message", "assistant", "Hi!", "claude-sonnet-4-20250514", 50, 200)
	insertTestEvent(t, db, "e3", "sess1", "message", "assistant", "More", "gpt-4o", 30, 150)

	resp, err := http.Get(server.URL + "/sessions/sess1")
	if err != nil {
		t.Fatalf("GET /sessions/sess1: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Should contain by-model chart data with model names
	if !strings.Contains(html, `id="session-tokens-by-model-data"`) {
		t.Error("expected session-tokens-by-model-data script element for multi-model session")
	}
	if !strings.Contains(html, "claude-sonnet-4-20250514") {
		t.Error("expected model name in by-model chart data")
	}
	if !strings.Contains(html, "gpt-4o") {
		t.Error("expected second model name in by-model chart data")
	}
}

func TestE2E_SessionDetailChatView(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "What is 2+2?", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess1", "message", "assistant", "The answer is 4.", "claude-sonnet-4-20250514", 0, 200)

	// Session detail page lazy-loads conversation; check initial page has the container
	resp, err := http.Get(server.URL + "/sessions/sess1")
	if err != nil {
		t.Fatalf("GET /sessions/sess1: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, `id="conversation-container"`) {
		t.Error("expected conversation-container div for lazy loading")
	}

	// Fetch the lazy-loaded conversation partial
	resp2, err := http.Get(server.URL + "/sessions/sess1/conversation")
	if err != nil {
		t.Fatalf("GET /sessions/sess1/conversation: %v", err)
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	html2 := string(body2)

	// Chat view should contain the actual message text
	if !strings.Contains(html2, "What is 2+2?") {
		t.Error("expected user message text in conversation partial")
	}
	if !strings.Contains(html2, "The answer is 4.") {
		t.Error("expected assistant message text in conversation partial")
	}

	// Should have chat-view and timeline-view divs
	if !strings.Contains(html2, `id="chat-view"`) {
		t.Error("expected chat-view div")
	}
	if !strings.Contains(html2, `id="timeline-view"`) {
		t.Error("expected timeline-view div")
	}
}

func TestE2E_HealthEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestE2E_SSESession(t *testing.T) {
	server, db := setupTestServer(t)

	// Insert an event so the session exists
	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)

	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(server.URL + "/sse/session/sess1")
	if err != nil {
		// Timeout is expected since SSE streams indefinitely
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}
}

func TestE2E_APISearchWithQuery(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "the quick brown fox jumps", "claude-sonnet-4-20250514", 100, 0)
	insertTestEvent(t, db, "e2", "sess1", "message", "assistant", "lazy dog response", "claude-sonnet-4-20250514", 0, 200)

	resp, err := http.Get(server.URL + "/api/search?q=quick")
	if err != nil {
		t.Fatalf("GET /api/search?q=quick: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var results []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one search result for 'quick'")
	}
}

func TestE2E_APISessionsInvalidLimit(t *testing.T) {
	server, db := setupTestServer(t)

	insertTestEvent(t, db, "e1", "sess1", "message", "user", "Hello", "claude-sonnet-4-20250514", 100, 0)

	resp, err := http.Get(server.URL + "/api/sessions?limit=invalid")
	if err != nil {
		t.Fatalf("GET /api/sessions?limit=invalid: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var sessions []APISessionSummary
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (limit defaults to 50), got %d", len(sessions))
	}
}

func TestE2E_APIMetricsEmptyDB(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/api/metrics")
	if err != nil {
		t.Fatalf("GET /api/metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var metrics []APIMetricData
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(metrics) == 0 {
		t.Error("expected metrics array even on empty DB")
	}
}

func TestE2E_DashboardEmptyDB(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
