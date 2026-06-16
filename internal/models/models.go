package models

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const (
	MCPToolPrefix = "mcp__"

	RuntimeClaudeCode    = "claude-code"
	RuntimeCodex         = "codex"
	RuntimeHermesAgent   = "hermes-agent"
	RuntimeOpenCode      = "opencode"
	RuntimePiCodingAgent = "pi-coding-agent"

	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderMulti     = "multi"

	FormatJSONL  = "jsonl"
	FormatSQLite = "sqlite"

	EventKindMessage         = "message"
	EventKindToolCall        = "tool_call"
	EventKindToolResult      = "tool_result"
	EventKindToolError       = "tool_error"
	EventKindError           = "error"
	EventKindReasoning       = "reasoning"
	EventKindSessionMeta     = "session_meta"
	EventKindSessionEnd      = "session_end"
	EventKindEventMsg        = "event_msg"
	EventKindTurnContext     = "turn_context"
	EventKindContextSnapshot = "context_snapshot"
	EventKindToolPrefix      = "tool_"

	ActorRoleUser      = "user"
	ActorRoleAssistant = "assistant"
	ActorRoleTool      = "tool"
	ActorRoleSystem    = "system"

	ToolPhaseCall   = "call"
	ToolPhaseResult = "result"
)

// IsMCPTool returns true if the tool name indicates an MCP tool.
func IsMCPTool(name string) bool {
	return strings.HasPrefix(name, MCPToolPrefix)
}

type Event struct {
	EventUID           string     `json:"event_uid"`
	SessionID          string     `json:"session_id"`
	RawSessionID       string     `json:"raw_session_id,omitempty"`
	ParentSessionID    string     `json:"parent_session_id,omitempty"`
	RawParentSessionID string     `json:"raw_parent_session_id,omitempty"`
	SessionDate        *time.Time `json:"session_date,omitempty"`
	SourceName         string     `json:"source_name"`
	Runtime            string     `json:"runtime"`
	Provider           string     `json:"provider"`
	Format             string     `json:"format"`
	EventKind          string     `json:"event_kind"`
	PayloadType        string     `json:"payload_type"`
	ActorRole          string     `json:"actor_role"`
	Timestamp          time.Time  `json:"timestamp"`
	TextContent        string     `json:"text_content"`
	TextPreview        string     `json:"text_preview"`
	ToolName           string     `json:"tool_name"`
	ToolUseID          string     `json:"tool_use_id"`
	Model              string     `json:"model"`
	InputTokens        int64      `json:"input_tokens"`
	OutputTokens       int64      `json:"output_tokens"`
	CacheReadTokens    int64      `json:"cache_read_tokens"`
	CacheCreateTokens  int64      `json:"cache_create_tokens"`
	DurationMs         int64      `json:"duration_ms"`
	CostUSD            float64    `json:"cost_usd"`
	ErrorCode          string     `json:"error_code"`
	ErrorMessage       string     `json:"error_message"`
	EventVersion       int        `json:"event_version"`
	PayloadJSON        string     `json:"payload_json"`
	CWD                string     `json:"cwd"`
	SourceFile         string     `json:"source_file"`
	SourceLineNo       int        `json:"source_line_no"`
	SourceOffset       int64      `json:"source_offset"`
	SourceGeneration   int        `json:"source_generation"`
	RawEventID         string     `json:"raw_event_id,omitempty"`
	SourceEventIndex   uint64     `json:"source_event_index,omitempty"`
	PayloadDigest      string     `json:"payload_digest,omitempty"`
	RedactionStatus    string     `json:"redaction_status,omitempty"`
	RedactionVersion   string     `json:"redaction_version,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type RawRecord struct {
	RecordUID        string    `json:"record_uid"`
	EventUID         string    `json:"event_uid"`
	SourceName       string    `json:"source_name"`
	Runtime          string    `json:"runtime"`
	Provider         string    `json:"provider"`
	Format           string    `json:"format"`
	SourceFile       string    `json:"source_file"`
	SourceLineNo     int       `json:"source_line_no"`
	SourceOffset     int64     `json:"source_offset"`
	SourceGeneration int       `json:"source_generation"`
	SessionID        string    `json:"session_id"`
	RawSessionID     string    `json:"raw_session_id,omitempty"`
	RawEventID       string    `json:"raw_event_id,omitempty"`
	SourceEventIndex uint64    `json:"source_event_index,omitempty"`
	PayloadDigest    string    `json:"payload_digest,omitempty"`
	RedactionStatus  string    `json:"redaction_status,omitempty"`
	RedactionVersion string    `json:"redaction_version,omitempty"`
	PayloadJSON      string    `json:"payload_json"`
	CapturedAt       time.Time `json:"captured_at"`
}

type EventLink struct {
	EventUID           string `json:"event_uid"`
	LinkedEventUID     string `json:"linked_event_uid"`
	LinkType           string `json:"link_type"`
	LinkScope          string `json:"link_scope,omitempty"`
	ResolutionStatus   string `json:"resolution_status,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	RawSessionID       string `json:"raw_session_id,omitempty"`
	LinkedSessionID    string `json:"linked_session_id,omitempty"`
	RawLinkedSessionID string `json:"raw_linked_session_id,omitempty"`
	RawLinkedEventID   string `json:"raw_linked_event_id,omitempty"`
}

type ToolPayload struct {
	EventUID         string `json:"event_uid"`
	ToolName         string `json:"tool_name"`
	ToolPhase        string `json:"tool_phase"`
	InputJSON        string `json:"input_json"`
	OutputJSON       string `json:"output_json"`
	InputPreview     string `json:"input_preview"`
	OutputPreview    string `json:"output_preview"`
	PayloadDigest    string `json:"payload_digest,omitempty"`
	RedactionStatus  string `json:"redaction_status,omitempty"`
	RedactionVersion string `json:"redaction_version,omitempty"`
}

type CaptureError struct {
	ID              string    `json:"id"`
	SourceName      string    `json:"source_name"`
	SourceFile      string    `json:"source_file"`
	SourceLineNo    int       `json:"source_line_no"`
	SourceOffset    int64     `json:"source_offset"`
	ErrorClass      string    `json:"error_class"`
	ErrorMessage    string    `json:"error_message"`
	ContextFragment string    `json:"context_fragment"`
	CreatedAt       time.Time `json:"created_at"`
}

type Checkpoint struct {
	SourceName       string `json:"source_name"`
	SourceFileKey    string `json:"source_file_key,omitempty"`
	SourceFile       string `json:"source_file"`
	SourceInode      int64  `json:"source_inode"`
	SourceGeneration int    `json:"source_generation"`
	LastOffset       int64  `json:"last_offset"`
	LastLineNo       int    `json:"last_line_no"`
	StateJSON        string `json:"state_json"`
}

func CheckpointSourceFileKey(sourceName, sourceFile string) string {
	sourceName = strings.TrimSpace(sourceName)
	sourceFile = strings.TrimSpace(sourceFile)
	if sourceName == "" || sourceFile == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sourceName + "\x00" + sourceFile))
	return "file_" + hex.EncodeToString(sum[:])[:32]
}

func (cp Checkpoint) EffectiveSourceFileKey() string {
	if strings.TrimSpace(cp.SourceFileKey) != "" {
		return strings.TrimSpace(cp.SourceFileKey)
	}
	return CheckpointSourceFileKey(cp.SourceName, cp.SourceFile)
}

type SearchDocument struct {
	EventUID       string    `json:"event_uid"`
	SessionID      string    `json:"session_id"`
	SourceName     string    `json:"source_name,omitempty"`
	Runtime        string    `json:"runtime,omitempty"`
	Format         string    `json:"format,omitempty"`
	ProjectKey     string    `json:"project_key,omitempty"`
	ProjectPath    string    `json:"project_path,omitempty"`
	EventKind      string    `json:"event_kind"`
	Timestamp      time.Time `json:"timestamp"`
	TextPreview    string    `json:"text_preview"`
	ToolName       string    `json:"tool_name"`
	Model          string    `json:"model"`
	Provider       string    `json:"provider"`
	SearchableText string    `json:"searchable_text"`
	DocumentLength int       `json:"document_length"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SearchPosting struct {
	Token          string    `json:"token"`
	EventUID       string    `json:"event_uid"`
	SessionID      string    `json:"session_id"`
	SourceName     string    `json:"source_name,omitempty"`
	Runtime        string    `json:"runtime,omitempty"`
	Format         string    `json:"format,omitempty"`
	ProjectKey     string    `json:"project_key,omitempty"`
	ProjectPath    string    `json:"project_path,omitempty"`
	EventKind      string    `json:"event_kind"`
	Timestamp      time.Time `json:"timestamp"`
	TermFrequency  int       `json:"term_frequency"`
	DocumentLength int       `json:"document_length"`
	TextPreview    string    `json:"text_preview"`
	ToolName       string    `json:"tool_name"`
	Model          string    `json:"model"`
	Provider       string    `json:"provider"`
	UpdatedAt      time.Time `json:"updated_at"`
}
