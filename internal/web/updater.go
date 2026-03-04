package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/technodrome-ai/technodrome/internal/sse"
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
// It queries the DB for fresh data, renders templ partials, and broadcasts
// them as named SSE events that HTMX sse-swap attributes consume.
func (u *Updater) NotifyDashboard() {
	if u.broker.SubscriberCount() == 0 {
		return
	}

	ctx := context.Background()

	// Render and broadcast metrics partial
	metrics := QueryDashboardMetrics(ctx, u.db)
	var metricsBuf bytes.Buffer
	if err := partials.DashboardMetrics(metrics).Render(ctx, &metricsBuf); err != nil {
		u.logger.Error("render metrics partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "metrics-update",
			Data:  metricsBuf.Bytes(),
		})
	}

	// Render and broadcast sessions partial
	sessions := QueryActiveSessions(ctx, u.db)
	var sessionsBuf bytes.Buffer
	if err := partials.SessionList(sessions).Render(ctx, &sessionsBuf); err != nil {
		u.logger.Error("render sessions partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "sessions-update",
			Data:  sessionsBuf.Bytes(),
		})
	}

	// Render and broadcast activity partial
	activity := QueryRecentActivity(ctx, u.db)
	var activityBuf bytes.Buffer
	if err := partials.ActivityFeed(activity).Render(ctx, &activityBuf); err != nil {
		u.logger.Error("render activity partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "activity-update",
			Data:  activityBuf.Bytes(),
		})
	}

	// Send chart data as JSON events for the JS chart bridge
	tokensChart, costChart := QueryChartData(ctx, u.db)
	if len(tokensChart.Labels) > 0 {
		last := len(tokensChart.Labels) - 1
		tokenData, _ := json.Marshal(map[string]any{
			"timestamp": tokensChart.Labels[last],
			"value":     tokensChart.Values[last],
		})
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "token-data",
			Data:  tokenData,
		})

		costData, _ := json.Marshal(map[string]any{
			"timestamp": costChart.Labels[last],
			"value":     costChart.Values[last],
		})
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "cost-data",
			Data:  costData,
		})
	}
}
