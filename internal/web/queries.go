package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
	"github.com/johnnygreco/beacon/internal/views"
)

const (
	defaultSessionPageSize   = 30
	defaultActivityPageSize  = 30
	defaultSearchPageSize    = 30
	recentActivityCandidates = 2000
	dashboardModelLimit      = 12
)

func latestActivityEventsSubquery(where string) string {
	return `(SELECT event_uid,
	               argMax(session_id, captured_at) AS session_id,
	               argMax(parent_session_id, captured_at) AS parent_session_id,
	               argMax(source_name, captured_at) AS source_name,
	               argMax(provider, captured_at) AS provider,
	               argMax(timestamp, captured_at) AS timestamp,
	               argMax(event_kind, captured_at) AS event_kind,
	               argMax(payload_type, captured_at) AS payload_type,
	               argMax(actor_role, captured_at) AS actor_role,
	               argMax(text_content, captured_at) AS text_content,
	               argMax(text_preview, captured_at) AS text_preview,
	               argMax(tool_name, captured_at) AS tool_name,
	               argMax(tool_use_id, captured_at) AS tool_use_id,
	               argMax(model, captured_at) AS model,
	               argMax(input_tokens, captured_at) AS input_tokens,
	               argMax(output_tokens, captured_at) AS output_tokens,
	               argMax(cache_read_tokens, captured_at) AS cache_read_tokens,
	               argMax(cache_create_tokens, captured_at) AS cache_create_tokens,
	               argMax(duration_ms, captured_at) AS duration_ms,
	               argMax(cost_usd, captured_at) AS cost_usd,
	               argMax(error_code, captured_at) AS error_code,
	               argMax(error_message, captured_at) AS error_message,
	               argMax(cwd, captured_at) AS cwd,
	               max(captured_at) AS latest_captured_at
	        FROM activity_events AS ae ` + sqlWhereClause(where) + `
	        GROUP BY event_uid)`
}

func recentActivityEventsSubquery(where string) string {
	return fmt.Sprintf(`(SELECT event_uid,
	               argMax(session_id, captured_at) AS session_id,
	               argMax(provider, captured_at) AS provider,
	               argMax(timestamp, captured_at) AS timestamp,
	               argMax(event_kind, captured_at) AS event_kind,
	               argMax(actor_role, captured_at) AS actor_role,
	               argMax(text_preview, captured_at) AS text_preview,
	               argMax(tool_name, captured_at) AS tool_name,
	               argMax(error_code, captured_at) AS error_code,
	               argMax(error_message, captured_at) AS error_message
	        FROM (
			SELECT event_uid,
			       session_id,
			       provider,
			       timestamp,
			       event_kind,
			       actor_role,
			       text_preview,
			       tool_name,
			       error_code,
			       error_message,
			       captured_at
			FROM activity_events AS ae `+sqlWhereClause(where)+`
			ORDER BY timestamp DESC
			LIMIT %d
	        )
	        GROUP BY event_uid)`, recentActivityCandidates)
}

func sqlWhereClause(where string) string {
	if strings.TrimSpace(where) == "" {
		return ""
	}
	return "WHERE " + where
}

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

func dashboardBucketMinutes(rangeVal string) int {
	switch rangeVal {
	case "1h":
		return 1
	case "24h":
		return 15
	case "7d":
		return 120
	case "30d":
		return 720
	default:
		return 1440
	}
}

func dashboardTimeUnit(bucketMinutes int) string {
	switch {
	case bucketMinutes <= 1:
		return "minute"
	case bucketMinutes < 1440:
		return "hour"
	default:
		return "day"
	}
}

// QueryDashboardData queries all data needed for the dashboard page.
func QueryDashboardData(ctx context.Context, db *sql.DB) views.DashboardData {
	var data views.DashboardData
	var wg sync.WaitGroup
	defaultRange := "24h"
	wg.Add(4)
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
// dashboard charts. Tokens are cumulative within the selected range; activity
// metrics remain per bucket so rates and spikes stay visible.
func QueryDashboardModelAnalytics(ctx context.Context, db *sql.DB, since *time.Time, rangeVal string) (views.ModelSeriesChart, views.ModelMetricChart) {
	bucketMinutes := dashboardBucketMinutes(rangeVal)
	timeUnit := dashboardTimeUnit(bucketMinutes)
	tokenChart, metricChart := emptyDashboardModelCharts(bucketMinutes, timeUnit)

	rangeFilter := ""
	args := []any{}
	if since != nil {
		rangeFilter = " AND minute >= ?"
		args = append(args, *since)
	}

	query := fmt.Sprintf(`WITH top_models AS (
			SELECT COALESCE(NULLIF(provider, ''), 'unknown') AS provider_key,
			       COALESCE(NULLIF(model, ''), 'unknown') AS model_key
			FROM `+analyticsProjectionSQL+`
			WHERE model != '<synthetic>'`+rangeFilter+`
			GROUP BY provider_key, model_key
			ORDER BY sum(total_tokens) DESC, sum(tool_call_count) DESC, sum(event_count) DESC
			LIMIT %d
		)
		SELECT toStartOfInterval(minute, INTERVAL %d MINUTE) AS bucket,
		       COALESCE(NULLIF(provider, ''), 'unknown') AS provider_key,
		       COALESCE(NULLIF(model, ''), 'unknown') AS model_key,
		       sum(total_tokens) AS tokens,
		       sum(input_tokens) AS input_tokens,
		       sum(output_tokens) AS output_tokens,
		       sum(cache_read_tokens) AS cache_read_tokens,
		       sum(tool_call_count) AS tool_calls,
		       sum(call_count) AS calls,
		       sumIf(event_count, event_kind IN ('error', 'tool_error')) AS errors
		FROM `+analyticsProjectionSQL+`
		WHERE model != '<synthetic>'`+rangeFilter+`
		  AND (COALESCE(NULLIF(provider, ''), 'unknown'), COALESCE(NULLIF(model, ''), 'unknown')) IN (
			SELECT provider_key, model_key FROM top_models
		  )
		GROUP BY bucket, provider_key, model_key
		ORDER BY bucket ASC`, dashboardModelLimit, bucketMinutes)
	if since != nil {
		args = append(args, *since)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return tokenChart, metricChart
	}
	defer rows.Close()

	points := make([]dashboardModelPoint, 0, 256)
	for rows.Next() {
		var p dashboardModelPoint
		if err := rows.Scan(&p.Bucket, &p.Provider, &p.Model, &p.Tokens, &p.InputTokens, &p.OutputTokens,
			&p.CacheReadTokens, &p.ToolCalls, &p.Calls, &p.Errors); err != nil {
			continue
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
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
			"error_rate": {Label: "Error Rate", Unit: "%", Datasets: []views.ModelSeriesDataset{}},
			"errors":     {Label: "Errors", Unit: "errors", Datasets: []views.ModelSeriesDataset{}},
			"tool_calls": {Label: "Tool Calls", Unit: "calls", Datasets: []views.ModelSeriesDataset{}},
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
		errorRateDataset := base
		errorDataset := base
		toolDataset := base

		var cumulative int64
		for _, bucket := range buckets {
			point := valuesByBucket[bucket][key]
			cumulative += point.Tokens
			tokenDataset.Values = append(tokenDataset.Values, float64(cumulative))
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
		metricChart.Metrics["error_rate"] = appendMetricDataset(metricChart.Metrics["error_rate"], errorRateDataset)
		metricChart.Metrics["errors"] = appendMetricDataset(metricChart.Metrics["errors"], errorDataset)
		metricChart.Metrics["tool_calls"] = appendMetricDataset(metricChart.Metrics["tool_calls"], toolDataset)

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
		        countIf(ended_at >= ? AND COALESCE(has_session_end, 0) = 0),
		        COALESCE(SUM(total_input_tokens), 0),
		        COALESCE(SUM(total_output_tokens), 0),
		        COALESCE(SUM(total_cache_read_tokens), 0),
		        COALESCE(SUM(tool_call_count), 0),
		        COALESCE(SUM(mcp_call_count), 0)
		 FROM `+sessionProjectionSQL, activeCutoff,
	).Scan(&totalSessions, &activeSessions, &inputTokens, &outputTokens, &cacheReadTokens, &toolCalls, &mcpCalls); err != nil {
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

// QueryActiveSessions returns active session summaries only.
// Active sessions are those with recent activity and no definitive end signal.
func QueryActiveSessions(ctx context.Context, db *sql.DB) []views.SessionSummary {
	now := time.Now()
	// Use Go's time.Now() (UTC-aware) instead of SQL current_timestamp to avoid
	// timezone mismatch — stored timestamps are UTC but current_timestamp is local.
	cutoff := now.Add(-idleThreshold)

	// Fetch active sessions: recent activity AND not explicitly ended.
	activeRows, err := db.QueryContext(ctx,
		`SELECT `+sessionSummaryColumns+`
		 FROM `+sessionProjectionSQL+`
		 WHERE ended_at >= ?
		   AND COALESCE(has_session_end, 0) = 0
		 ORDER BY ended_at DESC`, cutoff)
	if err != nil {
		return nil
	}
	defer activeRows.Close()
	var active []views.SessionSummary
	for activeRows.Next() {
		s, err := scanSessionSummary(activeRows, now)
		if err != nil {
			continue
		}
		active = append(active, s)
	}
	if err := activeRows.Err(); err != nil {
		return nil
	}

	return views.GroupActiveSessions(active)
}

// QueryDashboardSessions returns session summaries split into active and completed.
// Completed sessions are fetched separately with LIMIT+1 to determine hasMore.
func QueryDashboardSessions(ctx context.Context, db *sql.DB) (active, completed []views.SessionSummary, hasMore bool) {
	active = QueryActiveSessions(ctx, db)
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
		            WHEN event_kind = 'tool_error' THEN 'Tool error: ' || COALESCE(NULLIF(tool_name, ''), 'unknown') || ' — ' || COALESCE(NULLIF(error_message, ''), NULLIF(text_preview, ''), 'failed')
		            WHEN event_kind = 'session_meta' THEN 'Session started'
		            ELSE event_kind
		        END`

// QueryRecentActivity returns a feed of recent events within the last 24 hours.
func QueryRecentActivity(ctx context.Context, db *sql.DB) []views.ActivityItem {
	since := time.Now().Add(-24 * time.Hour)
	return QueryRecentActivityFiltered(ctx, db, &since)
}

// QueryCompletedSessions returns paginated completed sessions with optional time filter.
// Only returns parent sessions (excludes subagents); subagent counts are attached.
func QueryCompletedSessions(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int) ([]views.SessionSummary, bool) {
	return QueryCompletedSessionsFiltered(ctx, db, since, offset, limit, "", nil)
}

// QueryCompletedSessionsFiltered returns paginated completed sessions with optional
// time and text filters. Search matches session metadata plus session IDs
// discovered by the tokenized event search path.
func QueryCompletedSessionsFiltered(ctx context.Context, db *sql.DB, since *time.Time, offset, limit int, searchText string, eventSessionIDs []string) ([]views.SessionSummary, bool) {
	cutoff := time.Now().Add(-idleThreshold)
	query := `SELECT ` + sessionSummaryColumns + `
		 FROM ` + sessionProjectionSQL + `
		 WHERE (ended_at < ?
		    OR COALESCE(has_session_end, 0) = 1)
		   AND (parent_session_id = '' OR parent_session_id IS NULL)`
	args := []any{cutoff}
	if since != nil {
		query += " AND ended_at >= ?"
		args = append(args, *since)
	}
	if searchText = strings.TrimSpace(searchText); searchText != "" {
		clause, searchArgs := completedSessionSearchClause(searchText, eventSessionIDs)
		query += clause
		args = append(args, searchArgs...)
	}
	query += ` ORDER BY ended_at DESC, session_id DESC`
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

func completedSessionSearchClause(searchText string, eventSessionIDs []string) (string, []any) {
	columns := []string{
		"session_id",
		"COALESCE(source_name, '')",
		"COALESCE(provider, '')",
		"COALESCE(last_model, '')",
		"COALESCE(working_dir, '')",
	}
	terms := make([]string, 0, len(columns)+1)
	args := make([]any, 0, len(columns)+len(eventSessionIDs))
	for _, col := range columns {
		terms = append(terms, "positionCaseInsensitive("+col+", ?) > 0")
		args = append(args, searchText)
	}
	if len(eventSessionIDs) > 0 {
		terms = append(terms, "session_id IN ("+strings.TrimRight(strings.Repeat("?,", len(eventSessionIDs)), ",")+")")
		for _, id := range eventSessionIDs {
			args = append(args, id)
		}
	}
	return ` AND (` + strings.Join(terms, " OR ") + `)`, args
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
		placeholders[i] = "?"
		args[i] = s.ID
	}
	query := `SELECT parent_session_id, COUNT(*)
		 FROM ` + sessionProjectionSQL + `
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

// QueryRecentActivityFiltered returns activity items with optional time filter.
func QueryRecentActivityFiltered(ctx context.Context, db *sql.DB, since *time.Time) []views.ActivityItem {
	return QueryRecentActivityFilteredByKind(ctx, db, since, nil)
}

// QueryRecentActivityFilteredByKind returns activity items with optional time and event kind filters.
// When eventKinds is non-empty, only those event types are returned (enables server-side filtering
// so that low-volume event types like errors aren't crowded out by high-volume types).
func QueryRecentActivityFilteredByKind(ctx context.Context, db *sql.DB, since *time.Time, eventKinds []string) []views.ActivityItem {
	var kindFilter string
	if len(eventKinds) > 0 {
		quoted := make([]string, len(eventKinds))
		for i, k := range eventKinds {
			quoted[i] = "'" + strings.ReplaceAll(k, "'", "''") + "'"
		}
		kindFilter = "(" + strings.Join(quoted, ",") + ")"
	} else {
		kindFilter = "('message', 'tool_call', 'error', 'tool_error', 'session_meta')"
	}

	where := "ae.event_kind IN " + kindFilter
	var args []any
	if since != nil {
		where += " AND ae.timestamp >= ?"
		args = append(args, *since)
	}

	query := `SELECT event_uid,
		        event_kind,
		        ` + activitySummaryExpr + ` AS summary,
		        COALESCE(session_id, ''),
		        COALESCE(provider, ''),
		        timestamp
		 FROM ` + recentActivityEventsSubquery(where)
	query += ` ORDER BY timestamp DESC LIMIT 200`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
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
		return nil
	}
	return deduplicateActivity(items)
}

// deduplicateActivity removes duplicate activity items that arise from
// Claude Code JSONL logging the same content in multiple line types.
// Items are considered duplicates when they share the same summary,
// session_id, and event type.
func deduplicateActivity(items []views.ActivityItem) []views.ActivityItem {
	if len(items) <= 1 {
		return items
	}
	var result []views.ActivityItem
	seen := make(map[string]bool)
	for _, item := range items {
		key := item.Type + "|" + item.SessionID + "|" + item.Summary
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	return result
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
		   SELECT minute,
		          sum(input_tokens) AS total_input,
		          sum(output_tokens) AS total_output,
		          sum(cache_read_tokens) AS total_cache_read
		   FROM `+analyticsProjectionSQL+`
		   GROUP BY minute
		   ORDER BY minute DESC
		   LIMIT 60
		 ) ORDER BY minute ASC`)
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
		`SELECT minute, tokens_total FROM (
		   SELECT minute,
		          sum(total_tokens) AS tokens_total
		   FROM `+analyticsProjectionSQL+`
		   GROUP BY minute
		   ORDER BY minute DESC
		   LIMIT 60
		 ) ORDER BY minute ASC`)
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
		 FROM `+sessionProjectionSubquery("session_id = ?"), id)
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
		`WITH session_analytics AS (
			SELECT * FROM `+analyticsProjectionSubquery("session_id = ?")+`
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
			SELECT COALESCE(NULLIF(model, ''), NULLIF(?, ''), 'unknown') AS model,
			       COALESCE(NULLIF(provider, ''), NULLIF(?, ''), 'unknown') AS provider,
			       COALESCE(SUM(input_tokens), 0) AS input,
			       COALESCE(SUM(output_tokens), 0) AS output,
			       COALESCE(SUM(cache_read_tokens), 0) AS cache_read,
			       COALESCE(SUM(total_tokens), 0) AS total
			FROM session_analytics
			WHERE COALESCE(model, '') != '<synthetic>'
			  AND (input_tokens != 0 OR output_tokens != 0 OR cache_read_tokens != 0 OR total_tokens != 0)
			GROUP BY provider, model ORDER BY total DESC
		)
		SELECT 'token' AS kind, timestamp, toInt64(tokens_total), '' AS tool_name, toInt64(0) AS calls, toFloat64(0) AS avg_dur, '' AS model, '' AS provider, toInt64(0) AS input, toInt64(0) AS output, toInt64(0) AS cache_read FROM token_series
		UNION ALL
		SELECT 'tool', toDateTime64(0, 3), toInt64(0), tool_name, toInt64(calls), toFloat64(avg_duration), '', '', toInt64(0), toInt64(0), toInt64(0) FROM tool_stats
		UNION ALL
		SELECT 'model', toDateTime64(0, 3), toInt64(total), '', toInt64(0), toFloat64(0), model, provider, toInt64(input), toInt64(output), toInt64(cache_read) FROM model_breakdown`, id, session.ActiveModel, session.Provider)
	if err != nil {
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
		return data, nil
	}

	return data, nil
}

// QuerySessionConversation returns the conversation trace for a session.
func QuerySessionConversation(ctx context.Context, db *sql.DB, id string) ([]views.ChatTurn, []views.TurnDetail) {
	traceRows, err := db.QueryContext(ctx,
		`WITH trace AS (
			SELECT e.*,
			       row_number() OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS event_order,
			       sum(if(event_kind = 'message' AND actor_role = 'user', 1, 0))
			         OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS turn_seq
			FROM `+latestActivityEventsSubquery("ae.session_id = ?")+` e
		),
		payload_previews AS (
			SELECT event_uid,
			       argMax(input_preview, captured_at) AS input_preview,
			       argMax(output_preview, captured_at) AS output_preview
			FROM tool_payloads
			WHERE event_uid IN (SELECT event_uid FROM trace)
			GROUP BY event_uid
		)
		 SELECT e.event_uid, e.event_kind, COALESCE(e.payload_type, ''), COALESCE(e.actor_role, ''),
		        COALESCE(e.text_content, ''), COALESCE(e.text_preview, ''),
		        COALESCE(e.tool_name, ''), COALESCE(e.tool_use_id, ''), COALESCE(e.model, ''),
		        e.input_tokens + e.output_tokens, e.duration_ms, e.timestamp, turn_seq,
		        COALESCE(tio.input_preview, ''), COALESCE(tio.output_preview, ''),
		        '' AS input_json, '' AS output_json
		 FROM trace e
		 LEFT JOIN payload_previews tio ON e.event_uid = tio.event_uid
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
			&es.InputPreview, &es.OutputPreview, &es.InputJSON, &es.OutputJSON); err != nil {
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
				inputForParams := e.InputJSON
				if inputForParams == "" {
					inputForParams = e.InputPreview
				}
				item := views.ToolChainItem{
					CallEvent:    e,
					ToolName:     e.ToolName,
					InputPreview: e.InputPreview,
					InputJSON:    e.InputJSON,
				}
				item.Params = parseToolParams(inputForParams)

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
						item.OutputJSON = result.OutputJSON
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
							item.OutputJSON = result.OutputJSON
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
					OutputJSON:    e.OutputJSON,
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

			case "tool_error":
				flushReasoning()
				flushToolChain()
				eCopy := e
				ct.Blocks = append(ct.Blocks, views.ChatBlock{
					Kind:    views.ChatBlockToolError,
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
				key = e.EventUID + "|" + e.EventKind + "|" + e.ActorRole + "|" + e.TextContent + "|" + e.ToolName + "|" + e.InputJSON + "|" + e.InputPreview
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
		 FROM `+sessionProjectionSQL+`
		 WHERE parent_session_id = ?
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

var sessionProjectionSQL = sessionProjectionSubquery("")

func sessionProjectionSubquery(where string) string {
	return `(SELECT
		session_id,
		source_name,
		provider,
		started_at,
		ended_at,
		event_count,
		turn_count,
		total_input_tokens,
		total_output_tokens,
		total_cache_read_tokens,
		total_cache_create_tokens,
		total_tokens,
		tool_call_count,
		mcp_call_count,
		error_count,
		last_model,
		working_dir,
		parent_session_id,
		has_session_end
	FROM session_projection FINAL ` + sqlWhereClause(where) + `)`
}

var analyticsProjectionSQL = analyticsProjectionSubquery("")

func analyticsProjectionSubquery(where string) string {
	return `(SELECT
		session_id,
		minute,
		provider,
		model,
		tool_name,
		event_kind,
		event_count,
		call_count,
		tool_call_count,
		tool_result_count,
		input_tokens,
		output_tokens,
		cache_read_tokens,
		cache_create_tokens,
		total_tokens,
		duration_ms_sum
	FROM analytics_projection FINAL ` + sqlWhereClause(where) + `)`
}

// sessionSummaryColumns is the shared SELECT column list for session projection queries.
const sessionSummaryColumns = `session_id, COALESCE(source_name, ''), started_at, ended_at,
		COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
		COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
		COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
		COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
		COALESCE(error_count, 0),
		COALESCE(last_model, ''),
		COALESCE(working_dir, ''),
		COALESCE(parent_session_id, ''),
		COALESCE(has_session_end, 0),
		COALESCE(provider, '')`

// scanSessionSummary scans a row from session_projection into a SessionSummary.
func scanSessionSummary(scanner interface{ Scan(dest ...any) error }, now time.Time) (views.SessionSummary, error) {
	var s views.SessionSummary
	var source, model string
	var startedAt, endedAt time.Time
	var hasSessionEnd int
	err := scanner.Scan(&s.ID, &source, &startedAt, &endedAt,
		&s.TurnCount, &s.TotalTokens, &s.InputTokens, &s.OutputTokens,
		&s.CacheReadTokens, &s.CacheCreateTokens,
		&s.ToolCallCount, &s.MCPCallCount, &s.ErrorCount, &model, &s.WorkingDir,
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
		// Definitive end signal from the harness; always completed.
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
