package web

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/technodrome-ai/technodrome/internal/views"
)

// QueryDashboardData queries all data needed for the dashboard page.
func QueryDashboardData(ctx context.Context, db *sql.DB) views.DashboardData {
	return views.DashboardData{
		Metrics:        QueryDashboardMetrics(ctx, db),
		ActiveSessions: QueryActiveSessions(ctx, db),
		RecentActivity: QueryRecentActivity(ctx, db),
	}
}

// QueryDashboardMetrics returns the top-level metric cards.
func QueryDashboardMetrics(ctx context.Context, db *sql.DB) []views.MetricData {
	var totalSessions, activeSessions int
	var totalTokens int64
	var totalCost float64

	db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT session_id),
		        COUNT(DISTINCT CASE WHEN timestamp > current_timestamp - INTERVAL '1 hour' THEN session_id END),
		        COALESCE(SUM(input_tokens + output_tokens), 0),
		        COALESCE(SUM(cost_usd), 0)
		 FROM events`,
	).Scan(&totalSessions, &activeSessions, &totalTokens, &totalCost)

	return []views.MetricData{
		{Label: "Total Sessions", Value: fmt.Sprintf("%d", totalSessions)},
		{Label: "Active Sessions", Value: fmt.Sprintf("%d", activeSessions), Trend: "neutral"},
		{Label: "Total Tokens", Value: formatTokens(totalTokens)},
		{Label: "Total Cost", Value: fmt.Sprintf("$%.2f", totalCost)},
	}
}

// QueryActiveSessions returns session summaries ordered by most recent.
func QueryActiveSessions(ctx context.Context, db *sql.DB) []views.SessionSummary {
	rows, err := db.QueryContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
		        total_cost, turn_count, total_tokens, COALESCE(last_model, '')
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
		if err := rows.Scan(&s.ID, &source, &startedAt, &endedAt, &s.TotalCost, &s.TurnCount, &s.TotalTokens, &model); err != nil {
			continue
		}
		s.Actor = source
		s.ActiveModel = model
		s.StartedAt = startedAt
		if !endedAt.IsZero() && endedAt.After(startedAt) {
			s.Status = "completed"
			s.Duration = endedAt.Sub(startedAt).Truncate(time.Second).String()
		} else {
			s.Status = "active"
			s.Duration = time.Since(startedAt).Truncate(time.Second).String()
		}
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

// QueryChartData returns token and cost time-series data for charts.
func QueryChartData(ctx context.Context, db *sql.DB) (views.ChartData, views.ChartData) {
	tokensChart := views.ChartData{}
	costChart := views.ChartData{}

	rows, err := db.QueryContext(ctx,
		`SELECT minute, total_tokens, total_cost
		 FROM v_tokens_per_minute
		 ORDER BY minute ASC
		 LIMIT 60`)
	if err != nil {
		return tokensChart, costChart
	}
	defer rows.Close()

	var cumCost float64
	for rows.Next() {
		var minute string
		var tokens int64
		var cost float64
		if err := rows.Scan(&minute, &tokens, &cost); err != nil {
			continue
		}
		cumCost += cost
		tokensChart.Labels = append(tokensChart.Labels, minute)
		tokensChart.Values = append(tokensChart.Values, float64(tokens))
		costChart.Labels = append(costChart.Labels, minute)
		costChart.Values = append(costChart.Values, cumCost)
	}
	return tokensChart, costChart
}

// QuerySessionDetail returns full detail for a single session.
func QuerySessionDetail(ctx context.Context, db *sql.DB, id string) (views.SessionDetailData, error) {
	var data views.SessionDetailData

	// Session info from view
	var source, model string
	var startedAt, endedAt time.Time
	err := db.QueryRowContext(ctx,
		`SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
		        total_cost, turn_count, total_tokens, COALESCE(last_model, '')
		 FROM v_session_summary WHERE session_id = $1`, id,
	).Scan(&data.Session.ID, &source, &startedAt, &endedAt, &data.Session.TotalCost,
		&data.Session.TurnCount, &data.Session.TotalTokens, &model)
	if err != nil {
		return data, err
	}
	data.Session.Actor = source
	data.Session.ActiveModel = model
	data.Session.StartedAt = startedAt
	if !endedAt.IsZero() && endedAt.After(startedAt) {
		data.Session.Status = "completed"
		data.Session.Duration = endedAt.Sub(startedAt).Truncate(time.Second).String()
	} else {
		data.Session.Status = "active"
		data.Session.Duration = time.Since(startedAt).Truncate(time.Second).String()
	}

	// Get events grouped by turn from v_conversation_trace
	traceRows, err := db.QueryContext(ctx,
		`SELECT event_uid, event_kind, COALESCE(actor_role, ''), COALESCE(text_preview, ''),
		        COALESCE(tool_name, ''), COALESCE(model, ''),
		        input_tokens + output_tokens, cost_usd, duration_ms, timestamp, turn_seq
		 FROM v_conversation_trace
		 WHERE session_id = $1
		 ORDER BY event_order`, id)
	if err == nil {
		defer traceRows.Close()

		turnMap := make(map[int]*views.TurnDetail)
		var turnOrder []int

		for traceRows.Next() {
			var es views.EventSummary
			var turnSeq int
			if err := traceRows.Scan(&es.EventUID, &es.EventKind, &es.ActorRole, &es.TextPreview,
				&es.ToolName, &es.Model, &es.Tokens, &es.Cost, &es.DurationMs, &es.Timestamp, &turnSeq); err != nil {
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
			td.TotalCost += es.Cost
		}

		for _, seq := range turnOrder {
			data.Turns = append(data.Turns, *turnMap[seq])
		}
	}

	// Token chart data for session
	mcRows, _ := db.QueryContext(ctx,
		`SELECT timestamp, input_tokens + output_tokens AS tokens
		 FROM events WHERE session_id = $1 AND (input_tokens + output_tokens) > 0
		 ORDER BY timestamp`, id)
	if mcRows != nil {
		defer mcRows.Close()
		for mcRows.Next() {
			var ts time.Time
			var tokens int64
			mcRows.Scan(&ts, &tokens)
			data.TokensChart.Labels = append(data.TokensChart.Labels, ts.Format(time.RFC3339))
			data.TokensChart.Values = append(data.TokensChart.Values, float64(tokens))
		}
	}

	return data, nil
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
