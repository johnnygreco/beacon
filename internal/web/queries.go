package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"strings"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

const (
	defaultSessionPageSize  = 30
	defaultActivityPageSize = 30
	defaultSearchPageSize   = 30
	activityTimelineLimit   = 500 // max activity items for 24h window
)

// parseRange converts a range string ("1h", "24h", "7d", "30d") to a *time.Time cutoff.
func parseRange(v string) *time.Time {
	now := time.Now()
	switch v {
	case "1h":
		t := now.Add(-time.Hour)
		return &t
	case "24h":
		t := now.Add(-24 * time.Hour)
		return &t
	case "7d":
		t := now.Add(-7 * 24 * time.Hour)
		return &t
	case "30d":
		t := now.Add(-30 * 24 * time.Hour)
		return &t
	}
	return nil
}

// QueryDashboardData queries all data needed for the dashboard page.
func QueryDashboardData(ctx context.Context, db *sql.DB) views.DashboardData {
	var data views.DashboardData
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		data.ActiveSessions, data.CompletedSessions, data.HasMoreSessions = QueryDashboardSessions(ctx, db)
	}()
	go func() {
		defer wg.Done()
		data.RecentActivity, data.HasMoreActivity = QueryRecentActivity(ctx, db)
	}()
	go func() { defer wg.Done(); data.TokensByModel = QueryTokensByModelSummary(ctx, db) }()
	go func() { defer wg.Done(); data.TokensChart = QueryTotalTokensTimeSeries(ctx, db) }()
	wg.Wait()
	return data
}

// QueryTokensByModelSummary returns token counts broken down by model.
func QueryTokensByModelSummary(ctx context.Context, db *sql.DB) []views.ModelTokens {
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(model, 'unknown'),
		        COALESCE(MAX(provider), ''),
		        COALESCE(SUM(input_tokens), 0),
		        COALESCE(SUM(output_tokens), 0),
		        COALESCE(SUM(cache_read_tokens), 0),
		        COALESCE(SUM(input_tokens + output_tokens), 0)
		 FROM events
		 WHERE model IS NOT NULL AND model != '' AND model != '<synthetic>'
		 GROUP BY model
		 ORDER BY COALESCE(SUM(input_tokens + output_tokens), 0) DESC
		 LIMIT 10`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []views.ModelTokens
	for rows.Next() {
		var m views.ModelTokens
		if err := rows.Scan(&m.Model, &m.Provider, &m.Input, &m.Output, &m.CacheRead, &m.Total); err != nil {
			continue
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return result
}

// QueryDashboardMetrics returns the top-level metric cards.
func QueryDashboardMetrics(ctx context.Context, db *sql.DB) []views.MetricData {
	var totalSessions, activeSessions int
	var inputTokens, outputTokens, cacheReadTokens int64
	var toolCalls, mcpCalls int

	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT session_id),
		        COUNT(DISTINCT CASE WHEN timestamp > current_timestamp - INTERVAL '24 hours' THEN session_id END),
		        COALESCE(SUM(input_tokens), 0),
		        COALESCE(SUM(output_tokens), 0),
		        COALESCE(SUM(cache_read_tokens), 0),
		        COUNT(CASE WHEN event_kind = 'tool_call' THEN 1 END),
		        COUNT(CASE WHEN event_kind = 'tool_call' AND tool_name LIKE 'mcp__%' THEN 1 END)
		 FROM events`,
	).Scan(&totalSessions, &activeSessions, &inputTokens, &outputTokens, &cacheReadTokens, &toolCalls, &mcpCalls); err != nil {
		return nil
	}

	totalTokens := inputTokens + outputTokens

	return []views.MetricData{
		{Label: "Total Sessions", Value: fmt.Sprintf("%d", totalSessions)},
		{Label: "Sessions (24h)", Value: fmt.Sprintf("%d", activeSessions)},
		{Label: "Total Tokens", Value: views.FormatTokens(totalTokens),
			Sublabel: fmt.Sprintf("In: %s  Out: %s  Cache: %s", views.FormatTokens(inputTokens), views.FormatTokens(outputTokens), views.FormatTokens(cacheReadTokens))},
		{Label: "Tool Calls", Value: fmt.Sprintf("%d", toolCalls),
			Sublabel: fmt.Sprintf("%d MCP", mcpCalls)},
	}
}

// QueryDashboardSessions returns session summaries split into active and completed.
// Active sessions are those with last activity within activeSessionThreshold.
// Completed sessions are fetched separately with LIMIT+1 to determine hasMore.
func QueryDashboardSessions(ctx context.Context, db *sql.DB) (active, completed []views.SessionSummary, hasMore bool) {
	now := time.Now()
	// Use Go's time.Now() (UTC-aware) instead of SQL current_timestamp to avoid
	// timezone mismatch — stored timestamps are UTC but current_timestamp is local.
	cutoff := now.Add(-idleThreshold)

	// Fetch active sessions: recent activity AND not explicitly ended.
	activeRows, err := db.QueryContext(ctx,
		`SELECT `+sessionSummaryColumns+`
		 FROM v_session_summary
		 WHERE ended_at >= $1
		   AND COALESCE(has_session_end, 0) = 0
		 ORDER BY ended_at DESC`, cutoff)
	if err != nil {
		return nil, nil, false
	}
	defer activeRows.Close()
	for activeRows.Next() {
		s, err := scanSessionSummary(activeRows, now)
		if err != nil {
			continue
		}
		active = append(active, s)
	}
	if err := activeRows.Err(); err != nil {
		return nil, nil, false
	}

	// Group subagents under their parent sessions
	active = views.GroupActiveSessions(active)

	// Fetch completed sessions with LIMIT+1 for hasMore detection
	completed, hasMore = QueryCompletedSessions(ctx, db, nil, 0, defaultSessionPageSize)
	return active, completed, hasMore
}

// activitySummaryExpr is the SQL CASE expression for activity item summaries.
const activitySummaryExpr = `CASE
		            WHEN event_kind = 'tool_call' THEN 'Tool: ' || COALESCE(NULLIF(tool_name, ''), 'unknown')
		            WHEN event_kind = 'message' AND actor_role = 'user' AND COALESCE(NULLIF(text_preview, ''), '') != '' THEN text_preview
		            WHEN event_kind = 'message' AND actor_role = 'assistant' AND COALESCE(NULLIF(text_preview, ''), '') != '' THEN text_preview
		            WHEN event_kind = 'message' THEN COALESCE(NULLIF(actor_role, ''), 'message') || ' message'
		            WHEN event_kind = 'error' THEN COALESCE(NULLIF(error_code, ''), 'error') || ': ' || COALESCE(NULLIF(error_message, ''), NULLIF(text_preview, ''), 'unknown error')
		            WHEN event_kind = 'session_meta' THEN 'Session started'
		            ELSE event_kind
		        END`

// QueryRecentActivity returns a feed of recent events within the last 24 hours.
func QueryRecentActivity(ctx context.Context, db *sql.DB) ([]views.ActivityItem, bool) {
	since := time.Now().Add(-24 * time.Hour)
	return QueryRecentActivityFiltered(ctx, db, &since, 0, activityTimelineLimit)
}

// QueryCompletedSessions returns paginated completed sessions with optional time filter.
// Only returns parent sessions (excludes subagents); subagent counts are attached.
func QueryCompletedSessions(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int) ([]views.SessionSummary, bool) {
	cutoff := time.Now().Add(-idleThreshold)
	query := `SELECT ` + sessionSummaryColumns + `
		 FROM v_session_summary
		 WHERE (ended_at < $1
		    OR COALESCE(has_session_end, 0) = 1)
		   AND (parent_session_id = '' OR parent_session_id IS NULL)`
	args := []any{cutoff}
	if since != nil {
		query += " AND ended_at >= $2"
		args = append(args, *since)
	}
	query += ` ORDER BY ended_at DESC`
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit+1, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	now := time.Now()
	var sessions []views.SessionSummary
	for rows.Next() {
		s, err := scanSessionSummary(rows, now)
		if err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	attachSubagentCounts(ctx, db, sessions)
	return sessions, hasMore
}

// attachSubagentCounts queries subagent counts for the given sessions and attaches them.
func attachSubagentCounts(ctx context.Context, db *sql.DB, sessions []views.SessionSummary) {
	if len(sessions) == 0 {
		return
	}
	// Build placeholders for the parent IDs on this page
	placeholders := make([]string, len(sessions))
	args := make([]any, len(sessions))
	for i, s := range sessions {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = s.ID
	}
	query := `SELECT parent_session_id, COUNT(*)
		 FROM v_session_summary
		 WHERE parent_session_id IN (` + strings.Join(placeholders, ",") + `)
		 GROUP BY parent_session_id`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var parentID string
		var count int
		if err := rows.Scan(&parentID, &count); err != nil {
			continue
		}
		counts[parentID] = count
	}
	for i := range sessions {
		sessions[i].SubagentCount = counts[sessions[i].ID]
	}
}

// QueryRecentActivityFiltered returns paginated activity items with optional time filter.
func QueryRecentActivityFiltered(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int) ([]views.ActivityItem, bool) {
	query := `SELECT event_uid,
		        event_kind,
		        ` + activitySummaryExpr + ` AS summary,
		        COALESCE(session_id, ''),
		        COALESCE(provider, ''),
		        timestamp
		 FROM events
		 WHERE event_kind IN ('message', 'tool_call', 'error', 'session_meta')`
	var args []any
	if since != nil {
		query += " AND timestamp >= $1"
		args = append(args, *since)
	}
	query += ` ORDER BY timestamp DESC`
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit+1, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	var items []views.ActivityItem
	for rows.Next() {
		var item views.ActivityItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Summary, &item.SessionID, &item.Provider, &item.Timestamp); err != nil {
			continue
		}
		item.Summary = shortenActivitySummary(item.Summary)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore
}

// QueryChartData returns token time-series data with breakdown by type.
func QueryChartData(ctx context.Context, db *sql.DB) views.MultiSeriesChart {
	chart := views.MultiSeriesChart{
		Datasets: []views.ChartDataset{
			{Label: "Input"},
			{Label: "Output"},
			{Label: "Cache Read"},
		},
	}

	rows, err := db.QueryContext(ctx,
		`SELECT minute, total_input, total_output, total_cache_read FROM (
		   SELECT minute, total_input, total_output, total_cache_read
		   FROM v_tokens_per_minute
		   ORDER BY minute DESC
		   LIMIT 60
		 ) sub ORDER BY minute ASC`)
	if err != nil {
		return chart
	}
	defer rows.Close()

	for rows.Next() {
		var minute time.Time
		var input, output, cacheRead int64
		if err := rows.Scan(&minute, &input, &output, &cacheRead); err != nil {
			continue
		}
		label := minute.Local().Format(time.RFC3339)
		chart.Labels = append(chart.Labels, label)
		chart.Datasets[0].Values = append(chart.Datasets[0].Values, float64(input))
		chart.Datasets[1].Values = append(chart.Datasets[1].Values, float64(output))
		chart.Datasets[2].Values = append(chart.Datasets[2].Values, float64(cacheRead))
	}
	if err := rows.Err(); err != nil {
		return chart
	}
	return chart
}

// QueryTotalTokensTimeSeries returns a single-curve time series of total tokens
// (all models combined) for the dashboard line chart.
func QueryTotalTokensTimeSeries(ctx context.Context, db *sql.DB) views.MultiSeriesChart {
	chart := views.MultiSeriesChart{
		Datasets: []views.ChartDataset{{Label: "Total Tokens"}},
	}
	rows, err := db.QueryContext(ctx,
		`SELECT minute, total_tokens FROM (
		   SELECT minute, total_tokens
		   FROM v_tokens_per_minute
		   ORDER BY minute DESC
		   LIMIT 60
		 ) sub ORDER BY minute ASC`)
	if err != nil {
		return chart
	}
	defer rows.Close()
	for rows.Next() {
		var minute time.Time
		var total int64
		if err := rows.Scan(&minute, &total); err != nil {
			continue
		}
		chart.Labels = append(chart.Labels, minute.Local().Format(time.RFC3339))
		chart.Datasets[0].Values = append(chart.Datasets[0].Values, float64(total))
	}
	if err := rows.Err(); err != nil {
		return chart
	}
	return chart
}

// QuerySessionDetail returns header, chart, and tool data for a session.
// The conversation trace is loaded separately via QuerySessionConversation.
func QuerySessionDetail(ctx context.Context, db *sql.DB, id string) (views.SessionDetailData, error) {
	var data views.SessionDetailData

	// Session info from view
	row := db.QueryRowContext(ctx,
		`SELECT `+sessionSummaryColumns+`
		 FROM v_session_summary WHERE session_id = $1`, id)
	session, err := scanSessionSummary(row, time.Now())
	if err != nil {
		return data, err
	}
	data.Session = session

	// Query child subagent sessions
	data.Session.ChildSessions = QueryChildSessions(ctx, db, id)

	// Single CTE query for chart, tool stats, and model breakdown
	data.TokensChart = views.MultiSeriesChart{
		Datasets: []views.ChartDataset{{Label: "Total Tokens"}},
	}

	rows, err := db.QueryContext(ctx,
		`WITH session_events AS (
			SELECT * FROM events WHERE session_id = $1
		),
		token_series AS (
			SELECT timestamp, (input_tokens + output_tokens) AS total_tokens
			FROM session_events
			WHERE (input_tokens + output_tokens) > 0
			ORDER BY timestamp
		),
		tool_stats AS (
			SELECT tool_name, COUNT(*) AS calls, COALESCE(AVG(duration_ms), 0) AS avg_duration
			FROM session_events
			WHERE event_kind = 'tool_call' AND tool_name IS NOT NULL AND tool_name != ''
			GROUP BY tool_name ORDER BY calls DESC
		),
		model_breakdown AS (
			SELECT COALESCE(model, 'unknown') AS model,
			       COALESCE(MAX(provider), '') AS provider,
			       COALESCE(SUM(input_tokens), 0) AS input,
			       COALESCE(SUM(output_tokens), 0) AS output,
			       COALESCE(SUM(cache_read_tokens), 0) AS cache_read
			FROM session_events
			WHERE model IS NOT NULL AND model != '' AND model != '<synthetic>'
			GROUP BY model ORDER BY (input + output) DESC
		)
		SELECT 'token' AS kind, timestamp, total_tokens, '' AS tool_name, 0 AS calls, 0 AS avg_dur, '' AS model, '' AS provider, 0 AS input, 0 AS output, 0 AS cache_read FROM token_series
		UNION ALL
		SELECT 'tool', NULL, 0, tool_name, calls, avg_duration, '', '', 0, 0, 0 FROM tool_stats
		UNION ALL
		SELECT 'model', NULL, 0, '', 0, 0, model, provider, input, output, cache_read FROM model_breakdown`, id)
	if err != nil {
		return data, nil // Return partial data on query error
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var ts *time.Time
		var totalTokens int64
		var toolName string
		var calls int
		var avgDur float64
		var model, provider string
		var input, output, cacheRead float64
		if err := rows.Scan(&kind, &ts, &totalTokens, &toolName, &calls, &avgDur, &model, &provider, &input, &output, &cacheRead); err != nil {
			continue
		}
		switch kind {
		case "token":
			if ts != nil {
				data.TokensChart.Labels = append(data.TokensChart.Labels, ts.Local().Format(time.RFC3339))
				data.TokensChart.Datasets[0].Values = append(data.TokensChart.Datasets[0].Values, float64(totalTokens))
			}
		case "tool":
			stat := views.ToolStat{Name: toolName, Calls: calls, AvgDuration: avgDur}
			stat.IsMCP = models.IsMCPTool(toolName)
			data.ToolStats = append(data.ToolStats, stat)
		case "model":
			mt := views.ModelTokens{
				Model:    model,
				Provider: provider,
				Input:    int64(input),
				Output:   int64(output),
				CacheRead: int64(cacheRead),
				Total:    int64(input + output),
			}
			data.TokensByModel = append(data.TokensByModel, mt)
		}
	}
	if err := rows.Err(); err != nil {
		return data, nil
	}

	return data, nil
}

// QuerySessionConversation returns the conversation trace for a session.
func QuerySessionConversation(ctx context.Context, db *sql.DB, id string) ([]views.ChatTurn, []views.TurnDetail) {
	traceRows, err := db.QueryContext(ctx,
		`SELECT e.event_uid, e.event_kind, COALESCE(e.payload_type, ''), COALESCE(e.actor_role, ''),
		        COALESCE(e.text_content, ''), COALESCE(e.text_preview, ''),
		        COALESCE(e.tool_name, ''), COALESCE(e.tool_use_id, ''), COALESCE(e.model, ''),
		        e.input_tokens + e.output_tokens, e.duration_ms, e.timestamp, turn_seq,
		        COALESCE(tio.input_preview, ''), COALESCE(tio.output_preview, ''),
		        COALESCE(tio.input_json, '')
		 FROM v_conversation_trace e
		 LEFT JOIN tool_io tio ON e.event_uid = tio.event_uid
		 WHERE e.session_id = $1
		 ORDER BY event_order`, id)
	if err != nil {
		return nil, nil
	}
	defer traceRows.Close()

	turnMap := make(map[int]*views.TurnDetail)
	var turnOrder []int

	for traceRows.Next() {
		var es views.EventSummary
		var turnSeq int
		if err := traceRows.Scan(&es.EventUID, &es.EventKind, &es.PayloadType, &es.ActorRole,
			&es.TextContent, &es.TextPreview,
			&es.ToolName, &es.ToolUseID, &es.Model, &es.Tokens, &es.DurationMs, &es.Timestamp, &turnSeq,
			&es.InputPreview, &es.OutputPreview, &es.InputJSON); err != nil {
			continue
		}

		td, ok := turnMap[turnSeq]
		if !ok {
			td = &views.TurnDetail{
				TurnSeq:   turnSeq,
				StartedAt: es.Timestamp,
			}
			turnMap[turnSeq] = td
			turnOrder = append(turnOrder, turnSeq)
		}

		td.Events = append(td.Events, es)
		td.TotalTokens += es.Tokens
	}
	if err := traceRows.Err(); err != nil {
		return nil, nil
	}

	var turns []views.TurnDetail
	for _, seq := range turnOrder {
		turns = append(turns, *turnMap[seq])
	}

	chatTurns := buildChatTurns(deduplicateTurns(turns))
	return chatTurns, turns
}

// parseToolParams extracts structured parameters from tool input JSON.
// Returns nil if the JSON is empty or unparseable.
func parseToolParams(inputJSON string) *views.ToolCallParams {
	if inputJSON == "" {
		return nil
	}
	var params views.ToolCallParams
	if err := json.Unmarshal([]byte(inputJSON), &params); err != nil {
		return nil
	}
	return &params
}

// buildChatTurns converts flat TurnDetail slices into structured ChatTurns
// by grouping consecutive tool_call/tool_result events into tool chains.
func buildChatTurns(turns []views.TurnDetail) []views.ChatTurn {
	var chatTurns []views.ChatTurn
	for _, t := range turns {
		ct := views.ChatTurn{
			TurnSeq:     t.TurnSeq,
			TotalTokens: t.TotalTokens,
			StartedAt:   t.StartedAt,
		}

		var pendingToolChain []views.ToolChainItem
		var pendingReasoning []views.EventSummary

		flushToolChain := func() {
			if len(pendingToolChain) > 0 {
				// Separate Agent tool calls into their own top-level blocks
				var regularTools []views.ToolChainItem
				for _, item := range pendingToolChain {
					if item.ToolName == "Agent" {
						// Flush any accumulated regular tools first
						if len(regularTools) > 0 {
							ct.Blocks = append(ct.Blocks, views.ChatBlock{
								Kind:      views.ChatBlockToolChain,
								ToolChain: regularTools,
							})
							regularTools = nil
						}
						ct.Blocks = append(ct.Blocks, views.ChatBlock{
							Kind:      views.ChatBlockSubagentDispatch,
							ToolChain: []views.ToolChainItem{item},
						})
					} else {
						regularTools = append(regularTools, item)
					}
				}
				if len(regularTools) > 0 {
					ct.Blocks = append(ct.Blocks, views.ChatBlock{
						Kind:      views.ChatBlockToolChain,
						ToolChain: regularTools,
					})
				}
				pendingToolChain = nil
			}
		}

		flushReasoning := func() {
			if len(pendingReasoning) > 0 {
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:     views.ChatBlockReasoning,
					Messages: pendingReasoning,
				})
				pendingReasoning = nil
			}
		}

		// Pre-build a map of tool_use_id → tool_result for call_id-based matching.
		// This handles Codex's pattern of batched calls then batched results.
		resultByCallID := make(map[string]int) // tool_use_id → event index
		consumedResults := make(map[int]bool)
		for idx, e := range t.Events {
			if e.EventKind == "tool_result" && e.ToolUseID != "" {
				resultByCallID[e.ToolUseID] = idx
			}
		}

		for i := 0; i < len(t.Events); i++ {
			e := t.Events[i]

			switch e.EventKind {
			case "tool_call":
				item := views.ToolChainItem{
					CallEvent:    e,
					ToolName:     e.ToolName,
					InputPreview: e.InputPreview,
					InputJSON:    e.InputJSON,
				}
				item.Params = parseToolParams(e.InputJSON)

				// Try call_id-based matching first
				if e.ToolUseID != "" {
					if ridx, ok := resultByCallID[e.ToolUseID]; ok {
						result := t.Events[ridx]
						// Copy tool name to result if missing
						if result.ToolName == "" {
							result.ToolName = e.ToolName
						}
						item.ResultEvent = &result
						item.OutputPreview = result.OutputPreview
						if item.OutputPreview == "" {
							item.OutputPreview = result.TextPreview
						}
						consumedResults[ridx] = true
					}
				} else {
					// Fallback: sequential look-ahead for sources without call_id
					for j := i + 1; j < len(t.Events); j++ {
						if t.Events[j].EventKind == "tool_result" && !consumedResults[j] {
							consumedResults[j] = true
							i = j
							result := t.Events[j]
							item.ResultEvent = &result
							item.OutputPreview = result.OutputPreview
							if item.OutputPreview == "" {
								item.OutputPreview = result.TextPreview
							}
							break
						} else if t.Events[j].EventKind == "event_msg" {
							continue // skip intermediate log events
						} else {
							break // something else — don't consume it
						}
					}
				}
				pendingToolChain = append(pendingToolChain, item)

			case "tool_result":
				if consumedResults[i] {
					break // Already paired with a tool_call
				}
				// Orphan tool_result (no preceding tool_call)
				item := views.ToolChainItem{
					CallEvent:     e,
					ToolName:      e.ToolName,
					OutputPreview: e.OutputPreview,
				}
				if item.OutputPreview == "" {
					item.OutputPreview = e.TextPreview
				}
				pendingToolChain = append(pendingToolChain, item)

			case "message":
				// Don't let empty assistant messages break a tool chain
				text := e.TextContent
				if text == "" {
					text = e.TextPreview
				}
				if e.ActorRole == "user" || strings.TrimSpace(text) != "" {
					flushReasoning()
					flushToolChain()
					eCopy := e
					kind := views.ChatBlockAssistantMessage
					if e.ActorRole == "user" {
						kind = views.ChatBlockUserMessage
					}
					ct.Blocks = append(ct.Blocks, views.ChatBlock{
						Kind:    kind,
						Message: &eCopy,
					})
				}

			case "reasoning":
				// Accumulate consecutive reasoning events into a group
				pendingReasoning = append(pendingReasoning, e)

			case "error":
				flushReasoning()
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockError,
					Message: &eCopy,
				})

			case "event_msg":
				// Intermediate log events — skip without breaking tool chains

			default:
				flushReasoning()
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockAssistantMessage,
					Message: &eCopy,
				})
			}
		}

		flushReasoning()
		flushToolChain()

		// Compute per-turn tool stats
		statsMap := make(map[string]int)
		for _, block := range ct.Blocks {
			if block.Kind == views.ChatBlockToolChain {
				for _, item := range block.ToolChain {
					statsMap[item.ToolName]++
				}
			}
		}
		if len(statsMap) > 0 {
			stats := make([]views.ToolStatEntry, 0, len(statsMap))
			for name, count := range statsMap {
				stats = append(stats, views.ToolStatEntry{Name: name, Count: count})
			}
			sort.Slice(stats, func(i, j int) bool {
				if stats[i].Count != stats[j].Count {
					return stats[i].Count > stats[j].Count
				}
				return stats[i].Name < stats[j].Name
			})
			ct.ToolStats = stats
		}

		chatTurns = append(chatTurns, ct)
	}
	return chatTurns
}

// deduplicateTurns removes duplicate events within and across turns.
// Claude Code JSONL can log the same content twice (e.g. "human"+"result" entries).
// This merges orphan turns (single user msg duplicated in the next turn) and
// removes consecutive duplicate events within each turn.
func deduplicateTurns(turns []views.TurnDetail) []views.TurnDetail {
	if len(turns) <= 1 {
		return turns
	}

	var result []views.TurnDetail
	for i, t := range turns {
		// Merge orphan turn: if this turn has a single user message that's
		// identical to the first user message in the next turn, skip it.
		if i+1 < len(turns) && len(t.Events) == 1 &&
			t.Events[0].EventKind == "message" && t.Events[0].ActorRole == "user" {
			nextTurn := turns[i+1]
			if len(nextTurn.Events) > 0 && nextTurn.Events[0].EventKind == "message" &&
				nextTurn.Events[0].ActorRole == "user" &&
				nextTurn.Events[0].TextContent == t.Events[0].TextContent {
				continue // skip this orphan turn
			}
		}

		// Remove duplicate events within the turn.
		// Claude Code JSONL logs the same content in multiple line types
		// with different UIDs. Deduplicate by content for reasoning and
		// message events to avoid showing the same text twice.
		var deduped []views.EventSummary
		seen := make(map[string]bool)
		for _, e := range t.Events {
			var key string
			switch {
			case e.EventKind == "reasoning" && e.TextContent != "":
				key = e.EventKind + "|" + e.TextContent
			case e.EventKind == "reasoning":
				// Empty reasoning (redacted thinking) — use UID to preserve each block
				key = e.EventUID
			case e.EventKind == "message" && e.TextContent != "":
				key = e.EventKind + "|" + e.ActorRole + "|" + e.TextContent
			default:
				key = e.EventUID + "|" + e.EventKind + "|" + e.ActorRole + "|" + e.TextContent + "|" + e.ToolName + "|" + e.InputJSON
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			deduped = append(deduped, e)
		}
		t.Events = deduped
		// Recompute total tokens after dedup
		t.TotalTokens = views.SumTokens(deduped)
		result = append(result, t)
	}
	return result
}

// QueryChildSessions returns subagent sessions spawned from a parent session.
func QueryChildSessions(ctx context.Context, db *sql.DB, parentID string) []views.SessionSummary {
	rows, err := db.QueryContext(ctx,
		`SELECT `+sessionSummaryColumns+`
		 FROM v_session_summary
		 WHERE parent_session_id = $1
		 ORDER BY started_at ASC`, parentID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	now := time.Now()
	var children []views.SessionSummary
	for rows.Next() {
		s, err := scanSessionSummary(rows, now)
		if err != nil {
			continue
		}
		children = append(children, s)
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return children
}

// sessionSummaryColumns is the shared SELECT column list for v_session_summary queries.
const sessionSummaryColumns = `session_id, COALESCE(source_name, ''), started_at, ended_at,
		COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
		COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
		COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
		COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
		COALESCE(last_model, ''),
		COALESCE(working_dir, ''),
		COALESCE(parent_session_id, ''),
		COALESCE(has_session_end, 0),
		COALESCE(provider, '')`

// scanSessionSummary scans a row from v_session_summary into a SessionSummary.
func scanSessionSummary(scanner interface{ Scan(dest ...any) error }, now time.Time) (views.SessionSummary, error) {
	var s views.SessionSummary
	var source, model string
	var startedAt, endedAt time.Time
	var hasSessionEnd int
	err := scanner.Scan(&s.ID, &source, &startedAt, &endedAt,
		&s.TurnCount, &s.TotalTokens, &s.InputTokens, &s.OutputTokens,
		&s.CacheReadTokens, &s.CacheCreateTokens,
		&s.ToolCallCount, &s.MCPCallCount, &model, &s.WorkingDir,
		&s.ParentSessionID, &hasSessionEnd, &s.Provider)
	if err != nil {
		return s, err
	}
	s.Actor = source
	s.ActiveModel = model
	s.HasSessionEnd = hasSessionEnd > 0
	setSessionTiming(&s, startedAt, endedAt, now)
	return s, nil
}

const (
	// activeThreshold: session is "active" (green Live badge) if last event within this window.
	activeThreshold = 90 * time.Second
	// idleThreshold: session is "idle" (amber badge) between activeThreshold and this.
	// Beyond this, or if has_session_end is true, session is "completed".
	idleThreshold = 5 * time.Minute
)

func setSessionTiming(s *views.SessionSummary, startedAt, endedAt, now time.Time) {
	s.StartedAt = startedAt
	s.EndedAt = endedAt

	// Use the most recent activity timestamp to determine session state.
	lastActivity := startedAt
	if !endedAt.IsZero() && endedAt.After(startedAt) {
		lastActivity = endedAt
	}

	elapsed := now.Sub(lastActivity)

	if s.HasSessionEnd {
		// Definitive end signal (last-prompt) — always completed.
		s.Status = "completed"
		s.Duration = formatDuration(lastActivity.Sub(startedAt))
	} else if elapsed < activeThreshold {
		// Actively producing events.
		s.Status = "active"
		s.Duration = formatDuration(now.Sub(startedAt))
	} else if elapsed < idleThreshold {
		// No recent events but hasn't timed out — waiting for user input.
		s.Status = "idle"
		s.Duration = formatDuration(lastActivity.Sub(startedAt))
	} else {
		// Timed out without explicit end signal.
		s.Status = "completed"
		s.Duration = formatDuration(lastActivity.Sub(startedAt))
	}
}

func shortenActivitySummary(s string) string {
	if strings.HasPrefix(s, "Tool: mcp__") {
		name := strings.TrimPrefix(s, "Tool: ")
		parts := strings.Split(name, "__")
		if len(parts) >= 3 {
			return "Tool: " + parts[len(parts)-1]
		}
	}
	return s
}

func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

