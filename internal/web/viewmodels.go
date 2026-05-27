package web

import (
	"time"

	"github.com/johnnygreco/beacon/internal/views"
)

type APIMetricData struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Delta float64 `json:"delta,omitempty"`
}

type APITokensPerMinute struct {
	Minute          string `json:"minute"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens"`
	TotalTokens     int64  `json:"total_tokens"`
	CallCount       int    `json:"call_count"`
}

type APIToolStats struct {
	ToolName      string  `json:"tool_name"`
	Calls         int     `json:"calls"`
	Results       int     `json:"results"`
	Total         int     `json:"total"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	IsMCP         bool    `json:"is_mcp"`
}

type APITokensByModel struct {
	Model             string `json:"model"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	CacheCreateTokens int64  `json:"cache_create_tokens"`
	TotalTokens       int64  `json:"total_tokens"`
	CallCount         int    `json:"call_count"`
}

type APISessionSummary struct {
	ID                string              `json:"id"`
	Title             string              `json:"title"`
	Source            string              `json:"source"`
	Provider          string              `json:"provider"`
	Status            string              `json:"status"`
	StartedAt         time.Time           `json:"started_at"`
	EndedAt           time.Time           `json:"ended_at"`
	Duration          string              `json:"duration"`
	TurnCount         int64               `json:"turn_count"`
	TotalTokens       int64               `json:"total_tokens"`
	InputTokens       int64               `json:"input_tokens"`
	OutputTokens      int64               `json:"output_tokens"`
	CacheReadTokens   int64               `json:"cache_read_tokens"`
	CacheCreateTokens int64               `json:"cache_create_tokens"`
	ToolCallCount     int64               `json:"tool_call_count"`
	MCPCallCount      int64               `json:"mcp_call_count"`
	ErrorCount        int64               `json:"error_count"`
	LastModel         string              `json:"last_model"`
	WorkingDir        string              `json:"working_dir"`
	ParentSessionID   string              `json:"parent_session_id,omitempty"`
	HasSessionEnd     bool                `json:"has_session_end"`
	SubagentCount     int                 `json:"subagent_count"`
	ChildSessions     []APISessionSummary `json:"child_sessions,omitempty"`
}

type APIDashboardSessionsResponse struct {
	State   string              `json:"state"`
	Range   string              `json:"range"`
	Query   string              `json:"query,omitempty"`
	Offset  int                 `json:"offset"`
	Limit   int                 `json:"limit"`
	HasMore bool                `json:"has_more"`
	Items   []APISessionSummary `json:"items"`
}

type APIDashboardSearchResponse struct {
	State     string                     `json:"state"`
	Query     string                     `json:"query,omitempty"`
	Range     string                     `json:"range,omitempty"`
	EventKind string                     `json:"event_kind,omitempty"`
	SessionID string                     `json:"session_id,omitempty"`
	Sort      string                     `json:"sort"`
	Limit     int                        `json:"limit"`
	HasMore   bool                       `json:"has_more"`
	Items     []APIDashboardSearchResult `json:"items"`
}

type APIDashboardSearchResult struct {
	ResultType   string    `json:"result_type,omitempty"`
	EventUID     string    `json:"event_uid"`
	SessionID    string    `json:"session_id"`
	EventKind    string    `json:"event_kind"`
	Snippet      string    `json:"snippet"`
	ToolName     string    `json:"tool_name,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Model        string    `json:"model,omitempty"`
	Score        float64   `json:"score,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	RelativeTime string    `json:"relative_time"`
	SessionTitle string    `json:"session_title,omitempty"`
	WorkingDir   string    `json:"working_dir,omitempty"`
}

type APIActivityItem struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Summary      string    `json:"summary"`
	SessionID    string    `json:"session_id"`
	Provider     string    `json:"provider"`
	Timestamp    time.Time `json:"timestamp"`
	RelativeTime string    `json:"relative_time"`
}

type APIDashboardCharts struct {
	Range           string                 `json:"range"`
	TokenCumulative views.ModelSeriesChart `json:"token_cumulative"`
	ModelActivity   views.ModelMetricChart `json:"model_activity"`
}

type APISessionDetail struct {
	Session APISessionSummary `json:"session"`
}

type APISessionEvent struct {
	EventUID      string    `json:"event_uid"`
	SessionID     string    `json:"session_id"`
	EventKind     string    `json:"event_kind"`
	PayloadType   string    `json:"payload_type"`
	ActorRole     string    `json:"actor_role"`
	Timestamp     time.Time `json:"timestamp"`
	TextPreview   string    `json:"text_preview"`
	ToolName      string    `json:"tool_name"`
	ToolUseID     string    `json:"tool_use_id"`
	Model         string    `json:"model"`
	Tokens        int64     `json:"tokens"`
	DurationMs    int64     `json:"duration_ms"`
	InputPreview  string    `json:"input_preview,omitempty"`
	OutputPreview string    `json:"output_preview,omitempty"`
}

type APIToolPayload struct {
	EventUID      string `json:"event_uid"`
	ToolName      string `json:"tool_name"`
	ToolPhase     string `json:"tool_phase"`
	InputJSON     string `json:"input_json"`
	OutputJSON    string `json:"output_json"`
	InputPreview  string `json:"input_preview"`
	OutputPreview string `json:"output_preview"`
}
