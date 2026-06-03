package web

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/views"
)

func TestBuildChatTurns_EmptyInput(t *testing.T) {
	result := buildChatTurns(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 chat turns, got %d", len(result))
	}
}

func TestCompletedSessionSearchClause(t *testing.T) {
	clause, args := completedSessionSearchClause("needle", []string{"session-2", "session-1"})
	if got := strings.Count(clause, "?"); got != len(args) {
		t.Fatalf("placeholder count = %d, arg count = %d", got, len(args))
	}
	for _, expected := range []string{
		"positionCaseInsensitive(session_id, ?)",
		"positionCaseInsensitive(COALESCE(working_dir, ''), ?)",
		"session_id IN (?,?)",
	} {
		if !strings.Contains(clause, expected) {
			t.Fatalf("search clause missing %q: %s", expected, clause)
		}
	}
	for _, unexpected := range []string{
		"search_documents FINAL",
		"positionCaseInsensitive(text_preview, ?)",
	} {
		if strings.Contains(clause, unexpected) {
			t.Fatalf("search clause should not contain %q: %s", unexpected, clause)
		}
	}
	expectedArgs := []any{"needle", "needle", "needle", "needle", "needle", "session-2", "session-1"}
	if fmt.Sprint(args) != fmt.Sprint(expectedArgs) {
		t.Fatalf("args = %#v, want %#v", args, expectedArgs)
	}
}

func TestCompletedSessionSearchClause_MetadataOnly(t *testing.T) {
	clause, args := completedSessionSearchClause("metadata", nil)
	if strings.Contains(clause, "session_id IN") {
		t.Fatalf("metadata-only search should not include event session IDs: %s", clause)
	}
	if strings.Contains(clause, "search_documents FINAL") {
		t.Fatalf("metadata-only search should not scan search_documents: %s", clause)
	}
	if len(args) != 5 {
		t.Fatalf("arg count = %d, want 5", len(args))
	}
	for _, arg := range args {
		if arg != "metadata" {
			t.Fatalf("metadata arg = %#v, want metadata", arg)
		}
	}
}

func TestCompletedSessionIDPrefixClause(t *testing.T) {
	clause, args := completedSessionIDPrefixClause("  session-prefix  ")
	if !strings.Contains(clause, "positionCaseInsensitive(session_id, ?) = 1") {
		t.Fatalf("prefix clause = %q", clause)
	}
	if fmt.Sprint(args) != fmt.Sprint([]any{"session-prefix"}) {
		t.Fatalf("prefix args = %#v", args)
	}

	clause, args = completedSessionIDPrefixClause("  ")
	if clause != "" || args != nil {
		t.Fatalf("blank prefix clause/args = %q/%#v, want empty/nil", clause, args)
	}
}

func TestSQLHelperSubqueries(t *testing.T) {
	if got := sqlWhereClause("  "); got != "" {
		t.Fatalf("blank where clause = %q, want empty", got)
	}
	if got := sqlWhereClause("session_id = ?"); got != "WHERE session_id = ?" {
		t.Fatalf("where clause = %q", got)
	}

	latest := latestActivityEventsSubquery("ae.session_id = ?")
	for _, fragment := range []string{
		"FROM activity_events AS ae WHERE ae.session_id = ?",
		"argMax(text_preview, captured_at) AS text_preview",
		"GROUP BY event_uid",
	} {
		if !strings.Contains(latest, fragment) {
			t.Fatalf("latest activity subquery missing %q: %s", fragment, latest)
		}
	}

	recent := recentActivityEventsSubquery("ae.event_kind IN ('message')")
	if !strings.Contains(recent, fmt.Sprintf("LIMIT %d", recentActivityCandidates)) {
		t.Fatalf("recent activity subquery missing candidate limit: %s", recent)
	}
	if !strings.Contains(sessionProjectionSubquery("session_id = ?"), "FROM session_projection FINAL WHERE session_id = ?") {
		t.Fatalf("session projection subquery did not apply where clause")
	}
	if !strings.Contains(analyticsProjectionSubquery("session_id = ?"), "FROM analytics_projection FINAL WHERE session_id = ?") {
		t.Fatalf("analytics projection subquery did not apply where clause")
	}
}

func TestReopenedSessionPredicatesUseActivityAfterLatestEnd(t *testing.T) {
	reopened := reopenedSessionIDsSubquery()
	for _, fragment := range []string{
		"FROM activity_events",
		"PREWHERE timestamp >= ?",
		"GROUP BY event_uid",
		"GROUP BY session_id",
		"maxIf(timestamp, event_kind != 'session_end') > maxIf(timestamp, event_kind = 'session_end')",
	} {
		if !strings.Contains(reopened, fragment) {
			t.Fatalf("reopened session subquery missing %q: %s", fragment, reopened)
		}
	}

	active := activeSessionPredicate()
	if !strings.Contains(active, "ended_at >= ?") {
		t.Fatalf("active predicate should keep the idle cutoff placeholder: %s", active)
	}
	if !strings.Contains(active, "COALESCE(has_session_end, 0) = 0 OR session_id IN") {
		t.Fatalf("active predicate should admit reopened sessions: %s", active)
	}

	completed := completedSessionPredicate()
	if !strings.Contains(completed, "ended_at < ?") {
		t.Fatalf("completed predicate should keep the idle cutoff placeholder: %s", completed)
	}
	if !strings.Contains(completed, "COALESCE(has_session_end, 0) = 1 AND NOT (session_id IN") {
		t.Fatalf("completed predicate should exclude reopened terminal sessions: %s", completed)
	}

	if got := strings.Count(active, "?"); got != 2 {
		t.Fatalf("active predicate placeholders = %d, want idle cutoff plus reopened cutoff: %s", got, active)
	}
	if got := strings.Count(completed, "?"); got != 2 {
		t.Fatalf("completed predicate placeholders = %d, want idle cutoff plus reopened cutoff: %s", got, completed)
	}
}

func TestCompletedSessionPredicateForQualifiesOnlyOuterColumns(t *testing.T) {
	predicate := completedSessionPredicateFor("s.ended_at", "s.has_session_end", "s.session_id")
	for _, fragment := range []string{
		"s.ended_at < ?",
		"COALESCE(s.has_session_end, 0) = 1",
		"NOT (s.session_id IN",
		"SELECT session_id",
		"WHERE session_id != ''",
	} {
		if !strings.Contains(predicate, fragment) {
			t.Fatalf("aliased completed predicate missing %q: %s", fragment, predicate)
		}
	}
	if strings.Contains(predicate, "SELECT s.session_id") || strings.Contains(predicate, "WHERE s.session_id") {
		t.Fatalf("aliased completed predicate leaked outer alias into reopened subquery: %s", predicate)
	}
}

func TestRecentActivityKindFilterUsesParameterizedArgs(t *testing.T) {
	hostile := "message') OR 1=1 --"
	clause, args := recentActivityKindFilter([]string{" tool_call ", hostile, ""})
	if clause != "ae.event_kind IN (?,?)" {
		t.Fatalf("kind filter clause = %q, want placeholders only", clause)
	}
	if strings.Contains(clause, hostile) || strings.Contains(clause, "OR 1=1") || strings.Contains(clause, "'") {
		t.Fatalf("kind filter interpolated event kind: %s", clause)
	}
	expectedArgs := []any{"tool_call", hostile}
	if fmt.Sprint(args) != fmt.Sprint(expectedArgs) {
		t.Fatalf("kind filter args = %#v, want %#v", args, expectedArgs)
	}

	clause, args = recentActivityKindFilter(nil)
	if clause != "ae.event_kind IN (?,?,?,?,?)" {
		t.Fatalf("default kind filter clause = %q", clause)
	}
	if len(args) != len(defaultActivityEventKinds) {
		t.Fatalf("default kind args = %#v, want %d", args, len(defaultActivityEventKinds))
	}
}

func TestTimeAndPresentationHelpers(t *testing.T) {
	if got := dashboardTimeUnit(1); got != "minute" {
		t.Fatalf("dashboardTimeUnit(1) = %q", got)
	}
	if got := dashboardTimeUnit(120); got != "hour" {
		t.Fatalf("dashboardTimeUnit(120) = %q", got)
	}
	if got := dashboardTimeUnit(1440); got != "day" {
		t.Fatalf("dashboardTimeUnit(1440) = %q", got)
	}
	if got := shortenActivitySummary("Tool: mcp__repo__search"); got != "Tool: search" {
		t.Fatalf("shortenActivitySummary = %q", got)
	}
	if got := shortenActivitySummary("message"); got != "message" {
		t.Fatalf("shortenActivitySummary plain = %q", got)
	}
	if got := formatDuration(90 * time.Second); got != "1m 30s" {
		t.Fatalf("formatDuration = %q", got)
	}
	if got := formatDuration(2*time.Hour + 3*time.Minute + 4*time.Second); got != "2h 3m" {
		t.Fatalf("formatDuration hours = %q", got)
	}
}

func TestCompletedSessionsOrderByNameUsesRenderedProjectName(t *testing.T) {
	orderBy := completedSessionsOrderBy("name", true)
	if !strings.Contains(orderBy, "replaceRegexpOne") {
		t.Fatalf("name sort should derive a basename from working_dir, got %s", orderBy)
	}
	if !strings.Contains(orderBy, "/.claude/worktrees/") {
		t.Fatalf("name sort should match SessionTitle worktree normalization, got %s", orderBy)
	}
	if !strings.Contains(orderBy, "NULLIF(source_name") {
		t.Fatalf("name sort should retain source_name fallback, got %s", orderBy)
	}
}

func TestCompletedSessionsOrderByWhitelistsSortKey(t *testing.T) {
	orderBy := completedSessionsOrderBy("ended_at DESC; DROP TABLE session_projection", true)
	if strings.Contains(orderBy, "DROP") || strings.Contains(orderBy, ";") {
		t.Fatalf("order by interpolated hostile sort key: %s", orderBy)
	}
	if !strings.Contains(orderBy, "ORDER BY ended_at DESC") {
		t.Fatalf("hostile sort should fall back to ended_at DESC, got %s", orderBy)
	}
}

func TestSearchResultSessionIDs_DedupesAndSkipsEmpty(t *testing.T) {
	ids := searchResultSessionIDs([]search.SearchResult{
		{SessionID: "session-1"},
		{SessionID: ""},
		{SessionID: "session-2"},
		{SessionID: "session-1"},
	})
	expected := []string{"session-1", "session-2"}
	if fmt.Sprint(ids) != fmt.Sprint(expected) {
		t.Fatalf("ids = %#v, want %#v", ids, expected)
	}
}

func TestBuildDashboardModelCharts_TokenBucketsAndActivity(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	points := []dashboardModelPoint{
		{Bucket: t0, Provider: "openai", Model: "gpt-5.4", Tokens: 100, InputTokens: 60, OutputTokens: 30, CacheReadTokens: 10, ToolCalls: 2, Calls: 4},
		{Bucket: t0.Add(15 * time.Minute), Provider: "openai", Model: "gpt-5.4", Tokens: 50, InputTokens: 20, OutputTokens: 25, CacheReadTokens: 5, ToolCalls: 1, Calls: 1, Errors: 1},
		{Bucket: t0, Provider: "anthropic", Model: "claude-opus-4-6", Tokens: 25, Calls: 1},
		{Bucket: t0.Add(15 * time.Minute), Provider: "anthropic", Model: "claude-opus-4-6", Tokens: 75, ToolCalls: 3, Calls: 3},
	}

	tokens, activity := buildDashboardModelCharts(points, 15, "hour")

	if len(tokens.Labels) != 2 || len(tokens.Datasets) != 2 {
		t.Fatalf("unexpected token chart shape: %#v", tokens)
	}
	if tokens.Datasets[0].Label != "gpt-5.4" {
		t.Fatalf("expected top model first, got %q", tokens.Datasets[0].Label)
	}
	if got := tokens.Datasets[0].Values; len(got) != 2 || got[0] != 100 || got[1] != 50 {
		t.Fatalf("gpt bucket token values = %#v", got)
	}
	if got := tokens.Datasets[1].Values; len(got) != 2 || got[0] != 25 || got[1] != 75 {
		t.Fatalf("claude bucket token values = %#v", got)
	}
	if tokens.Summary.TotalTokens != 250 || tokens.Summary.ToolCallCount != 6 || tokens.Summary.ErrorCount != 1 || tokens.Summary.ModelCount != 2 {
		t.Fatalf("summary = %#v", tokens.Summary)
	}

	totalTokens := activity.Metrics["total_tokens"].Datasets[0].Values
	if len(totalTokens) != 2 || totalTokens[0] != 100 || totalTokens[1] != 50 {
		t.Fatalf("total token metric values = %#v", totalTokens)
	}
	inputTokens := activity.Metrics["input_tokens"].Datasets[0].Values
	if len(inputTokens) != 2 || inputTokens[0] != 60 || inputTokens[1] != 20 {
		t.Fatalf("input token values = %#v", inputTokens)
	}
	outputTokens := activity.Metrics["output_tokens"].Datasets[0].Values
	if len(outputTokens) != 2 || outputTokens[0] != 30 || outputTokens[1] != 25 {
		t.Fatalf("output token values = %#v", outputTokens)
	}
	cacheReadTokens := activity.Metrics["cache_read_tokens"].Datasets[0].Values
	if len(cacheReadTokens) != 2 || cacheReadTokens[0] != 10 || cacheReadTokens[1] != 5 {
		t.Fatalf("cache read token values = %#v", cacheReadTokens)
	}
	errorRate := activity.Metrics["error_rate"].Datasets[0].Values
	if len(errorRate) != 2 || errorRate[0] != 0 || errorRate[1] != 50 {
		t.Fatalf("error rate values = %#v", errorRate)
	}
	toolCalls := activity.Metrics["tool_calls"].Datasets[1].Values
	if len(toolCalls) != 2 || toolCalls[0] != 0 || toolCalls[1] != 3 {
		t.Fatalf("tool call values = %#v", toolCalls)
	}
}

func TestQueryDashboardModelAnalytics_PlottableIncludesCacheReadTokens(t *testing.T) {
	db := newDashboardQueryCaptureDB(t)
	QueryDashboardModelAnalytics(context.Background(), db, nil, "24h")

	query := capturedDashboardModelAnalyticsQuery()
	if !strings.Contains(query, "OR cache_read_tokens != 0") {
		t.Fatalf("dashboard model analytics should plot cache-read-only rows, got query:\n%s", query)
	}
}

func TestDashboardBucketMinutes(t *testing.T) {
	cases := map[string]int{
		"1h":  1,
		"24h": 15,
		"7d":  120,
		"30d": 720,
		"":    1440,
	}
	for rangeVal, want := range cases {
		if got := dashboardBucketMinutes(rangeVal); got != want {
			t.Fatalf("dashboardBucketMinutes(%q) = %d, want %d", rangeVal, got, want)
		}
	}
}

var (
	registerDashboardQueryCaptureDriver sync.Once
	dashboardQueryCaptureMu             sync.Mutex
	dashboardQueryCaptureSQL            string
)

type dashboardQueryCaptureDriver struct{}

func (dashboardQueryCaptureDriver) Open(string) (driver.Conn, error) {
	return dashboardQueryCaptureConn{}, nil
}

type dashboardQueryCaptureConn struct{}

func (dashboardQueryCaptureConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("dashboard query capture driver does not prepare statements")
}

func (dashboardQueryCaptureConn) Close() error {
	return nil
}

func (dashboardQueryCaptureConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("dashboard query capture driver does not support transactions")
}

func (dashboardQueryCaptureConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	dashboardQueryCaptureMu.Lock()
	dashboardQueryCaptureSQL = query
	dashboardQueryCaptureMu.Unlock()
	return dashboardQueryCaptureRows{}, nil
}

type dashboardQueryCaptureRows struct{}

func (dashboardQueryCaptureRows) Columns() []string {
	return []string{"bucket", "provider_key", "model_key", "tokens", "input_tokens", "output_tokens", "cache_read_tokens", "tool_calls", "calls", "errors"}
}

func (dashboardQueryCaptureRows) Close() error {
	return nil
}

func (dashboardQueryCaptureRows) Next([]driver.Value) error {
	return io.EOF
}

func newDashboardQueryCaptureDB(t *testing.T) *sql.DB {
	t.Helper()
	registerDashboardQueryCaptureDriver.Do(func() {
		sql.Register("beacon_dashboard_query_capture", dashboardQueryCaptureDriver{})
	})
	dashboardQueryCaptureMu.Lock()
	dashboardQueryCaptureSQL = ""
	dashboardQueryCaptureMu.Unlock()

	db, err := sql.Open("beacon_dashboard_query_capture", "")
	if err != nil {
		t.Fatalf("open dashboard query capture db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func capturedDashboardModelAnalyticsQuery() string {
	dashboardQueryCaptureMu.Lock()
	defer dashboardQueryCaptureMu.Unlock()
	return dashboardQueryCaptureSQL
}

func TestBuildChatTurns_SingleUserMessage(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq:     1,
		TotalTokens: 100,
		StartedAt:   time.Now(),
		Events: []views.EventSummary{{
			EventKind:   "message",
			ActorRole:   "user",
			TextContent: "Hello",
		}},
	}}

	result := buildChatTurns(turns)
	if len(result) != 1 {
		t.Fatalf("expected 1 chat turn, got %d", len(result))
	}
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result[0].Blocks))
	}
	if result[0].Blocks[0].Kind != views.ChatBlockUserMessage {
		t.Errorf("expected user_message, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_AssistantMessage(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{{
			EventKind:   "message",
			ActorRole:   "assistant",
			TextContent: "Hi there!",
		}},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result[0].Blocks))
	}
	if result[0].Blocks[0].Kind != views.ChatBlockAssistantMessage {
		t.Errorf("expected assistant_message, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ToolCallWithResult(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "file.txt"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "contents"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block (tool chain), got %d", len(result[0].Blocks))
	}
	block := result[0].Blocks[0]
	if block.Kind != views.ChatBlockToolChain {
		t.Errorf("expected tool_chain, got %s", block.Kind)
	}
	if len(block.ToolChain) != 1 {
		t.Fatalf("expected 1 tool chain item, got %d", len(block.ToolChain))
	}
	item := block.ToolChain[0]
	if item.ToolName != "Read" {
		t.Errorf("expected tool name Read, got %s", item.ToolName)
	}
	if item.ResultEvent == nil {
		t.Error("expected result event to be set")
	}
	if item.OutputPreview != "contents" {
		t.Errorf("expected output preview 'contents', got '%s'", item.OutputPreview)
	}
}

func TestBuildChatTurns_OrphanToolResult(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_result", ToolName: "Bash", OutputPreview: "output"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(result[0].Blocks))
	}
	if result[0].Blocks[0].Kind != views.ChatBlockToolChain {
		t.Errorf("expected tool_chain, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ReasoningBlock(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "reasoning", TextContent: "thinking..."},
		},
	}}

	result := buildChatTurns(turns)
	if result[0].Blocks[0].Kind != views.ChatBlockReasoning {
		t.Errorf("expected reasoning, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ErrorBlock(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "error", TextContent: "something failed"},
		},
	}}

	result := buildChatTurns(turns)
	if result[0].Blocks[0].Kind != views.ChatBlockError {
		t.Errorf("expected error, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_MixedConversation(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "message", ActorRole: "user", TextContent: "Do something"},
			{EventKind: "reasoning", TextContent: "thinking"},
			{EventKind: "message", ActorRole: "assistant", TextContent: "I'll help"},
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "input"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "output"},
			{EventKind: "tool_call", ToolName: "Write", InputPreview: "data"},
			{EventKind: "tool_result", ToolName: "Write", OutputPreview: "ok"},
			{EventKind: "message", ActorRole: "assistant", TextContent: "Done!"},
		},
	}}

	result := buildChatTurns(turns)
	blocks := result[0].Blocks

	expected := []string{
		views.ChatBlockUserMessage,
		views.ChatBlockReasoning,
		views.ChatBlockAssistantMessage,
		views.ChatBlockToolChain,
		views.ChatBlockAssistantMessage,
	}

	if len(blocks) != len(expected) {
		t.Fatalf("expected %d blocks, got %d", len(expected), len(blocks))
	}
	for i, exp := range expected {
		if blocks[i].Kind != exp {
			t.Errorf("block %d: expected %s, got %s", i, exp, blocks[i].Kind)
		}
	}

	// The tool chain should have 2 items (Read and Write)
	toolChain := blocks[3].ToolChain
	if len(toolChain) != 2 {
		t.Errorf("expected 2 tool chain items, got %d", len(toolChain))
	}
}

func TestBuildChatTurns_MultipleTurns(t *testing.T) {
	turns := []views.TurnDetail{
		{
			TurnSeq: 1,
			Events: []views.EventSummary{
				{EventKind: "message", ActorRole: "user", TextContent: "First"},
			},
		},
		{
			TurnSeq: 2,
			Events: []views.EventSummary{
				{EventKind: "message", ActorRole: "user", TextContent: "Second"},
			},
		},
	}

	result := buildChatTurns(turns)
	if len(result) != 2 {
		t.Errorf("expected 2 chat turns, got %d", len(result))
	}
	if result[0].TurnSeq != 1 || result[1].TurnSeq != 2 {
		t.Errorf("turn sequences incorrect: %d, %d", result[0].TurnSeq, result[1].TurnSeq)
	}
}

func TestParseToolParams_Empty(t *testing.T) {
	result := parseToolParams("")
	if result != nil {
		t.Error("expected nil for empty input")
	}
}

func TestParseToolParams_InvalidJSON(t *testing.T) {
	result := parseToolParams("not json")
	if result != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestParseToolParams_BashCommand(t *testing.T) {
	result := parseToolParams(`{"command":"ls -la","description":"list files"}`)
	if result == nil {
		t.Fatal("expected non-nil result")
		return
	}
	if result.Command != "ls -la" {
		t.Errorf("expected command 'ls -la', got '%s'", result.Command)
	}
	if result.Description != "list files" {
		t.Errorf("expected description 'list files', got '%s'", result.Description)
	}
}

func TestParseToolParams_EditTool(t *testing.T) {
	result := parseToolParams(`{"file_path":"/tmp/test.go","old_string":"foo","new_string":"bar"}`)
	if result == nil {
		t.Fatal("expected non-nil result")
		return
	}
	if result.FilePath != "/tmp/test.go" {
		t.Errorf("expected file_path '/tmp/test.go', got '%s'", result.FilePath)
	}
	if result.OldString != "foo" {
		t.Errorf("expected old_string 'foo', got '%s'", result.OldString)
	}
	if result.NewString != "bar" {
		t.Errorf("expected new_string 'bar', got '%s'", result.NewString)
	}
}

func TestParseToolParams_SearchTool(t *testing.T) {
	result := parseToolParams(`{"pattern":"func.*Test","path":"./internal"}`)
	if result == nil {
		t.Fatal("expected non-nil result")
		return
	}
	if result.Pattern != "func.*Test" {
		t.Errorf("expected pattern 'func.*Test', got '%s'", result.Pattern)
	}
	if result.Path != "./internal" {
		t.Errorf("expected path './internal', got '%s'", result.Path)
	}
}

func TestBuildChatTurns_ToolStats(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "f1"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "c1"},
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "f2"},
			{EventKind: "tool_result", ToolName: "Read", OutputPreview: "c2"},
			{EventKind: "tool_call", ToolName: "Edit", InputPreview: "e1"},
			{EventKind: "tool_result", ToolName: "Edit", OutputPreview: "ok"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(result))
	}
	stats := result[0].ToolStats
	if len(stats) != 2 {
		t.Fatalf("expected 2 tool stat entries, got %d", len(stats))
	}
	// Sorted by count descending, then name ascending
	if stats[0].Name != "Read" || stats[0].Count != 2 {
		t.Errorf("expected first stat Read:2, got %s:%d", stats[0].Name, stats[0].Count)
	}
	if stats[1].Name != "Edit" || stats[1].Count != 1 {
		t.Errorf("expected second stat Edit:1, got %s:%d", stats[1].Name, stats[1].Count)
	}
}

func TestBuildChatTurns_InputJSONAndParams(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Bash", InputPreview: "ls", InputJSON: `{"command":"ls -la","description":"list files"}`},
			{EventKind: "tool_result", ToolName: "Bash", OutputPreview: "file1\nfile2"},
		},
	}}

	result := buildChatTurns(turns)
	item := result[0].Blocks[0].ToolChain[0]
	if item.InputJSON != `{"command":"ls -la","description":"list files"}` {
		t.Errorf("InputJSON not preserved: %s", item.InputJSON)
	}
	if item.Params == nil {
		t.Fatal("expected Params to be populated")
	}
	if item.Params.Command != "ls -la" {
		t.Errorf("expected command 'ls -la', got '%s'", item.Params.Command)
	}
}

func TestBuildChatTurns_UnknownEventKind(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "unknown_event", TextContent: "data"},
		},
	}}

	result := buildChatTurns(turns)
	// Unknown events should be treated as assistant messages
	if result[0].Blocks[0].Kind != views.ChatBlockAssistantMessage {
		t.Errorf("expected assistant_message for unknown kind, got %s", result[0].Blocks[0].Kind)
	}
}

func TestBuildChatTurns_ToolCallWithoutResult(t *testing.T) {
	turns := []views.TurnDetail{{
		TurnSeq: 1,
		Events: []views.EventSummary{
			{EventKind: "tool_call", ToolName: "Read", InputPreview: "file.txt"},
			{EventKind: "message", ActorRole: "assistant", TextContent: "Moving on"},
		},
	}}

	result := buildChatTurns(turns)
	if len(result[0].Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(result[0].Blocks))
	}
	// First block: tool chain with no result
	if result[0].Blocks[0].Kind != views.ChatBlockToolChain {
		t.Errorf("expected tool_chain, got %s", result[0].Blocks[0].Kind)
	}
	if result[0].Blocks[0].ToolChain[0].ResultEvent != nil {
		t.Error("expected no result event for tool call without result")
	}
}

func TestSetSessionTiming_Active(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-5 * time.Minute)
	end := time.Now().Add(-1 * time.Minute) // ended 1 min ago — still active

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", s.Status)
	}
	if s.Duration == "" {
		t.Error("expected non-empty duration")
	}
	if s.EndedAt != end {
		t.Errorf("expected EndedAt to be set")
	}
}

func TestScanSessionSummaryIncludesErrorCount(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Minute)
	end := now.Add(-6 * time.Minute)
	scanner := stubScanner{values: []any{
		"session-1",
		"codex",
		start,
		end,
		int64(3),
		int64(120),
		int64(70),
		int64(50),
		int64(10),
		int64(5),
		int64(4),
		int64(1),
		int64(2),
		"gpt-5.4",
		"/repo",
		"parent-1",
		1,
		"openai",
	}}

	s, err := scanSessionSummary(scanner, now)
	if err != nil {
		t.Fatalf("scanSessionSummary: %v", err)
	}
	if s.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2", s.ErrorCount)
	}
	if s.ActiveModel != "gpt-5.4" || s.Provider != "openai" || !s.HasSessionEnd {
		t.Fatalf("summary fields shifted during scan: %#v", s)
	}
}

func TestScanSessionSummaryIncludingReopenedClearsTerminalEnd(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Minute)
	end := now.Add(-30 * time.Second)
	scanner := stubScanner{values: []any{
		"session-reopened",
		"codex",
		start,
		end,
		int64(3),
		int64(120),
		int64(70),
		int64(50),
		int64(10),
		int64(5),
		int64(4),
		int64(1),
		int64(0),
		"gpt-5.4",
		"/repo",
		"",
		1,
		"openai",
		1,
	}}

	s, err := scanSessionSummaryIncludingReopened(scanner, now)
	if err != nil {
		t.Fatalf("scanSessionSummaryIncludingReopened: %v", err)
	}
	if s.HasSessionEnd {
		t.Fatalf("HasSessionEnd = true, want reopened sessions reclassified as non-terminal")
	}
	if s.Status != "active" {
		t.Fatalf("Status = %q, want active", s.Status)
	}
}

func TestAPISessionSummaryFromViewIncludesErrorCount(t *testing.T) {
	api := apiSessionSummaryFromView(views.SessionSummary{
		ID:          "session-1",
		ErrorCount:  2,
		TotalTokens: 42_000,
		ActiveModel: "claude-sonnet-4",
	})
	if api.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2", api.ErrorCount)
	}
}

type stubScanner struct {
	values []any
}

func (s stubScanner) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan destinations = %d, values = %d", len(dest), len(s.values))
	}
	for i, value := range s.values {
		switch d := dest[i].(type) {
		case *string:
			v, ok := value.(string)
			if !ok {
				return fmt.Errorf("value %d is %T, want string", i, value)
			}
			*d = v
		case *time.Time:
			v, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("value %d is %T, want time.Time", i, value)
			}
			*d = v
		case *int:
			switch v := value.(type) {
			case int:
				*d = v
			case int64:
				*d = int(v)
			default:
				return fmt.Errorf("value %d is %T, want int", i, value)
			}
		case *int64:
			switch v := value.(type) {
			case int64:
				*d = v
			case int:
				*d = int64(v)
			default:
				return fmt.Errorf("value %d is %T, want int64", i, value)
			}
		default:
			return fmt.Errorf("unsupported destination %d: %T", i, dest[i])
		}
	}
	return nil
}

func TestSetSessionTiming_Completed(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-20 * time.Minute)
	end := time.Now().Add(-10 * time.Minute) // ended 10 min ago — completed

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", s.Status)
	}
	if s.Duration != "10m 0s" {
		t.Errorf("expected duration '10m 0s', got '%s'", s.Duration)
	}
}

func TestSetSessionTiming_RecentlyActive(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-10 * time.Minute)
	// Ended within active threshold (90s) — active
	end := time.Now().Add(-30 * time.Second)

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "active" {
		t.Errorf("expected status 'active' within 90s, got '%s'", s.Status)
	}

	// Between active (90s) and idle (5m) threshold — idle
	var s2 views.SessionSummary
	end2 := time.Now().Add(-2 * time.Minute)
	setSessionTiming(&s2, start, end2, time.Now())

	if s2.Status != "idle" {
		t.Errorf("expected status 'idle' at 2 minutes, got '%s'", s2.Status)
	}

	// Past idle threshold — completed
	var s3 views.SessionSummary
	end3 := time.Now().Add(-5*time.Minute - 1*time.Second)
	setSessionTiming(&s3, start, end3, time.Now())

	if s3.Status != "completed" {
		t.Errorf("expected status 'completed' past 5 minutes, got '%s'", s3.Status)
	}
}

func TestSetSessionTiming_ZeroEndTime(t *testing.T) {
	var s views.SessionSummary
	start := time.Now().Add(-2 * time.Minute)
	end := time.Time{} // zero time — lastActivity falls back to startedAt (2m ago → idle)

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "idle" {
		t.Errorf("expected status 'idle' for zero endedAt with 2m start, got '%s'", s.Status)
	}
	if s.Duration == "" {
		t.Error("expected non-empty duration")
	}
}

func TestSetSessionTiming_HasSessionEnd(t *testing.T) {
	var s views.SessionSummary
	s.HasSessionEnd = true
	start := time.Now().Add(-1 * time.Minute)
	end := time.Now().Add(-30 * time.Second) // recent activity, but has session_end

	setSessionTiming(&s, start, end, time.Now())

	if s.Status != "completed" {
		t.Errorf("expected status 'completed' with session_end signal, got '%s'", s.Status)
	}
}

func TestDeduplicateTurns_OrphanMerge(t *testing.T) {
	turns := []views.TurnDetail{
		{TurnSeq: 1, Events: []views.EventSummary{
			{EventUID: "a1", EventKind: "message", ActorRole: "user", TextContent: "hello"},
		}},
		{TurnSeq: 2, Events: []views.EventSummary{
			{EventUID: "a2", EventKind: "message", ActorRole: "user", TextContent: "hello"},
			{EventUID: "b1", EventKind: "message", ActorRole: "assistant", TextContent: "hi"},
		}},
	}

	result := deduplicateTurns(turns)
	if len(result) != 1 {
		t.Fatalf("expected 1 turn after orphan merge, got %d", len(result))
	}
	if result[0].TurnSeq != 2 {
		t.Errorf("expected turn 2 to remain, got turn %d", result[0].TurnSeq)
	}
}

func TestDeduplicateTurns_LastSingleTurnKept(t *testing.T) {
	turns := []views.TurnDetail{
		{TurnSeq: 1, Events: []views.EventSummary{
			{EventUID: "a1", EventKind: "message", ActorRole: "user", TextContent: "hello"},
			{EventUID: "b1", EventKind: "message", ActorRole: "assistant", TextContent: "hi"},
		}},
		{TurnSeq: 2, Events: []views.EventSummary{
			{EventUID: "a2", EventKind: "message", ActorRole: "user", TextContent: "bye"},
		}},
	}

	result := deduplicateTurns(turns)
	if len(result) != 2 {
		t.Fatalf("expected 2 turns (last single turn kept), got %d", len(result))
	}
}

func TestDeduplicateTurns_DifferentUIDsNotDeduped(t *testing.T) {
	turns := []views.TurnDetail{
		{TurnSeq: 1, Events: []views.EventSummary{
			{EventUID: "uid-1", EventKind: "tool_call", ToolName: "Read", InputJSON: `{"file_path":"f.go"}`},
			{EventUID: "uid-2", EventKind: "tool_call", ToolName: "Read", InputJSON: `{"file_path":"f.go"}`},
		}},
	}

	result := deduplicateTurns(turns)
	if len(result[0].Events) != 2 {
		t.Errorf("expected 2 events (different UIDs), got %d", len(result[0].Events))
	}
}
