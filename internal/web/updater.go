package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/johnnygreco/beacon/internal/sse"
	"github.com/johnnygreco/beacon/internal/views"
	"github.com/johnnygreco/beacon/internal/views/partials"
)

// Updater queries fresh data and broadcasts rendered HTML partials via SSE
// after each batcher flush.
type Updater struct {
	db     *sql.DB
	broker *sse.Broker
	logger *slog.Logger
}

// NewUpdater creates a new dashboard updater.
func NewUpdater(db *sql.DB, broker *sse.Broker, logger *slog.Logger) *Updater {
	return &Updater{db: db, broker: broker, logger: logger}
}

// NotifyDashboard is the callback invoked after each batcher flush.
func (u *Updater) NotifyDashboard() {
	if u.broker.SubscriberCount() == 0 {
		return
	}

	ctx := context.Background()

	var metrics []views.MetricData
	var activeSessions, completedSessions []views.SessionSummary
	var activity []views.ActivityItem
	var tokensChart views.MultiSeriesChart

	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); metrics = QueryDashboardMetrics(ctx, u.db) }()
	go func() {
		defer wg.Done()
		activeSessions, completedSessions = QueryDashboardSessions(ctx, u.db)
	}()
	go func() { defer wg.Done(); activity = QueryRecentActivity(ctx, u.db) }()
	go func() { defer wg.Done(); tokensChart = QueryTotalTokensTimeSeries(ctx, u.db) }()
	wg.Wait()

	// Render sidebar metrics wrapped in an OOB swap so the SSE update
	// targets the nav sidebar from the dashboard SSE connection.
	var metricsBuf bytes.Buffer
	metricsBuf.WriteString(`<div id="sidebar-metrics" hx-swap-oob="innerHTML">`)
	if err := partials.SidebarMetrics(metrics).Render(ctx, &metricsBuf); err != nil {
		u.logger.Error("render metrics partial", "error", err)
	} else {
		metricsBuf.WriteString(`</div>`)
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "metrics-update",
			Data:  metricsBuf.Bytes(),
		})
	}

	var activeBuf bytes.Buffer
	if err := partials.ActiveSessionList(activeSessions).Render(ctx, &activeBuf); err != nil {
		u.logger.Error("render active sessions partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "active-sessions-update",
			Data:  activeBuf.Bytes(),
		})
	}

	var completedBuf bytes.Buffer
	if err := partials.CompletedSessionList(completedSessions).Render(ctx, &completedBuf); err != nil {
		u.logger.Error("render completed sessions partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "completed-sessions-update",
			Data:  completedBuf.Bytes(),
		})
	}

	var activityBuf bytes.Buffer
	if err := partials.ActivityTimeline(activity).Render(ctx, &activityBuf); err != nil {
		u.logger.Error("render activity partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "activity-update",
			Data:  activityBuf.Bytes(),
		})
	}

	if len(tokensChart.Labels) > 0 {
		tokenData, _ := json.Marshal(tokensChart)
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "token-data",
			Data:  tokenData,
		})
	}
}
