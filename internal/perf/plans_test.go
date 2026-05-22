package perf_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/perf"
)

type explainWorkload struct {
	name  string
	query string
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
		})
	}
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
			name: "dashboard-active-sessions",
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
			name: "dashboard-completed-sessions-filtered",
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
			name: "dashboard-model-analytics",
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
			name: "transcript-open-large-session",
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
			name: "search-bm25",
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
			name: "search-browse-filtered",
			query: `SELECT event_uid, session_id, event_kind, text_preview, 0.0 AS score,
			       timestamp, tool_name, model, provider
			FROM search_documents FINAL
			WHERE event_kind IN ('tool_call')
			  AND timestamp >= now64(3) - INTERVAL 7 DAY
			ORDER BY timestamp DESC
			LIMIT 25`,
		},
		{
			name: "mcp-open-context",
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
			name: "mcp-list-sessions",
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
