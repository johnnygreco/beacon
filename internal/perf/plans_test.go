package perf_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johnnygreco/beacon/internal/perf"
	"github.com/johnnygreco/beacon/internal/search"
	"github.com/johnnygreco/beacon/internal/web"
)

type explainWorkload struct {
	name            string
	query           string
	expectedTables  []string
	forbiddenTables []string
	requiredSQL     []string
}

func TestExplainQueryPlans(t *testing.T) {
	if os.Getenv("BEACON_PERF_EXPLAIN") != "1" {
		t.Skip("set BEACON_PERF_EXPLAIN=1 to print representative ClickHouse plans")
	}
	db := requirePerfStoreForTest(t)
	ctx := context.Background()

	for _, workload := range explainWorkloads() {
		t.Run(workload.name, func(t *testing.T) {
			plan, err := explainQuery(ctx, db, workload.query)
			if err != nil {
				t.Fatalf("explain %s: %v", workload.name, err)
			}
			t.Logf("\n%s\n%s", workload.name, plan)
			if os.Getenv("BEACON_PERF_EXPLAIN_ASSERT") == "1" {
				assertPlanGuards(t, workload, plan)
			}
		})
	}
}

func TestExplainWorkloadGuardsMatchProductionQueries(t *testing.T) {
	workloads := explainWorkloadIndex()
	ctx := context.Background()
	db := newPlanCaptureDB(t)
	since := time.Now().Add(-24 * time.Hour)
	profile := perf.ProfileFor(perf.ParseSeedSize(os.Getenv("PERF_SIZE")))

	cases := []struct {
		name     string
		run      func()
		match    []string
		required []string
		maxRefs  map[string]int
	}{
		{
			name: "dashboard-active-sessions",
			run: func() {
				_ = web.QueryActiveSessionsLimited(ctx, db, 200)
			},
			match:    []string{"from (select", "session_projection final", "order by ended_at desc"},
			required: []string{"coalesce(has_session_end", "order by ended_at desc", "limit ?"},
			maxRefs:  map[string]int{"activity_events": 1},
		},
		{
			name: "dashboard-completed-sessions-filtered",
			run: func() {
				_, _ = web.QueryCompletedSessionsFiltered(ctx, db, &since, 0, 30, "perf", nil, "project", true)
			},
			match:    []string{"session_projection final", "parent_session_id", "limit ? offset ?"},
			required: []string{"parent_session_id", "ended_at >=", "limit ? offset ?"},
			maxRefs:  map[string]int{"activity_events": 1},
		},
		{
			name: "dashboard-model-analytics",
			run: func() {
				_, _ = web.QueryDashboardModelAnalytics(ctx, db, &since, "24h")
			},
			match:    []string{"top_models", "analytics_projection final"},
			required: []string{"minute >= ?", "top_models", "limit 12"},
		},
		{
			name: "search-common-token-scoped",
			run: func() {
				searcher := search.NewSearcher(db, slog.New(slog.NewTextHandler(io.Discard, nil)), 25, 0)
				_, _ = searcher.Search(ctx, search.SearchQuery{
					Query:        profile.CommonSearchToken,
					Limit:        25,
					CollectorIDs: []string{profile.ScopedCollectorID},
					SourceIDs:    []string{profile.ScopedSourceID},
					ProjectKeys:  []string{profile.ScopedProjectKey},
					SkipQueryLog: true,
				})
			},
			match:    []string{"from (select * from search_postings final) as p", "p.project_key in"},
			required: []string{"p.collector_id in", "p.source_id in", "p.project_key in", "where p.token in"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workload, ok := workloads[tc.name]
			if !ok {
				t.Fatalf("missing explain workload %q", tc.name)
			}
			resetPlanCaptureQueries()
			tc.run()
			query := capturedPlanQueryContaining(t, tc.match...)
			assertSQLGuards(t, tc.name, query, workload.expectedTables, workload.forbiddenTables, tc.required, tc.maxRefs)
		})
	}
}

func assertPlanGuards(t *testing.T, workload explainWorkload, plan string) {
	t.Helper()
	if strings.TrimSpace(plan) == "" {
		t.Fatalf("%s plan is empty", workload.name)
	}
	assertSQLGuards(t, workload.name, workload.query+"\n"+plan, workload.expectedTables, workload.forbiddenTables, workload.requiredSQL, nil)
}

func assertSQLGuards(t *testing.T, name, sql string, expectedTables, forbiddenTables, requiredSQL []string, maxRefs map[string]int) {
	t.Helper()
	combined := strings.ToLower(sql)
	for _, table := range expectedTables {
		if !strings.Contains(combined, strings.ToLower(table)) {
			t.Fatalf("%s query missing expected table %q:\n%s", name, table, sql)
		}
	}
	for _, table := range forbiddenTables {
		tableLower := strings.ToLower(table)
		if max, ok := maxRefs[tableLower]; ok {
			if refs := strings.Count(combined, tableLower); refs > max {
				t.Fatalf("%s query references table %q %d times, want <= %d:\n%s", name, table, refs, max, sql)
			}
			continue
		}
		if strings.Contains(combined, tableLower) {
			t.Fatalf("%s query unexpectedly references table %q:\n%s", name, table, sql)
		}
	}
	for _, required := range requiredSQL {
		if !strings.Contains(combined, strings.ToLower(required)) {
			t.Fatalf("%s query missing required guard %q:\n%s", name, required, sql)
		}
	}
}

func explainWorkloadIndex() map[string]explainWorkload {
	index := map[string]explainWorkload{}
	for _, workload := range explainWorkloads() {
		index[workload.name] = workload
	}
	return index
}

var (
	registerPlanCaptureDriver sync.Once
	planCaptureMu             sync.Mutex
	planCaptureQueries        []string
)

type planCaptureDriver struct{}

func (planCaptureDriver) Open(string) (driver.Conn, error) {
	return planCaptureConn{}, nil
}

type planCaptureConn struct{}

func (planCaptureConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("plan capture driver does not support prepared statements")
}

func (planCaptureConn) Close() error {
	return nil
}

func (planCaptureConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("plan capture driver does not support transactions")
}

func (planCaptureConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	recordPlanCaptureQuery(query)
	return planCaptureRowsFor(query), nil
}

func (planCaptureConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	recordPlanCaptureQuery(query)
	return driver.RowsAffected(0), nil
}

type planCaptureRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r planCaptureRows) Columns() []string {
	return r.columns
}

func (r planCaptureRows) Close() error {
	return nil
}

func (r *planCaptureRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func planCaptureRowsFor(query string) driver.Rows {
	queryLower := strings.ToLower(query)
	if strings.Contains(queryLower, "count() as documents") && strings.Contains(queryLower, "avg_doc_len") {
		return &planCaptureRows{
			columns: []string{"documents", "avg_doc_len"},
			values:  [][]driver.Value{{int64(100000), float64(96)}},
		}
	}
	return &planCaptureRows{columns: []string{"empty"}}
}

func newPlanCaptureDB(t *testing.T) *sql.DB {
	t.Helper()
	registerPlanCaptureDriver.Do(func() {
		sql.Register("beacon_plan_capture", planCaptureDriver{})
	})
	resetPlanCaptureQueries()
	db, err := sql.Open("beacon_plan_capture", "")
	if err != nil {
		t.Fatalf("open plan capture db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func recordPlanCaptureQuery(query string) {
	planCaptureMu.Lock()
	defer planCaptureMu.Unlock()
	planCaptureQueries = append(planCaptureQueries, query)
}

func resetPlanCaptureQueries() {
	planCaptureMu.Lock()
	defer planCaptureMu.Unlock()
	planCaptureQueries = nil
}

func capturedPlanQueryContaining(t *testing.T, fragments ...string) string {
	t.Helper()
	planCaptureMu.Lock()
	defer planCaptureMu.Unlock()
	for _, query := range planCaptureQueries {
		queryLower := strings.ToLower(query)
		matches := true
		for _, fragment := range fragments {
			if !strings.Contains(queryLower, strings.ToLower(fragment)) {
				matches = false
				break
			}
		}
		if matches {
			return query
		}
	}
	t.Fatalf("no captured production query contained %v; captured %d queries:\n%s", fragments, len(planCaptureQueries), strings.Join(planCaptureQueries, "\n---\n"))
	return ""
}

func requirePerfStoreForTest(t *testing.T) *sql.DB {
	t.Helper()
	if sharedStore == nil {
		t.Skip("set BEACON_TEST_CLICKHOUSE to run ClickHouse perf plan capture")
	}
	return sharedStore.DB
}

func explainQuery(ctx context.Context, db *sql.DB, query string) (string, error) {
	var lastErr error
	for _, prefix := range []string{"EXPLAIN indexes = 1 ", "EXPLAIN "} {
		rows, err := db.QueryContext(ctx, prefix+query)
		if err != nil {
			lastErr = err
			continue
		}
		defer rows.Close()

		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return "", err
			}
			lines = append(lines, line)
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
		return strings.Join(lines, "\n"), nil
	}
	return "", fmt.Errorf("ClickHouse rejected EXPLAIN prefixes: %w", lastErr)
}

func explainWorkloads() []explainWorkload {
	mcpEventUID := perf.EventUIDForBench(0, 8)
	return []explainWorkload{
		{
			name:            "dashboard-active-sessions",
			expectedTables:  []string{"session_projection"},
			forbiddenTables: []string{"activity_events", "search_postings"},
			requiredSQL:     []string{"coalesce(has_session_end", "order by ended_at desc", "limit 200"},
			query: `SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
			       COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
			       COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
			       COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
			       COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
			       COALESCE(error_count, 0), COALESCE(last_model, ''),
			       COALESCE(working_dir, ''), COALESCE(parent_session_id, ''),
			       COALESCE(has_session_end, 0), COALESCE(provider, '')
			FROM session_projection FINAL
			WHERE ended_at >= now64(3) - INTERVAL 5 MINUTE
			  AND COALESCE(has_session_end, 0) = 0
			ORDER BY ended_at DESC
			LIMIT 200`,
		},
		{
			name:            "dashboard-completed-sessions-filtered",
			expectedTables:  []string{"session_projection"},
			forbiddenTables: []string{"activity_events", "search_postings"},
			requiredSQL:     []string{"parent_session_id", "ended_at >=", "limit 31 offset 0"},
			query: `SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
			       COALESCE(turn_count, 0), COALESCE(total_tokens, 0),
			       COALESCE(total_input_tokens, 0), COALESCE(total_output_tokens, 0),
			       COALESCE(total_cache_read_tokens, 0), COALESCE(total_cache_create_tokens, 0),
			       COALESCE(tool_call_count, 0), COALESCE(mcp_call_count, 0),
			       COALESCE(error_count, 0), COALESCE(last_model, ''),
			       COALESCE(working_dir, ''), COALESCE(parent_session_id, ''),
			       COALESCE(has_session_end, 0), COALESCE(provider, '')
			FROM session_projection FINAL
			WHERE (ended_at < now64(3) - INTERVAL 5 MINUTE OR COALESCE(has_session_end, 0) = 1)
			  AND (parent_session_id = '' OR parent_session_id IS NULL)
			  AND ended_at >= now64(3) - INTERVAL 24 HOUR
			  AND (
			      positionCaseInsensitive(session_id, 'perf') > 0
			      OR positionCaseInsensitive(COALESCE(source_name, ''), 'perf') > 0
			      OR positionCaseInsensitive(COALESCE(provider, ''), 'perf') > 0
			      OR positionCaseInsensitive(COALESCE(last_model, ''), 'perf') > 0
			      OR positionCaseInsensitive(COALESCE(working_dir, ''), 'perf') > 0
			  )
			ORDER BY lower(COALESCE(working_dir, '')) ASC, ended_at DESC, session_id DESC
			LIMIT 31 OFFSET 0`,
		},
		{
			name:            "dashboard-model-analytics",
			expectedTables:  []string{"analytics_projection"},
			forbiddenTables: []string{"activity_events", "search_postings"},
			requiredSQL:     []string{"minute >=", "top_models", "limit 12"},
			query: `WITH range_sessions AS (
			       SELECT session_id
			       FROM analytics_projection FINAL
			       WHERE model != '<synthetic>' AND minute >= now64(3) - INTERVAL 24 HOUR
			       GROUP BY session_id
			   ),
			   session_analytics AS (
			       SELECT *
			       FROM analytics_projection FINAL
			       WHERE model != '<synthetic>'
			         AND session_id IN (SELECT session_id FROM range_sessions)
			   ),
			   session_model_fallbacks AS (
			       SELECT session_id,
			              if(uniqExactIf(model, model != '' AND model != '<synthetic>') = 1,
			                 anyIf(model, model != '' AND model != '<synthetic>'), '') AS fallback_model,
			              if(uniqExactIf(model, model != '' AND model != '<synthetic>') = 1,
			                 anyIf(provider, model != '' AND model != '<synthetic>'), '') AS fallback_provider
			       FROM session_analytics
			       GROUP BY session_id
			   ),
			   attributed AS (
			       SELECT a.session_id, a.minute, a.provider, a.model,
			              last_value(if(a.model != '', toNullable(a.model), NULL)) IGNORE NULLS
			                  OVER (PARTITION BY a.session_id ORDER BY a.minute, a.model = '', a.provider, a.model, a.tool_name, a.event_kind ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS prior_model,
			              last_value(if(a.model != '', toNullable(a.provider), NULL)) IGNORE NULLS
			                  OVER (PARTITION BY a.session_id ORDER BY a.minute, a.model = '', a.provider, a.model, a.tool_name, a.event_kind ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS prior_provider,
			              a.event_kind, a.event_count, a.call_count, a.tool_call_count,
			              a.input_tokens, a.output_tokens, a.cache_read_tokens, a.total_tokens
			       FROM session_analytics AS a
			   ),
			   model_analytics AS (
			       SELECT a.session_id, a.minute,
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
			              a.event_kind, a.event_count, a.call_count, a.tool_call_count,
			              a.input_tokens, a.output_tokens, a.cache_read_tokens, a.total_tokens
			       FROM attributed AS a
			       LEFT JOIN session_model_fallbacks AS sf ON a.session_id = sf.session_id
			       WHERE a.minute >= now64(3) - INTERVAL 24 HOUR
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
			       SELECT provider_key, model_key
			       FROM plottable_model_analytics
			       GROUP BY provider_key, model_key
			       ORDER BY sum(total_tokens) DESC, sum(tool_call_count) DESC, sum(event_count) DESC
			       LIMIT 12
			   )
			SELECT toStartOfInterval(minute, INTERVAL 15 MINUTE) AS bucket,
			       provider_key, model_key,
			       sum(total_tokens) AS tokens,
			       sum(input_tokens) AS input_tokens,
			       sum(output_tokens) AS output_tokens,
			       sum(cache_read_tokens) AS cache_read_tokens,
			       sum(tool_call_count) AS tool_calls,
			       sum(call_count) AS calls,
			       sumIf(event_count, event_kind IN ('error', 'tool_error')) AS errors
			FROM plottable_model_analytics
			WHERE (provider_key, model_key) IN (SELECT provider_key, model_key FROM top_models)
			GROUP BY bucket, provider_key, model_key
			ORDER BY bucket ASC`,
		},
		{
			name:           "transcript-open-large-session",
			expectedTables: []string{"activity_events", "tool_payloads"},
			requiredSQL:    []string{"where ae.session_id", "left join payload_previews", "order by event_order"},
			query: `WITH trace AS (
			       SELECT e.*,
			              row_number() OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS event_order,
			              sum(if(event_kind = 'message' AND actor_role = 'user', 1, 0))
			                OVER (PARTITION BY session_id ORDER BY timestamp, event_uid) AS turn_seq
			       FROM (
			           SELECT event_uid,
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
			           FROM activity_events AS ae
			           WHERE ae.session_id = 'perf-sess-00000'
			           GROUP BY event_uid
			       ) e
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
			       COALESCE(tio.input_preview, ''), COALESCE(tio.output_preview, '')
			FROM trace e
			LEFT JOIN payload_previews tio ON e.event_uid = tio.event_uid
			ORDER BY event_order`,
		},
		{
			name:            "search-bm25",
			expectedTables:  []string{"search_postings"},
			forbiddenTables: []string{"activity_events"},
			requiredSQL:     []string{"where token in", "group by p.event_uid", "limit 25"},
			query: `WITH toFloat64(100000) AS total_docs,
			       toFloat64(96) AS avg_doc_len
			SELECT p.event_uid,
			       any(p.session_id) AS session_id,
			       any(p.event_kind) AS event_kind,
			       any(p.text_preview) AS text_preview,
			       sum(log(1 + ((greatest(total_docs, p.doc_freq) - p.doc_freq + 0.5) / (p.doc_freq + 0.5))) *
			           ((p.term_frequency * 2.2) /
			            (p.term_frequency + 1.2 * (0.25 + 0.75 * (p.document_len / avg_doc_len))))) AS score,
			       max(p.timestamp) AS timestamp,
			       any(p.tool_name) AS tool_name,
			       any(p.model) AS model,
			       any(p.provider) AS provider
			FROM (
			       SELECT *,
			              toFloat64(count() OVER (PARTITION BY token)) AS doc_freq
			       FROM search_postings FINAL
			       WHERE token IN ('binary', 'search')
			) p
			WHERE 1 = 1
			GROUP BY p.event_uid
			HAVING score >= 0
			ORDER BY score DESC, timestamp DESC
			LIMIT 25`,
		},
		{
			name:            "search-common-token-scoped",
			expectedTables:  []string{"search_postings", "search_documents"},
			forbiddenTables: []string{"activity_events"},
			requiredSQL:     []string{"p.collector_id in", "p.source_id in", "p.project_key in", "where p.token in"},
			query: `WITH toFloat64(100000) AS total_docs,
			       toFloat64(96) AS avg_doc_len
			SELECT p.event_uid,
			       any(p.session_id) AS session_id,
			       any(p.event_kind) AS event_kind,
			       any(p.text_preview) AS text_preview,
			       sum(log(1 + ((greatest(total_docs, p.doc_freq) - p.doc_freq + 0.5) / (p.doc_freq + 0.5))) *
			           ((p.term_frequency * 2.2) /
			            (p.term_frequency + 1.2 * (0.25 + 0.75 * (p.document_len / avg_doc_len))))) AS score,
			       max(p.timestamp) AS timestamp,
			       any(p.tool_name) AS tool_name,
			       any(p.model) AS model,
			       any(p.provider) AS provider
			FROM (
			       SELECT p.*,
			              toFloat64(count() OVER (PARTITION BY p.token)) AS doc_freq
			       FROM (SELECT * FROM search_postings FINAL) AS p
			       INNER JOIN (SELECT event_uid, updated_at FROM search_documents FINAL) AS d ON d.event_uid = p.event_uid
			       WHERE p.token IN ('fleetcommon')
			         AND p.updated_at >= d.updated_at
			         AND p.collector_id IN ('collector-perf-00')
			         AND p.source_id IN ('source-perf-00-claude-code')
			         AND p.project_key IN ('project-000')
			) p
			GROUP BY p.event_uid
			HAVING score >= 0
			ORDER BY score DESC, timestamp DESC
			LIMIT 25`,
		},
		{
			name:            "search-browse-filtered",
			expectedTables:  []string{"search_documents"},
			forbiddenTables: []string{"activity_events", "search_postings"},
			requiredSQL:     []string{"event_kind in", "timestamp >=", "limit 25"},
			query: `SELECT event_uid, session_id, event_kind, text_preview, 0.0 AS score,
			       timestamp, tool_name, model, provider
			FROM search_documents FINAL
			WHERE event_kind IN ('tool_call')
			  AND timestamp >= now64(3) - INTERVAL 7 DAY
			ORDER BY timestamp DESC
			LIMIT 25`,
		},
		{
			name:           "mcp-open-context",
			expectedTables: []string{"activity_events"},
			requiredSQL:    []string{"where event_uid", "where ae.session_id in", "row_number()"},
			query: fmt.Sprintf(`WITH target AS (
			       SELECT event_uid,
			              argMax(session_id, captured_at) AS target_session_id,
			              argMax(timestamp, captured_at) AS timestamp
			       FROM activity_events
			       WHERE event_uid = '%s'
			       GROUP BY event_uid
			   ),
			   session_events AS (
			       SELECT event_uid,
			              argMax(ae.session_id, captured_at) AS event_session_id,
			              argMax(event_kind, captured_at) AS event_kind,
			              argMax(actor_role, captured_at) AS actor_role,
			              argMax(text_preview, captured_at) AS text_preview,
			              argMax(tool_name, captured_at) AS tool_name,
			              argMax(model, captured_at) AS model,
			              argMax(input_tokens, captured_at) + argMax(output_tokens, captured_at) AS tokens,
			              argMax(timestamp, captured_at) AS timestamp
			       FROM activity_events AS ae
			       WHERE ae.session_id IN (SELECT target_session_id FROM target)
			       GROUP BY event_uid
			   ),
			   numbered AS (
			       SELECT e.event_uid, e.event_kind, COALESCE(e.actor_role, '') AS actor_role,
			              COALESCE(e.text_preview, '') AS text_preview,
			              COALESCE(e.tool_name, '') AS tool_name, COALESCE(e.model, '') AS model,
			              e.tokens, e.timestamp,
			              ROW_NUMBER() OVER (ORDER BY e.timestamp, e.event_uid) AS rn
			       FROM session_events e, target t
			       WHERE e.event_session_id = t.target_session_id
			   )
			SELECT n.event_uid, n.event_kind, n.actor_role, n.text_preview,
			       n.tool_name, n.model, n.tokens, n.timestamp
			FROM numbered n, (SELECT rn FROM numbered WHERE event_uid = '%s') t
			WHERE n.rn BETWEEN t.rn - 3 AND t.rn + 3
			ORDER BY n.rn`, mcpEventUID, mcpEventUID),
		},
		{
			name:            "mcp-list-sessions",
			expectedTables:  []string{"session_projection"},
			forbiddenTables: []string{"activity_events", "search_postings"},
			requiredSQL:     []string{"sp.started_at >=", "limit 20"},
			query: `SELECT session_id, COALESCE(source_name, ''), started_at, ended_at,
			       event_count, turn_count, total_tokens, tool_call_count, mcp_call_count, error_count, COALESCE(last_model, '')
			FROM (
			       SELECT sp.session_id AS session_id,
			              argMax(sp.source_name, sp.updated_at) AS source_name,
			              argMax(sp.started_at, sp.updated_at) AS started_at,
			              argMax(sp.ended_at, sp.updated_at) AS ended_at,
			              argMax(sp.event_count, sp.updated_at) AS event_count,
			              argMax(sp.turn_count, sp.updated_at) AS turn_count,
			              argMax(sp.total_tokens, sp.updated_at) AS total_tokens,
			              argMax(sp.tool_call_count, sp.updated_at) AS tool_call_count,
			              argMax(sp.mcp_call_count, sp.updated_at) AS mcp_call_count,
			              argMax(sp.error_count, sp.updated_at) AS error_count,
			              argMax(sp.last_model, sp.updated_at) AS last_model
			       FROM session_projection AS sp
			       WHERE sp.started_at >= now64(3) - INTERVAL 7 DAY
			       GROUP BY sp.session_id
			)
			ORDER BY started_at DESC
			LIMIT 20`,
		},
	}
}
