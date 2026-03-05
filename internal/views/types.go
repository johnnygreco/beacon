package views

//go:generate go tool templ generate

import "time"

// View model types for templates.

type MetricData struct {
	Label    string
	Value    string
	Sublabel string
	Trend    string // "up", "down", "neutral"
}

type SessionSummary struct {
	ID          string
	Actor       string
	Status      string // "active", "completed"
	StartedAt   time.Time
	Duration    string
	TotalTokens int64
	TotalCost   float64
	TurnCount   int64
	ActiveModel string
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
	EventUID   string
	EventKind  string // message, tool_call, tool_result, reasoning, etc.
	ActorRole  string
	TextPreview string
	ToolName   string
	Model      string
	Tokens     int64
	Cost       float64
	DurationMs int64
	Timestamp  time.Time
}

type TurnDetail struct {
	TurnSeq     int
	Events      []EventSummary
	TotalTokens int64
	TotalCost   float64
	StartedAt   time.Time
}

type ChartData struct {
	Labels []string
	Values []float64
}

type DashboardData struct {
	Metrics        []MetricData
	ActiveSessions []SessionSummary
	RecentActivity []ActivityItem
	TokensChart    ChartData
	CostChart      ChartData
}

type SessionDetailData struct {
	Session     SessionSummary
	Turns       []TurnDetail
	TokensChart ChartData
}
