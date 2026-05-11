package capture

import "time"

// NormalizedEvent is the common format that all source parsers produce.
type NormalizedEvent struct {
	SessionID  string
	SourceName string // configured capture source name
	Runtime    string // agent harness/runtime identifier
	Provider   string // model provider when known, otherwise "multi"
	Format     string // source storage format, e.g. "jsonl" or "sqlite"

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

	// Provider-specific cumulative usage snapshot used for deduplicating
	// bookkeeping token events that repeat the same totals.
	TokenUsageTotalKey string

	// Source coordinates (set by watcher)
	SourceFile   string
	SourceLineNo int
	SourceOffset int64
	// SourceGeneration increments when a watched file rotates so replayed byte
	// coordinates from a new file do not collide with earlier records.
	SourceGeneration int
	RawPayload       string
}

// InsertEvent is sent to the batcher for INSERT operations.
type InsertEvent struct {
	Normalized NormalizedEvent
}

// BatchEvent wraps an InsertEvent for the batcher channel.
type BatchEvent struct {
	Insert *InsertEvent
}
