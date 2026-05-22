package web

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

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
		logQueryError("recent activity", err)
		return nil
	}
	defer rows.Close()

	var items []views.ActivityItem
	for rows.Next() {
		var item views.ActivityItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Summary, &item.SessionID, &item.Provider, &item.Timestamp); err != nil {
			logQueryScanError("recent activity", err)
			continue
		}
		item.Summary = shortenActivitySummary(item.Summary)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		logQueryError("recent activity rows", err)
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
