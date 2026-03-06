package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/technodrome-ai/technodrome/internal/sse"
	"github.com/technodrome-ai/technodrome/internal/views"
	"github.com/technodrome-ai/technodrome/internal/views/partials"
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
	var sessions []views.SessionSummary
	var activity []views.ActivityItem
	var tokensChart views.MultiSeriesChart

	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); metrics = QueryDashboardMetrics(ctx, u.db) }()
	go func() { defer wg.Done(); sessions = QueryActiveSessions(ctx, u.db) }()
	go func() { defer wg.Done(); activity = QueryRecentActivity(ctx, u.db) }()
	go func() { defer wg.Done(); tokensChart = QueryChartData(ctx, u.db) }()
	wg.Wait()

	var metricsBuf bytes.Buffer
	if err := partials.DashboardMetrics(metrics).Render(ctx, &metricsBuf); err != nil {
		u.logger.Error("render metrics partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "metrics-update",
			Data:  metricsBuf.Bytes(),
		})
	}

	var sessionsBuf bytes.Buffer
	if err := partials.SessionList(sessions).Render(ctx, &sessionsBuf); err != nil {
		u.logger.Error("render sessions partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "sessions-update",
			Data:  sessionsBuf.Bytes(),
		})
	}

	var activityBuf bytes.Buffer
	if err := partials.ActivityFeed(activity).Render(ctx, &activityBuf); err != nil {
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
