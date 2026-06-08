package web

import (
	"context"
	"database/sql"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

// QuerySessionDetail returns header, chart, and tool data for a session.
// The conversation trace is loaded separately via QuerySessionConversation.
func QuerySessionDetail(ctx context.Context, db *sql.DB, id string) (views.SessionDetailData, error) {
	return QuerySessionDetailScoped(ctx, db, id, APIScopeFilters{})
}

func QuerySessionDetailScoped(ctx context.Context, db *sql.DB, id string, scope APIScopeFilters) (views.SessionDetailData, error) {
	var data views.SessionDetailData
	now := time.Now()
	activeCutoff := now.Add(-idleThreshold)

	// Session info from view
	sessionWhere := "session_id = ?"
	sessionArgs := []any{id}
	sessionScope := scope.withoutProjectKeys()
	if clause, scopeArgs := sessionScope.sqlAndClause(""); clause != "" {
		sessionWhere += clause
		sessionArgs = append(sessionArgs, scopeArgs...)
	}
	sessionSource, sourceArgs := sessionProjectionSubqueryForScope(sessionWhere, scope)
	queryArgs := []any{activeCutoff}
	queryArgs = append(queryArgs, sourceArgs...)
	queryArgs = append(queryArgs, sessionArgs...)
	row := db.QueryRowContext(ctx,
		`SELECT `+sessionSummaryColumnsWithReopenedFlag()+`
		 FROM `+sessionSource, queryArgs...)
	session, err := scanSessionSummaryIncludingReopened(row, now)
	if err != nil {
		return data, err
	}
	data.Session = session

	// Query child subagent sessions
	data.Session.ChildSessions = QueryChildSessionsScoped(ctx, db, id, scope)

	// Single CTE query for chart, tool stats, and model breakdown
	data.TokensChart = views.MultiSeriesChart{
		Datasets: []views.ChartDataset{{Label: "Total Tokens"}},
	}

	analyticsWhere := "session_id = ?"
	analyticsArgs := []any{id}
	if clause, scopeArgs := scope.sqlAndClause(""); clause != "" {
		analyticsWhere += clause
		analyticsArgs = append(analyticsArgs, scopeArgs...)
	}
	analyticsArgs = append(analyticsArgs, session.Provider)
	rows, err := db.QueryContext(ctx,
		`WITH session_analytics AS (
			SELECT * FROM `+analyticsProjectionSubquery(analyticsWhere)+`
		),
		token_series AS (
			SELECT minute AS timestamp, sum(total_tokens) AS tokens_total
			FROM session_analytics
			WHERE total_tokens > 0
			GROUP BY minute
			ORDER BY minute
		),
		tool_stats AS (
			SELECT tool_name,
			       sum(tool_call_count) AS calls,
			       if(sum(tool_call_count) > 0, sumIf(duration_ms_sum, event_kind = 'tool_call') / sum(tool_call_count), 0) AS avg_duration
			FROM session_analytics
			WHERE tool_name IS NOT NULL AND tool_name != ''
			GROUP BY tool_name ORDER BY calls DESC
		),
		model_breakdown AS (
			SELECT model_key AS model,
			       provider_key AS provider,
			       COALESCE(SUM(input_tokens), 0) AS input,
			       COALESCE(SUM(output_tokens), 0) AS output,
			       COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
			       COALESCE(SUM(total_tokens), 0) AS total
			FROM (
				SELECT COALESCE(NULLIF(model, ''), 'unknown') AS model_key,
				       COALESCE(NULLIF(provider, ''), NULLIF(?, ''), 'unknown') AS provider_key,
				       input_tokens,
				       output_tokens,
				       cache_read_tokens,
				       total_tokens
				FROM session_analytics
				WHERE COALESCE(model, '') != '<synthetic>'
				  AND (input_tokens != 0 OR output_tokens != 0 OR cache_read_tokens != 0 OR total_tokens != 0)
			)
			GROUP BY provider_key, model_key ORDER BY COALESCE(SUM(total_tokens), 0) DESC
			)
		SELECT 'token' AS kind, timestamp, toInt64(tokens_total), '' AS tool_name, toInt64(0) AS calls, toFloat64(0) AS avg_dur, '' AS model, '' AS provider, toInt64(0) AS input, toInt64(0) AS output, toInt64(0) AS cache_read FROM token_series
		UNION ALL
		SELECT 'tool', toDateTime64(0, 3), toInt64(0), tool_name, toInt64(calls), toFloat64(avg_duration), '', '', toInt64(0), toInt64(0), toInt64(0) FROM tool_stats
		UNION ALL
		SELECT 'model', toDateTime64(0, 3), toInt64(total), '', toInt64(0), toFloat64(0), model, provider, toInt64(input), toInt64(output), toInt64(cache_read) FROM model_breakdown`, analyticsArgs...)
	if err != nil {
		logQueryError("session detail analytics", err)
		return data, nil // Return partial data on query error
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var ts time.Time
		var totalTokens int64
		var toolName string
		var calls int64
		var avgDur float64
		var model, provider string
		var input, output, cacheRead int64
		if err := rows.Scan(&kind, &ts, &totalTokens, &toolName, &calls, &avgDur, &model, &provider, &input, &output, &cacheRead); err != nil {
			logQueryScanError("session detail analytics", err)
			continue
		}
		switch kind {
		case "token":
			data.TokensChart.Labels = append(data.TokensChart.Labels, ts.Local().Format(time.RFC3339))
			data.TokensChart.Datasets[0].Values = append(data.TokensChart.Datasets[0].Values, float64(totalTokens))
		case "tool":
			stat := views.ToolStat{Name: toolName, Calls: int(calls), AvgDuration: avgDur}
			stat.IsMCP = models.IsMCPTool(toolName)
			data.ToolStats = append(data.ToolStats, stat)
		case "model":
			if input == 0 && output == 0 && cacheRead == 0 && totalTokens > 0 {
				output = totalTokens
			}
			mt := views.ModelTokens{
				Model:     model,
				Provider:  provider,
				Input:     input,
				Output:    output,
				CacheRead: cacheRead,
				Total:     totalTokens,
			}
			if mt.Total == 0 {
				mt.Total = input + output + cacheRead
			}
			data.TokensByModel = append(data.TokensByModel, mt)
		}
	}
	if err := rows.Err(); err != nil {
		logQueryError("session detail analytics rows", err)
		return data, nil
	}

	return data, nil
}
