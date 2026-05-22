package store

import (
	"strings"
	"time"

	"github.com/johnnygreco/beacon/internal/models"
)

func runtimeForSource(source string) string {
	switch source {
	case "claude":
		return "claude-code"
	case "codex":
		return "codex"
	default:
		return source
	}
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func sessionIDs(events []models.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if event.SessionID != "" {
			ids = append(ids, event.SessionID)
		}
	}
	return ids
}

func uniqStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func nonZeroTime(t time.Time, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback.UTC()
	}
	return t.UTC()
}

func nonNegativeInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func nonNegativeInt64(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
