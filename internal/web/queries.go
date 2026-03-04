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

	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&totalSessions)
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE ended_at IS NULL`).Scan(&activeSessions)
	db.QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens + output_tokens), 0) FROM model_calls`).Scan(&totalTokens)
	db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_usd), 0) FROM model_calls`).Scan(&totalCost)

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
		`SELECT id, source, started_at, ended_at, total_cost,
		        COALESCE(turn_count, 0), COALESCE(total_tokens, 0)
		 FROM session_summaries
		 ORDER BY started_at DESC
		 LIMIT 20`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var sessions []views.SessionSummary
	for rows.Next() {
		var s views.SessionSummary
		var source string
		var startedAt time.Time
		var endedAt sql.NullTime
		if err := rows.Scan(&s.ID, &source, &startedAt, &endedAt, &s.TotalCost, &s.TurnCount, &s.TotalTokens); err != nil {
			continue
		}
		s.Actor = source
		s.StartedAt = startedAt
		if endedAt.Valid {
			s.Status = "completed"
			s.Duration = endedAt.Time.Sub(startedAt).Truncate(time.Second).String()
		} else {
			s.Status = "active"
			s.Duration = time.Since(startedAt).Truncate(time.Second).String()
		}
		sessions = append(sessions, s)
	}
	return sessions
}

// QueryRecentActivity returns a mixed feed of recent model calls, tool calls, and errors.
func QueryRecentActivity(ctx context.Context, db *sql.DB) []views.ActivityItem {
	rows, err := db.QueryContext(ctx,
		`SELECT * FROM (
			SELECT id, 'model_call' AS type,
			       COALESCE(model, 'unknown') || ' (' || CAST(input_tokens + output_tokens AS VARCHAR) || ' tok)' AS summary,
			       COALESCE(session_id, '') AS session_id, created_at
			FROM model_calls
			UNION ALL
			SELECT id, 'tool_call' AS type,
			       COALESCE(tool_name, 'unknown') || CASE WHEN success THEN ' (ok)' ELSE ' (fail)' END AS summary,
			       COALESCE(session_id, '') AS session_id, created_at
			FROM tool_calls
			UNION ALL
			SELECT id, 'error' AS type,
			       COALESCE(error_code, 'error') || ': ' || COALESCE(message, '') AS summary,
			       COALESCE(session_id, '') AS session_id, created_at
			FROM api_errors
		) combined
		ORDER BY created_at DESC
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
		 FROM tokens_per_minute
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

	// Session info
	var source string
	var startedAt time.Time
	var endedAt sql.NullTime
	err := db.QueryRowContext(ctx,
		`SELECT id, source, started_at, ended_at, total_cost,
		        COALESCE(turn_count, 0), COALESCE(total_tokens, 0)
		 FROM session_summaries WHERE id = $1`, id,
	).Scan(&data.Session.ID, &source, &startedAt, &endedAt, &data.Session.TotalCost,
		&data.Session.TurnCount, &data.Session.TotalTokens)
	if err != nil {
		return data, err
	}
	data.Session.Actor = source
	data.Session.StartedAt = startedAt
	if endedAt.Valid {
		data.Session.Status = "completed"
		data.Session.Duration = endedAt.Time.Sub(startedAt).Truncate(time.Second).String()
	} else {
		data.Session.Status = "active"
		data.Session.Duration = time.Since(startedAt).Truncate(time.Second).String()
	}

	// Turns
	turnRows, err := db.QueryContext(ctx,
		`SELECT id, turn_number, user_prompt, started_at,
		        input_tokens, output_tokens, cost_usd
		 FROM turns WHERE session_id = $1 ORDER BY turn_number`, id)
	if err == nil {
		defer turnRows.Close()
		for turnRows.Next() {
			var td views.TurnDetail
			var turnNum int
			var inputTokens, outputTokens int64
			if err := turnRows.Scan(&td.ID, &turnNum, &td.Content, &td.Timestamp,
				&inputTokens, &outputTokens, &td.Cost); err != nil {
				continue
			}
			td.Role = "user"
			td.Tokens = inputTokens + outputTokens

			// Get tool calls for this turn
			tcRows, _ := db.QueryContext(ctx,
				`SELECT tool_name, success, duration_ms FROM tool_calls WHERE turn_id = $1`, td.ID)
			if tcRows != nil {
				for tcRows.Next() {
					var tc views.ToolCallInfo
					var success bool
					var durMs int64
					tcRows.Scan(&tc.Name, &success, &durMs)
					if success {
						tc.Status = "success"
					} else {
						tc.Status = "error"
					}
					tc.Duration = fmt.Sprintf("%dms", durMs)
					td.ToolCalls = append(td.ToolCalls, tc)
				}
				tcRows.Close()
			}

			data.Turns = append(data.Turns, td)
		}
	}

	// Token chart data for session
	mcRows, _ := db.QueryContext(ctx,
		`SELECT created_at, input_tokens + output_tokens AS tokens
		 FROM model_calls WHERE session_id = $1 ORDER BY created_at`, id)
	if mcRows != nil {
		defer mcRows.Close()
		for mcRows.Next() {
			var ts time.Time
			var tokens int64
			mcRows.Scan(&ts, &tokens)
			data.TokensChart.Labels = append(data.TokensChart.Labels, ts.Format("15:04:05"))
			data.TokensChart.Values = append(data.TokensChart.Values, float64(tokens))
		}
	}

	// Context chart data
	csRows, _ := db.QueryContext(ctx,
		`SELECT created_at, tokens_in_context FROM context_snapshots
		 WHERE session_id = $1 ORDER BY created_at`, id)
	if csRows != nil {
		defer csRows.Close()
		for csRows.Next() {
			var ts time.Time
			var tokens int64
			csRows.Scan(&ts, &tokens)
			data.ContextChart.Labels = append(data.ContextChart.Labels, ts.Format("15:04:05"))
			data.ContextChart.Values = append(data.ContextChart.Values, float64(tokens))
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
