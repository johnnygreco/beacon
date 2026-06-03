package web

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnnygreco/beacon/internal/sse"
)

// Updater queries fresh data and broadcasts dashboard invalidations via SSE.
// It decouples capture from rendering via a dirty-signal channel. Active
// session invalidations are sent as soon as a dirty signal is observed so the
// live board can refetch immediately; heavier dashboard panels are still
// coalesced with a 250ms debounce and 1s max-staleness guarantee.
type Updater struct {
	broker                *sse.Broker
	logger                *slog.Logger
	dirty                 chan struct{}
	mu                    sync.Mutex
	pendingSessions       map[string]struct{}
	coalescedDirtySignals atomic.Uint64
}

// NewUpdater creates a new dashboard updater.
func NewUpdater(broker *sse.Broker, logger *slog.Logger) *Updater {
	return &Updater{
		broker:          broker,
		logger:          logger,
		dirty:           make(chan struct{}, 1),
		pendingSessions: make(map[string]struct{}),
	}
}

// MarkDirty signals that new data has been flushed and the dashboard should
// refresh. It is non-blocking: if a signal is already pending, only the wakeup
// is coalesced while session IDs remain buffered until the next notification.
func (u *Updater) MarkDirty(sessionIDs []string) {
	u.mu.Lock()
	for _, id := range sessionIDs {
		if id != "" {
			u.pendingSessions[id] = struct{}{}
		}
	}
	pendingCount := len(u.pendingSessions)
	u.mu.Unlock()

	select {
	case u.dirty <- struct{}{}:
	default:
		coalesced := u.coalescedDirtySignals.Add(1)
		u.logger.Debug("coalesced dashboard dirty signal",
			"coalesced_total", coalesced,
			"pending_sessions", pendingCount,
		)
	}
}

// CoalescedSignalCount returns dirty wakeups that were already represented by
// a pending updater signal. Session IDs from those calls are still retained.
func (u *Updater) CoalescedSignalCount() uint64 {
	return u.coalescedDirtySignals.Load()
}

func (u *Updater) drainPendingSessions() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.pendingSessions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(u.pendingSessions))
	for id := range u.pendingSessions {
		ids = append(ids, id)
	}
	u.pendingSessions = make(map[string]struct{})
	return ids
}

// Run is the main updater loop. It sends active-session dashboard invalidations
// immediately on dirty flush wakeups, then coalesces completed sessions,
// activity, charts, and transcript invalidations with a 250ms debounce and a
// 1s max-staleness cap. It also handles periodic refresh (every 10s when
// subscribers exist) for time-based session state transitions.
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
			u.NotifyActiveDashboard()
			if firstDirty.IsZero() {
				firstDirty = now
			}
			stopDrain()
			if now.Sub(firstDirty) >= maxStale {
				u.NotifyDashboardPanels()
				u.NotifySessions(u.drainPendingSessions())
				firstDirty = time.Time{}
			} else {
				debounce.Reset(debounceDelay)
			}
		case <-debounce.C:
			u.NotifyDashboardPanels()
			u.NotifySessions(u.drainPendingSessions())
			firstDirty = time.Time{}
		case <-periodic.C:
			if u.broker.SubscriberCount() > 0 {
				u.NotifyDashboard()
			}
		}
	}
}

// NotifyChanges broadcasts dashboard and per-session invalidations after a
// flushed capture batch. Session pages use this to refresh active transcripts
// without polling or waiting for a full navigation.
func (u *Updater) NotifyChanges(sessionIDs []string) {
	u.NotifyDashboard()
	u.NotifySessions(sessionIDs)
}

// NotifyDashboard broadcasts all dashboard invalidation events via SSE. The
// dashboard fetches JSON projections on demand, so flush notifications do not
// spend time querying or rendering server-side dashboard fragments. Dirty flush
// handling in Run intentionally sends the active-session event immediately and
// uses NotifyDashboardPanels for the heavier debounced panels.
func (u *Updater) NotifyDashboard() {
	u.notifyDashboardEvents("NotifyDashboard", []string{
		"active-sessions-update",
		"completed-sessions-update",
		"activity-update",
		"dashboard-charts-update",
	})
}

// NotifyActiveDashboard broadcasts the active-session dashboard invalidation.
// It is kept separate from heavier dashboard panels so live sessions can appear
// promptly after a capture flush instead of waiting for the dashboard debounce.
func (u *Updater) NotifyActiveDashboard() {
	u.notifyDashboardEvents("NotifyActiveDashboard", []string{"active-sessions-update"})
}

// NotifyDashboardPanels broadcasts the heavier dashboard invalidations that can
// be safely coalesced behind the dashboard debounce.
func (u *Updater) NotifyDashboardPanels() {
	u.notifyDashboardEvents("NotifyDashboardPanels", []string{
		"completed-sessions-update",
		"activity-update",
		"dashboard-charts-update",
	})
}

func (u *Updater) notifyDashboardEvents(label string, events []string) {
	start := time.Now()
	defer func() {
		u.logger.Debug(label+" complete", "duration", time.Since(start))
	}()

	if u.broker.SubscriberCount() == 0 {
		return
	}

	dirty := []byte(`{"dirty":true}`)
	for _, event := range events {
		u.broker.Broadcast("dashboard", sse.SSEMessage{
			Event: event,
			Data:  dirty,
		})
	}
}

// NotifySessions broadcasts lightweight invalidations to open transcript pages.
func (u *Updater) NotifySessions(sessionIDs []string) {
	if len(sessionIDs) == 0 || u.broker.SubscriberCount() == 0 {
		return
	}
	dirty := []byte(`{"dirty":true}`)
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		u.broker.Broadcast("session:"+id, sse.SSEMessage{
			Event: "conversation-update",
			Data:  dirty,
		})
	}
}
