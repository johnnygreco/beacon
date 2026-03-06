package web

import "time"

type APIMetricData struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Delta float64 `json:"delta,omitempty"`
}

type APISessionSummary struct {
	ID                string    `json:"id"`
	Source            string    `json:"source"`
	StartedAt         time.Time `json:"started_at"`
	Duration          string    `json:"duration"`
	TurnCount         int64     `json:"turn_count"`
	TotalTokens       int64     `json:"total_tokens"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CacheReadTokens   int64     `json:"cache_read_tokens"`
	CacheCreateTokens int64     `json:"cache_create_tokens"`
	ToolCallCount     int64     `json:"tool_call_count"`
	MCPCallCount      int64     `json:"mcp_call_count"`
	ErrorCount        int64     `json:"error_count"`
	LastModel         string    `json:"last_model"`
}
