package views

//go:generate go tool templ generate

import (
	"fmt"
	"time"
)

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
}

type TurnDetail struct {
	TurnSeq     int
	Events      []EventSummary
	TotalTokens int64
	StartedAt   time.Time
}

type ChartData struct {
	Labels []string
	Values []float64
}

type MultiSeriesChart struct {
	Labels   []string
	Datasets []ChartDataset
}

type ChartDataset struct {
	Label  string
	Values []float64
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
	Metrics        []MetricData
	ActiveSessions []SessionSummary
	RecentActivity []ActivityItem
	TokensChart    MultiSeriesChart
	TokensByModel  []ModelTokens
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
	ChatBlockUserMessage      = "user_message"
	ChatBlockAssistantMessage = "assistant_message"
	ChatBlockToolChain        = "tool_chain"
	ChatBlockReasoning        = "reasoning"
	ChatBlockError            = "error"
)

type ToolChainItem struct {
	CallEvent     EventSummary
	ResultEvent   *EventSummary
	ToolName      string
	InputPreview  string
	OutputPreview string
}

type ChatBlock struct {
	Kind      string // "user_message", "assistant_message", "tool_chain", "reasoning", "error"
	Message   *EventSummary
	ToolChain []ToolChainItem
}

type ChatTurn struct {
	TurnSeq     int
	Blocks      []ChatBlock
	TotalTokens int64
	StartedAt   time.Time
}
