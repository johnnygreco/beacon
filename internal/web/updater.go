package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnnygreco/beacon/internal/sse"
	"github.com/johnnygreco/beacon/internal/views"
	"github.com/johnnygreco/beacon/internal/views/partials"
)

// Updater queries fresh data and broadcasts rendered HTML partials via SSE.
// It decouples ingestion from rendering via a dirty-signal channel with
// debounce (250ms) and max-staleness (1s) guarantees.
type Updater struct {
	db       *sql.DB
	broker   *sse.Broker
	logger   *slog.Logger
	snapshot atomic.Pointer[views.DashboardData]
	dirty    chan struct{}
}

// NewUpdater creates a new dashboard updater.
func NewUpdater(db *sql.DB, broker *sse.Broker, logger *slog.Logger) *Updater {
	return &Updater{db: db, broker: broker, logger: logger, dirty: make(chan struct{}, 1)}
}

// MarkDirty signals that new data has been flushed and the dashboard should
// refresh. It is non-blocking: if a signal is already pending it is a no-op.
func (u *Updater) MarkDirty() {
	select {
	case u.dirty <- struct{}{}:
	default:
	}
}

// Run is the main updater loop. It coalesces dirty signals with a 250ms
// debounce and a 1s max-staleness cap so the dashboard stays fresh without
// rebuilding on every single flush. It also handles periodic refresh (every
// 10s when subscribers exist) for time-based session state transitions.
func (u *Updater) Run(ctx context.Context) {
	const (
		debounceDelay    = 250 * time.Millisecond
		maxStale         = 1 * time.Second
		periodicInterval = 10 * time.Second
	)

	periodic := time.NewTicker(periodicInterval)
	defer periodic.Stop()

	var (
		debounce   <-chan time.Time // nil until dirty
		firstDirty time.Time
	)

	refresh := func() {
		u.NotifyDashboard()
		debounce = nil
		firstDirty = time.Time{}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-u.dirty:
			now := time.Now()
			if firstDirty.IsZero() {
				firstDirty = now
			}
			if now.Sub(firstDirty) >= maxStale {
				refresh()
			} else {
				debounce = time.After(debounceDelay)
			}
		case <-debounce:
			refresh()
		case <-periodic.C:
			if u.broker.SubscriberCount() > 0 {
				u.NotifyDashboard()
			}
		}
	}
}

// Snapshot returns the latest cached dashboard data, or nil if not yet computed.
func (u *Updater) Snapshot() *views.DashboardData {
	return u.snapshot.Load()
}

// NotifyDashboard queries fresh dashboard data and broadcasts rendered HTML
// partials via SSE. Called by Run() after coalescing dirty signals.
func (u *Updater) NotifyDashboard() {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		u.logger.Debug("NotifyDashboard complete", "duration", time.Since(start))
	}()

	var activeSessions, completedSessions []views.SessionSummary
	var hasMoreSessions bool
	var activity []views.ActivityItem
	var tokensChart views.MultiSeriesChart
	var tokensByModel []views.ModelTokens

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		activeSessions, completedSessions, hasMoreSessions = QueryDashboardSessions(ctx, u.db)
	}()
	go func() {
		defer wg.Done()
		activity = QueryRecentActivity(ctx, u.db)
	}()
	go func() { defer wg.Done(); tokensChart = QueryTotalTokensTimeSeries(ctx, u.db) }()
	go func() { defer wg.Done(); tokensByModel = QueryTokensByModelSummary(ctx, u.db) }()
	wg.Wait()

	// Store snapshot for cache reads
	u.snapshot.Store(&views.DashboardData{
		ActiveSessions:    activeSessions,
		CompletedSessions: completedSessions,
		RecentActivity:    activity,
		TokensChart:       tokensChart,
		TokensByModel:     tokensByModel,
		HasMoreSessions:   hasMoreSessions,
	})

	if u.broker.SubscriberCount() == 0 {
		return
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
	if err := partials.CompletedSessionListPaginated(completedSessions, hasMoreSessions, "", 0, defaultSessionPageSize).Render(ctx, &completedBuf); err != nil {
		u.logger.Error("render completed sessions partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "completed-sessions-update",
			Data:  completedBuf.Bytes(),
		})
	}

	var activityBuf bytes.Buffer
	if err := partials.ActivityTimelineFull(activity).Render(ctx, &activityBuf); err != nil {
		u.logger.Error("render activity partial", "error", err)
	} else {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: "activity-update",
			Data:  activityBuf.Bytes(),
		})
	}

	if len(tokensChart.Labels) > 0 {
		tokenData, err := json.Marshal(tokensChart)
		if err != nil {
			u.logger.Error("marshal token chart data", "error", err)
		} else {
			u.broker.Broadcast("dashboard", sse.SSEMessage{
				Event: "token-data",
				Data:  tokenData,
			})
		}
	}
}
