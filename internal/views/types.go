package views

//go:generate go tool templ generate

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// RelativeTime formats a time as a human-friendly relative string (e.g. "3m ago").
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// TruncateID shortens an ID string to 8 characters for display.
func TruncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Pluralize returns "N singular" or "N singulars" based on count.
func Pluralize(n int, singular string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

// SumTokens returns the total tokens across a slice of EventSummary.
// Used for reasoning block groups and anywhere a sub-group total is needed.
func SumTokens(events []EventSummary) int64 {
	var total int64
	for _, e := range events {
		total += e.Tokens
	}
	return total
}

// EstimateTokens estimates the number of tokens in a text string.
// Uses ~4 characters per token as a rough heuristic for English/code text.
func EstimateTokens(text string) int64 {
	n := len(text)
	if n == 0 {
		return 0
	}
	return int64(n+3) / 4
}

// EstimateTokensMulti estimates total tokens across multiple EventSummary
// text contents. Used when API-reported token counts are unavailable or
// unreliable (e.g. thinking blocks where output_tokens is partial).
func EstimateTokensMulti(events []EventSummary) int64 {
	var total int64
	for _, e := range events {
		total += EstimateTokens(e.TextContent)
	}
	return total
}

// DisplayTokens returns the best available token count for a slice of events.
// If the API-reported sum is meaningful (> 0 and at least half the text-based
// estimate), it is used directly. Otherwise the text estimate is returned.
func DisplayTokens(events []EventSummary) int64 {
	apiTotal := SumTokens(events)
	estimated := EstimateTokensMulti(events)
	if apiTotal > 0 && (estimated == 0 || apiTotal >= estimated/2) {
		return apiTotal
	}
	if estimated > 0 {
		return estimated
	}
	return apiTotal
}

// DisplayTokensSingle returns the best available token count for a single event.
func DisplayTokensSingle(e EventSummary) int64 {
	if e.Tokens > 0 {
		estimated := EstimateTokens(e.TextContent)
		if estimated == 0 || e.Tokens >= estimated/2 {
			return e.Tokens
		}
		return estimated
	}
	return EstimateTokens(e.TextContent)
}

// FormatTokens formats a token count for display (e.g. 1500 -> "1.5K").
func FormatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// View model types for templates.

type MetricData struct {
	Label    string
	Value    string
	Sublabel string
	Trend    string // "up", "down", "neutral"
}

type SessionSummary struct {
	ID                string
	Actor             string
	Status            string // "active", "completed"
	StartedAt         time.Time
	EndedAt           time.Time
	Duration          string
	TotalTokens       int64
	InputTokens       int64
	OutputTokens      int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	TurnCount         int64
	ToolCallCount     int64
	MCPCallCount      int64
	ActiveModel       string
	WorkingDir        string // full working directory path from cwd field
	ParentSessionID   string // non-empty if this is a subagent session
	ChildSessions     []SessionSummary // subagent sessions spawned from this session
}

// IsSubagent returns true if this session is a subagent of another session.
func (s SessionSummary) IsSubagent() bool {
	return s.ParentSessionID != ""
}

// projectName extracts the project name from a working directory path.
// For worktree paths like "/code/myproject/.claude/worktrees/funny-name",
// it returns the project name ("myproject") instead of the worktree name.
func projectName(dir string) string {
	if idx := strings.Index(dir, "/.claude/worktrees/"); idx >= 0 {
		return path.Base(dir[:idx])
	}
	return path.Base(dir)
}

// FormatTime formats a time in 12-hour clock with numeric date (e.g. "3/7/2026 3:04 PM").
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("1/2/2006 3:04 PM")
}

// FormatTimeShort formats a time in short 12-hour format (e.g. "3/7 3:04 PM").
func FormatTimeShort(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("1/2 3:04 PM")
}

// SessionTitle returns a display title for a session.
// When detailed is true, date info is appended to the project name.
func SessionTitle(s SessionSummary, detailed bool) string {
	if s.WorkingDir != "" {
		name := projectName(s.WorkingDir)
		if detailed && !s.StartedAt.IsZero() {
			return name + " — " + FormatTimeShort(s.StartedAt)
		}
		return name
	}
	if s.Actor != "" && s.Actor != "claude" {
		return s.Actor
	}
	if !s.StartedAt.IsZero() {
		if detailed {
			return "Session — " + FormatTimeShort(s.StartedAt)
		}
		return FormatTimeShort(s.StartedAt)
	}
	return "Session"
}

type SearchResult struct {
	EventUID  string
	SessionID string
	EventKind string
	Snippet   string
	Score     float64
	MatchType string // "bm25", "keyword"
	Timestamp time.Time
}

type ActivityItem struct {
	ID        string
	Type      string // event_kind: "message", "tool_call", "tool_result", "error"
	Summary   string
	SessionID string
	Timestamp time.Time
}

type EventSummary struct {
	EventUID      string
	EventKind     string // message, tool_call, tool_result, reasoning, etc.
	ActorRole     string
	TextContent   string
	TextPreview   string
	ToolName      string
	Model         string
	Tokens        int64
	DurationMs    int64
	Timestamp     time.Time
	InputPreview  string
	OutputPreview string
	InputJSON     string
}

type TurnDetail struct {
	TurnSeq     int
	Events      []EventSummary
	TotalTokens int64
	StartedAt   time.Time
}

type ChartData struct {
	Labels []string  `json:"labels"`
	Values []float64 `json:"values"`
}

type MultiSeriesChart struct {
	Labels   []string       `json:"labels"`
	Datasets []ChartDataset `json:"datasets"`
}

type ChartDataset struct {
	Label  string    `json:"label"`
	Values []float64 `json:"values"`
}

type ToolStat struct {
	Name        string
	Calls       int
	AvgDuration float64
	IsMCP       bool
}

type ModelTokens struct {
	Model     string
	Input     int64
	Output    int64
	CacheRead int64
	Total     int64
}

type DashboardData struct {
	Metrics           []MetricData
	ActiveSessions    []SessionSummary
	CompletedSessions []SessionSummary
	RecentActivity    []ActivityItem
	TokensChart       MultiSeriesChart
	TokensByModel     []ModelTokens
}

type SessionDetailData struct {
	Session        SessionSummary
	Turns          []TurnDetail
	ChatTurns      []ChatTurn
	TokensChart    MultiSeriesChart
	ToolStats      []ToolStat
	TokensByModel  []ChartDataset
}

const (
	ChatBlockUserMessage        = "user_message"
	ChatBlockAssistantMessage   = "assistant_message"
	ChatBlockToolChain          = "tool_chain"
	ChatBlockReasoning          = "reasoning"
	ChatBlockError              = "error"
	ChatBlockSubagentDispatch   = "subagent_dispatch"
)

// ToolCallParams holds parsed tool input for specialized rendering.
type ToolCallParams struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	FilePath    string `json:"file_path"`
	OldString   string `json:"old_string"`
	NewString   string `json:"new_string"`
	Content     string `json:"content"`
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	// Agent tool fields
	Prompt string `json:"prompt"`
}

type ToolStatEntry struct {
	Name  string
	Count int
}

type ToolChainItem struct {
	CallEvent     EventSummary
	ResultEvent   *EventSummary
	ToolName      string
	InputPreview  string
	OutputPreview string
	InputJSON     string          // full JSON from tool_io.input_json
	Params        *ToolCallParams // parsed tool input for specialized rendering
}

type ChatBlock struct {
	Kind      string // "user_message", "assistant_message", "tool_chain", "reasoning", "error"
	Message   *EventSummary
	Messages  []EventSummary  // for grouped reasoning blocks
	ToolChain []ToolChainItem
}

type ChatTurn struct {
	TurnSeq     int
	Blocks      []ChatBlock
	TotalTokens int64
	StartedAt   time.Time
	ToolStats   []ToolStatEntry // tool name -> call count for turn separator
}

// ChatContext holds additional context passed to the chat view for rendering.
type ChatContext struct {
	ChildSessions []SessionSummary // subagent sessions for this parent
}
