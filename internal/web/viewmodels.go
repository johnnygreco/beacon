package web

import "time"

type MetricData struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Delta float64 `json:"delta,omitempty"`
}

type SessionSummary struct {
	ID           string    `json:"id"`
	Source       string    `json:"source"`
	StartedAt    time.Time `json:"started_at"`
	Duration     string    `json:"duration"`
	TurnCount    int       `json:"turn_count"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCost    float64   `json:"total_cost"`
	TopTools     []string  `json:"top_tools"`
	ErrorCount   int       `json:"error_count"`
	CWD          string    `json:"cwd"`
	GitRepo      string    `json:"git_repo"`
}

type SearchResult struct {
	DocumentID string  `json:"document_id"`
	SessionID  string  `json:"session_id"`
	DocType    string  `json:"doc_type"`
	Content    string  `json:"content"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
	Source     string  `json:"source"`
	CreatedAt  time.Time `json:"created_at"`
}

type ActivityItem struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // model_call, tool_call, error, prompt
	Summary   string    `json:"summary"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type DashboardData struct {
	Metrics          []MetricData     `json:"metrics"`
	ActiveSessions   []SessionSummary `json:"active_sessions"`
	RecentActivity   []ActivityItem   `json:"recent_activity"`
	TokensPerMinute  []TimeSeriesPoint `json:"tokens_per_minute"`
	CostToday        float64          `json:"cost_today"`
	TotalSessions    int              `json:"total_sessions"`
	TotalTokens      int64            `json:"total_tokens"`
}

type TimeSeriesPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type SessionDetail struct {
	Session    SessionSummary   `json:"session"`
	Turns      []TurnSummary    `json:"turns"`
	ModelCalls []ModelCallView  `json:"model_calls"`
	ToolCalls  []ToolCallView   `json:"tool_calls"`
	Errors     []ErrorView      `json:"errors"`
	Context    []ContextPoint   `json:"context"`
}

type TurnSummary struct {
	ID           string    `json:"id"`
	TurnNumber   int       `json:"turn_number"`
	Prompt       string    `json:"prompt"`
	StartedAt    time.Time `json:"started_at"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	ToolCount    int       `json:"tool_count"`
}

type ModelCallView struct {
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	DurationMs   int64     `json:"duration_ms"`
	CostUSD      float64   `json:"cost_usd"`
	Timestamp    time.Time `json:"timestamp"`
}

type ToolCallView struct {
	ToolName   string    `json:"tool_name"`
	Success    bool      `json:"success"`
	DurationMs int64     `json:"duration_ms"`
	Timestamp  time.Time `json:"timestamp"`
}

type ErrorView struct {
	ErrorCode  string    `json:"error_code"`
	ErrorClass string    `json:"error_class"`
	Message    string    `json:"message"`
	Provider   string    `json:"provider"`
	Timestamp  time.Time `json:"timestamp"`
}

type ContextPoint struct {
	Timestamp       time.Time `json:"timestamp"`
	TokensInContext int64     `json:"tokens_in_context"`
	MaxTokens       int64     `json:"max_tokens"`
	CompactionEvent bool      `json:"compaction_event"`
}

type SSEEvent struct {
	Type string `json:"type"` // dashboard_update, session_update, new_activity
	Data any    `json:"data"`
}
