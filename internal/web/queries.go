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

// QueryDashboardData queries all data needed for the dashboard page.
func QueryDashboardData(ctx context.Context, db *sql.DB) views.DashboardData {
	var data views.DashboardData
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		data.ActiveSessions, data.CompletedSessions = QueryDashboardSessions(ctx, db)
	}()
	go func() { defer wg.Done(); data.RecentActivity = QueryRecentActivity(ctx, db) }()
	go func() { defer wg.Done(); data.TokensByModel = QueryTokensByModelSummary(ctx, db) }()
	go func() { defer wg.Done(); data.TokensChart = QueryTotalTokensTimeSeries(ctx, db) }()
	wg.Wait()
	return data
}

// QueryTokensByModelSummary returns token counts broken down by model.
func QueryTokensByModelSummary(ctx context.Context, db *sql.DB) []views.ModelTokens {
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(model, 'unknown'),
		        COALESCE(SUM(input_tokens), 0),
		        COALESCE(SUM(output_tokens), 0),
		        COALESCE(SUM(cache_read_tokens), 0),
		        COALESCE(SUM(input_tokens + output_tokens), 0)
		 FROM events
		 WHERE model IS NOT NULL AND model != ''
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
		if err := rows.Scan(&m.Model, &m.Input, &m.Output, &m.CacheRead, &m.Total); err != nil {
			continue
		}
		result = append(result, m)
	}
	return result
}

// QueryDashboardMetrics returns the top-level metric cards.
func QueryDashboardMetrics(ctx context.Context, db *sql.DB) []views.MetricData {
	var totalSessions, activeSessions int
	var inputTokens, outputTokens, cacheReadTokens int64
	var toolCalls, mcpCalls int

	db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT session_id),
		        COUNT(DISTINCT CASE WHEN timestamp > current_timestamp - INTERVAL '24 hours' THEN session_id END),
		        COALESCE(SUM(input_tokens), 0),
		        COALESCE(SUM(output_tokens), 0),
		        COALESCE(SUM(cache_read_tokens), 0),
		        COUNT(CASE WHEN event_kind = 'tool_call' THEN 1 END),
		        COUNT(CASE WHEN event_kind = 'tool_call' AND tool_name LIKE 'mcp__%' THEN 1 END)
		 FROM events`,
	).Scan(&totalSessions, &activeSessions, &inputTokens, &outputTokens, &cacheReadTokens, &toolCalls, &mcpCalls)

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
// A session is "active" if its last event was within 5 minutes.
func QueryDashboardSessions(ctx context.Context, db *sql.DB) (active, completed []views.SessionSummary) {
	rows, err := db.QueryContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
		        COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
		        COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
		        COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
		        COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
		        COALESCE(last_model, ''),
		        COALESCE(working_dir, '')
		 FROM v_session_summary
		 ORDER BY ended_at DESC
		 LIMIT 30`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var s views.SessionSummary
		var source, model string
		var startedAt, endedAt time.Time
		if err := rows.Scan(&s.ID, &source, &startedAt, &endedAt,
			&s.TurnCount, &s.TotalTokens, &s.InputTokens, &s.OutputTokens,
			&s.CacheReadTokens, &s.CacheCreateTokens,
			&s.ToolCallCount, &s.MCPCallCount, &model, &s.WorkingDir); err != nil {
			continue
		}
		s.Actor = source
		s.ActiveModel = model
		setSessionTiming(&s, startedAt, endedAt, now)
		if s.Status == "active" {
			active = append(active, s)
		} else {
			completed = append(completed, s)
		}
	}
	return active, completed
}

// QueryRecentActivity returns a feed of recent events, filtered to meaningful entries.
func QueryRecentActivity(ctx context.Context, db *sql.DB) []views.ActivityItem {
	rows, err := db.QueryContext(ctx,
		`SELECT event_uid,
		        event_kind,
		        CASE
		            WHEN event_kind = 'tool_call' THEN 'Tool: ' || COALESCE(NULLIF(tool_name, ''), 'unknown')
		            WHEN event_kind = 'message' AND actor_role = 'user' AND COALESCE(NULLIF(text_preview, ''), '') != '' THEN text_preview
		            WHEN event_kind = 'message' AND actor_role = 'assistant' AND COALESCE(NULLIF(text_preview, ''), '') != '' THEN text_preview
		            WHEN event_kind = 'message' THEN COALESCE(NULLIF(actor_role, ''), 'message') || ' message'
		            WHEN event_kind = 'error' THEN COALESCE(NULLIF(error_code, ''), 'error') || ': ' || COALESCE(NULLIF(error_message, ''), NULLIF(text_preview, ''), 'unknown error')
		            WHEN event_kind = 'session_meta' THEN 'Session started'
		            ELSE event_kind
		        END AS summary,
		        COALESCE(session_id, ''),
		        timestamp
		 FROM events
		 WHERE event_kind IN ('message', 'tool_call', 'error', 'session_meta')
		 ORDER BY timestamp DESC
		 LIMIT 20`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var items []views.ActivityItem
	for rows.Next() {
		var item views.ActivityItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Summary, &item.SessionID, &item.Timestamp); err != nil {
			continue
		}
		item.Summary = shortenActivitySummary(item.Summary)
		items = append(items, item)
	}
	return items
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
	return chart
}

// QuerySessionDetail returns header, chart, and tool data for a session.
// The conversation trace is loaded separately via QuerySessionConversation.
func QuerySessionDetail(ctx context.Context, db *sql.DB, id string) (views.SessionDetailData, error) {
	var data views.SessionDetailData

	// Session info from view
	var source, model string
	var startedAt, endedAt time.Time
	err := db.QueryRowContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
		        COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
		        COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
		        COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
		        COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
		        COALESCE(last_model, ''),
		        COALESCE(working_dir, '')
		 FROM v_session_summary WHERE session_id = $1`, id,
	).Scan(&data.Session.ID, &source, &startedAt, &endedAt,
		&data.Session.TurnCount, &data.Session.TotalTokens,
		&data.Session.InputTokens, &data.Session.OutputTokens,
		&data.Session.CacheReadTokens, &data.Session.CacheCreateTokens,
		&data.Session.ToolCallCount, &data.Session.MCPCallCount, &model, &data.Session.WorkingDir)
	if err != nil {
		return data, err
	}
	data.Session.Actor = source
	data.Session.ActiveModel = model
	setSessionTiming(&data.Session, startedAt, endedAt, time.Now())

	// Run chart + tool queries in parallel
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		data.TokensChart = views.MultiSeriesChart{
			Datasets: []views.ChartDataset{{Label: "Total Tokens"}},
		}
		mcRows, _ := db.QueryContext(ctx,
			`SELECT timestamp, (input_tokens + output_tokens) AS total_tokens
			 FROM events WHERE session_id = $1 AND (input_tokens + output_tokens) > 0
			 ORDER BY timestamp`, id)
		if mcRows != nil {
			defer mcRows.Close()
			for mcRows.Next() {
				var ts time.Time
				var total int64
				if err := mcRows.Scan(&ts, &total); err != nil {
					continue
				}
				data.TokensChart.Labels = append(data.TokensChart.Labels, ts.Local().Format(time.RFC3339))
				data.TokensChart.Datasets[0].Values = append(data.TokensChart.Datasets[0].Values, float64(total))
			}
		}
	}()

	go func() {
		defer wg.Done()
		toolRows, _ := db.QueryContext(ctx,
			`SELECT tool_name,
			        COUNT(*) AS calls,
			        COALESCE(AVG(duration_ms), 0) AS avg_duration
			 FROM events
			 WHERE session_id = $1 AND event_kind = 'tool_call' AND tool_name IS NOT NULL AND tool_name != ''
			 GROUP BY tool_name ORDER BY calls DESC`, id)
		if toolRows != nil {
			defer toolRows.Close()
			for toolRows.Next() {
				var ts views.ToolStat
				if err := toolRows.Scan(&ts.Name, &ts.Calls, &ts.AvgDuration); err != nil {
					continue
				}
				ts.IsMCP = models.IsMCPTool(ts.Name)
				data.ToolStats = append(data.ToolStats, ts)
			}
		}
	}()

	go func() {
		defer wg.Done()
		modelRows, _ := db.QueryContext(ctx,
			`SELECT COALESCE(model, 'unknown'),
			        COALESCE(SUM(input_tokens), 0) AS input,
			        COALESCE(SUM(output_tokens), 0) AS output,
			        COALESCE(SUM(cache_read_tokens), 0) AS cache_read
			 FROM events
			 WHERE session_id = $1 AND model IS NOT NULL AND model != ''
			 GROUP BY model ORDER BY (input + output) DESC`, id)
		if modelRows != nil {
			defer modelRows.Close()
			for modelRows.Next() {
				var ds views.ChartDataset
				var input, output, cacheRead float64
				if err := modelRows.Scan(&ds.Label, &input, &output, &cacheRead); err != nil {
					continue
				}
				ds.Values = []float64{input, output, cacheRead}
				data.TokensByModel = append(data.TokensByModel, ds)
			}
		}
	}()

	wg.Wait()
	return data, nil
}

// QuerySessionConversation returns the conversation trace for a session.
func QuerySessionConversation(ctx context.Context, db *sql.DB, id string) ([]views.ChatTurn, []views.TurnDetail) {
	traceRows, err := db.QueryContext(ctx,
		`SELECT e.event_uid, e.event_kind, COALESCE(e.actor_role, ''),
		        COALESCE(e.text_content, ''), COALESCE(e.text_preview, ''),
		        COALESCE(e.tool_name, ''), COALESCE(e.model, ''),
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
		if err := traceRows.Scan(&es.EventUID, &es.EventKind, &es.ActorRole,
			&es.TextContent, &es.TextPreview,
			&es.ToolName, &es.Model, &es.Tokens, &es.DurationMs, &es.Timestamp, &turnSeq,
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
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:      views.ChatBlockToolChain,
					ToolChain: pendingToolChain,
				})
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
				// Look ahead for a matching tool_result, skipping event_msg
				for j := i + 1; j < len(t.Events); j++ {
					if t.Events[j].EventKind == "tool_result" {
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
				pendingToolChain = append(pendingToolChain, item)

			case "tool_result":
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
			case e.EventKind == "reasoning":
				key = e.EventKind + "|" + e.TextContent
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

const activeSessionThreshold = 5 * time.Minute

func setSessionTiming(s *views.SessionSummary, startedAt, endedAt, now time.Time) {
	s.StartedAt = startedAt
	s.EndedAt = endedAt

	// Use the most recent activity timestamp to determine if session is still active.
	lastActivity := startedAt
	if !endedAt.IsZero() && endedAt.After(startedAt) {
		lastActivity = endedAt
	}

	if now.Sub(lastActivity) < activeSessionThreshold {
		s.Status = "active"
		s.Duration = formatDuration(now.Sub(startedAt))
	} else {
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

