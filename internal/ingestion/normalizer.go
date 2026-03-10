package ingestion

import "time"

// NormalizedEvent is the common format that all source parsers produce.
type NormalizedEvent struct {
	SessionID  string
	SourceName string // "claude" or "codex"
	Provider   string // "anthropic" or "openai"

	EventKind   string // message, tool_call, tool_result, reasoning, session_meta, turn_context, event_msg, error, context_snapshot
	PayloadType string // sub-type within event_kind
	ActorRole   string // user, assistant, system, tool

	Timestamp time.Time

	TextContent string
	ToolName    string
	Model       string

	InputTokens       int64
	OutputTokens      int64
	CacheReadTokens   int64
	CacheCreateTokens int64
	DurationMs        int64
	CostUSD           float64

	ErrorCode    string
	ErrorMessage string

	// Tool I/O (only for tool_call/tool_result)
	ToolPhase  string // "call" or "result"
	ToolInput  string
	ToolOutput string

	// Links
	ParentUUID string
	ToolUseID  string

	// Session context
	CWD string // working directory of the session

	// Subagent / team support
	ParentSessionID string // parent session ID (non-empty for subagent sessions)

	// Message grouping (used to deduplicate tokens across JSONL lines from the same API call)
	MessageUUID string

	// Source coordinates (set by watcher)
	SourceFile   string
	SourceLineNo int
	SourceOffset int64
	RawPayload   string
}

// InsertEvent is sent to the batcher for INSERT operations.
type InsertEvent struct {
	Normalized NormalizedEvent
}

// BatchEvent wraps an InsertEvent for the batcher channel.
type BatchEvent struct {
	Insert *InsertEvent
}
