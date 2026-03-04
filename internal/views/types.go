package views

import "time"

// View model types for templates.
// These mirror the types in internal/web/viewmodels.go.

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
	TurnCount   int
	ActiveModel string
}

type SearchResult struct {
	DocumentID string
	SessionID  string
	Content    string
	Snippet    string
	Score      float64
	MatchType  string // "semantic", "keyword", "hybrid"
	Timestamp  time.Time
}

type ActivityItem struct {
	ID        string
	Type      string // "prompt", "response", "tool_call", "error"
	Summary   string
	SessionID string
	Timestamp time.Time
	Actor     string
}

type TurnDetail struct {
	ID         string
	Role       string
	Content    string
	Tokens     int64
	Cost       float64
	Model      string
	ToolCalls  []ToolCallInfo
	Timestamp  time.Time
	DurationMs int64
}

type ToolCallInfo struct {
	Name     string
	Status   string // "success", "error"
	Duration string
	Input    string
	Output   string
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
	Session      SessionSummary
	Turns        []TurnDetail
	TokensChart  ChartData
	ContextChart ChartData
}
