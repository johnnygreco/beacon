package ingestion

import "time"

// NormalizedEvent is the common format that all source parsers produce.
type NormalizedEvent struct {
	// Identity
	SessionID string
	TurnID    string
	EventID   string
	Source    string // claude_code, codex, cursor

	// Event classification
	EventType string // session_start, session_end, user_prompt, api_request, api_response, tool_use, tool_result, api_error, context_snapshot

	// Timing
	Timestamp time.Time

	// Token data (for api_request/api_response)
	Model        string
	Provider     string
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheCreate  int64
	DurationMs   int64
	StatusCode   int
	CostUSD      float64

	// Tool data (for tool_use/tool_result)
	ToolName   string
	ToolInput  string
	ToolOutput string
	ToolSuccess bool

	// Prompt data
	UserPrompt string
	TurnNumber int

	// Error data
	ErrorCode  string
	ErrorClass string
	ErrorMsg   string
	RetryCount int

	// Context data
	TokensInContext int64
	MaxTokens       int64
	CompactionEvent bool

	// Document content for search indexing
	DocContent string
	DocType    string

	// Actor data
	UserID    string
	MachineID string
	CWD       string
	GitRepo   string

	// Raw payload for archival
	RawPayload string
}

// InsertEvent is sent to the batcher for INSERT operations.
type InsertEvent struct {
	Normalized NormalizedEvent
}

// UpdateEvent is sent to the batcher for UPDATE operations (e.g., embedding updates).
type UpdateEvent struct {
	Table string
	ID    string
	SQL   string
	Args  []any
}

// BatchEvent wraps either an InsertEvent or UpdateEvent for the batcher channel.
type BatchEvent struct {
	Insert *InsertEvent
	Update *UpdateEvent
}
