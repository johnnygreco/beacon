package web

import (
	"fmt"
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

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

const (
	// activeThreshold: session is "active" (green Live badge) if last event within this window.
	activeThreshold = 90 * time.Second
	// idleThreshold: session is "idle" (amber badge) between activeThreshold and this.
	// Beyond this, sessions without an end signal are "archived".
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
		// Timed out without explicit end signal; keep completion_state event-backed.
		s.Status = "archived"
		s.Duration = formatDuration(lastActivity.Sub(startedAt))
		if s.ArchiveReason == "" {
			s.ArchiveReason = "idle_timeout"
		}
		if s.ArchivedAt.IsZero() {
			s.ArchivedAt = lastActivity.Add(idleThreshold)
		}
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
