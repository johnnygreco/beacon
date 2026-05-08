package web

import (
	"context"
	"log/slog"
	"time"

	"github.com/johnnygreco/beacon/internal/sse"
)

// Updater queries fresh data and broadcasts dashboard invalidations via SSE.
// It decouples capture from rendering via a dirty-signal channel with
// debounce (250ms) and max-staleness (1s) guarantees.
type Updater struct {
	broker *sse.Broker
	logger *slog.Logger
	dirty  chan struct{}
}

// NewUpdater creates a new dashboard updater.
func NewUpdater(broker *sse.Broker, logger *slog.Logger) *Updater {
	return &Updater{broker: broker, logger: logger, dirty: make(chan struct{}, 1)}
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

	debounce := time.NewTimer(debounceDelay)
	debounce.Stop()
	defer debounce.Stop()

	var firstDirty time.Time

	// stopDrain stops the debounce timer and drains its channel if already
	// fired. Safe to call when the timer is already stopped or expired.
	stopDrain := func() {
		if !debounce.Stop() {
			select {
			case <-debounce.C:
			default:
			}
		}
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
			stopDrain()
			if now.Sub(firstDirty) >= maxStale {
				u.NotifyDashboard()
				firstDirty = time.Time{}
			} else {
				debounce.Reset(debounceDelay)
			}
		case <-debounce.C:
			u.NotifyDashboard()
			firstDirty = time.Time{}
		case <-periodic.C:
			if u.broker.SubscriberCount() > 0 {
				u.NotifyDashboard()
			}
		}
	}
}

// NotifyDashboard broadcasts lightweight invalidation events via SSE. The
// dashboard fetches JSON projections on demand, so flush notifications do not
// spend time querying or rendering server-side dashboard fragments.
func (u *Updater) NotifyDashboard() {
	start := time.Now()
	defer func() {
		u.logger.Debug("NotifyDashboard complete", "duration", time.Since(start))
	}()

	if u.broker.SubscriberCount() == 0 {
		return
	}

	dirty := []byte(`{"dirty":true}`)
	for _, event := range []string{"active-sessions-update", "completed-sessions-update", "activity-update", "dashboard-charts-update"} {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: event,
			Data:  dirty,
		})
	}
}
