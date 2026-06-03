package web

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

// QueryDashboardData queries all data needed for the dashboard page.
func QueryDashboardData(ctx context.Context, db *sql.DB) views.DashboardData {
	var data views.DashboardData
	var wg sync.WaitGroup
	defaultRange := "24h"
	wg.Add(4)
	// QueryDashboardData owns these short-lived query workers and waits for all
	// of them before returning. Each worker uses the caller's context.
	go func() {
		defer wg.Done()
		data.ActiveSessions, data.CompletedSessions, data.HasMoreSessions = QueryDashboardSessions(ctx, db)
	}()
	go func() {
		defer wg.Done()
		data.RecentActivity = QueryRecentActivity(ctx, db)
	}()
	go func() { defer wg.Done(); data.TokensByModel = QueryTokensByModelSummary(ctx, db) }()
	go func() {
		defer wg.Done()
		data.TokenCumulative, data.ModelActivity = QueryDashboardModelAnalytics(ctx, db, parseRange(defaultRange), defaultRange)
	}()
	wg.Wait()
	return data
}

// QueryTokensByModelSummary returns token counts broken down by model.
func QueryTokensByModelSummary(ctx context.Context, db *sql.DB) []views.ModelTokens {
	rows, err := db.QueryContext(ctx,
		`SELECT COALESCE(model, 'unknown'),
		        COALESCE(provider, ''),
		        COALESCE(SUM(input_tokens), 0),
		        COALESCE(SUM(output_tokens), 0),
		        COALESCE(SUM(cache_read_tokens), 0),
		        COALESCE(SUM(total_tokens), 0)
		 FROM `+analyticsProjectionSQL+`
		 WHERE model IS NOT NULL AND model != '' AND model != '<synthetic>'
		 GROUP BY provider, model
		 ORDER BY COALESCE(SUM(total_tokens), 0) DESC
		 LIMIT 10`)
	if err != nil {
		logQueryError("tokens by model summary", err)
		return nil
	}
	defer rows.Close()

	var result []views.ModelTokens
	for rows.Next() {
		var m views.ModelTokens
		if err := rows.Scan(&m.Model, &m.Provider, &m.Input, &m.Output, &m.CacheRead, &m.Total); err != nil {
			logQueryScanError("tokens by model summary", err)
			continue
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		logQueryError("tokens by model summary rows", err)
		return nil
	}
	return result
}

type dashboardModelPoint struct {
	Bucket          time.Time
	Provider        string
	Model           string
	Tokens          int64
	InputTokens     int64
	OutputTokens    int64
	CacheReadTokens int64
	ToolCalls       int64
	Calls           int64
	Errors          int64
}

type dashboardModelKey struct {
	Provider string
	Model    string
}

// QueryDashboardModelAnalytics returns range-aware model series used by the live
// dashboard charts. Tokens and activity metrics remain per bucket so volume,
// rates, and spikes stay visible inside the selected range.
func QueryDashboardModelAnalytics(ctx context.Context, db *sql.DB, since *time.Time, rangeVal string) (views.ModelSeriesChart, views.ModelMetricChart) {
	bucketMinutes := dashboardBucketMinutes(rangeVal)
	timeUnit := dashboardTimeUnit(bucketMinutes)
	tokenChart, metricChart := emptyDashboardModelCharts(bucketMinutes, timeUnit)

	rangeSessionFilter := "WHERE model != '<synthetic>'"
	rangeResultFilter := ""
	args := []any{}
	if since != nil {
		rangeSessionFilter += " AND minute >= ?"
		args = append(args, *since)
		rangeResultFilter = "WHERE a.minute >= ?"
	}

	query := fmt.Sprintf(`WITH range_sessions AS (
			SELECT session_id
			FROM `+analyticsProjectionSQL+`
			%s
			GROUP BY session_id
		),
		session_analytics AS (
			SELECT *
			FROM `+analyticsProjectionSQL+`
			WHERE model != '<synthetic>'
			  AND session_id IN (SELECT session_id FROM range_sessions)
		),
		session_model_fallbacks AS (
			SELECT session_id,
			       if(
			           uniqExactIf(model, model != '' AND model != '<synthetic>') = 1,
			           anyIf(model, model != '' AND model != '<synthetic>'),
			           ''
			       ) AS fallback_model,
			       if(
			           uniqExactIf(model, model != '' AND model != '<synthetic>') = 1,
			           anyIf(provider, model != '' AND model != '<synthetic>'),
			           ''
			       ) AS fallback_provider
			FROM session_analytics
			GROUP BY session_id
		),
		attributed AS (
			SELECT a.session_id AS session_id,
			       a.minute AS minute,
			       a.provider AS provider,
			       a.model AS model,
			       last_value(if(a.model != '', toNullable(a.model), NULL)) IGNORE NULLS
			           OVER (PARTITION BY a.session_id ORDER BY a.minute, a.model = '', a.provider, a.model, a.tool_name, a.event_kind ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS prior_model,
			       last_value(if(a.model != '', toNullable(a.provider), NULL)) IGNORE NULLS
			           OVER (PARTITION BY a.session_id ORDER BY a.minute, a.model = '', a.provider, a.model, a.tool_name, a.event_kind ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS prior_provider,
			       a.event_kind AS event_kind,
			       a.event_count AS event_count,
			       a.call_count AS call_count,
			       a.tool_call_count AS tool_call_count,
			       a.input_tokens AS input_tokens,
			       a.output_tokens AS output_tokens,
			       a.cache_read_tokens AS cache_read_tokens,
			       a.total_tokens AS total_tokens
			FROM session_analytics AS a
		),
		model_analytics AS (
			SELECT a.session_id AS session_id,
			       a.minute AS minute,
			       multiIf(
			           a.model != '', COALESCE(NULLIF(a.provider, ''), 'unknown'),
			           ifNull(a.prior_model, '') != '', COALESCE(NULLIF(ifNull(a.prior_provider, ''), ''), NULLIF(a.provider, ''), 'unknown'),
			           sf.fallback_model != '', COALESCE(NULLIF(sf.fallback_provider, ''), NULLIF(a.provider, ''), 'unknown'),
			           COALESCE(NULLIF(a.provider, ''), 'unknown')
			       ) AS provider_key,
			       multiIf(
			           a.model != '', a.model,
			           ifNull(a.prior_model, '') != '', ifNull(a.prior_model, ''),
			           sf.fallback_model != '', sf.fallback_model,
			           ''
			       ) AS model_key,
			       a.event_kind AS event_kind,
			       a.event_count AS event_count,
			       a.call_count AS call_count,
			       a.tool_call_count AS tool_call_count,
			       a.input_tokens AS input_tokens,
			       a.output_tokens AS output_tokens,
			       a.cache_read_tokens AS cache_read_tokens,
			       a.total_tokens AS total_tokens
			FROM attributed AS a
			LEFT JOIN session_model_fallbacks AS sf ON a.session_id = sf.session_id
			%s
		),
		plottable_model_analytics AS (
			SELECT *
			FROM model_analytics
			WHERE model_key != ''
			  AND model_key != '<synthetic>'
			  AND (
				total_tokens != 0
				OR call_count != 0
				OR tool_call_count != 0
				OR event_kind IN ('error', 'tool_error')
			  )
		),
		top_models AS (
			SELECT provider_key,
			       model_key
			FROM plottable_model_analytics
			GROUP BY provider_key, model_key
			ORDER BY sum(total_tokens) DESC, sum(tool_call_count) DESC, sum(event_count) DESC
			LIMIT %d
		)
		SELECT toStartOfInterval(minute, INTERVAL %d MINUTE) AS bucket,
		       provider_key,
		       model_key,
		       sum(total_tokens) AS tokens,
		       sum(input_tokens) AS input_tokens,
		       sum(output_tokens) AS output_tokens,
		       sum(cache_read_tokens) AS cache_read_tokens,
		       sum(tool_call_count) AS tool_calls,
		       sum(call_count) AS calls,
		       sumIf(event_count, event_kind IN ('error', 'tool_error')) AS errors
		FROM plottable_model_analytics
		WHERE (provider_key, model_key) IN (
			SELECT provider_key, model_key FROM top_models
		  )
		GROUP BY bucket, provider_key, model_key
		ORDER BY bucket ASC`, rangeSessionFilter, rangeResultFilter, dashboardModelLimit, bucketMinutes)
	if since != nil {
		args = append(args, *since)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		logQueryError("dashboard model analytics", err)
		return tokenChart, metricChart
	}
	defer rows.Close()

	points := make([]dashboardModelPoint, 0, 256)
	for rows.Next() {
		var p dashboardModelPoint
		if err := rows.Scan(&p.Bucket, &p.Provider, &p.Model, &p.Tokens, &p.InputTokens, &p.OutputTokens,
			&p.CacheReadTokens, &p.ToolCalls, &p.Calls, &p.Errors); err != nil {
			logQueryScanError("dashboard model analytics", err)
			continue
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		logQueryError("dashboard model analytics rows", err)
		return tokenChart, metricChart
	}

	return buildDashboardModelCharts(points, bucketMinutes, timeUnit)
}

func emptyDashboardModelCharts(bucketMinutes int, timeUnit string) (views.ModelSeriesChart, views.ModelMetricChart) {
	summary := views.ModelAnalyticsSummary{}
	tokenChart := views.ModelSeriesChart{
		Labels:        []string{},
		Datasets:      []views.ModelSeriesDataset{},
		Summary:       summary,
		TimeUnit:      timeUnit,
		BucketMinutes: bucketMinutes,
	}
	metricChart := views.ModelMetricChart{
		Labels: []string{},
		Metrics: map[string]views.ModelMetricSeries{
			"total_tokens":      {Label: "Total Tokens", Unit: "tokens", Datasets: []views.ModelSeriesDataset{}},
			"input_tokens":      {Label: "Input Tokens", Unit: "tokens", Datasets: []views.ModelSeriesDataset{}},
			"output_tokens":     {Label: "Output Tokens", Unit: "tokens", Datasets: []views.ModelSeriesDataset{}},
			"cache_read_tokens": {Label: "Cache Read Tokens", Unit: "tokens", Datasets: []views.ModelSeriesDataset{}},
			"tool_calls":        {Label: "Tool Calls", Unit: "tool calls", Datasets: []views.ModelSeriesDataset{}},
			"errors":            {Label: "Errors", Unit: "errors", Datasets: []views.ModelSeriesDataset{}},
			"error_rate":        {Label: "Error Rate", Unit: "%", Datasets: []views.ModelSeriesDataset{}},
		},
		Summary:       summary,
		TimeUnit:      timeUnit,
		BucketMinutes: bucketMinutes,
	}
	return tokenChart, metricChart
}

func buildDashboardModelCharts(points []dashboardModelPoint, bucketMinutes int, timeUnit string) (views.ModelSeriesChart, views.ModelMetricChart) {
	tokenChart, metricChart := emptyDashboardModelCharts(bucketMinutes, timeUnit)
	if len(points) == 0 {
		return tokenChart, metricChart
	}

	bucketSeen := make(map[time.Time]bool)
	var buckets []time.Time
	modelTotals := make(map[dashboardModelKey]dashboardModelPoint)
	valuesByBucket := make(map[time.Time]map[dashboardModelKey]dashboardModelPoint)

	for _, p := range points {
		if p.Provider == "" {
			p.Provider = "unknown"
		}
		if p.Model == "" {
			p.Model = "unknown"
		}
		key := dashboardModelKey{Provider: p.Provider, Model: p.Model}
		if !bucketSeen[p.Bucket] {
			bucketSeen[p.Bucket] = true
			buckets = append(buckets, p.Bucket)
		}
		if valuesByBucket[p.Bucket] == nil {
			valuesByBucket[p.Bucket] = make(map[dashboardModelKey]dashboardModelPoint)
		}
		existing := valuesByBucket[p.Bucket][key]
		existing.Tokens += p.Tokens
		existing.InputTokens += p.InputTokens
		existing.OutputTokens += p.OutputTokens
		existing.CacheReadTokens += p.CacheReadTokens
		existing.ToolCalls += p.ToolCalls
		existing.Calls += p.Calls
		existing.Errors += p.Errors
		existing.Provider = key.Provider
		existing.Model = key.Model
		valuesByBucket[p.Bucket][key] = existing

		total := modelTotals[key]
		total.Tokens += p.Tokens
		total.InputTokens += p.InputTokens
		total.OutputTokens += p.OutputTokens
		total.CacheReadTokens += p.CacheReadTokens
		total.ToolCalls += p.ToolCalls
		total.Calls += p.Calls
		total.Errors += p.Errors
		total.Provider = key.Provider
		total.Model = key.Model
		modelTotals[key] = total
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Before(buckets[j]) })
	keys := make([]dashboardModelKey, 0, len(modelTotals))
	for key := range modelTotals {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := modelTotals[keys[i]], modelTotals[keys[j]]
		if a.Tokens != b.Tokens {
			return a.Tokens > b.Tokens
		}
		if a.ToolCalls != b.ToolCalls {
			return a.ToolCalls > b.ToolCalls
		}
		return modelDisplayLabel(a.Provider, a.Model, nil) < modelDisplayLabel(b.Provider, b.Model, nil)
	})

	labelCounts := make(map[string]int)
	for _, key := range keys {
		labelCounts[views.ShortModelName(key.Model)]++
	}

	tokenChart.Labels = make([]string, 0, len(buckets))
	metricChart.Labels = make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		label := bucket.Local().Format(time.RFC3339)
		tokenChart.Labels = append(tokenChart.Labels, label)
		metricChart.Labels = append(metricChart.Labels, label)
	}

	var summary views.ModelAnalyticsSummary
	for _, key := range keys {
		total := modelTotals[key]
		displayLabel := modelDisplayLabel(key.Provider, key.Model, labelCounts)
		providerLabel := views.ProviderShort(key.Provider)
		if providerLabel == "" {
			providerLabel = key.Provider
		}

		base := views.ModelSeriesDataset{
			Label:           displayLabel,
			Model:           key.Model,
			Provider:        key.Provider,
			ProviderLabel:   providerLabel,
			TotalTokens:     total.Tokens,
			InputTokens:     total.InputTokens,
			OutputTokens:    total.OutputTokens,
			CacheReadTokens: total.CacheReadTokens,
			ToolCallCount:   total.ToolCalls,
			CallCount:       total.Calls,
			ErrorCount:      total.Errors,
		}

		tokenDataset := base
		inputDataset := base
		outputDataset := base
		cacheReadDataset := base
		errorRateDataset := base
		errorDataset := base
		toolDataset := base

		for _, bucket := range buckets {
			point := valuesByBucket[bucket][key]
			tokenDataset.Values = append(tokenDataset.Values, float64(point.Tokens))
			inputDataset.Values = append(inputDataset.Values, float64(point.InputTokens))
			outputDataset.Values = append(outputDataset.Values, float64(point.OutputTokens))
			cacheReadDataset.Values = append(cacheReadDataset.Values, float64(point.CacheReadTokens))
			errorDataset.Values = append(errorDataset.Values, float64(point.Errors))
			toolDataset.Values = append(toolDataset.Values, float64(point.ToolCalls))
			attempts := point.Calls + point.Errors
			if attempts > 0 {
				errorRateDataset.Values = append(errorRateDataset.Values, (float64(point.Errors)/float64(attempts))*100)
			} else {
				errorRateDataset.Values = append(errorRateDataset.Values, 0)
			}
		}

		tokenChart.Datasets = append(tokenChart.Datasets, tokenDataset)
		metricChart.Metrics["total_tokens"] = appendMetricDataset(metricChart.Metrics["total_tokens"], tokenDataset)
		metricChart.Metrics["input_tokens"] = appendMetricDataset(metricChart.Metrics["input_tokens"], inputDataset)
		metricChart.Metrics["output_tokens"] = appendMetricDataset(metricChart.Metrics["output_tokens"], outputDataset)
		metricChart.Metrics["cache_read_tokens"] = appendMetricDataset(metricChart.Metrics["cache_read_tokens"], cacheReadDataset)
		metricChart.Metrics["tool_calls"] = appendMetricDataset(metricChart.Metrics["tool_calls"], toolDataset)
		metricChart.Metrics["error_rate"] = appendMetricDataset(metricChart.Metrics["error_rate"], errorRateDataset)
		metricChart.Metrics["errors"] = appendMetricDataset(metricChart.Metrics["errors"], errorDataset)

		summary.TotalTokens += total.Tokens
		summary.ToolCallCount += total.ToolCalls
		summary.CallCount += total.Calls
		summary.ErrorCount += total.Errors
	}

	summary.ModelCount = len(keys)
	if attempts := summary.CallCount + summary.ErrorCount; attempts > 0 {
		summary.ErrorRate = (float64(summary.ErrorCount) / float64(attempts)) * 100
	}
	tokenChart.Summary = summary
	metricChart.Summary = summary
	return tokenChart, metricChart
}

func appendMetricDataset(series views.ModelMetricSeries, dataset views.ModelSeriesDataset) views.ModelMetricSeries {
	series.Datasets = append(series.Datasets, dataset)
	return series
}

func modelDisplayLabel(provider, model string, labelCounts map[string]int) string {
	label := views.ShortModelName(model)
	if label == "" {
		label = "unknown"
	}
	if labelCounts != nil && labelCounts[label] > 1 {
		providerLabel := views.ProviderShort(provider)
		if providerLabel == "" {
			providerLabel = provider
		}
		if providerLabel != "" {
			label += " (" + providerLabel + ")"
		}
	}
	return label
}

// QueryDashboardMetrics returns the top-level metric cards.
func QueryDashboardMetrics(ctx context.Context, db *sql.DB) []views.MetricData {
	var totalSessions, activeSessions int
	var inputTokens, outputTokens, cacheReadTokens int64
	var toolCalls, mcpCalls int
	activeCutoff := time.Now().Add(-idleThreshold)

	if err := db.QueryRowContext(ctx,
		`SELECT count(),
		        countIf(`+activeSessionPredicate()+`),
		        COALESCE(SUM(total_input_tokens), 0),
		        COALESCE(SUM(total_output_tokens), 0),
		        COALESCE(SUM(total_cache_read_tokens), 0),
		        COALESCE(SUM(tool_call_count), 0),
		        COALESCE(SUM(mcp_call_count), 0)
		 FROM `+sessionProjectionSQL, activeCutoff, activeCutoff,
	).Scan(&totalSessions, &activeSessions, &inputTokens, &outputTokens, &cacheReadTokens, &toolCalls, &mcpCalls); err != nil {
		logQueryError("dashboard metrics", err)
		return nil
	}

	totalTokens := inputTokens + outputTokens

	return []views.MetricData{
		{Label: "Total Sessions", Value: fmt.Sprintf("%d", totalSessions)},
		{Label: "Active Sessions", Value: fmt.Sprintf("%d", activeSessions)},
		{Label: "Total Tokens", Value: views.FormatTokens(totalTokens),
			Sublabel: fmt.Sprintf("In: %s  Out: %s  Cache: %s", views.FormatTokens(inputTokens), views.FormatTokens(outputTokens), views.FormatTokens(cacheReadTokens))},
		{Label: "Tool Calls", Value: fmt.Sprintf("%d", toolCalls),
			Sublabel: fmt.Sprintf("%d MCP", mcpCalls)},
	}
}
