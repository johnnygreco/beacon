package mcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/technodrome-ai/technodrome/internal/search"
)

// FormatSearchResults formats search results as prose for agent consumption.
func FormatSearchResults(results []search.SearchResult) string {
	if len(results) == 0 {
		return "No results found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d results:\n\n", len(results))

	for i, r := range results {
		idPreview := r.SessionID
		if len(idPreview) > 12 {
			idPreview = idPreview[:12]
		}

		fmt.Fprintf(&b, "%d. [%s] session:%s", i+1, r.EventKind, idPreview)
		if r.ToolName != "" {
			fmt.Fprintf(&b, " tool:%s", r.ToolName)
		}
		if r.Model != "" {
			fmt.Fprintf(&b, " model:%s", r.Model)
		}
		if r.Score > 0 {
			fmt.Fprintf(&b, " (score: %.2f)", r.Score)
		}
		fmt.Fprintf(&b, "\n   %s\n", r.Timestamp.Format(time.RFC3339))

		preview := r.TextPreview
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		if preview != "" {
			fmt.Fprintf(&b, "   %s\n", preview)
		}
		fmt.Fprintf(&b, "   -> open(event_uid=\"%s\")\n\n", r.EventUID)
	}

	return b.String()
}

// contextEvent matches the struct used in toolOpen.
type contextEvent struct {
	EventUID    string
	EventKind   string
	ActorRole   string
	TextPreview string
	ToolName    string
	Model       string
	Tokens      int64
	Timestamp   time.Time
}

// FormatOpenContext formats a context window with the target event marked.
func FormatOpenContext(events []contextEvent, targetIdx int) string {
	var b strings.Builder

	for i, e := range events {
		marker := "  "
		if i == targetIdx {
			marker = ">>>"
		}

		fmt.Fprintf(&b, "%s [%s] %s", marker, e.EventKind, e.ActorRole)
		if e.ToolName != "" {
			fmt.Fprintf(&b, " tool:%s", e.ToolName)
		}
		if e.Model != "" {
			fmt.Fprintf(&b, " model:%s", e.Model)
		}
		if e.Tokens > 0 {
			fmt.Fprintf(&b, " (%d tok)", e.Tokens)
		}
		fmt.Fprintf(&b, " %s\n", e.Timestamp.Format("15:04:05"))

		if e.TextPreview != "" {
			preview := e.TextPreview
			if len(preview) > 300 {
				preview = preview[:300] + "..."
			}
			fmt.Fprintf(&b, "    %s\n", preview)
		}

		if i == targetIdx {
			fmt.Fprintf(&b, "    >>> TARGET <<<\n")
		}
		fmt.Fprintln(&b)
	}

	return b.String()
}

// sessionInfo matches the struct used in toolListSessions.
type sessionInfo struct {
	SessionID     string
	SourceName    string
	StartedAt     time.Time
	EndedAt       time.Time
	EventCount    int64
	TurnCount     int64
	TotalTokens   int64
	ToolCallCount int64
	MCPCallCount  int64
	ErrorCount    int64
	LastModel     string
}

// FormatSessionList formats a list of sessions as prose.
func FormatSessionList(sessions []sessionInfo) string {
	if len(sessions) == 0 {
		return "No sessions found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d sessions:\n\n", len(sessions))

	for i, s := range sessions {
		idPreview := s.SessionID
		if len(idPreview) > 12 {
			idPreview = idPreview[:12]
		}

		duration := s.EndedAt.Sub(s.StartedAt)
		if s.EndedAt.IsZero() || s.EndedAt.Before(s.StartedAt) {
			duration = time.Since(s.StartedAt)
		}

		fmt.Fprintf(&b, "%d. [%s] %s  %s  dur:%s\n", i+1, s.SourceName, idPreview, s.StartedAt.Format(time.RFC3339), duration.Truncate(time.Second))
		fmt.Fprintf(&b, "   events:%d  turns:%d  tokens:%d  tools:%d  mcp:%d  errors:%d\n",
			s.EventCount, s.TurnCount, s.TotalTokens, s.ToolCallCount, s.MCPCallCount, s.ErrorCount)
		if s.LastModel != "" {
			fmt.Fprintf(&b, "   model:%s\n", s.LastModel)
		}
		fmt.Fprintln(&b)
	}

	return b.String()
}
