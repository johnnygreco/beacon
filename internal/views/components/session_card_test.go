package components

import (
	"strings"
	"testing"

	"github.com/johnnygreco/beacon/internal/views"
)

func TestSessionCardRendersSummaryFields(t *testing.T) {
	html := renderToString(t, SessionCard(views.SessionSummary{
		ID:            "session-1234567890",
		Status:        "active",
		Duration:      "12m",
		TotalTokens:   12_500,
		TurnCount:     4,
		ToolCallCount: 7,
		ActiveModel:   "claude-opus-4-20250514",
		WorkingDir:    "/tmp/projects/beacon",
	}))

	for _, want := range []string{
		`href="/sessions/session-1234567890"`,
		"beacon",
		"Active",
		"claude-opus-4-20250514",
		"session-",
		"12m",
		"12.5K",
		"4",
		"7",
		`title="/tmp/projects/beacon"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected session card to contain %q: %s", want, html)
		}
	}
}

func TestActiveSessionCardRendersParentAndChildSessions(t *testing.T) {
	html := renderToString(t, ActiveSessionCard(views.SessionSummary{
		ID:            "parent-session-1234",
		Provider:      "anthropic",
		Status:        "active",
		Duration:      "3m",
		TotalTokens:   1_234,
		TurnCount:     2,
		ToolCallCount: 1,
		ActiveModel:   "claude-haiku-4-5-20251001",
		WorkingDir:    "/tmp/workspaces/beacon",
		ChildSessions: []views.SessionSummary{{
			ID:            "child-session-5678",
			Duration:      "1m",
			TotalTokens:   987,
			ToolCallCount: 3,
			ActiveModel:   "gpt-5.4",
		}},
	}))

	for _, want := range []string{
		`href="/sessions/parent-session-1234"`,
		`href="/sessions/child-session-5678"`,
		"Live",
		"Claude Code",
		"haiku-4-5",
		"1 subagent",
		"987",
		"3t",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected active session card to contain %q: %s", want, html)
		}
	}
}

func TestActiveSessionCardRendersOrphanSubagentContext(t *testing.T) {
	html := renderToString(t, ActiveSessionCard(views.SessionSummary{
		ID:              "child-session-1234",
		ParentSessionID: "parent-session-9999",
		Status:          "idle",
		Duration:        "45s",
		TotalTokens:     250,
		ToolCallCount:   2,
		ActiveModel:     "claude-sonnet-4-20250514",
	}))

	for _, want := range []string{
		`href="/sessions/child-session-1234"`,
		"Idle",
		"parent-s",
		"sonnet-4",
		"45s",
		"250 tok",
		"2 tools",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected orphan subagent card to contain %q: %s", want, html)
		}
	}
}
