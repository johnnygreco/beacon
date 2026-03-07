package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/technodrome-ai/technodrome/internal/models"
	"github.com/technodrome-ai/technodrome/internal/views"
)

// QueryDashboardData queries all data needed for the dashboard page.
func QueryDashboardData(ctx context.Context, db *sql.DB) views.DashboardData {
	return views.DashboardData{
		Metrics:        QueryDashboardMetrics(ctx, db),
		ActiveSessions: QueryActiveSessions(ctx, db),
		RecentActivity: QueryRecentActivity(ctx, db),
		TokensByModel:  QueryTokensByModelSummary(ctx, db),
	}
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
		        COUNT(DISTINCT CASE WHEN timestamp > current_timestamp - INTERVAL '1 hour' THEN session_id END),
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
		{Label: "Active Sessions", Value: fmt.Sprintf("%d", activeSessions), Trend: "neutral"},
		{Label: "Total Tokens", Value: views.FormatTokens(totalTokens),
			Sublabel: fmt.Sprintf("In: %s  Out: %s  Cache: %s", views.FormatTokens(inputTokens), views.FormatTokens(outputTokens), views.FormatTokens(cacheReadTokens))},
		{Label: "Tool Calls", Value: fmt.Sprintf("%d", toolCalls),
			Sublabel: fmt.Sprintf("%d MCP", mcpCalls)},
	}
}

// QueryActiveSessions returns session summaries ordered by most recent.
func QueryActiveSessions(ctx context.Context, db *sql.DB) []views.SessionSummary {
	rows, err := db.QueryContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
		        turn_count, total_tokens, total_input_tokens, total_output_tokens,
		        total_cache_read_tokens, total_cache_create_tokens,
		        tool_call_count, mcp_call_count, COALESCE(last_model, '')
		 FROM v_session_summary
		 ORDER BY started_at DESC
		 LIMIT 20`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var sessions []views.SessionSummary
	for rows.Next() {
		var s views.SessionSummary
		var source, model string
		var startedAt, endedAt time.Time
		if err := rows.Scan(&s.ID, &source, &startedAt, &endedAt,
			&s.TurnCount, &s.TotalTokens, &s.InputTokens, &s.OutputTokens,
			&s.CacheReadTokens, &s.CacheCreateTokens,
			&s.ToolCallCount, &s.MCPCallCount, &model); err != nil {
			continue
		}
		s.Actor = source
		s.ActiveModel = model
		setSessionTiming(&s, startedAt, endedAt)
		sessions = append(sessions, s)
	}
	return sessions
}

// QueryRecentActivity returns a feed of recent events.
func QueryRecentActivity(ctx context.Context, db *sql.DB) []views.ActivityItem {
	rows, err := db.QueryContext(ctx,
		`SELECT event_uid,
		        event_kind,
		        CASE
		            WHEN event_kind = 'tool_call' OR event_kind = 'tool_result' THEN COALESCE(tool_name, 'unknown')
		            WHEN event_kind = 'message' THEN COALESCE(actor_role, '') || ': ' || COALESCE(text_preview, '')
		            WHEN event_kind = 'error' THEN COALESCE(error_code, 'error') || ': ' || COALESCE(error_message, '')
		            ELSE COALESCE(text_preview, event_kind)
		        END AS summary,
		        COALESCE(session_id, ''),
		        timestamp
		 FROM events
		 ORDER BY timestamp DESC
		 LIMIT 30`)
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
		`SELECT minute, total_input, total_output, total_cache_read
		 FROM v_tokens_per_minute
		 ORDER BY minute ASC
		 LIMIT 60`)
	if err != nil {
		return chart
	}
	defer rows.Close()

	for rows.Next() {
		var minute string
		var input, output, cacheRead int64
		if err := rows.Scan(&minute, &input, &output, &cacheRead); err != nil {
			continue
		}
		chart.Labels = append(chart.Labels, minute)
		chart.Datasets[0].Values = append(chart.Datasets[0].Values, float64(input))
		chart.Datasets[1].Values = append(chart.Datasets[1].Values, float64(output))
		chart.Datasets[2].Values = append(chart.Datasets[2].Values, float64(cacheRead))
	}
	return chart
}

// QuerySessionDetail returns full detail for a single session.
func QuerySessionDetail(ctx context.Context, db *sql.DB, id string) (views.SessionDetailData, error) {
	var data views.SessionDetailData

	// Session info from view
	var source, model string
	var startedAt, endedAt time.Time
	err := db.QueryRowContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
		        turn_count, total_tokens, total_input_tokens, total_output_tokens,
		        total_cache_read_tokens, total_cache_create_tokens,
		        tool_call_count, mcp_call_count, COALESCE(last_model, '')
		 FROM v_session_summary WHERE session_id = $1`, id,
	).Scan(&data.Session.ID, &source, &startedAt, &endedAt,
		&data.Session.TurnCount, &data.Session.TotalTokens,
		&data.Session.InputTokens, &data.Session.OutputTokens,
		&data.Session.CacheReadTokens, &data.Session.CacheCreateTokens,
		&data.Session.ToolCallCount, &data.Session.MCPCallCount, &model)
	if err != nil {
		return data, err
	}
	data.Session.Actor = source
	data.Session.ActiveModel = model
	setSessionTiming(&data.Session, startedAt, endedAt)

	// Get events grouped by turn from v_conversation_trace
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
	if err == nil {
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

		for _, seq := range turnOrder {
			data.Turns = append(data.Turns, *turnMap[seq])
		}

		data.ChatTurns = buildChatTurns(data.Turns)
	}

	// Run remaining queries in parallel
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		data.TokensChart = views.MultiSeriesChart{
			Datasets: []views.ChartDataset{
				{Label: "Input"},
				{Label: "Output"},
				{Label: "Cache Read"},
			},
		}
		mcRows, _ := db.QueryContext(ctx,
			`SELECT timestamp, input_tokens, output_tokens, cache_read_tokens
			 FROM events WHERE session_id = $1 AND (input_tokens + output_tokens) > 0
			 ORDER BY timestamp`, id)
		if mcRows != nil {
			defer mcRows.Close()
			for mcRows.Next() {
				var ts time.Time
				var input, output, cacheRead int64
				mcRows.Scan(&ts, &input, &output, &cacheRead)
				data.TokensChart.Labels = append(data.TokensChart.Labels, ts.Format(time.RFC3339))
				data.TokensChart.Datasets[0].Values = append(data.TokensChart.Datasets[0].Values, float64(input))
				data.TokensChart.Datasets[1].Values = append(data.TokensChart.Datasets[1].Values, float64(output))
				data.TokensChart.Datasets[2].Values = append(data.TokensChart.Datasets[2].Values, float64(cacheRead))
			}
		}
	}()

	go func() {
		defer wg.Done()
		toolRows, _ := db.QueryContext(ctx,
			`SELECT tool_name,
			        COUNT(*) AS calls,
			        AVG(duration_ms) AS avg_duration
			 FROM events
			 WHERE session_id = $1 AND event_kind = 'tool_call' AND tool_name IS NOT NULL AND tool_name != ''
			 GROUP BY tool_name ORDER BY calls DESC`, id)
		if toolRows != nil {
			defer toolRows.Close()
			for toolRows.Next() {
				var ts views.ToolStat
				toolRows.Scan(&ts.Name, &ts.Calls, &ts.AvgDuration)
				ts.IsMCP = models.IsMCPTool(ts.Name)
				data.ToolStats = append(data.ToolStats, ts)
			}
		}
	}()

	go func() {
		defer wg.Done()
		modelRows, _ := db.QueryContext(ctx,
			`SELECT COALESCE(model, 'unknown'),
			        SUM(input_tokens) AS input,
			        SUM(output_tokens) AS output,
			        SUM(cache_read_tokens) AS cache_read
			 FROM events
			 WHERE session_id = $1 AND model IS NOT NULL AND model != ''
			 GROUP BY model ORDER BY (input + output) DESC`, id)
		if modelRows != nil {
			defer modelRows.Close()
			for modelRows.Next() {
				var ds views.ChartDataset
				var input, output, cacheRead float64
				modelRows.Scan(&ds.Label, &input, &output, &cacheRead)
				ds.Values = []float64{input, output, cacheRead}
				data.TokensByModel = append(data.TokensByModel, ds)
			}
		}
	}()

	wg.Wait()
	return data, nil
}

// parseToolParams extracts structured parameters from tool input JSON.
// Returns nil if the JSON is empty or unparseable.
func parseToolParams(toolName, inputJSON string) *views.ToolCallParams {
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

		flushToolChain := func() {
			if len(pendingToolChain) > 0 {
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:      views.ChatBlockToolChain,
					ToolChain: pendingToolChain,
				})
				pendingToolChain = nil
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
				item.Params = parseToolParams(e.ToolName, e.InputJSON)
				// Look ahead for a matching tool_result
				if i+1 < len(t.Events) && t.Events[i+1].EventKind == "tool_result" {
					i++
					result := t.Events[i]
					item.ResultEvent = &result
					item.OutputPreview = result.OutputPreview
					if item.OutputPreview == "" {
						item.OutputPreview = result.TextPreview
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

			case "reasoning":
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockReasoning,
					Message: &eCopy,
				})

			case "error":
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockError,
					Message: &eCopy,
				})

			default:
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockAssistantMessage,
					Message: &eCopy,
				})
			}
		}

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

func setSessionTiming(s *views.SessionSummary, startedAt, endedAt time.Time) {
	s.StartedAt = startedAt
	if !endedAt.IsZero() && endedAt.After(startedAt) {
		s.Status = "completed"
		s.Duration = endedAt.Sub(startedAt).Truncate(time.Second).String()
	} else {
		s.Status = "active"
		s.Duration = time.Since(startedAt).Truncate(time.Second).String()
	}
}

